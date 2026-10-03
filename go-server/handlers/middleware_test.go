package handlers_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAPIGuard(t *testing.T) {
	const validKey = "test-internal-secret-api-key"
	h := handlers.NewHandler(nil, nil, nil, validKey, nil)
	pipeline := h.APIGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name      string
		headerKey string
		headerVal string
		wantCode  int
	}{
		{"Valid X-API-Key", "X-API-Key", validKey, http.StatusOK},
		{"Valid Bearer key", "Authorization", "Bearer " + validKey, http.StatusOK},
		{"Valid X-API-Key trimmed", "X-API-Key", "  " + validKey + "  ", http.StatusOK},
		{"Valid Bearer trimmed", "Authorization", "Bearer   " + validKey + "  ", http.StatusOK},
		{"Missing headers", "", "", http.StatusUnauthorized},
		{"Empty X-API-Key", "X-API-Key", "", http.StatusUnauthorized},
		{"Wrong X-API-Key", "X-API-Key", "wrong", http.StatusUnauthorized},
		{"Wrong Bearer key", "Authorization", "Bearer wrong", http.StatusUnauthorized},
		{"Non-Bearer prefix", "Authorization", "Basic " + validKey, http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal", nil)
			if tc.headerKey != "" {
				req.Header.Set(tc.headerKey, tc.headerVal)
			}
			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantCode)
			}
		})
	}
}

func TestInjectPathIDContext(t *testing.T) {
	tests := []struct {
		name     string
		pathID   string
		wantCode int
		wantID   int64
	}{
		{"Valid int64", "42", http.StatusOK, 42},
		{"Large int64", "9223372036854775806", http.StatusOK, 9223372036854775806},
		{"Non-numeric", "abc", http.StatusBadRequest, 0},
		{"Empty string", "", http.StatusBadRequest, 0},
		{"Float string", "42.5", http.StatusBadRequest, 0},
		{"Overflow", "9999999999999999999999999", http.StatusBadRequest, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotID int64
			handler := handlers.InjectPathIDContext(func(w http.ResponseWriter, r *http.Request) {
				gotID, _ = handlers.UserIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/users/"+tc.pathID, nil)
			req.SetPathValue("id", tc.pathID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusOK && gotID != tc.wantID {
				t.Errorf("user ID: got %d, want %d", gotID, tc.wantID)
			}
		})
	}
}

func TestJWTGuard(t *testing.T) {
	privKey, pubKey := getTestKeys(t)
	otherPrivKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	user := testutil.MakeNTestUsers(t, testDB, 1)[0]
	h := handlers.NewHandler(nil, privKey, pubKey, "", nil)

	validAuth := makeAuthHeader(t, user, privKey)
	foreignAuth := makeAuthHeader(t, user, otherPrivKey)

	expClaims := handlers.JWTClaims{UserID: user.ID, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
	expTok, _ := jwt.NewWithClaims(jwt.SigningMethodRS256, expClaims).SignedString(privKey)
	hmacTok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, expClaims).SignedString([]byte("secret"))

	tests := []struct {
		name     string
		auth     string
		wantCode int
		wantID   int64
	}{
		{"Valid RS256 token", validAuth, http.StatusOK, user.ID},
		{"Missing auth header", "", http.StatusUnauthorized, 0},
		{"Missing Bearer prefix", strings.TrimPrefix(validAuth, "Bearer "), http.StatusUnauthorized, 0},
		{"Empty Bearer token", "Bearer ", http.StatusUnauthorized, 0},
		{"Malformed token", "Bearer not.a.jwt", http.StatusUnauthorized, 0},
		{"Expired token", "Bearer " + expTok, http.StatusUnauthorized, 0},
		{"Wrong private key", foreignAuth, http.StatusUnauthorized, 0},
		{"Wrong algorithm HS256", "Bearer " + hmacTok, http.StatusUnauthorized, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotID int64
			pipeline := h.JWTGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotID, _ = handlers.UserIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusOK && gotID != tc.wantID {
				t.Errorf("user ID: got %d, want %d", gotID, tc.wantID)
			}
		})
	}
}

func TestSessionGuard(t *testing.T) {
	ctx := context.Background()
	user := testutil.MakeNTestUsers(t, testDB, 1)[0]
	h := handlers.NewHandler(testDB, nil, nil, "", nil)

	token := "session_guard_token_" + user.Username
	session := &models.Session{UserID: user.ID, SessionToken: token, ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := testDB.NewInsert().Model(session).Exec(ctx); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	tests := []struct {
		name       string
		cookieName string
		cookieVal  string
		wantCode   int
		wantUser   bool
	}{
		{"Valid session", handlers.SessionCookieName, token, http.StatusOK, true},
		{"Missing cookie", "", "", http.StatusUnauthorized, false},
		{"Wrong cookie name", "other_cookie", token, http.StatusUnauthorized, false},
		{"Unknown session token", handlers.SessionCookieName, "unknown_tok", http.StatusUnauthorized, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotUser *models.User
			pipeline := h.SessionGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUser, _ = handlers.UserFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodPost, "/renew", nil)
			if tc.cookieName != "" {
				req.AddCookie(&http.Cookie{Name: tc.cookieName, Value: tc.cookieVal})
			}
			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantUser && (gotUser == nil || gotUser.ID != user.ID) {
				t.Errorf("got user %+v, want ID %d", gotUser, user.ID)
			}
		})
	}
}

