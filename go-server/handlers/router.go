package handlers

import (
	"bytes"
	"crypto/rsa"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uptrace/bun"
)

func RegisterRoutes(mux *http.ServeMux, db *bun.DB) {
	authHandler := &AuthHandler{DB: db}
	mux.HandleFunc("POST /api/login", authHandler.CheckPassword)

	registerHandler := &RegisterHandler{DB: db}
	mux.HandleFunc("POST /api/register", registerHandler.TryRegister)

	rsaKey, err := GetPrivateKey()
	if err != nil {
		log.Fatalf("%v", err)
	}
	authGuard := AuthRequired(db)
	tokenHandler := &TokenHandler{PrivateKey: rsaKey}
	mux.Handle("POST /api/renew", authGuard(http.HandlerFunc(tokenHandler.IssueToken)))

	profileHandler := ProfileHandler{DB: db}
	mux.Handle("GET /api/profile", authGuard(http.HandlerFunc(profileHandler.GetProfile)))
	mux.Handle("PATCH /api/profile", authGuard(http.HandlerFunc(profileHandler.UpdateProfile)))

	matchHistoryHandler := MatchHistoryHandler{DB: db}
	mux.Handle("GET /api/match-history", authGuard((http.HandlerFunc(matchHistoryHandler.GetHistory))))

	apiKey, err := GetAPIKey()
	if err != nil {
		log.Fatalf("%v", err)
	}
	gameServerGuard := GameServerAuth(apiKey)
	mux.Handle("POST /api/internal/match", gameServerGuard((http.HandlerFunc(matchHistoryHandler.PostHistory))))
}

func GetPrivateKey() (*rsa.PrivateKey, error) {
	keyPath := os.Getenv("JWT_PRIVATE_KEY_PATH")
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("Critical: Failed to read JWT private key file at %s: %v", keyPath, err)
	}
	signKey, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("Critical: Failed to parse JWT private key layout: %v", err)
	}
	return signKey, nil
}

func GetAPIKey() (string, error) {
	keyPath := os.Getenv("GAMESERVER_API_KEY_PATH")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("Critical: Failed to read API key file at %s: %v", keyPath, err)
	}
	return string(bytes.TrimSpace(keyBytes)), nil
}
