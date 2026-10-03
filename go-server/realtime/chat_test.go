package realtime

import (
	"context"
	"dbBackend/models"
	"fmt"
	"errors"
	"testing"
	"time"
)

func TestChat_RateLimit(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "Burst allowed up to limit then rejected",
			test: func(t *testing.T) {
				c := &Client{
					tokens:     chatBurstLimit,
					lastRefill: time.Now(),
				}

				for i := range int(chatBurstLimit) {
					if !c.allowChatMessage() {
						t.Fatalf("expected message %d to be allowed", i+1)
					}
				}

				if c.allowChatMessage() {
					t.Errorf("expected 6th message to be rejected")
				}
			},
		},
		{
			name: "Replenishes token over time",
			test: func(t *testing.T) {
				c := &Client{
					tokens:     0,
					lastRefill: time.Now().Add(-1100 * time.Millisecond),
				}

				if !c.allowChatMessage() {
					t.Errorf("expected message to be allowed after 1.1s refill")
				}
				if c.allowChatMessage() {
					t.Errorf("expected consecutive message to be rejected")
				}
			},
		},
		{
			name: "HandleSendMsg rejects message when rate limit exceeded",
			test: func(t *testing.T) {
				hub, clients := setupTestHub(t, "alice")
				alice := clients["alice"]
				alice.tokens = 0
				alice.lastRefill = time.Now()

				_ = alice.HandleSendMsg(DMPayload{Username: "bob", Content: "spam"})

				drainUnicast(hub)
				assertErrorMessage(t, alice, errRateLimitExceeded)
			},
		},
		{
			name: "HandleSendMsg rejects message and sends error when save message fails (not friends)",
			test: func(t *testing.T) {
				store := &mockDataStore{
					saveMessageFunc: func(ctx context.Context, userID int64, recipient, content string) (*models.Message, error) {
						return nil, fmt.Errorf("users are not friends")
					},
				}
				hub := NewHub(store)
				alice := &Client{
					Hub:        hub,
					UserID:     1,
					Username:   "alice",
					Send:       make(chan []byte, 10),
					tokens:     chatBurstLimit,
					lastRefill: time.Now(),
				}
				hub.clients[alice.UserID] = alice

				err := alice.HandleSendMsg(DMPayload{Username: "bob", Content: "hello"})
				if err != nil {
					t.Fatalf("expected nil error returned from HandleSendMsg, got %v", err)
				}

				drainUnicast(hub)
				assertErrorMessage(t, alice, errNotFriends)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.test(t)
		})
	}
}

func TestHandleSendMsg(t *testing.T) {
	t.Run("Rejects self messaging without dispatch or error", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice")
		alice := clients["alice"]

		err := alice.HandleSendMsg(DMPayload{Username: "alice", Content: "talking to myself"})
		if err != nil {
			t.Errorf("expected nil error on self-message, got %v", err)
		}
		select {
		case msg := <-hub.unicast:
			t.Errorf("unexpected unicast message sent on self-message: %+v", msg)
		default:
		}
	})

	t.Run("Successfully saves message and dispatches DM to recipient", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		alice := clients["alice"]
		bob := clients["bob"]

		now := time.Now()
		mockStore := &mockDataStore{
			saveMessageFunc: func(ctx context.Context, userID int64, recipient, content string) (*models.Message, error) {
				return &models.Message{
					ID:          123,
					SenderID:    alice.UserID,
					RecipientID: bob.UserID,
					Content:     content,
					CreatedAt:   now,
				}, nil
			},
		}
		hub.store = mockStore

		err := alice.HandleSendMsg(DMPayload{Username: "bob", Content: "hey bob!"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		drainUnicast(hub)

		msg, ok := readWSMessage(t, bob.Send, 50*time.Millisecond)
		if !ok {
			t.Fatalf("bob did not receive direct message")
		}
		if msg.Type != TypeDMRecv {
			t.Errorf("expected type %s, got %s", TypeDMRecv, msg.Type)
		}
		payload := parsePayload[DMPayload](t, msg.Payload)
		if payload.ID != 123 || payload.Username != "alice" || payload.Content != "hey bob!" {
			t.Errorf("payload mismatch: %+v", payload)
		}
	})

	t.Run("Rejects self messaging case-insensitively without dispatch or error", func(t *testing.T) {
		hub, clients := setupTestHub(t, "Alice")
		alice := clients["Alice"]

		err := alice.HandleSendMsg(DMPayload{Username: "alice", Content: "talking to myself"})
		if err != nil {
			t.Errorf("expected nil error on self-message, got %v", err)
		}
		select {
		case msg := <-hub.unicast:
			t.Errorf("unexpected unicast message sent on self-message: %+v", msg)
		default:
		}
	})

	t.Run("Sends error to client when DB save fails", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		alice := clients["alice"]
		mockStore := &mockDataStore{
			saveMessageFunc: func(ctx context.Context, userID int64, recipient, content string) (*models.Message, error) {
				return nil, errors.New("db failure")
			},
		}
		hub.store = mockStore

		err := alice.HandleSendMsg(DMPayload{Username: "bob", Content: "hello"})
		if err != nil {
			t.Fatalf("expected nil error when DB save fails, got %v", err)
		}
		drainUnicast(hub)
		assertErrorMessage(t, alice, errNotFriends)
	})
}

