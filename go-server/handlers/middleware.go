package handlers

import (
	"context"
	"crypto/subtle"
	"dbBackend/models"
	"net/http"

	"github.com/uptrace/bun"
)

type contextKey string

const userContextKey contextKey = "user"

func AuthRequired(db *bun.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			session := new(models.Session)
			err = db.NewSelect().
				Model(session).
				Relation("User").
				Where("session_token = ?", cookie.Value).
				Scan(r.Context())

			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, session.User)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok
}

func GameServerAuth(expectedAPIKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedAPIKey)) != 1 {
				http.Error(w, "Unauthorized: Invalid or missing Game Server API Key", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
