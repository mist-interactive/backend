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
