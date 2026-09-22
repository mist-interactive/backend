package handlers

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"
)

func (h *Handler) ProfileGet(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	slog.Debug("profile get request", "user_id", userID)

	user, err := h.getUserByID(r.Context(), userID)
	if err != nil {
		HandleDBError(w, err, "User profile get")
		return
	}

	profile, err := h.buildProfile(r.Context(), user, true)
	if err != nil {
		HandleDBError(w, err, "User stats calculate")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) ProfilePatch(w http.ResponseWriter, r *http.Request) {
	//first, check what middleware passed and what input contains
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[models.ProfilePatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isProfilePatchEmpty(&input) {
		http.Error(w, "At least one field must be provided", http.StatusBadRequest)
		return
	}
	//everything was ok, start building the update query, basics first
	now := time.Now()
	query := h.DB.NewUpdate().
		Model((*models.User)(nil)).
		Where("id = ?", userID).
		Set("updated_at = ?", now)
	// for each field, if it's not nil, add it to the update
	if input.Bio != nil {
		query = query.Set("bio = ?", *input.Bio)
	}
	if input.Email != nil {
		query = query.Set("email = ?", *input.Email)
	}
	if input.AvatarURL != nil {
		query = query.Set("avatar_url = ?", *input.AvatarURL)
	}
	//Do the update while scanning data to memory
	profile := new(models.UserProfile)
	err = query.Returning("username, email, bio, avatar_url").Scan(r.Context(), profile)
	if err != nil {
		HandleDBError(w, err, "User profile get")
		return
	}

	stats, err := h.getUserStats(r.Context(), userID)
	if err != nil {
		HandleDBError(w, err, "User stats calculate")
		return
	}
	profile.Stats = stats

	//Return the updated profile data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func isProfilePatchEmpty(p *models.ProfilePatchInput) bool {
	return p.Bio == nil && p.AvatarURL == nil && p.Email == nil
}

func (h *Handler) ProfileGetByUsername(w http.ResponseWriter, r *http.Request) {
	callerID, ok := UserIDFromContext(r.Context())
	if !ok || callerID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userStr := r.PathValue("username")
	if userStr == "" {
		http.Error(w, "No username provided", http.StatusBadRequest)
		return
	}
	slog.Debug("profile get by username request", "username", userStr, "caller_id", callerID)

	targetUser, err := h.getUserByUsername(r.Context(), userStr)
	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User profile get by username '%s'", userStr))
		return
	}

	isSelf := (targetUser.ID == callerID)
	profile, err := h.buildProfile(r.Context(), targetUser, isSelf)
	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User stats for '%s'", userStr))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) buildProfile(ctx context.Context, user *models.User, isSelf bool) (*models.UserProfile, error) {
	profile := &models.UserProfile{
		Username:  user.Username,
		Bio:       user.Bio,
		AvatarURL: user.AvatarURL,
	}
	if isSelf {
		profile.Email = &user.Email
	}

	stats, err := h.getUserStats(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	profile.Stats = stats

	return profile, nil
}

func (h *Handler) getUserStats(ctx context.Context, userID int64) (models.UserStats, error) {
	var stats models.UserStats

	err := h.DB.NewSelect().
		Table("matches").
		ColumnExpr("COUNT(*) FILTER (WHERE status = ?) AS games_played", models.StatusFinished).
		ColumnExpr("COUNT(*) FILTER (WHERE status = ? AND ((player_one = ? AND result = ?) OR (player_two = ? AND result = ?))) AS wins",
			models.StatusFinished, userID, models.ResultPlayer1Win, userID, models.ResultPlayer2Win).
		ColumnExpr("COUNT(*) FILTER (WHERE status = ? AND ((player_one = ? AND result = ?) OR (player_two = ? AND result = ?))) AS losses",
			models.StatusFinished, userID, models.ResultPlayer2Win, userID, models.ResultPlayer1Win).
		Where("player_one = ? OR player_two = ?", userID, userID).
		Scan(ctx, &stats)
	if err != nil {
		return stats, err
	}

	if stats.GamesPlayed > 0 {
		stats.WinRate = math.Round((float64(stats.Wins) / float64(stats.GamesPlayed)) * 100)
	}

	return stats, nil
}

func (h *Handler) ProfileDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	slog.Info("profile deleting user account", "user_id", userID)
	ctx := r.Context()

	// Begin a transaction: a connected set of database actions, that can be undone if any of them goes wrong
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		slog.Error("profile delete begin tx failed", "user_id", userID, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() //register the Rollback to run if we exit befor committing the transaction
	anonymized := fmt.Sprintf("deleted_user_%d", userID)
	now := time.Now()

	// Add things to the transaction
	// 1. anonymize the users table entry
	updateUserQuery := tx.NewUpdate().
		Table("users").
		Where("id = ?", userID).
		Set("username = ?", anonymized).
		Set("email = ?", anonymized+"@internal").
		Set("password_hash = ?", "deleted").
		Set("bio = ''").
		Set("avatar_url = NULL").
		Set("updated_at = ?", now)
	// 2. delete any active sessions
	deleteSessionsQuery := tx.NewDelete().
		Table("sessions").
		Where("user_id = ?", userID)

	// Execute transactions
	if _, err := updateUserQuery.Exec(ctx); err != nil {
		HandleDBError(w, err, fmt.Sprintf("User deletion '%d'", userID))
		return
	}
	if _, err := deleteSessionsQuery.Exec(ctx); err != nil {
		HandleDBError(w, err, fmt.Sprintf("Session deletion for user '%d'", userID))
		return
	}

	//everything went through, so we commit all actions
	if err := tx.Commit(); err != nil {
		HandleDBError(w, err, fmt.Sprintf("Committing profile deletion transaction for user '%d'", userID))
		return
	}

	//Set a non-valid Cookie to replace the old one
	ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
