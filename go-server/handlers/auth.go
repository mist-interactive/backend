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

// CheckPassword godoc
// @Summary      User Login
// @Description  Authenticate user credentials via username and password. On success, sets a secure HttpOnly session cookie.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        login  body      handlers.LoginRequest  true  "Login Credentials"
// @Success      200    {object}  map[string]string      "{"message": "Login successful"}"
// @Failure      400    {string}  string                 "Invalid JSON payload or validation failure"
// @Failure      403    {string}  string                 "Forbidden - Incorrect password"
// @Failure      404    {string}  string                 "User not found"
// @Failure      500    {string}  string                 "Internal Server Error - Session generation failed"
// @Router       /api/login [post]
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
	w.Write([]byte(`{"message": "Login successful"}`))
}
