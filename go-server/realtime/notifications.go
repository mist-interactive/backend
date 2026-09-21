package realtime

import (
	"dbBackend/models"
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
