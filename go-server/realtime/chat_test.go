package realtime

import (
	"context"
	"dbBackend/models"
	"fmt"
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
