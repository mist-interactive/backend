package handlers

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// MatchCreate handles POST /api/internal/matches.
// It is called by the WebSocket service when a match challenge is accepted.
// It resolves player usernames to database primary keys and inserts a new match
// with status 'in_progress', returning the newly generated match ID.
func (h *Handler) MatchCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[models.MatchCreateInput](r)
	if err != nil {
		http.Error(w, "Problem validating request", http.StatusBadRequest)
		return
	}
	if input.Player1 == input.Player2 {
		slog.Warn("match create rejected: player cannot play themselves", "player", input.Player1)
		http.Error(w, "Players cannot play themselves", http.StatusConflict)
		return
	}

	p1, err := h.getUserByUsername(r.Context(), input.Player1)
	if err != nil {
		slog.Warn("match create failed: player 1 not found", "player1", input.Player1, "error", err)
		http.Error(w, "Player 1 not found", http.StatusNotFound)
		return
	}
	p2, err := h.getUserByUsername(r.Context(), input.Player2)
	if err != nil {
		slog.Warn("match create failed: player 2 not found", "player2", input.Player2, "error", err)
		http.Error(w, "Player 2 not found", http.StatusNotFound)
		return
	}

	if _, err := h.SweepStaleMatches(r.Context(), models.DefaultHeartbeatTimeout); err != nil {
		slog.Warn("failed to sweep stale matches before match create", "error", err)
	}

	hasActiveMatch, err := h.DB.NewSelect().
		Model((*models.MatchRecord)(nil)).
		Where("status = ?", models.StatusInProgress).
		Where("player_one IN (?, ?) OR player_two IN (?, ?)", p1.ID, p2.ID, p1.ID, p2.ID).
		Exists(r.Context())
	if err != nil {
		HandleDBError(w, err, "Checking active match")
		return
	}
	if hasActiveMatch {
		slog.Warn("match create rejected: player already in an active match", "player1", input.Player1, "player2", input.Player2)
		http.Error(w, "One or more players are already in an active match", http.StatusConflict)
		return
	}

	match := &models.MatchRecord{
		Player1: p1.ID,
		Player2: p2.ID,
		Status:  models.StatusInProgress,
	}
	err = h.DB.NewInsert().
		Model(match).
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Match creation")
		return
	}

	slog.Info("match record created in database", "match_id", match.ID, "player1", input.Player1, "player2", input.Player2)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": match.ID,
	})
}

