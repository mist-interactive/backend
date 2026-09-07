package realtime

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

type Hub struct {
	clients       map[int64]*Client               // map of Clients connected to the Hub. key is the userID
	register      chan *Client                    // way to add Clients to the Hub
	unregister    chan *Client                    // way to remove Clients from the Hub
	unicast       chan UserMessage                // Universal channel to deliver data to any specific user
	presenceSync  chan PresenceSync               // Channel to update a users friends that they came online
	invites       map[inviteKey]time.Time         // In-memory pending challenges with creation timestamp
	matchAction   chan MatchAction                // Match invitation events dispatched to the Hub
	activeMatches map[string]*MatchSessionPayload // username -> active match session
	store         DataStore                       // DB connection
}

// create a new Hub using the specified DB connection
func NewHub(store DataStore) *Hub {
	return &Hub{
		clients:       make(map[int64]*Client),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		unicast:       make(chan UserMessage, 256),
		presenceSync:  make(chan PresenceSync),
		invites:       make(map[inviteKey]time.Time),
		matchAction:   make(chan MatchAction),
		activeMatches: make(map[string]*MatchSessionPayload),
		store:         store,
	}
}

// main loop of the service: notice when clients come and go, and when messages need to be sent
func (h *Hub) Run() {
	pruneTicker := time.NewTicker(invitePruneInterval)
	defer pruneTicker.Stop()

	for {
		select {
		case client := <-h.register:
			h.handleRegister(client)
		case client := <-h.unregister:
			h.handleUnregister(client)
		case msg := <-h.unicast:
			if msg.UserID != 0 {
				if recipient, isOnline := h.clients[msg.UserID]; isOnline {
					recipient.TrySend(msg.Data)
				}
			} else if msg.Username != "" {
				h.sendToUsernameDirect(msg.Username, msg.Data)
			}
		case sync := <-h.presenceSync:
			h.handlePresenceSync(sync.client, sync.friendIDs)
		case action := <-h.matchAction:
			h.handleMatchAction(action)
		case <-pruneTicker.C:
			h.pruneExpiredInvites()
		}
	}
}

func (h *Hub) handleRegister(client *Client) {
	if oldClient, alreadyConnected := h.clients[client.UserID]; alreadyConnected {
		slog.Info("Disconnecting previous connection for user", "user_id", client.UserID, "username", client.Username)
		if oldClient.Conn != nil {
			oldClient.Conn.Close()
		}
	}
	h.clients[client.UserID] = client
	slog.Info("Client registered in hub", "user_id", client.UserID, "username", client.Username, "total_clients", len(h.clients))
	h.handleActiveMatchOnRegister(client)
	go h.sendInitialPresence(client) //run the possibly slow DB and messaging in it's own thread
}

func (h *Hub) handleUnregister(client *Client) {
	if currentClient, ok := h.clients[client.UserID]; ok && currentClient == client { //check that this isn't an old instance of client being cleaned up, when a new connection was registered
		delete(h.clients, client.UserID)
		close(client.Send)
		cleanedInvites := h.cleanUpInvites(client.Username)
		slog.Info("Client unregistered from hub",
			"user_id", client.UserID,
			"username", client.Username,
			"total_clients", len(h.clients),
			"cleaned_invites", cleanedInvites,
		)
		go h.broadcastOfflineStatus(client.UserID, client.Username) //run the possibly slow DB and messaging in it's own thread
	}
}

func (h *Hub) findClientByUsername(username string) *Client {
	for _, client := range h.clients {
		if client.Username == username {
			return client
		}
	}
	return nil
}

func (h *Hub) sendToUsernameDirect(username string, data []byte) bool {
	if client := h.findClientByUsername(username); client != nil {
		return client.TrySend(data)
	}
	return false
}

func (h *Hub) SendToUser(userID int64, data []byte) {
	h.unicast <- UserMessage{UserID: userID, Data: data}
}

func (h *Hub) SendToUsername(username string, data []byte) {
	h.unicast <- UserMessage{Username: username, Data: data}
}

// NotifyUser delivers an event payload to a connected user via the thread-safe unicast channel.
// It accepts []byte, string, or any struct (which is marshaled to JSON).
// It is safe for concurrent use by HTTP handler goroutines and does not block if the buffer is full.
func (h *Hub) NotifyUser(userID int64, event any) error {
	var data []byte
	var err error

	switch v := event.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		data, err = json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to marshal notification for user %d: %w", userID, err)
		}
	}

	select {
	case h.unicast <- UserMessage{UserID: userID, Data: data}:
		return nil
	default:
		slog.Warn("hub unicast buffer full, notification dropped", "user_id", userID)
		return fmt.Errorf("hub unicast buffer full")
	}
}
