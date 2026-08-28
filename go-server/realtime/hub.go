package realtime

import (
	"context"
	"encoding/json"
	"log"
)

type Hub struct {
	clients    map[int64]*Client //map of Clients connected to the Hub. key is the userID
	register   chan *Client      //way to add Clients to the Hub
	unregister chan *Client      //way to remove Clients from the Hub
	store      DataStore         //DB connection
}

// create a new Hub using the specified DB connection
func NewHub(store DataStore) *Hub {
	return &Hub{
		clients:    make(map[int64]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		store:      store}
}

// main loop of the service: notice when clients come and go
func (h *Hub) Run() {
	for { //this is an event listener, it selects the first active channel. if no channels are active, Go puts this thread to sleep until one activates
		select {
		case client := <-h.register: //h.register has an element, catch it as `client`
			h.handleRegister(client)
		case client := <-h.unregister: //h.unregister has an element, catch it as `client`
			h.handleUnregister(client)
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	if oldClient, alreadyConnected := h.clients[client.UserID]; alreadyConnected {
		oldClient.Conn.Close()
	}
	h.clients[client.UserID] = client
	friendIDs, err := h.store.GetFriendsList(context.Background(), client.UserID)
	if err != nil {
		log.Println("problem with DB connection")
		return
	}
	onlineFriendUsernames := make([]string, 0)
	presenceMessage, err := json.Marshal(WebsocketMessage{TypePresenceUpdate, PresenceUpdatePayload{client.Username, true}})
	if err != nil {
		log.Println("problem marshalling presence update json")
		return
	}
	for _, friendID := range friendIDs {
		if friend, isOnline := h.clients[friendID]; isOnline { //map lookup returns pointer to client, and true if key was in map, nil and false otherwise
			onlineFriendUsernames = append(onlineFriendUsernames, friend.Username)
			friend.TrySend(presenceMessage)
		}
	}
	initialMessage, err := json.Marshal(WebsocketMessage{TypeInitialPresence, InitialPresencePayload{onlineFriendUsernames}})
	if err != nil {
		log.Println("problem marshalling initial presence json")
		return
	}
	client.TrySend(initialMessage)
}

func (h *Hub) handleUnregister(client *Client) {
	if _, ok := h.clients[client.UserID]; ok {
		delete(h.clients, client.UserID)
		close(client.Send)

		friendIDs, err := h.store.GetFriendsList(context.Background(), client.UserID)
		if err != nil {
			log.Println("problem with DB connection on unregister:", err)
			return
		}

		// Marshal offline status message
		offlineMessage, err := json.Marshal(WebsocketMessage{TypePresenceUpdate, PresenceUpdatePayload{client.Username, false}})
		if err != nil {
			return
		}

		// Notify all online friends that this user went offline
		for _, friendID := range friendIDs {
			if friend, isOnline := h.clients[friendID]; isOnline {
				friend.TrySend(offlineMessage)
			}
		}
	}
}
