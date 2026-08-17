package handlers

import (
	"encoding/json"
	"log"
	"net/http"

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
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}
