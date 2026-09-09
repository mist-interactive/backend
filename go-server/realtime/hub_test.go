package realtime

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"testing"
)

func TestHub_NotifyFriendRequest(t *testing.T) {
	tests := []struct {
		name         string
		targetUserID int64
		item         models.FriendshipItemResponse
	}{
		{
			name:         "Success: Deliver friend request notification",
			targetUserID: 42,
			item: models.FriendshipItemResponse{
				FriendshipID: 101,
				UserID:       7,
				Username:     "alice",
				AvatarURL:    nil,
				Status:       models.StatusPending,
				IsIncoming:   true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewHub(nil)

			err := hub.NotifyFriendRequest(tc.targetUserID, tc.item)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify message delivered to unicast channel
			select {
			case msg := <-hub.unicast:
				if msg.UserID != tc.targetUserID {
					t.Errorf("got target userID %d, want %d", msg.UserID, tc.targetUserID)
				}

				var wsMsg WebsocketMessage
				if err := json.Unmarshal(msg.Data, &wsMsg); err != nil {
					t.Fatalf("failed to decode websocket envelope: %v", err)
				}
				if wsMsg.Type != TypeFriendRequestRecv {
					t.Errorf("got message type %s, want %s", wsMsg.Type, TypeFriendRequestRecv)
				}

				var receivedItem models.FriendshipItemResponse
				if err := json.Unmarshal(wsMsg.Payload, &receivedItem); err != nil {
					t.Fatalf("failed to decode payload: %v", err)
				}
				if receivedItem.FriendshipID != tc.item.FriendshipID {
					t.Errorf("got friendship_id %d, want %d", receivedItem.FriendshipID, tc.item.FriendshipID)
				}
				if receivedItem.UserID != tc.item.UserID {
					t.Errorf("got user_id %d, want %d", receivedItem.UserID, tc.item.UserID)
				}
				if receivedItem.Username != tc.item.Username {
					t.Errorf("got username %s, want %s", receivedItem.Username, tc.item.Username)
				}
				if receivedItem.Status != tc.item.Status {
					t.Errorf("got status %s, want %s", receivedItem.Status, tc.item.Status)
				}
				if !receivedItem.IsIncoming {
					t.Errorf("got is_incoming %v, want true", receivedItem.IsIncoming)
				}
			default:
				t.Fatal("expected message on unicast channel, but channel was empty")
			}
		})
	}
}

func TestHub_NotifyFriendResponse(t *testing.T) {
	tests := []struct {
		name         string
		targetUserID int64
		item         models.FriendshipItemResponse
	}{
		{
			name:         "Success: Deliver friend response notification",
			targetUserID: 7,
			item: models.FriendshipItemResponse{
				FriendshipID: 101,
				UserID:       42,
				Username:     "bob",
				AvatarURL:    nil,
				Status:       models.StatusAccepted,
				IsIncoming:   false,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub := NewHub(nil)

			err := hub.NotifyFriendResponse(tc.targetUserID, tc.item)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			select {
			case msg := <-hub.unicast:
				if msg.UserID != tc.targetUserID {
					t.Errorf("got target userID %d, want %d", msg.UserID, tc.targetUserID)
				}

				var wsMsg WebsocketMessage
				if err := json.Unmarshal(msg.Data, &wsMsg); err != nil {
					t.Fatalf("failed to decode websocket envelope: %v", err)
				}
				if wsMsg.Type != TypeFriendRequestResponse {
					t.Errorf("got message type %s, want %s", wsMsg.Type, TypeFriendRequestResponse)
				}

				var receivedItem models.FriendshipItemResponse
				if err := json.Unmarshal(wsMsg.Payload, &receivedItem); err != nil {
					t.Fatalf("failed to decode payload: %v", err)
				}
				if receivedItem.FriendshipID != tc.item.FriendshipID {
					t.Errorf("got friendship_id %d, want %d", receivedItem.FriendshipID, tc.item.FriendshipID)
				}
				if receivedItem.UserID != tc.item.UserID {
					t.Errorf("got user_id %d, want %d", receivedItem.UserID, tc.item.UserID)
				}
				if receivedItem.Username != tc.item.Username {
					t.Errorf("got username %s, want %s", receivedItem.Username, tc.item.Username)
				}
				if receivedItem.Status != tc.item.Status {
					t.Errorf("got status %s, want %s", receivedItem.Status, tc.item.Status)
				}
				if receivedItem.IsIncoming {
					t.Errorf("got is_incoming %v, want false", receivedItem.IsIncoming)
				}
			default:
				t.Fatal("expected message on unicast channel, but channel was empty")
			}
		})
	}
}

func TestHub_NotifyUser_BufferFull(t *testing.T) {
	hub := NewHub(nil)

	// Fill buffer capacity (256 messages)
	for i := 0; i < 256; i++ {
		err := hub.NotifyUser(int64(i), fmt.Sprintf("msg-%d", i))
		if err != nil {
			t.Fatalf("unexpected error on message %d: %v", i, err)
		}
	}

	// 257th message must fail without blocking
	err := hub.NotifyUser(999, "overflow")
	if err == nil {
		t.Error("expected error when unicast buffer is full, got nil")
	}
}
