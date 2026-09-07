package realtime

import (
	"fmt"
	"log/slog"
	"time"
)

const (
	inviteDisplayDuration = 20               // Duration in seconds for client-side countdown timer display
	inviteTTL             = 25 * time.Second // Server-side TTL (20s client timer + 5s network latency buffer)
	invitePruneInterval   = 30 * time.Second // Periodic memory sweep interval for stale expired invites
)

// pruneExpiredInvites sweeps h.invites and removes any entries that have exceeded inviteTTL.
func (h *Hub) pruneExpiredInvites() {
	now := time.Now()
	pruned := 0
	for key, createdAt := range h.invites {
		if now.Sub(createdAt) > inviteTTL {
			delete(h.invites, key)
			pruned++
		}
	}
	if pruned > 0 {
		slog.Debug("Pruned expired match invites from memory",
			"pruned_count", pruned,
			"remaining_invites", len(h.invites),
		)
	}
}

// cleanUpInvites cancels all pending invites involving the disconnected user and notifies the other party.
func (h *Hub) cleanUpInvites(username string) int {
	cleaned := 0
	now := time.Now()
	for key, createdAt := range h.invites {
		// Ignore and delete invites that already timed out
		if now.Sub(createdAt) > inviteTTL {
			delete(h.invites, key)
			continue
		}

		if key.challenger == username {
			delete(h.invites, key)
			cleaned++
			cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
				Username: username,
				Status:   "canceled",
			})
			if err == nil {
				h.sendToUsernameDirect(key.target, cancelBytes)
			}
			slog.Info("Canceled pending invite: challenger disconnected",
				"challenger", username,
				"target", key.target,
			)
		} else if key.target == username {
			delete(h.invites, key)
			cleaned++
			cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
				Username: username,
				Status:   "canceled",
			})
			if err == nil {
				h.sendToUsernameDirect(key.challenger, cancelBytes)
			}
			slog.Info("Canceled pending invite: target disconnected",
				"challenger", key.challenger,
				"target", username,
			)
		}
	}
	return cleaned
}

// onInviteSend validates an outgoing challenge, registers it in h.invites with a timestamp,
// enforces single active invite / duplicate prevention, and delivers "match_invite_recv" to the target.
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

	// 3. Prevent challenger from issuing concurrent challenges to different friends
	for k, createdAt := range h.invites {
		if k.challenger == sender.Username && now.Sub(createdAt) < inviteTTL {
			slog.Warn("Match invite rejected: challenger already has active outgoing invite",
				"challenger", sender.Username,
				"active_target", k.target,
			)
			sender.SendError(fmt.Sprintf("You already have an active challenge to '%s'", k.target))
			return
		}
	}

	h.invites[key] = now
	slog.Info("Match invite sent",
		"challenger", sender.Username,
		"target", target,
		"duration_sec", inviteDisplayDuration,
		"total_pending_invites", len(h.invites),
	)

	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: sender.Username,
		Status:   "pending",
		Duration: inviteDisplayDuration,
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
	cancelBytes, err := EncodeMessage(TypeInviteCancel, MatchInvitePayload{
		Username: sender.Username,
		Status:   "canceled",
	})
	if err == nil {
		if !h.sendToUsernameDirect(target, cancelBytes) {
			slog.Debug("Cancel invite notification not delivered to target (offline)",
				"challenger", sender.Username,
				"target", target,
			)
		}
	}
}
