package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/uptrace/bun"
)

type MatchHandler struct {
	DB *bun.DB
}

type MatchCreateInput struct {
	Player1 int64 `json:"player_one" validate:"required"`
	Player2 int64 `json:"player_two" validate:"required"`
}

type MatchPatchInput struct {
	Result string `json:"result" validate:"required,oneof=player1_win player2_win draw aborted"`
	Status string `json:"status" validate:"required,oneof=finished abandoned"`
}

func (h *MatchHandler) MatchesCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[MatchCreateInput](r)
	if err != nil {
		http.Error(w, "Problem validating request", http.StatusBadRequest)
		return
	}
	if input.Player1 == input.Player2 {
		http.Error(w, "Players cannot play themselves", http.StatusConflict)
		return
	}

	match := &models.MatchRecord{
		Player1: input.Player1,
		Player2: input.Player2,
		Status:  models.StatusInProgress,
	}
	err = h.DB.NewInsert().
		Model(match).
		Scan(r.Context()) //Scan updates the match struct populating all fields, including the ID
	if err != nil {
		http.Error(w, "Failed to create match entry in database"+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": match.ID,
	})
}

func (h *MatchHandler) MatchesPatch(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	matchID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid match ID", http.StatusBadRequest)
		return
	}
	input, err := DecodeAndValidate[MatchPatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	res, err := h.DB.NewUpdate().
		Model((*models.MatchRecord)(nil)).
		Where("id = ?", matchID).
		Where("status = ?", models.StatusInProgress).
		Set("status = ?", input.Status).
		Set("result = ?", input.Result).
		Set("finished_at = ?", now).
		Exec(r.Context())

	if err != nil {
		http.Error(w, "Database error: Failed to update match history", http.StatusInternalServerError)
		return
	}
	rows, err := res.RowsAffected()
	if err != nil {
		http.Error(w, "Database error: Failed to update match history", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		msg := fmt.Sprintf("No match in progress with ID %d was found", matchID)
		http.Error(w, msg, http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
