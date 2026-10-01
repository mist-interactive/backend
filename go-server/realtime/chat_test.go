package realtime

import (
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.test(t)
		})
	}
}
