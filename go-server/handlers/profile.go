package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/uptrace/bun"
)

type ProfileHandler struct {
	DB *bun.DB
}

type ProfilePatchInput struct {
	Bio       *string `json:"bio" validate:"omitempty,max=500"`
	AvatarURL *string `json:"avatarUrl" validate:"omitempty,url"`
	Email     *string `json:"email" validate:"omitempty,email,max=255"`
}

type UserProfile struct {
	Username  string  `bun:"username" json:"username"`
	Email     string  `bun:"email" json:"email"`
	Bio       string  `bun:"bio" json:"bio"`
	AvatarURL *string `bun:"avatar_url" json:"avatarUrl"`
}

func (h *ProfileHandler) ProfileGet(w http.ResponseWriter, r *http.Request) {
	log.Println("--> ProfileGet hit!")

	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	profile := new(UserProfile)
	err := h.DB.NewSelect().
		Table("users").
		Where("id = ?", claims.UserID).
		Column("username", "email", "bio", "avatar_url").
		Scan(r.Context(), profile)

	if err != nil {
		HandleDBError(w, err, "User profile get")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *ProfileHandler) ProfilePatch(w http.ResponseWriter, r *http.Request) {
	//first, check what middleware passed and what input contains
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[ProfilePatchInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if input.isEmpty() {
		http.Error(w, "At least one field must be provided", http.StatusBadRequest)
		return
	}
	//everything was ok, start building the update query, basics first
	now := time.Now()
	query := h.DB.NewUpdate().
		Model((*models.User)(nil)).
		Where("id = ?", claims.UserID).
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
	profile := new(UserProfile)
	err = query.Returning("username, email, bio, avatar_url").Scan(r.Context(), profile)
	if err != nil {
		HandleDBError(w, err, "User")
		return
	}
	//Return the updated profile data
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (p ProfilePatchInput) isEmpty() bool {
	return p.Bio == nil && p.AvatarURL == nil && p.Email == nil
}

func (h *ProfileHandler) ProfileGetByUsername(w http.ResponseWriter, r *http.Request) {
	userStr := r.PathValue("username")
	log.Printf("Getting profile with username '%s'\n", userStr)
	if userStr == "" {
		http.Error(w, "No username provided", http.StatusBadRequest)
		return
	}
	profile := new(UserProfile)
	err := h.DB.NewSelect().
		Table("users").
		Where("username = ?", userStr).
		Column("username", "email", "bio", "avatar_url").
		Scan(r.Context(), profile)

	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User profile get by username '%s'", userStr))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}
