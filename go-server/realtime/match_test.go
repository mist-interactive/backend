package realtime

import (
	"testing"
)

func TestMatchDispatchHandlers(t *testing.T) {
	t.Run("HandleMatchInvite rejects self invite", func(t *testing.T) {
		_, clients := setupTestHub(t, "alice")
		alice := clients["alice"]

		err := alice.HandleMatchInvite(MatchInvitePayload{Username: "alice"})
		if err == nil {
			t.Errorf("expected error for self-invite, got nil")
		}
	})

	t.Run("HandleMatchInvite pushes ActionInviteSend to Hub", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		hub.matchAction = make(chan MatchAction, 1)
		alice := clients["alice"]

		err := alice.HandleMatchInvite(MatchInvitePayload{Username: "bob"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case action := <-hub.matchAction:
			if action.Type != ActionInviteSend || action.Sender != alice || action.Target != "bob" {
				t.Errorf("action mismatch: %+v", action)
			}
		default:
			t.Errorf("expected matchAction to receive ActionInviteSend")
		}
	})

	t.Run("HandleMatchInviteResponse pushes ActionInviteResponse to Hub", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		hub.matchAction = make(chan MatchAction, 1)
		bob := clients["bob"]

		err := bob.HandleMatchInviteResponse(MatchInvitePayload{Username: "alice", Status: "accepted"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case action := <-hub.matchAction:
			if action.Type != ActionInviteResponse || action.Sender != bob || action.Target != "alice" || action.Status != "accepted" {
				t.Errorf("action mismatch: %+v", action)
			}
		default:
			t.Errorf("expected matchAction to receive ActionInviteResponse")
		}
	})

	t.Run("HandleMatchInviteCancel pushes ActionInviteCancel to Hub", func(t *testing.T) {
		hub, clients := setupTestHub(t, "alice", "bob")
		hub.matchAction = make(chan MatchAction, 1)
		alice := clients["alice"]

		err := alice.HandleMatchInviteCancel(MatchInvitePayload{Username: "bob"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case action := <-hub.matchAction:
			if action.Type != ActionInviteCancel || action.Sender != alice || action.Target != "bob" {
				t.Errorf("action mismatch: %+v", action)
			}
		default:
			t.Errorf("expected matchAction to receive ActionInviteCancel")
		}
	})
}
