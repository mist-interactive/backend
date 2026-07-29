package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestToken_Integration(t *testing.T) {
	ctx := context.Background()

	testUser, userCleanup := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, testUser, testDB)
	t.Cleanup(userCleanup)

	sessionToken := "test_session_token_12345"
	dbSession := &models.Session{
		SessionToken: sessionToken,
		UserID:       testUser.ID,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	_, err := testDB.NewInsert().Model(dbSession).Exec(ctx)
	if err != nil {
		t.Fatalf("failed to add session to db: %v", err)
	}
	privateKey, err := handlers.GetPrivateKey()
	if err != nil {
		t.Fatalf("failed to read private key: %v", err)
	}

	tokenHandler := &handlers.TokenHandler{PrivateKey: privateKey}
	req := httptest.NewRequest(http.MethodPost, "/api/renew", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: sessionToken,
	})
	rec := httptest.NewRecorder()
	authGuard := handlers.AuthRequired(testDB)
	protectedPipeline := authGuard(http.HandlerFunc(tokenHandler.IssueToken))
	protectedPipeline.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var respBody map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("failed to decode response payload JSON: %v", err)
	}

	tokenJWT := respBody["token"]
	if tokenJWT == "" {
		t.Fatal("response JSON body missing 'ticket' key string")
	}

	publicKey, err := getPublicKey()
	if err != nil {
		t.Fatalf("Failed to read public key: %v", err)
	}
	var parsedClaims handlers.JWTClaims
	token, err := jwt.ParseWithClaims(tokenJWT, &parsedClaims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			t.Errorf("unexpected signing method detected in header: %v", token.Header["alg"])
		}
		return publicKey, nil
	})

	if err != nil {
		t.Fatalf("JWT verification failed, signature or claims are mathematically invalid: %v", err)
	}
	if !token.Valid {
		t.Fatal("JWT structure reported as invalid by the parsing framework")
	}
	if parsedClaims.UserID != testUser.ID {
		t.Errorf("claims data mismatch: expected user ID %d, got %d", testUser.ID, parsedClaims.UserID)
	}
	if parsedClaims.Username != testUser.Username {
		t.Errorf("claims data mismatch: expected username %s, got %s", testUser.Username, parsedClaims.Username)
	}
	if parsedClaims.Issuer != "dbBackend" {
		t.Errorf("claims security flaw: expected issuer 'dbBackend', got %s", parsedClaims.Issuer)
	}
	if val, err := strconv.ParseInt(parsedClaims.Subject, 10, 64); err != nil || val != testUser.ID {
		t.Errorf("claims security flaw: expected subject id '%d', got '%s'", testUser.ID, parsedClaims.Subject)
	}
}

func getPublicKey() (*rsa.PublicKey, error) {
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
