package realtime

import (
	"fmt"
	"log/slog"
)

// cleanUpInvites cancels all pending invites involving the disconnected user and notifies the other party.
func (h *Hub) cleanUpInvites(username string) int {
	cleaned := 0
	for key := range h.invites {
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

// onInviteSend validates an outgoing challenge, registers it in h.invites, and delivers
// a "match_invite_recv" notification to the target player if they are currently connected.
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

	key := inviteKey{challenger: sender.Username, target: target}
	alreadyPending := h.invites[key]

	if alreadyPending {
		slog.Warn("Duplicate match invite sent while already pending",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites),
		)
	} else {
		slog.Info("Match invite sent",
			"challenger", sender.Username,
			"target", target,
			"total_pending_invites", len(h.invites)+1,
		)
	}

	h.invites[key] = true
	inviteBytes, err := EncodeMessage(TypeInviteRecv, MatchInvitePayload{
		Username: sender.Username,
		Status:   "pending",
	})
	if err == nil {
		targetClient.TrySend(inviteBytes)
	}
}

// onInviteResponse handles an accept or decline from the target player.
// It verifies that a challenge is actively pending in h.invites (anti-spoof protection).
// If accepted, it deletes the invite and launches createAndStartMatch in a separate goroutine.
// If declined, it deletes the invite and forwards the decline to the challenger.
func (h *Hub) onInviteResponse(sender *Client, challenger, status string) {
	key := inviteKey{challenger: challenger, target: sender.Username}
	if !h.invites[key] {
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
	if !h.invites[key] {
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
