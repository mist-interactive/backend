package handlers

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"dbBackend/models"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userContextKey contextKey = "user"
const userIDKey contextKey = "user_id"

// Middleware for confirming a session token exists in DB. TODO: check expiration
func (h *Handler) SessionGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		session := new(models.Session)
		err = h.DB.NewSelect().
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

func UserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok
}

// Middleware for authenticating a JWT token
func (h *Handler) JWTGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			http.Error(w, "Unauthorized: Missing Bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := ValidateToken(tokenStr, h.PublicKey)
		if err != nil {
			http.Error(w, "Unauthorized: Invalid or expired token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID) //add the user ID to context
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Helper to extract Bearer token from Authorization header
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

// Helper to extract UserID from request context in handlers
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

func (h *Handler) APIGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := ExtractAPIKey(r)
		if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(h.APIKey)) != 1 {
			log.Printf("API key '%s' failed comparison to '%s'\n", key, h.APIKey)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ExtractAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return strings.TrimSpace(key)
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

// For internal paths, inject the ID into the context, so handlers can be reused
func InjectPathIDContext(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		userID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid user ID in path", http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

// ValidateToken parses and verifies an RSA-signed JWT string, returning the parsed claims
func ValidateToken(tokenStr string, publicKey *rsa.PublicKey) (*JWTClaims, error) {
	var claims JWTClaims
	//third argument is a function that returns the key to be used
	//if the token has the correct signing method, returns the (public key, nil), else returns (nil, error)
	//that key is then used by ParseWithClaims to validate the payload
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is invalid or expired")
	}

	return &claims, nil
}
