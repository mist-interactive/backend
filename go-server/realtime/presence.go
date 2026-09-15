package realtime

import (
	"context"
	"log/slog"
	"time"
)

// used to communicate a client joining, needed so only the main thread needs to access the clients struct
type PresenceSync struct {
	client    *Client
	friendIDs []int64
}

// Contain DB access and messaging to it's own function, so it can be run in a thread
func (h *Hub) broadcastOfflineStatus(userID int64, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	friendIDs, err := h.store.GetFriendsList(ctx, userID)
	if err != nil {
		slog.Error("Problem with DB connection on unregister", "user_id", userID, "username", username, "error", err)
		return
	}

	// Marshal offline status message
	offlineMessage, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{username, false})
	if err != nil {
		slog.Error("Failed to encode offline presence update", "user_id", userID, "username", username, "error", err)
		return
	}

	// Notify all online friends that this user went offline
	for _, friendID := range friendIDs {
		h.SendToUser(friendID, offlineMessage)
	}
}

func (h *Hub) sendInitialPresence(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	friendIDs, err := h.store.GetFriendsList(ctx, client.UserID)
	if err != nil {
		slog.Error("Problem with DB connection on initial presence", "user_id", client.UserID, "username", client.Username, "error", err)
		return
	}
	h.presenceSync <- PresenceSync{client: client, friendIDs: friendIDs} //sending presence update requires accessing the clients map, only the main thread is allowed to do that, so put it on a channel for it to read
}

func (h *Hub) handlePresenceSync(client *Client, friendIDs []int64) {
	if currentClient, ok := h.clients[client.UserID]; !ok || currentClient != client { //only run if this is still an active connection on the Hub
		return
	}
	onlineFriendUsernames := make([]string, 0)
	presenceMessage, _ := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{client.Username, true})

	for _, friendID := range friendIDs {
		if friend, isOnline := h.clients[friendID]; isOnline {
			onlineFriendUsernames = append(onlineFriendUsernames, friend.Username)
			friend.TrySend(presenceMessage)
		}
	}
	initialMessage, _ := EncodeMessage(TypeInitialPresence, InitialPresencePayload{onlineFriendUsernames})
	client.TrySend(initialMessage)
	slog.Debug("Dispatched presence sync", "user_id", client.UserID, "username", client.Username, "online_friends", len(onlineFriendUsernames))
}
