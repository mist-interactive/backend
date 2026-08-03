package handlers

import (
	"crypto/rsa"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uptrace/bun"
)

const APIKeyLength = 65

func RegisterRoutes(mux *http.ServeMux, db *bun.DB) {
	//mux handles the unprotected routes, and submuxes for the different protected routes

	authHandler := &AuthHandler{DB: db}
	mux.HandleFunc("POST /api/login", authHandler.CheckPassword)
	registerHandler := &RegisterHandler{DB: db}
	mux.HandleFunc("POST /api/register", registerHandler.TryRegister)

	//protectedMux is the submux of routes that require an active login session, under `/protected/`
	protectedMux := http.NewServeMux()
	authGuard := AuthRequired(db)
	mux.Handle("/protected/", authGuard(protectedMux)) //register all '/protected/*' routes to use this guard

	rsaKey, err := GetPrivateKey()
	if err != nil {
		log.Fatalf("%v", err)
	}
	tokenHandler := &TokenHandler{PrivateKey: rsaKey}
	protectedMux.HandleFunc("POST /protected/renew", tokenHandler.IssueToken)

	//internalMux is the submux of routes that require an internal api key, under `/internal/`
	internalMux := http.NewServeMux()
	apiKey, err := getAPIKey()
	if err != nil {
		log.Fatalf("%v", err)
	}
	apiGuard := APIGuard(apiKey)
	mux.Handle("/internal/", apiGuard(internalMux))

	matchHandler := &MatchHandler{DB: db, APIKey: apiKey}
	internalMux.HandleFunc("POST /internal/matches", matchHandler.MatchCreate)
	internalMux.HandleFunc("PATCH /internal/matches/{id}", matchHandler.MatchPatch)
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

func getAPIKey() (string, error) {
	keyPath := os.Getenv("GAMESERVER_API_KEY_PATH")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("Critical: Failed to read API key file at %s: %v", keyPath, err)
	} else if len(keyBytes) != APIKeyLength {
		return "", fmt.Errorf("Critical: Unexpected length of key : %d, expected %d", len(keyBytes), APIKeyLength)
	}
	return string(keyBytes), nil
}
