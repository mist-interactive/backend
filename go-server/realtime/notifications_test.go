package realtime

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"testing"
)

// Test constants for user IDs, match IDs, and friendship IDs.
const (
	testSenderID        = int64(7)
	testTargetUserID    = int64(42)
	testFriendshipID    = int64(101)
	testDeletedFriendID = int64(99)
	testDeletedID       = int64(555)
	testMatchID         = int64(200)
	testPlayer1ID       = int64(10)
	testPlayer2ID       = int64(20)
)

// newTestHub initializes a test Hub with matchAction pre-buffered (capacity 1).
// In production, NewHub creates matchAction as an unbuffered channel that relies on
// a concurrent hub.Run() loop to receive events. In unit tests where hub.Run() is not
// active, pre-buffering matchAction prevents operations like MatchFinished from deadlocking.
func newTestHub() *Hub {
	hub := NewHub(nil)
	hub.matchAction = make(chan MatchAction, 1)
	return hub
}

// expectUnicastMessage reads a message from the hub's unicast channel, verifies the recipient
// user ID and envelope message type, decodes the generic payload T, and returns it.
func expectUnicastMessage[T any](t *testing.T, ch <-chan UserMessage, wantUserID int64, wantType MessageType) T {
	t.Helper()
	select {
	case msg := <-ch:
		if msg.UserID != wantUserID {
			t.Errorf("got target userID %d, want %d", msg.UserID, wantUserID)
		}

		var wsMsg WebsocketMessage
		if err := json.Unmarshal(msg.Data, &wsMsg); err != nil {
			t.Fatalf("failed to decode websocket envelope: %v", err)
		}
		if wsMsg.Type != wantType {
			t.Errorf("got message type %s, want %s", wsMsg.Type, wantType)
		}

		return parsePayload[T](t, wsMsg.Payload)
	default:
		t.Fatalf("expected message of type %s on unicast channel, but channel was empty", wantType)
		return *new(T)
	}
}

