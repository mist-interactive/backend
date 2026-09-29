package realtime

import (
	"fmt"
	"log/slog"
	"time"
)

const (
	inviteTTL           = 25 * time.Second // Server-side TTL
	invitePruneInterval = 30 * time.Second // Periodic memory sweep interval for stale expired invites
)

// sendInviteCancel sends a match_invite_cancel notification to recipient indicating that the challenge involving username was canceled.
func (h *Hub) sendInviteCancel(recipient, username string) {
	cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
		Username: username,
		Status:   "canceled",
	})
	if err == nil {
		h.sendToUsernameDirect(recipient, cancelBytes)
	}
}

// notifyInviteCanceled sends match_invite_cancel notifications to both the target and the challenger.
func (h *Hub) notifyInviteCanceled(challenger, target string) {
	h.sendInviteCancel(target, challenger)
	h.sendInviteCancel(challenger, target)
}

// pruneExpiredInvites sweeps h.invites and removes any entries that have exceeded inviteTTL,
// notifying both parties that the match challenge has timed out.
func (h *Hub) pruneExpiredInvites() {
	now := time.Now()
	pruned := 0
	for key, createdAt := range h.invites {
		if now.Sub(createdAt) > inviteTTL {
			delete(h.invites, key)
			pruned++
			h.notifyInviteCanceled(key.challenger, key.target)
		}
	}
	if pruned > 0 {
		slog.Debug("Pruned expired match invites from memory",
			"pruned_count", pruned,
			"remaining_invites", len(h.invites),
		)
	}
}

// cleanUpInvites cancels all pending invites involving the specified user and notifies the other party.
func (h *Hub) cleanUpInvites(username string) int {
	cleaned := 0
	for key := range h.invites {
		if key.challenger == username {
			delete(h.invites, key)
			cleaned++
			h.sendInviteCancel(key.target, username)
			slog.Info("Canceled pending invite for user",
				"user", username,
				"challenger", username,
				"target", key.target,
			)
		} else if key.target == username {
			delete(h.invites, key)
			cleaned++
			h.sendInviteCancel(key.challenger, username)
			slog.Info("Canceled pending invite for user",
				"user", username,
				"challenger", key.challenger,
				"target", username,
			)
		}
	}
	return cleaned
}

// onInviteSend validates an outgoing challenge, registers it in h.invites with a timestamp,
// enforces duplicate prevention and mutual challenge safeguards, and delivers "match_invite_recv" to the target.
func (h *Hub) onInviteSend(sender *Client, target string) {
	if sender.Username == target {
		slog.Warn("Match invite rejected: self challenge", "challenger", sender.Username)
		sender.SendError("Cannot invite yourself to a match")
		return
	}

	targetClient := h.findClientByUsername(target)
	if targetClient == nil {
		slog.Warn("Match invite rejected: target user is offline", "challenger", sender.Username, "target", target)
		sender.SendError(fmt.Sprintf("User '%s' is not online", target))
		return
	}

	now := time.Now()

	// 1. Check if this challenger already has an unexpired challenge to the exact same target
	key := inviteKey{challenger: sender.Username, target: target}
	if createdAt, exists := h.invites[key]; exists && now.Sub(createdAt) < inviteTTL {
		slog.Warn("Match invite rejected: challenge already pending",
			"challenger", sender.Username,
			"target", target,
			"remaining_seconds", int((inviteTTL - now.Sub(createdAt)).Seconds()),
		)
		sender.SendError(fmt.Sprintf("Challenge to '%s' is already pending", target))
		return
	}

	// 2. Check if the target has already challenged this sender (mutual challenge safeguard)
	reverseKey := inviteKey{challenger: target, target: sender.Username}
	if createdAt, exists := h.invites[reverseKey]; exists && now.Sub(createdAt) < inviteTTL {
		slog.Warn("Match invite rejected: reverse challenge pending",
			"challenger", sender.Username,
			"target", target,
		)
		sender.SendError(fmt.Sprintf("'%s' has already challenged you! Please accept their invite.", target))
		return
	}

	h.invites[key] = now
	slog.Info("Match invite sent",
		"challenger", sender.Username,
		"target", target,
		"total_pending_invites", len(h.invites),
	)

	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: sender.Username,
		Status:   "pending",
	})
	if err == nil {
		targetClient.TrySend(inviteBytes)
	}
}

// onInviteResponse handles an accept or decline from the target player.
// It verifies that a challenge is actively pending and within the TTL in h.invites.
// If accepted, it deletes the invite and launches createAndStartMatch in a separate goroutine.
// If declined, it deletes the invite and forwards the decline to the challenger.
func (h *Hub) onInviteResponse(sender *Client, challenger, status string) {
	key := inviteKey{challenger: challenger, target: sender.Username}
	createdAt, exists := h.invites[key]
	if !exists || time.Since(createdAt) > inviteTTL {
		delete(h.invites, key)
		slog.Warn("Match invite response rejected: invite not found or expired",
			"responder", sender.Username,
			"challenger", challenger,
			"status", status,
			"total_pending_invites", len(h.invites),
		)
		sender.SendError("Invite not found or has expired")
		return
	}
	delete(h.invites, key)

	slog.Info("Match invite response processed",
		"responder", sender.Username,
		"challenger", challenger,
		"status", status,
		"remaining_pending_invites", len(h.invites),
	)

	switch status {
	case "accepted":
		// Preemptively cancel any remaining pending challenges involving either player
		h.cleanUpInvites(challenger)
		h.cleanUpInvites(sender.Username)

		// Run DB match creation in background so the Hub event loop never blocks on DB I/O
		go h.createAndStartMatch(challenger, sender.Username)
	case "declined":
		declineBytes, err := EncodeMessage(TypeInviteResponse, MatchInvitePayload{
			Username: sender.Username,
			Status:   "declined",
		})
		if err == nil {
			if !h.sendToUsernameDirect(challenger, declineBytes) {
				slog.Warn("Declined invite response could not be delivered to challenger (offline)",
					"challenger", challenger,
					"responder", sender.Username,
				)
			}
		}
	}
}

// onInviteCancel deletes a pending challenge from h.invites and sends "match_invite_cancel"
// to the target player to dismiss the challenge prompt on their client.
func (h *Hub) onInviteCancel(sender *Client, target string) {
	key := inviteKey{challenger: sender.Username, target: target}
	createdAt, exists := h.invites[key]
	if !exists || time.Since(createdAt) > inviteTTL {
		delete(h.invites, key)
		slog.Warn("Match invite cancel rejected: invite not found or expired",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites),
		)
		return
	}
	delete(h.invites, key)
	slog.Info("Match invite canceled",
		"challenger", sender.Username,
		"target", target,
		"remaining_pending_invites", len(h.invites),
	)
	h.sendInviteCancel(target, sender.Username)
}
