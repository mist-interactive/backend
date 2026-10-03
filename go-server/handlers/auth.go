package handlers

import (
	"crypto/rand"
	"dbBackend/models"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const SessionCookieName string = "session_id"

// to be able to be registered as a handler in the server, the function prototype has to be exactly (http.ResponseWriter, *http.Request)
// so any further input parameters have to be in the receiver, which is why it's a struct
func (h *Handler) CheckPassword(w http.ResponseWriter, r *http.Request) {
	request, err := DecodeAndValidate[models.LoginRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	identifier := request.Username
	var user models.User
	query := h.DB.NewSelect().Model(&user)

	if strings.Contains(identifier, "@") {
		query = query.Where("LOWER(email) = LOWER(?)", identifier)
	} else {
		query = query.Where("LOWER(username) = LOWER(?)", identifier)
	}
	err = query.Scan(r.Context())
	if err != nil {
		slog.Warn("login failed: user not found", "identifier", identifier)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	err = bcrypt.CompareHashAndPassword([]byte(user.PWHash), []byte(request.Password))
	if err != nil {
		slog.Warn("login failed: incorrect password", "identifier", identifier, "user_id", user.ID)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	slog.Info("login successful", "username", user.Username, "user_id", user.ID)

	// Invalidate any existing sessions for this user so only one session is active
	_, err = h.DB.NewDelete().
		Model((*models.Session)(nil)).
		Where("user_id = ?", user.ID).
		Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Session invalidation")
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
		HandleDBError(w, err, "Session creation")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
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

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,              //these two ensure the cookie is no longer valid
		Expires:  time.Unix(1, 0), //Jan 1, 1970
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}