// TestHub_NotifyFriendRequest verifies that NotifyFriendRequest encodes a TypeFriendRequestRecv
// websocket envelope with the complete friendship response payload and queues it onto the Hub's
// unicast channel targeted to the recipient user.
func TestHub_NotifyFriendRequest(t *testing.T) {
	hub := newTestHub()
	item := models.FriendshipItemResponse{
		FriendshipID: testFriendshipID,
		UserID:       testSenderID,
		Username:     "alice",
		AvatarURL:    nil,
		Status:       models.StatusPending,
		IsIncoming:   true,
	}

	err := hub.NotifyFriendRequest(testTargetUserID, item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := expectUnicastMessage[models.FriendshipItemResponse](t, hub.unicast, testTargetUserID, TypeFriendRequestRecv)
	if payload.FriendshipID != item.FriendshipID {
		t.Errorf("got friendship_id %d, want %d", payload.FriendshipID, item.FriendshipID)
	}
	if payload.UserID != item.UserID {
		t.Errorf("got user_id %d, want %d", payload.UserID, item.UserID)
	}
	if payload.Username != item.Username {
		t.Errorf("got username %s, want %s", payload.Username, item.Username)
	}
	if payload.Status != item.Status {
		t.Errorf("got status %s, want %s", payload.Status, item.Status)
	}
	if !payload.IsIncoming {
		t.Errorf("got is_incoming %v, want true", payload.IsIncoming)
	}
}

// TestHub_NotifyFriendResponse verifies that NotifyFriendResponse encodes a TypeFriendRequestResponse
// websocket envelope and queues it onto the Hub's unicast channel for the original requester.
func TestHub_NotifyFriendResponse(t *testing.T) {
	hub := newTestHub()
	item := models.FriendshipItemResponse{
		FriendshipID: testFriendshipID,
		UserID:       testTargetUserID,
		Username:     "bob",
		AvatarURL:    nil,
		Status:       models.StatusAccepted,
		IsIncoming:   false,
	}

	err := hub.NotifyFriendResponse(testSenderID, item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := expectUnicastMessage[models.FriendshipItemResponse](t, hub.unicast, testSenderID, TypeFriendRequestResponse)
	if payload.FriendshipID != item.FriendshipID {
		t.Errorf("got friendship_id %d, want %d", payload.FriendshipID, item.FriendshipID)
	}
	if payload.UserID != item.UserID {
		t.Errorf("got user_id %d, want %d", payload.UserID, item.UserID)
	}
	if payload.Username != item.Username {
		t.Errorf("got username %s, want %s", payload.Username, item.Username)
	}
	if payload.Status != item.Status {
		t.Errorf("got status %s, want %s", payload.Status, item.Status)
	}
	if payload.IsIncoming {
		t.Errorf("got is_incoming %v, want false", payload.IsIncoming)
	}
}

// TestHub_NotifyFriendDeleted verifies that NotifyFriendDeleted encodes a TypeFriendDeleted
// envelope with the deleted friendship ID and username and queues it onto the Hub's unicast channel for the other participant.
func TestHub_NotifyFriendDeleted(t *testing.T) {
	hub := newTestHub()

	testDeleterUsername := "bob"
	err := hub.NotifyFriendDeleted(testDeletedFriendID, testDeletedID, testDeleterUsername)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := expectUnicastMessage[models.FriendDeletePayload](t, hub.unicast, testDeletedFriendID, TypeFriendDeleted)
	if payload.FriendshipID != testDeletedID {
		t.Errorf("got friendship_id %d, want %d", payload.FriendshipID, testDeletedID)
	}
	if payload.Username != testDeleterUsername {
		t.Errorf("got username %q, want %q", payload.Username, testDeleterUsername)
	}
}

// TestHub_MatchFinished verifies that MatchFinished dispatches TypeMatchFinished real-time notifications
// to both players via unicast and dispatches ActionMatchFinished to purge the match session from the Hub's memory.
func TestHub_MatchFinished(t *testing.T) {
	hub := newTestHub()
	winnerID := testPlayer1ID

	payload := models.MatchFinishedPayload{
		MatchID:      testMatchID,
		Player1:      testPlayer1ID,
		Player2:      testPlayer2ID,
		WinnerID:     &winnerID,
		Player1Score: 11,
		Player2Score: 5,
		Status:       models.StatusFinished,
		Result:       models.ResultPlayer1Win,
	}

	err := hub.MatchFinished(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify unicast message to Player1
	p1 := expectUnicastMessage[models.MatchFinishedPayload](t, hub.unicast, payload.Player1, TypeMatchFinished)
	if p1.MatchID != payload.MatchID || p1.WinnerID == nil || *p1.WinnerID != *payload.WinnerID {
		t.Errorf("msg1 payload: got %+v, want %+v", p1, payload)
	}

	// Verify unicast message to Player2
	p2 := expectUnicastMessage[models.MatchFinishedPayload](t, hub.unicast, payload.Player2, TypeMatchFinished)
	if p2.MatchID != payload.MatchID || p2.WinnerID == nil || *p2.WinnerID != *payload.WinnerID {
		t.Errorf("msg2 payload: got %+v, want %+v", p2, payload)
	}

	// Verify ActionMatchFinished pushed to Hub matchAction
	select {
	case action := <-hub.matchAction:
		if action.Type != ActionMatchFinished {
			t.Errorf("got action type %v, want %v", action.Type, ActionMatchFinished)
		}
		if action.MatchID != payload.MatchID {
			t.Errorf("got action matchID %d, want %d", action.MatchID, payload.MatchID)
		}
	default:
		t.Fatal("expected ActionMatchFinished on matchAction channel")
	}
}

// TestHub_NotifyUser_BufferFull verifies that NotifyUser safely drops messages and returns an error
// without blocking when the Hub's buffered unicast channel capacity is completely full.
func TestHub_NotifyUser_BufferFull(t *testing.T) {
	hub := newTestHub()
	capacity := cap(hub.unicast)

	// Fill unicast buffer capacity
	for i := range capacity {
		err := hub.NotifyUser(int64(i), fmt.Appendf(nil, "msg-%d", i))
		if err != nil {
			t.Fatalf("unexpected error filling buffer at index %d: %v", i, err)
		}
	}

	// Next message must fail immediately without blocking
	err := hub.NotifyUser(int64(capacity+1), []byte("overflow"))
	if err == nil {
		t.Error("expected error when unicast buffer is full, got nil")
	}
}

// TestHub_NotifyPresence verifies that NotifyPresence encodes a TypePresenceUpdate
// websocket envelope with the given username and online status and routes it to targetUserID.
func TestHub_NotifyPresence(t *testing.T) {
	hub := newTestHub()
	targetUserID := int64(42)
	username := "alice"

	err := hub.NotifyPresence(targetUserID, username, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := expectUnicastMessage[PresenceUpdatePayload](t, hub.unicast, targetUserID, TypePresenceUpdate)
	if payload.Username != username {
		t.Errorf("got username %s, want %s", payload.Username, username)
	}
	if !payload.OnlineStatus {
		t.Errorf("got online_status %v, want true", payload.OnlineStatus)
	}
}

// TestHub_NotifyMutualPresence verifies that NotifyMutualPresence sends mutual TypePresenceUpdate
// websocket messages to both user A and user B informing them that the other is online.
func TestHub_NotifyMutualPresence(t *testing.T) {
	hub := newTestHub()
	userAID := int64(10)
	userBID := int64(20)
	usernameA := "alice"
	usernameB := "bob"

	err := hub.NotifyMutualPresence(userAID, userBID, usernameA, usernameB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User A should receive presence update informing them that User B is online
	msgForA := expectUnicastMessage[PresenceUpdatePayload](t, hub.unicast, userAID, TypePresenceUpdate)
	if msgForA.Username != usernameB {
		t.Errorf("user A payload: got username %s, want %s", msgForA.Username, usernameB)
	}
	if !msgForA.OnlineStatus {
		t.Errorf("user A payload: got online_status %v, want true", msgForA.OnlineStatus)
	}

	// User B should receive presence update informing them that User A is online
	msgForB := expectUnicastMessage[PresenceUpdatePayload](t, hub.unicast, userBID, TypePresenceUpdate)
	if msgForB.Username != usernameA {
		t.Errorf("user B payload: got username %s, want %s", msgForB.Username, usernameA)
	}
	if !msgForB.OnlineStatus {
		t.Errorf("user B payload: got online_status %v, want true", msgForB.OnlineStatus)
	}
}

// TestHub_NotifyMutualPresence_BufferFull verifies that NotifyMutualPresence propagates
// errors up the chain when delivery to the unicast channel fails.
func TestHub_NotifyMutualPresence_BufferFull(t *testing.T) {
	hub := newTestHub()
	capacity := cap(hub.unicast)

	// Fill unicast buffer to capacity so subsequent sends fail
	for i := range capacity {
		err := hub.NotifyUser(int64(i), fmt.Appendf(nil, "fill-%d", i))
		if err != nil {
			t.Fatalf("unexpected error filling buffer at index %d: %v", i, err)
		}
	}

	err := hub.NotifyMutualPresence(1, 2, "alice", "bob")
	if err == nil {
		t.Fatalf("expected error from NotifyMutualPresence when buffer is full, got nil")
	}
}

