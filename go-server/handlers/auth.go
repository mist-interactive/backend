package handlers

import (
	"crypto/rand"
	"dbBackend/models"
	"net/http"
	"time"

	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB *bun.DB
}

type LoginRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Password string `json:"password" validate:"required,min=8,max=72"`
}

// to be able to be registered as a handler in the server, the function prototype has to be exactly (http.ResponseWriter, *http.Request)
// so any further input parameters have to be in the receiver, which is why it's a struct
func (h *AuthHandler) CheckPassword(w http.ResponseWriter, r *http.Request) {
	request, err := DecodeAndValidate[LoginRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var user models.User
	err = h.DB.NewSelect().
		Model(&user).
		Where("username = ?", request.Username).
		Scan(r.Context())
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	err = bcrypt.CompareHashAndPassword([]byte(user.PWHash), []byte(request.Password))
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	sessionToken := rand.Text()
	sessionDuration := 24 * time.Hour
	newSession := &models.Session{
		UserID:       user.ID,
		SessionToken: sessionToken,
		ExpiresAt:    time.Now().Add(sessionDuration),
	}

	_, err = h.DB.NewInsert().
		Model(newSession).
		Exec(r.Context())
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    newSession.SessionToken,
		Path:     "/",
		Expires:  newSession.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Login really successful"}`))
}
