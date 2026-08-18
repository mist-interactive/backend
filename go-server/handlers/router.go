package handlers

import (
	"crypto/rsa"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uptrace/bun"
)

type Group struct {
	mux        *http.ServeMux
	prefix     string
	middleware func(http.Handler) http.Handler
}

func NewGroup(mux *http.ServeMux, prefix string, middleware func(http.Handler) http.Handler) *Group {
	return &Group{
		mux:        mux,
		prefix:     strings.TrimSuffix(prefix, "/"),
		middleware: middleware,
	}
}

// helper to wrap functions in middleware
func (g *Group) HandleFunc(pattern string, handler http.HandlerFunc) {
	parts := strings.SplitN(pattern, " ", 2)
	var fullPattern string

	if len(parts) == 2 {
		method := parts[0]
		path := "/" + strings.TrimPrefix(parts[1], "/")
		fullPattern = method + " " + g.prefix + path
	} else {
		path := "/" + strings.TrimPrefix(parts[0], "/")
		fullPattern = g.prefix + path
	}
	g.mux.Handle(fullPattern, g.middleware(handler))
}

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

	// all /api/protected/* routes require a valid JWT
	pubKey, err := GetPublicKey()
	if err != nil {
		log.Fatalf("Failed to load JWT public key: %v", err)
	}
	jwtGuard := JWTGuard(pubKey)
	protected := NewGroup(mux, "/api/protected", jwtGuard)
	profileHandler := &ProfileHandler{DB: db}
	protected.HandleFunc("GET /profile", profileHandler.ProfileGet)
	protected.HandleFunc("PATCH /profile", profileHandler.ProfilePatch)

	//TODO: implement profile handlers

	//TODO: create APIGuard middleware
	matchHandler := &MatchHandler{DB: db}
	mux.HandleFunc("POST /api/internal/matches", matchHandler.MatchCreate)
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
