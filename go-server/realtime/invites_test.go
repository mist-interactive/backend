package realtime

import (
	"maps"
	"testing"
	"time"
)

// setupTestHub creates a test Hub with pre-registered clients by username (analogous to MakeNTestUsers).
func setupTestHub(t *testing.T, usernames ...string) (*Hub, map[string]*Client) {
	t.Helper()
	hub := NewHub(&mockDataStore{})
	clients := make(map[string]*Client, len(usernames))
	for i, name := range usernames {
		c := &Client{
			Hub:      hub,
			UserID:   int64(i + 1),
			Username: name,
			Send:     make(chan []byte, 10),
		}
		hub.clients[c.UserID] = c
		clients[name] = c
	}
	return hub, clients
}

// drainUnicast routes any pending messages in hub.unicast to their recipient clients in synchronous tests.
func drainUnicast(hub *Hub) {
	for {
		select {
		case msg := <-hub.unicast:
			if msg.UserID != 0 {
				if client, ok := hub.clients[msg.UserID]; ok {
					client.TrySend(msg.Data)
				}
			} else if msg.Username != "" {
				hub.sendToUsernameDirect(msg.Username, msg.Data)
			}
		default:
			return
		}
	}
}

// assertErrorMessage asserts that the client received a websocket error message with the expected text.
func assertErrorMessage(t *testing.T, c *Client, wantMsg string) {
	t.Helper()
	msg, ok := readWSMessage(t, c.Send, 50*time.Millisecond)
	if !ok {
		t.Fatalf("%s did not receive expected error message: %q", c.Username, wantMsg)
	}
	if msg.Type != TypeError {
		t.Errorf("%s message type: got %s, want %s", c.Username, msg.Type, TypeError)
	}
	payload := parsePayload[ErrorPayload](t, msg.Payload)
	if payload.Message != wantMsg {
		t.Errorf("%s error text: got %q, want %q", c.Username, payload.Message, wantMsg)
	}
}

// assertInviteRecv asserts that the client received a match_invite_recv message from challenger.
func assertInviteRecv(t *testing.T, c *Client, wantChallenger string) {
	t.Helper()
	msg, ok := readWSMessage(t, c.Send, 50*time.Millisecond)
	if !ok {
		t.Fatalf("%s did not receive match_invite_recv from %s", c.Username, wantChallenger)
	}
	if msg.Type != TypeInviteRecv {
		t.Errorf("%s message type: got %s, want %s", c.Username, msg.Type, TypeInviteRecv)
	}
	payload := parsePayload[MatchInvitePayload](t, msg.Payload)
	if payload.Username != wantChallenger || payload.Status != "pending" {
		t.Errorf("%s invite payload: got %+v, want challenger %s", c.Username, payload, wantChallenger)
	}
}

// assertNoMessage asserts that the client received no message within a short timeout.
func assertNoMessage(t *testing.T, c *Client) {
	t.Helper()
	if msg, ok := readWSMessage(t, c.Send, 20*time.Millisecond); ok {
		t.Errorf("%s unexpectedly received message type %s", c.Username, msg.Type)
	}
}

