package handlers

import (
	"context"
	"database/sql"
	"dbBackend/models"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/uptrace/bun"
)

type MatchHistoryHandler struct {
	DB *bun.DB
}

type MatchInput struct {
	Player1   int64     `json:"player_one" validate:"required"`
	Player2   int64     `json:"player_two" validate:"required"`
	Result    string    `json:"result" validate:"required,oneof=player1_win player2_win draw aborted"`
	StartedAt time.Time `json:"started_at" validate:"required"`
}

func (h *MatchHistoryHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(userContextKey).(*models.User)
	if !ok {
		http.Error(w, "User context missing", http.StatusInternalServerError)
		return
	}

	matches := make([]models.MatchRecord, 0)
	err := h.DB.NewSelect().
		Model(&matches).
		Where("player_one = ?", user.ID).
		WhereOr("player_two = ?", user.ID).
		Order("started_at DESC").
		Scan(r.Context())
	if err != nil {
		http.Error(w, "Failed to retrieve match history data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(matches)
}

func (h *MatchHistoryHandler) PostHistory(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[MatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	match := &models.MatchRecord{
		Player1:   input.Player1,
		Player2:   input.Player2,
		Result:    input.Result,
		StartedAt: input.StartedAt,
	}
	if match.Result != "aborted" {
		now := time.Now()
		match.FinishedAt = &now
	}

	tx, err := h.DB.BeginTx(r.Context(), &sql.TxOptions{})
	if err != nil {
		http.Error(w, "Failed to start stats transaction in database", http.StatusInternalServerError)
	}
	defer tx.Rollback()
	_, err = tx.NewInsert().Model(match).Exec(r.Context())
	if err != nil {
		if HandleDBConflict(w, err) {
			return
		}
		http.Error(w, "Database error: Failed to record match history", http.StatusInternalServerError)
		return
	}
	err = updatePlayerProfiles(r.Context(), tx, match)
	if err != nil {
		log.Printf("Error updating player stats for match %d: %v", match.ID, err)
		http.Error(w, "Database error: Failed to record match history", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to save match transaction to database", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(match)
}

func updatePlayerProfiles(ctx context.Context, tx bun.Tx, mr *models.MatchRecord) error {
	switch mr.Result {
	case "player1_win":
		_, err := tx.NewUpdate().
			Model((*models.User)(nil)).
			Set("total_wins = total_wins + 1").
			Where("id = ?", mr.Player1).
			Exec(ctx)
		if err != nil {
			return err
		}
		_, err = tx.NewUpdate().
			Model((*models.User)(nil)).
			Set("total_losses = total_losses + 1").
			Where("id = ?", mr.Player2).
			Exec(ctx)
		if err != nil {
			return err
		}
	case "player2_win":
		_, err := tx.NewUpdate().
			Model((*models.User)(nil)).
			Set("total_wins = total_wins + 1").
			Where("id = ?", mr.Player2).
			Exec(ctx)
		if err != nil {
			return err
		}
		_, err = tx.NewUpdate().
			Model((*models.User)(nil)).
			Set("total_losses = total_losses + 1").
			Where("id = ?", mr.Player1).
			Exec(ctx)
		if err != nil {
			return err
		}
	case "draw", "aborted":
		log.Printf("Warning: Draws and aborted matches cause no stat updates: %s", mr.Result)
	default:
		log.Printf("Warning: Unhandled match result condition encountered: %s", mr.Result)
	}
	return nil
}
