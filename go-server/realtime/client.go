package realtime

import (
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
)

const sendBufferSize = 256

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn //handles network traffic
	UserID   int64
	Username string
	Send     chan []byte // Buffered channel of data waiting to be sent to client
}

func NewClient(hub *Hub, conn *websocket.Conn, userID int64, username string) *Client {
	return &Client{
		Hub:      hub,
		Conn:     conn,
		UserID:   userID,
		Username: username,
		Send:     make(chan []byte, sendBufferSize),
	}
}

// listens on the Send channel, pushes messages over websocket when channel gets data
func (c *Client) writePump() {
	defer func() {
		c.Conn.Close()
	}()

	for message := range c.Send {
		err := c.Conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}
}

// reads incoming data from the WebSocket until the user disconnects
func (c *Client) readPump() {
	defer func() { //this runs if something goes wrong
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	for { // ReadMessage blocks until data arrives or the socket closes (disconnect)
		messageType, p, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WS] Unexpected disconnect from %s: %v", c.Username, err)
			} else {
				log.Printf("[WS] %s disconnected cleanly", c.Username)
			}
			break // Disconnected: unregister via the deferred function
		}
		if messageType != websocket.TextMessage {
			continue //ignore non-text messages
		}
		var incoming WebsocketMessage
		err = json.Unmarshal(p, &incoming)
		if err != nil {
			log.Printf("[WS] Malformed JSON from %s: %v", c.Username, err)
			continue //not a fatal error
		}
		switch incoming.Type {
		case TypeDMSend:
			payload, err := UnmarshalAndValidate[DMSendPayload](incoming.Payload)
			if err != nil {
				log.Printf("[WS] Invalid message from %s: %v", c.Username, err)
				continue
			}
			err = c.HandleSendMsg(payload)
		}
	}
}

// helper to send messages without blocking
func (c *Client) TrySend(msg []byte) bool {
	select {
	case c.Send <- msg:
		return true
	default: //we only arrive here if the defined case didn't work
		log.Printf("[WS] Send buffer full for %s (id: %d), dropped message", c.Username, c.UserID)
		return false
	}
}
