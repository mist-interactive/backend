package realtime

import (
	"context"
	"dbBackend/models"
	"log/slog"
	"time"
)

// handleMatchAction serves as a thin dispatcher on the Hub's single-threaded event loop.
// By running these actions sequentially in Hub.Run(), access to h.invites is completely lock-free.
func (h *Hub) handleMatchAction(action MatchAction) {
	switch action.Type {
	case ActionInviteSend:
		h.onInviteSend(action.Sender, action.Target)
	case ActionInviteResponse:
		h.onInviteResponse(action.Sender, action.Target, action.Status)
	case ActionInviteCancel:
		h.onInviteCancel(action.Sender, action.Target)
	case ActionMatchStarted:
		h.onMatchStarted(action.Target, action.Opponent, action.MatchID)
	case ActionActiveMatchSync:
		h.onActiveMatchSync(action.Sender, action.Response)
	case ActionMatchFinished:
		h.onMatchFinished(action.MatchID)
	}
}

// handleActiveMatchOnRegister checks if the registering user is in an active match.
// If an active session exists in memory, it immediately sends TypeActiveMatch to the client.
// It also kicks off a background DB check to synchronize state.
func (h *Hub) handleActiveMatchOnRegister(client *Client) {
	if session, inMatch := h.activeMatches[client.Username]; inMatch {
		slog.Info("Reconnecting player found in memory activeMatches",
			"user_id", client.UserID,
			"username", client.Username,
			"match_id", session.MatchID,
		)
		activeMsg, err := EncodeMessage(TypeActiveMatch, session)
		if err == nil {
			client.TrySend(activeMsg)
		}
	}

	go h.checkActiveMatchDB(client)
}

// createAndStartMatch runs asynchronously in a worker goroutine to call the internal REST API
// and insert a match record in PostgreSQL without blocking the Hub's main event loop.
// Once the match ID is returned, it dispatches ActionMatchStarted to the Hub event loop.
func (h *Hub) createAndStartMatch(challenger, responder string) {
	slog.Info("Initiating match creation in database", "challenger", challenger, "responder", responder)
	matchID, err := h.store.CreateMatch(context.Background(), challenger, responder)
	if err != nil {
		slog.Error("Failed to create match between players in DB",
			"challenger", challenger,
			"responder", responder,
			"error", err,
		)
		errMsg, errEnc := EncodeMessage(TypeError, ErrorPayload{Message: "Failed to initialize match in database"})
		if errEnc == nil {
			h.SendToUsername(challenger, errMsg)
			h.SendToUsername(responder, errMsg)
		}
		return
	}

	slog.Info("Match created successfully, dispatching start to hub loop",
		"match_id", matchID,
		"challenger", challenger,
		"responder", responder,
	)

	h.matchAction <- MatchAction{
		Type:     ActionMatchStarted,
		Target:   challenger,
		Opponent: responder,
		MatchID:  matchID,
	}
}

// onMatchStarted records the active match session in memory for both players
// and delivers "match_started" messages to both players.
func (h *Hub) onMatchStarted(p1, p2 string, matchID int64) {
	p1Payload := &MatchSessionPayload{MatchID: matchID, Opponent: p2}
	p2Payload := &MatchSessionPayload{MatchID: matchID, Opponent: p1}

	h.activeMatches[p1] = p1Payload
	h.activeMatches[p2] = p2Payload

	p1Msg, err1 := EncodeMessage(TypeMatchStarted, p1Payload)
	if err1 == nil {
		h.sendToUsernameDirect(p1, p1Msg)
	}

	p2Msg, err2 := EncodeMessage(TypeMatchStarted, p2Payload)
	if err2 == nil {
		h.sendToUsernameDirect(p2, p2Msg)
	}

	slog.Info("Active match registered in memory and start broadcasted",
		"match_id", matchID,
		"player1", p1,
		"player2", p2,
	)
}

// onMatchFinished purges finished match sessions from the in-memory activeMatches map.
func (h *Hub) onMatchFinished(matchID int64) {
	for username, session := range h.activeMatches {
		if session.MatchID == matchID {
			delete(h.activeMatches, username)
			slog.Info("Cleared active match from memory on match finish", "username", username, "match_id", matchID)
		}
	}
}

// checkActiveMatchDB performs a background database query to check if a reconnecting user
// has an active match in progress, dispatching the result to the Hub event loop.
func (h *Hub) checkActiveMatchDB(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	activeMatch, err := h.store.GetActiveMatch(ctx, client.UserID)
	if err != nil {
		slog.Error("Problem checking active match in DB", "user_id", client.UserID, "username", client.Username, "error", err)
		return
	}

	h.matchAction <- MatchAction{
		Type:     ActionActiveMatchSync,
		Sender:   client,
		Response: activeMatch,
	}
}

// onActiveMatchSync reconciles the Hub's in-memory activeMatches map with the database state.
// If the DB reports no active match, any stale memory entry is pruned.
// If the DB reports an active match that was not in memory (e.g. after server restart), it restores it
// and notifies the reconnecting client.
func (h *Hub) onActiveMatchSync(client *Client, resp *models.ActiveMatchResponse) {
	currentClient, isConnected := h.clients[client.UserID]
	if !isConnected || currentClient != client {
		return
	}

	if resp == nil {
		// DB reports no active match for this user; clear any stale memory state
		if session, exists := h.activeMatches[client.Username]; exists {
			delete(h.activeMatches, client.Username)
			if oppSession, oppExists := h.activeMatches[session.Opponent]; oppExists && oppSession.MatchID == session.MatchID {
				delete(h.activeMatches, session.Opponent)
			}
			slog.Info("Cleared stale active match from memory", "username", client.Username, "match_id", session.MatchID)
		}
		return
	}

	session, alreadyInMemory := h.activeMatches[client.Username]
	if !alreadyInMemory || session.MatchID != resp.MatchID {
		payload := &MatchSessionPayload{
			MatchID:  resp.MatchID,
			Opponent: resp.OpponentUsername,
		}
		h.activeMatches[client.Username] = payload

		if _, oppExists := h.activeMatches[resp.OpponentUsername]; !oppExists {
			h.activeMatches[resp.OpponentUsername] = &MatchSessionPayload{
				MatchID:  resp.MatchID,
				Opponent: client.Username,
			}
		}

		slog.Info("Restored active match from DB into memory",
			"username", client.Username,
			"match_id", resp.MatchID,
			"opponent", resp.OpponentUsername,
		)

		activeMsg, err := EncodeMessage(TypeActiveMatch, payload)
		if err == nil {
			client.TrySend(activeMsg)
		}
	}
}