// MatchPatch handles PATCH /api/internal/matches/{id}.
// It is called by the Game Server when a match concludes to record player scores and result.
// It maps the reported scores to player_one_score and player_two_score based on participant IDs,
// and automatically infers the match result (player1_win, player2_win, draw) and status (finished).
func (h *Handler) MatchPatch(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	matchID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		slog.Warn("match patch rejected: invalid match ID", "id", idStr, "error", err)
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}
	input, err := DecodeAndValidate[models.MatchPatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if input.Scores[0].PlayerID == input.Scores[1].PlayerID {
		slog.Warn("match patch rejected: duplicate player ID in scores", "match_id", matchID, "player_id", input.Scores[0].PlayerID)
		http.Error(w, "Scores must be for two distinct players", http.StatusBadRequest)
		return
	}

	var match models.MatchRecord
	err = h.DB.NewSelect().
		Model(&match).
		Where("id = ?", matchID).
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Fetching match")
		return
	}

	if match.Status != models.StatusInProgress {
		slog.Warn("match patch rejected: match is not in progress", "match_id", matchID, "status", match.Status)
		http.Error(w, fmt.Sprintf("Match with ID %d is already %s", matchID, match.Status), http.StatusConflict)
		return
	}

	// Map reported scores to player 1 and player 2 regardless of order
	var p1Score, p2Score int
	var found1, found2 bool

	for _, s := range input.Scores {
		switch s.PlayerID {
		case match.Player1:
			p1Score = s.Score
			found1 = true
		case match.Player2:
			p2Score = s.Score
			found2 = true
		}
	}

	if !found1 || !found2 {
		slog.Warn("match patch rejected: score player IDs do not match participants",
			"match_id", matchID,
			"expected_p1", match.Player1,
			"expected_p2", match.Player2,
			"received_id1", input.Scores[0].PlayerID,
			"received_id2", input.Scores[1].PlayerID,
		)
		http.Error(w, "Reported scores do not match the registered match participants", http.StatusBadRequest)
		return
	}

	// Infer status (default to finished if omitted)
	status := models.StatusFinished
	if input.Status != nil && *input.Status != "" {
		status = *input.Status
	}

	// Infer result based on scores
	var result models.MatchResult
	if p1Score > p2Score {
		result = models.ResultPlayer1Win
	} else if p2Score > p1Score {
		result = models.ResultPlayer2Win
	} else {
		result = models.ResultDraw
	}

	now := time.Now()
	res, err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("id = ?", matchID).
		Where("status = ?", models.StatusInProgress).
		Set("player_one_score = ?", p1Score).
		Set("player_two_score = ?", p2Score).
		Set("status = ?", status).
		Set("result = ?", result).
		Set("finished_at = ?", now).
		Exec(r.Context())

	if err != nil {
		HandleDBError(w, err, "Updating match history")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		slog.Warn("match patch failed: match status changed concurrently", "match_id", matchID)
		http.Error(w, fmt.Sprintf("No match in progress with ID %d was found", matchID), http.StatusConflict)
		return
	}

	slog.Info("match result updated in database",
		"match_id", matchID,
		"player_one_id", match.Player1,
		"player_one_score", p1Score,
		"player_two_id", match.Player2,
		"player_two_score", p2Score,
		"status", status,
		"result", result,
	)

	p1Earned, p2Earned := h.evaluatePostMatchBadges(r.Context(), match, result)
	h.broadcastMatchFinished(matchID, match, p1Score, p2Score, status, result, p1Earned, p2Earned)

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) evaluatePostMatchBadges(ctx context.Context, match models.MatchRecord, result models.MatchResult) ([]models.BadgeDefinition, []models.BadgeDefinition) {
	p1Stats, err := h.getUserStats(ctx, match.Player1)
	if err != nil {
		slog.Error("failed to get player 1 stats for badge evaluation", "user_id", match.Player1, "error", err)
	}
	p2Stats, err := h.getUserStats(ctx, match.Player2)
	if err != nil {
		slog.Error("failed to get player 2 stats for badge evaluation", "user_id", match.Player2, "error", err)
	}

	p1Earned, _ := h.EvaluateAndGrantBadges(ctx, match.Player1, models.TriggerMatch, models.EvalContext{
		Stats:    p1Stats,
		WonMatch: result == models.ResultPlayer1Win,
	})
	p2Earned, _ := h.EvaluateAndGrantBadges(ctx, match.Player2, models.TriggerMatch, models.EvalContext{
		Stats:    p2Stats,
		WonMatch: result == models.ResultPlayer2Win,
	})

	return p1Earned, p2Earned
}

func (h *Handler) broadcastMatchFinished(matchID int64, match models.MatchRecord, p1Score, p2Score int, status models.MatchStatus, result models.MatchResult, p1Badges, p2Badges []models.BadgeDefinition) {
	if h.Notifier == nil {
		return
	}

	var winnerID *int64
	switch result {
	case models.ResultPlayer1Win:
		winnerID = &match.Player1
	case models.ResultPlayer2Win:
		winnerID = &match.Player2
	}

	payload := models.MatchFinishedPayload{
		MatchID:             matchID,
		Player1:             match.Player1,
		Player2:             match.Player2,
		Player1Score:        p1Score,
		Player2Score:        p2Score,
		Status:              status,
		Result:              result,
		WinnerID:            winnerID,
		Player1EarnedBadges: p1Badges,
		Player2EarnedBadges: p2Badges,
	}

	if err := h.Notifier.MatchFinished(payload); err != nil {
		slog.Warn("could not dispatch match finish notification to realtime hub", "match_id", matchID, "error", err)
	}
}

