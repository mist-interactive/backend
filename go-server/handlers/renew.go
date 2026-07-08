package handlers

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type TokenHandler struct {
	PrivateKey *rsa.PrivateKey
}

// IssueToken godoc
// @Summary      Issue Short-Lived JWT Token
// @Description  Generates a short-lived RS256 cryptographically signed access JWT using the authenticated cookie session data for validation.
// @Tags         auth
// @Produce      json
// @Security     CookieAuth
// @Success      200  {object}  map[string]string  "{"token": "ey..."}"
// @Failure      500  {string}  string             "Internal server error: Token signing failed or session missing"
// @Router       /api/renew [post]
func (h *TokenHandler) IssueToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized: Context missing user state", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	lifetime := time.Second * 60
	claims := JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dbBackend",
			Subject:   strconv.Itoa(int(user.ID)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(h.PrivateKey)
	if err != nil {
		http.Error(w, "Internal server error: Token signing failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]string{
		"token": signedToken,
	})
	if err != nil {
		http.Error(w, "Internal server error: Token signing failed", http.StatusInternalServerError)
		return
	}
}
