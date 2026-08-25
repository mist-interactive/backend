package realtime

import (
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

func (c *Client) writePump() {
	//TODO: listens on Send channel, and sends it over Conn when there is a message
}

func (c *Client) readPump() {
	//TODO: listens to the Conn, and passes any messages the client sends to the Hub
}

// helper to send messages without blocking
func (c *Client) TrySend(msg []byte) bool {
	select {
	case c.Send <- msg:
		return true
	default:
		log.Printf("[WS] Send buffer full for %s (id: %d), dropped message", c.Username, c.UserID)
		return false
	}
}