// UserActiveMatchGet handles GET /api/internal/users/{id}/active-match.
// In a single query with a JOIN, it retrieves the in-progress match and the opponent's profile.
// Returns 200 with ActiveMatchResponse if found, or 404 if no match is currently in progress.
func (h *Handler) UserActiveMatchGet(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		slog.Warn("user active match rejected: missing user id in context")
		http.Error(w, "Missing user ID in request context", http.StatusBadRequest)
		return
	}

	if _, err := h.SweepStaleMatches(r.Context(), models.DefaultHeartbeatTimeout); err != nil {
		slog.Warn("failed to sweep stale matches before active match get", "error", err)
	}

	var resp models.ActiveMatchResponse
	err := h.DB.NewSelect().
		TableExpr("matches AS m").
		ColumnExpr("m.id AS match_id").
		ColumnExpr("u.id AS opponent_id").
		ColumnExpr("u.username AS opponent").
		ColumnExpr("m.started_at AS started_at").
		Join("JOIN users AS u ON (m.player_one = ? AND m.player_two = u.id) OR (m.player_two = ? AND m.player_one = u.id)", userID, userID).
		Where("m.status = ?", models.StatusInProgress).
		Where("m.player_one = ? OR m.player_two = ?", userID, userID).
		Order("m.started_at DESC").
		Limit(1).
		Scan(r.Context(), &resp)

	if err != nil {
		HandleDBError(w, err, "Active match")
		return
	}

	slog.Info("active match retrieved for user",
		"user_id", userID,
		"match_id", resp.MatchID,
		"opponent", resp.OpponentUsername,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// MatchHeartbeat handles PUT /api/internal/matches/{id}/heartbeat.
// It is called periodically by the game server to report keepalives for an active match.
// Updates last_heartbeat_at timestamp to prevent the match from being swept as abandoned.
func (h *Handler) MatchHeartbeat(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	matchID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		slog.Warn("match heartbeat rejected: invalid match ID", "id", idStr, "error", err)
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}

	now := time.Now()
	res, err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("id = ?", matchID).
		Where("status = ?", models.StatusInProgress).
		Set("last_heartbeat_at = ?", now).
		Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Updating match heartbeat")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		slog.Warn("match heartbeat update failed: no match in progress found", "match_id", matchID)
		http.Error(w, fmt.Sprintf("No match in progress with ID %d was found", matchID), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SweepStaleMatches finds all matches still marked 'in_progress' whose last heartbeat
// is older than the timeout threshold, marks them 'abandoned'/'aborted', and emits
// real-time notifications to the Hub.
func (h *Handler) SweepStaleMatches(ctx context.Context, timeout time.Duration) ([]models.MatchRecord, error) {
	if timeout <= 0 {
		timeout = models.DefaultHeartbeatTimeout
	}
	now := time.Now()
	cutoff := now.Add(-timeout)

	var swept []models.MatchRecord
	err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("status = ?", models.StatusInProgress).
		Where("last_heartbeat_at < ?", cutoff).
		Set("status = ?", models.StatusAbandoned).
		Set("result = ?", models.ResultAborted).
		Set("finished_at = ?", now).
		Returning("*").
		Scan(ctx, &swept)
	if err != nil {
		return nil, err
	}

	if len(swept) > 0 {
		slog.Info("swept stale matches", "count", len(swept), "cutoff", cutoff)
		if h.Notifier != nil {
			for _, match := range swept {
				payload := models.MatchFinishedPayload{
					MatchID:      match.ID,
					Player1:      match.Player1,
					Player2:      match.Player2,
					Player1Score: 0,
					Player2Score: 0,
					Status:       match.Status,
					Result:       *match.Result,
					WinnerID:     nil,
				}
				if err := h.Notifier.MatchFinished(payload); err != nil {
					slog.Warn("could not dispatch match finish notification on sweep", "match_id", match.ID, "error", err)
				}
			}
		}
	}

	return swept, nil
}

// StartBackgroundSweeper starts a background goroutine that periodically sweeps
// abandoned matches whose last heartbeat is older than timeout.
func (h *Handler) StartBackgroundSweeper(ctx context.Context, interval, timeout time.Duration) {
	if interval <= 0 {
		interval = models.DefaultSweepInterval
	}
	if timeout <= 0 {
		timeout = models.DefaultHeartbeatTimeout
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := h.SweepStaleMatches(ctx, timeout); err != nil {
					slog.Warn("background match sweep failed", "error", err)
				}
			}
		}
	}()
}