func TestRequestLogger(t *testing.T) {
	privKey, pubKey := getTestKeys(t)
	user := testutil.MakeNTestUsers(t, testDB, 1)[0]
	h := handlers.NewHandler(testDB, privKey, pubKey, "", nil)

	tests := []struct {
		name   string
		method string
		path   string
		code   int
		auth   string
	}{
		{"200 OK", http.MethodGet, "/api/users", http.StatusOK, ""},
		{"Healthcheck trace", http.MethodGet, "/api/health", http.StatusOK, ""},
		{"404 Warning", http.MethodGet, "/api/missing", http.StatusNotFound, ""},
		{"500 Error", http.MethodPost, "/api/error", http.StatusInternalServerError, ""},
		{"Query string log", http.MethodGet, "/api/search?q=test", http.StatusOK, ""},
		{"Authenticated user log", http.MethodGet, "/api/profile", http.StatusOK, makeAuthHeader(t, user, privKey)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			})
			var pipeline http.Handler = handlers.RequestLogger(inner)
			if tc.auth != "" {
				pipeline = handlers.RequestLogger(h.JWTGuard(inner))
			}

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)

			if rec.Code != tc.code {
				t.Errorf("status: got %d, want %d", rec.Code, tc.code)
			}
		})
	}

	t.Run("Recorder Flush Hijack Unwrap", func(t *testing.T) {
		mock := &mockFlusherHijacker{ResponseRecorder: httptest.NewRecorder()}
		var flushed, hijacked, unwrapped bool

		handlers.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
				flushed = true
			}
			if hj, ok := w.(http.Hijacker); ok {
				_, _, _ = hj.Hijack()
				hijacked = true
			}
			if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
				unwrapped = u.Unwrap() == mock
			}
		})).ServeHTTP(mock, httptest.NewRequest(http.MethodGet, "/ws", nil))

		if !flushed || !mock.flushed || !hijacked || !mock.hijacked || !unwrapped {
			t.Errorf("recorder wrapper failed: flushed=%v, hijacked=%v, unwrapped=%v", flushed, hijacked, unwrapped)
		}

		// Non-hijacker returns error
		var nonHijackErr error
		handlers.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hj, ok := w.(http.Hijacker); ok {
				_, _, nonHijackErr = hj.Hijack()
			}
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if nonHijackErr == nil {
			t.Errorf("expected hijack error on non-hijacker recorder")
		}
	})
}

type mockFlusherHijacker struct {
	*httptest.ResponseRecorder
	flushed  bool
	hijacked bool
}

func (m *mockFlusherHijacker) Flush() { m.flushed = true }
func (m *mockFlusherHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, nil
}

func TestContextHelpers(t *testing.T) {
	t.Run("ContextWithUserID", func(t *testing.T) {
		if id, ok := handlers.UserIDFromContext(context.Background()); ok || id != 0 {
			t.Errorf("expected absent id=0, got %d", id)
		}
		if id, ok := handlers.UserIDFromContext(handlers.ContextWithUserID(context.Background(), 42)); !ok || id != 42 {
			t.Errorf("expected id=42, got %d", id)
		}
	})

	t.Run("UserFromContext", func(t *testing.T) {
		if u, ok := handlers.UserFromContext(context.Background()); ok || u != nil {
			t.Errorf("expected nil user, got %v", u)
		}
	})

	t.Run("ExtractAPIKey", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-API-Key", "  x-key  ")
		req.Header.Set("Authorization", "Bearer auth-key")
		if key := handlers.ExtractAPIKey(req); key != "x-key" {
			t.Errorf("expected X-API-Key priority, got %q", key)
		}

		reqBearer := httptest.NewRequest(http.MethodGet, "/", nil)
		reqBearer.Header.Set("Authorization", "Bearer   bearer-key  ")
		if key := handlers.ExtractAPIKey(reqBearer); key != "bearer-key" {
			t.Errorf("expected Bearer fallback, got %q", key)
		}
	})
}