func TestInviteSend(t *testing.T) {
	tests := []struct {
		name         string
		challenger   string
		target       string
		preInvites   map[inviteKey]time.Time
		wantError    string
		wantReceived bool
	}{
		{
			name:         "Success: Delivers challenge and stores in memory",
			challenger:   "alice",
			target:       "bob",
			wantReceived: true,
		},
		{
			name:       "Failure: Self challenge rejected",
			challenger: "alice",
			target:     "alice",
			wantError:  "Cannot invite yourself to a match",
		},
		{
			name:       "Failure: Offline user rejected",
			challenger: "alice",
			target:     "offline_user",
			wantError:  "User 'offline_user' is not online",
		},
		{
			name:       "Failure: Duplicate challenge to same target rejected while pending",
			challenger: "alice",
			target:     "bob",
			preInvites: map[inviteKey]time.Time{
				{challenger: "alice", target: "bob"}: time.Now(),
			},
			wantError: "Challenge to 'bob' is already pending",
		},
		{
			name:       "Failure: Reverse mutual challenge rejected",
			challenger: "bob",
			target:     "alice",
			preInvites: map[inviteKey]time.Time{
				{challenger: "alice", target: "bob"}: time.Now(),
			},
			wantError: "'alice' has already challenged you! Please accept their invite.",
		},
		{
			name:       "Success: Allows re-inviting once previous invite exceeds TTL",
			challenger: "alice",
			target:     "bob",
			preInvites: map[inviteKey]time.Time{
				{challenger: "alice", target: "bob"}: time.Now().Add(-26 * time.Second),
			},
			wantReceived: true,
		},
		{
			name:       "Success: Cross-friend challenge allowed while another friend challenge pending",
			challenger: "alice",
			target:     "charlie",
			preInvites: map[inviteKey]time.Time{
				{challenger: "alice", target: "bob"}: time.Now(),
			},
			wantReceived: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub, clients := setupTestHub(t, "alice", "bob", "charlie")
			maps.Copy(hub.invites, tc.preInvites)

			sender, ok := clients[tc.challenger]
			if !ok {
				t.Fatalf("challenger %s not found in test clients", tc.challenger)
			}

			hub.onInviteSend(sender, tc.target)
			drainUnicast(hub)

			if tc.wantError != "" {
				assertErrorMessage(t, sender, tc.wantError)
				if targetClient, isOnline := clients[tc.target]; isOnline && targetClient != sender {
					assertNoMessage(t, targetClient)
				}
			} else if tc.wantReceived {
				targetClient := clients[tc.target]
				assertInviteRecv(t, targetClient, tc.challenger)

				key := inviteKey{challenger: tc.challenger, target: tc.target}
				if createdAt, exists := hub.invites[key]; !exists || time.Since(createdAt) > 2*time.Second {
					t.Errorf("hub.invites[%+v]: got exists=%v createdAt=%v, want freshly set", key, exists, createdAt)
				}
			}
		})
	}
}

func TestInviteResponseAndCancel(t *testing.T) {
	t.Run("Late response to expired invite returns error and deletes key", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		key := inviteKey{challenger: "alice", target: "bob"}
		hub.invites[key] = time.Now().Add(-26 * time.Second)

		hub.onInviteResponse(clients["bob"], "alice", "accepted")
		drainUnicast(hub)

		if _, exists := hub.invites[key]; exists {
			t.Errorf("expired invite was not deleted from memory")
		}
		assertErrorMessage(t, clients["bob"], "Invite not found or has expired")
	})

	t.Run("Late cancel of expired invite removes key gracefully", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		key := inviteKey{challenger: "alice", target: "bob"}
		hub.invites[key] = time.Now().Add(-26 * time.Second)

		hub.onInviteCancel(clients["alice"], "bob")

		if _, exists := hub.invites[key]; exists {
			t.Errorf("expired invite was not deleted from memory on cancel")
		}
		assertNoMessage(t, clients["bob"])
	})

	t.Run("Accepting challenge cancels other challenges for both players", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob", "charlie", "dave")
		// Alice challenged Bob
		hub.invites[inviteKey{challenger: "alice", target: "bob"}] = time.Now()
		// Alice also challenged Charlie
		hub.invites[inviteKey{challenger: "alice", target: "charlie"}] = time.Now()
		// Dave challenged Bob
		hub.invites[inviteKey{challenger: "dave", target: "bob"}] = time.Now()

		// Bob accepts Alice's challenge
		hub.onInviteResponse(clients["bob"], "alice", "accepted")

		// Accepted invite was removed
		if _, exists := hub.invites[inviteKey{challenger: "alice", target: "bob"}]; exists {
			t.Errorf("accepted invite (alice, bob) was not removed from memory")
		}
		// Alice's other outgoing challenge to Charlie was canceled
		if _, exists := hub.invites[inviteKey{challenger: "alice", target: "charlie"}]; exists {
			t.Errorf("alice's outgoing invite to charlie was not canceled")
		}
		// Dave's incoming challenge to Bob was canceled
		if _, exists := hub.invites[inviteKey{challenger: "dave", target: "bob"}]; exists {
			t.Errorf("dave's incoming invite to bob was not canceled")
		}

		// Charlie receives match_invite_cancel from alice
		msgCharlie, okCharlie := readWSMessage(t, clients["charlie"].Send, 50*time.Millisecond)
		if !okCharlie || msgCharlie.Type != TypeInviteCancel {
			t.Fatalf("expected cancel on charlie.Send, got ok=%v, msg=%+v", okCharlie, msgCharlie)
		}
		payloadCharlie := parsePayload[MatchInvitePayload](t, msgCharlie.Payload)
		if payloadCharlie.Username != "alice" || payloadCharlie.Status != "canceled" {
			t.Errorf("charlie payload: got %+v, want alice/canceled", payloadCharlie)
		}

		// Dave receives match_invite_cancel from bob
		msgDave, okDave := readWSMessage(t, clients["dave"].Send, 50*time.Millisecond)
		if !okDave || msgDave.Type != TypeInviteCancel {
			t.Fatalf("expected cancel on dave.Send, got ok=%v, msg=%+v", okDave, msgDave)
		}
		payloadDave := parsePayload[MatchInvitePayload](t, msgDave.Payload)
		if payloadDave.Username != "bob" || payloadDave.Status != "canceled" {
			t.Errorf("dave payload: got %+v, want bob/canceled", payloadDave)
		}
	})
}