// MatchHistoryGet handles GET /api/protected/matches.
// It retrieves the match history for the target user (defaults to authenticated caller),
// mapping opponent info, user-relative scores, and outcome (win, loss, aborted).
// Supports optional query parameters:
//   - username: target user whose match history to view (defaults to authenticated caller)
//   - status: filter by match status (e.g., 'finished', 'in_progress', 'abandoned')
//   - limit: maximum number of records to return (default 50, max 100)
//   - offset: number of records to skip (default 0)
func (h *Handler) MatchHistoryGet(w http.ResponseWriter, r *http.Request) {
	targetUserID, err := h.ResolveTargetUserID(r)
	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User '%s'", r.URL.Query().Get("username")))
		return
	}

	matches := make([]models.MatchHistoryResponse, 0)

	//build the query: get all matches with target user as one party, and fill in opponent details
	q := h.DB.NewSelect().
		TableExpr("matches AS m").
		ColumnExpr("m.id AS id").
		ColumnExpr("u.id AS opponent_id").
		ColumnExpr("u.username AS opponent").
		ColumnExpr("u.avatar_url AS opponent_avatar_url").
		ColumnExpr("CASE WHEN m.player_one = ? THEN m.player_one_score ELSE m.player_two_score END AS user_score", targetUserID).
		ColumnExpr("CASE WHEN m.player_one = ? THEN m.player_two_score ELSE m.player_one_score END AS opponent_score", targetUserID).
		ColumnExpr("m.status AS status").
		ColumnExpr("m.result AS result").
		ColumnExpr(`CASE
			WHEN m.result IS NULL THEN NULL
			WHEN m.result = 'aborted' THEN 'aborted'
			WHEN (m.player_one = ? AND m.result = 'player1_win') OR (m.player_two = ? AND m.result = 'player2_win') THEN 'win'
			ELSE 'loss'
		END AS outcome`, targetUserID, targetUserID). //this CASE summarizes the outcome of the match
		ColumnExpr("m.started_at AS started_at").
		ColumnExpr("m.finished_at AS finished_at").
		Join("JOIN users AS u ON (m.player_one = ? AND m.player_two = u.id) OR (m.player_two = ? AND m.player_one = u.id)", targetUserID, targetUserID).
		Where("m.player_one = ? OR m.player_two = ?", targetUserID, targetUserID).
		Order("m.started_at DESC", "m.id DESC")

	//add status filter if one was provided
	if status := r.URL.Query().Get("status"); status != "" {
		q = q.Where("m.status = ?", status)
	}

	page := ParsePagination(r, 50, 100)
	q = q.Limit(page.Limit).Offset(page.Offset)

	err = q.Scan(r.Context(), &matches) //execute query
	if err != nil {
		HandleDBError(w, err, "Match history")
		return
	}

	callerID, _ := UserIDFromContext(r.Context())
	slog.Debug("match history retrieved", "caller_id", callerID, "target_user_id", targetUserID, "count", len(matches))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(matches)
}
