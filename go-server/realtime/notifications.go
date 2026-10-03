package realtime

import (
	"dbBackend/models"
	"errors"
	"fmt"
)

// NotifyFriendRequest delivers a real-time notification to a target user when receiving a friend request.
func (h *Hub) NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error {
	data, err := EncodeMessage(TypeFriendRequestRecv, item)
	if err != nil {
		return fmt.Errorf("failed to encode friend request notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// NotifyFriendResponse delivers a real-time notification to a target user when their friend request is answered.
func (h *Hub) NotifyFriendResponse(targetUserID int64, item models.FriendshipItemResponse) error {
	data, err := EncodeMessage(TypeFriendRequestResponse, item)
	if err != nil {
		return fmt.Errorf("failed to encode friend request response notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// NotifyFriendDeleted delivers a real-time notification to a target user when a friendship is deleted.
func (h *Hub) NotifyFriendDeleted(targetUserID int64, friendshipID int64) error {
	data, err := EncodeMessage(TypeFriendDeleted, models.FriendDeletePayload{FriendshipID: friendshipID})
	if err != nil {
		return fmt.Errorf("failed to encode friend deleted notification: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// MatchFinished dispatches match_finished real-time notifications to both participants
// and dispatches ActionMatchFinished to purge the match session from the Hub's in-memory activeMatches map.
func (h *Hub) MatchFinished(payload models.MatchFinishedPayload) error {
	data, err := EncodeMessage(TypeMatchFinished, payload)
	if err != nil {
		return fmt.Errorf("failed to encode match finished notification: %w", err)
	}

	_ = h.NotifyUser(payload.Player1, data)
	_ = h.NotifyUser(payload.Player2, data)

	h.matchAction <- MatchAction{
		Type:    ActionMatchFinished,
		MatchID: payload.MatchID,
	}

	return nil
}

// NotifyPresence delivers a real-time presence update regarding username to targetUserID.
func (h *Hub) NotifyPresence(targetUserID int64, username string, isOnline bool) error {
	data, err := EncodeMessage(TypePresenceUpdate, PresenceUpdatePayload{
		Username:     username,
		OnlineStatus: isOnline,
	})
	if err != nil {
		return fmt.Errorf("failed to encode presence update: %w", err)
	}
	return h.NotifyUser(targetUserID, data)
}

// NotifyMutualPresence dispatches mutual online presence updates between user A and user B,
// informing user A that user B is online and user B that user A is online.
// Any errors encountered while delivering the notifications are joined and returned.
func (h *Hub) NotifyMutualPresence(userAID, userBID int64, usernameA, usernameB string) error {
	errA := h.NotifyPresence(userAID, usernameB, true)
	errB := h.NotifyPresence(userBID, usernameA, true)
	return errors.Join(errA, errB)
}
