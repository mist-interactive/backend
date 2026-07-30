package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"net/http"

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
	Player1 int64  `json:"player_one" validate:"required"`
	Player2 int64  `json:"player_two" validate:"required"`
	Result  string `json:"result" validate:"required,oneof=player1_win player2_win draw aborted"`
	Status  string `json:"status" validate:"required,oneof=finished abandoned"`
}

func (h *MatchHandler) MatchCreate(w http.ResponseWriter, r *http.Request) {
	input, err := DecodeAndValidate[MatchCreateInput](r)
	if input.Player1 == input.Player2 {
		http.Error(w, "Players cannot play themselves", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Problem validating request", http.StatusBadRequest)
		return
	}
	match := &models.MatchRecord{
		Player1: input.Player1,
		Player2: input.Player2,
		Status:  models.StatusInProgress,
	}
	err = h.DB.NewInsert().
		Model(match).
		Scan(r.Context())
	if err != nil {
		http.Error(w, "Failed to create match entry in database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id": match.ID,
	})
}
