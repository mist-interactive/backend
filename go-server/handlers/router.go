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

	//requires session token
	rsaKey, err := GetPrivateKey()
	if err != nil {
		log.Fatalf("%v", err)
	}
	authGuard := AuthRequired(db)
	tokenHandler := &TokenHandler{PrivateKey: rsaKey}
	mux.Handle("POST /api/renew", authGuard(http.HandlerFunc(tokenHandler.IssueToken)))

	// all /protected/* routes require a valid JWT
	pubKey, err := GetPublicKey()
	if err != nil {
		log.Fatalf("Failed to load JWT public key: %v", err)
	}
	jwtGuard := JWTGuard(pubKey)
	protectedMux := http.NewServeMux()
	mux.Handle("/protected/", jwtGuard(protectedMux)) //all calls to /protected/ pass through jwtGuard
	profileHandler := &ProfileHandler{DB: db}
	protectedMux.HandleFunc("GET /protected/matches", profileHandler.ProfileGet)
	//TODO: implement profile handlers

	//TODO: create APIGuard middleware
	matchHandler := &MatchHandler{DB: db}
	mux.HandleFunc("POST /internal/matches", matchHandler.MatchCreate)
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

func GetPublicKey() (*rsa.PublicKey, error) {
	pubPath := os.Getenv("JWT_PUBLIC_KEY_PATH")
	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open public key verify file: %v", err)
	}
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(pubBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to cryptographically compile RSA public key: %v", err)
	}
	return publicKey, nil
}