func TestInvitePruneAndCleanup(t *testing.T) {
	t.Run("PruneExpiredInvites sweeps stale invites and notifies both parties", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob", "charlie", "dave")
		activeKey := inviteKey{challenger: "alice", target: "bob"}
		expiredKey := inviteKey{challenger: "charlie", target: "dave"}
		hub.invites[activeKey] = time.Now()
		hub.invites[expiredKey] = time.Now().Add(-30 * time.Second)

		hub.pruneExpiredInvites()

		if _, exists := hub.invites[activeKey]; !exists {
			t.Errorf("active invite was unexpectedly pruned")
		}
		if _, exists := hub.invites[expiredKey]; exists {
			t.Errorf("expired invite was not pruned")
		}

		// Active parties should receive no cancellation
		assertNoMessage(t, clients["alice"])
		assertNoMessage(t, clients["bob"])

		// Target of expired invite (dave) receives cancel with challenger name (charlie)
		msgDave, okDave := readWSMessage(t, clients["dave"].Send, 50*time.Millisecond)
		if !okDave || msgDave.Type != TypeInviteCancel {
			t.Fatalf("expected cancel on dave.Send, got ok=%v, msg=%+v", okDave, msgDave)
		}
		payloadDave := parsePayload[MatchInvitePayload](t, msgDave.Payload)
		if payloadDave.Username != "charlie" || payloadDave.Status != "canceled" {
			t.Errorf("dave payload: got %+v, want charlie/canceled", payloadDave)
		}

		// Challenger of expired invite (charlie) receives cancel with target name (dave)
		msgCharlie, okCharlie := readWSMessage(t, clients["charlie"].Send, 50*time.Millisecond)
		if !okCharlie || msgCharlie.Type != TypeInviteCancel {
			t.Fatalf("expected cancel on charlie.Send, got ok=%v, msg=%+v", okCharlie, msgCharlie)
		}
		payloadCharlie := parsePayload[MatchInvitePayload](t, msgCharlie.Payload)
		if payloadCharlie.Username != "dave" || payloadCharlie.Status != "canceled" {
			t.Errorf("charlie payload: got %+v, want dave/canceled", payloadCharlie)
		}
	})

	t.Run("CleanUpInvites removes pending invite and notifies partner", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		key := inviteKey{challenger: "alice", target: "bob"}
		hub.invites[key] = time.Now()

		cleaned := hub.cleanUpInvites("alice")
		if cleaned != 1 {
			t.Errorf("cleaned count: got %d, want 1", cleaned)
		}
		if _, exists := hub.invites[key]; exists {
			t.Errorf("invite was not removed from memory")
		}

		msg, ok := readWSMessage(t, clients["bob"].Send, 50*time.Millisecond)
		if !ok || msg.Type != TypeInviteCancel {
			t.Fatalf("expected cancel message on bob.Send, got ok=%v, msg=%+v", ok, msg)
		}
	})
}
