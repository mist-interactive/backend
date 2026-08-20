package handlers

import (
	"net/http"

	"github.com/uptrace/bun"
)

type FHandler struct {
	DB *bun.DB
}

type FPatchInput struct {
	Bio       *string `json:"bio" validate:"omitempty,max=500"`
	AvatarURL *string `json:"avatarUrl" validate:"omitempty,url"`
	Email     *string `json:"email" validate:"omitempty,email,max=255"`
}

type FRequest struct {
	Username  string  `bun:"username" json:"username"`
	Email     string  `bun:"email" json:"email"`
	Bio       string  `bun:"bio" json:"bio"`
	AvatarURL *string `bun:"avatar_url" json:"avatarUrl"`
}

func (h *FHandler) ProfileGet(w http.ResponseWriter, r *http.Request) {}
