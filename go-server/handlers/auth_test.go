package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
)

func TestLogin(t *testing.T) {
	t.Helper()
	testUser, cleanup := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup)
	testutil.RegisterUser(t, testUser, testDB)
	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		requestBody    models.LoginRequest
		expectedStatus int
		expectJSON     bool
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "Success - Correct credentials",
			requestBody: models.LoginRequest{
				Username: testUser.Username,
				Password: "password123",
			},
			expectedStatus: http.StatusOK,
			expectJSON:     true,
			validate:       validateSuccessfulLogin(testUser.ID),
		},
		{
			name: "Success - Correct credentials with email inferred",
			requestBody: models.LoginRequest{
				Username: testUser.Email,
				Password: "password123",
			},
			expectedStatus: http.StatusOK,
			expectJSON:     true,
			validate:       validateSuccessfulLogin(testUser.ID),
		},
		{
			name: "Failure - Correct user but incorrect password",
			requestBody: models.LoginRequest{
				Username: testUser.Username,
				Password: "wrongpassword",
			},
			expectedStatus: http.StatusForbidden,
			expectJSON:     false,
			validate:       nil,
		},
		{
			name: "Failure - Nonexistent user",
			requestBody: models.LoginRequest{
				Username: "completely_different_" + testUser.Username,
				Password: "somepassword",
			},
			expectedStatus: http.StatusNotFound,
			expectJSON:     false,
			validate:       nil,
		},
		{
			name: "Failure - Nonexistent email",
			requestBody: models.LoginRequest{
				Username: "nonexistent@testing.internal",
				Password: "password123",
			},
			expectedStatus: http.StatusNotFound,
			expectJSON:     false,
			validate:       nil,
		},
		{
			name: "Failure - Missing identifier",
			requestBody: models.LoginRequest{
				Password: "password123",
			},
			expectedStatus: http.StatusBadRequest,
			expectJSON:     false,
			validate:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/api/login", bytes.NewBuffer(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.CheckPassword(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.expectJSON {
				contentType := rec.Result().Header.Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("expected content-type application/json, got %s", contentType)
				}
			}
		})

	}
}

func validateSuccessfulLogin(expectedUserID int64) func(t *testing.T, rec *httptest.ResponseRecorder) {
	return func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		ctx := context.Background()

		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "session_id" {
				sessionCookie = c
				break
			}
		}

		if sessionCookie == nil {
			t.Error("expected 'session_id' cookie to be present in response headers")
			return
		}
		if sessionCookie.Value == "" {
			t.Error("expected session cookie token value to be populated")
		}
		if !sessionCookie.HttpOnly {
			t.Error("security breach: expected session cookie to be HttpOnly")
		}
		if !sessionCookie.Secure {
			t.Error("security breach: expected session cookie to have Secure flag")
		}
		if sessionCookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("expected SameSite Strict, got %v", sessionCookie.SameSite)
		}

		dbSession := new(models.Session)
		err := testDB.NewSelect().
			Model(dbSession).
			Where("session_token = ?", sessionCookie.Value).
			Scan(ctx)

		if err != nil {
			t.Errorf("failed to locate registered session in database: %v", err)
			return
		}
		if dbSession.UserID != expectedUserID {
			t.Errorf("session database row mismatched: expected user ID %d, got %d", expectedUserID, dbSession.UserID)
		}
	}
}

func extractSessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session_id" {
			return c
		}
	}
	return nil
}

func TestLogin_SingleSessionEnforcement(t *testing.T) {
	testUser := testutil.MakeNTestUsers(t, testDB, 1)[0]

	privateKey, err := handlers.GetPrivateKey()
	if err != nil {
		t.Fatalf("failed to read private key: %v", err)
	}
	handler := handlers.NewHandler(testDB, privateKey, nil, "", nil)

	loginReq := models.LoginRequest{
		Username: testUser.Username,
		Password: "password123",
	}
	jsonBytes, err := json.Marshal(loginReq)
	if err != nil {
		t.Fatalf("failed to marshal login request: %v", err)
	}

	t.Run("Subsequent login purges previous session and invalidates renewal", func(t *testing.T) {
		// First login
		req1 := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBuffer(jsonBytes))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		handler.CheckPassword(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first login failed: expected %d, got %d", http.StatusOK, rec1.Code)
		}

		cookie1 := extractSessionCookie(rec1)
		if cookie1 == nil || cookie1.Value == "" {
			t.Fatalf("expected valid session cookie from first login")
		}

		// Verify session 1 works with /api/renew
		renewPipeline := handler.SessionGuard(http.HandlerFunc(handler.IssueToken))
		renewReq1 := httptest.NewRequest(http.MethodPost, "/api/renew", nil)
		renewReq1.AddCookie(cookie1)
		renewRec1 := httptest.NewRecorder()
		renewPipeline.ServeHTTP(renewRec1, renewReq1)
		if renewRec1.Code != http.StatusOK {
			t.Fatalf("renew with session 1 failed: expected %d, got %d", http.StatusOK, renewRec1.Code)
		}

		// Second login for same user
		req2 := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBuffer(jsonBytes))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		handler.CheckPassword(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second login failed: expected %d, got %d", http.StatusOK, rec2.Code)
		}

		cookie2 := extractSessionCookie(rec2)
		if cookie2 == nil || cookie2.Value == "" {
			t.Fatalf("expected valid session cookie from second login")
		}

		// Verify displaced session 1 is rejected on /api/renew
		renewReqOld := httptest.NewRequest(http.MethodPost, "/api/renew", nil)
		renewReqOld.AddCookie(cookie1)
		renewRecOld := httptest.NewRecorder()
		renewPipeline.ServeHTTP(renewRecOld, renewReqOld)
		if renewRecOld.Code != http.StatusUnauthorized {
			t.Errorf("expected displaced session 1 to be unauthorized (401), got %d", renewRecOld.Code)
		}

		// Verify new session 2 succeeds on /api/renew
		renewReqNew := httptest.NewRequest(http.MethodPost, "/api/renew", nil)
		renewReqNew.AddCookie(cookie2)
		renewRecNew := httptest.NewRecorder()
		renewPipeline.ServeHTTP(renewRecNew, renewReqNew)
		if renewRecNew.Code != http.StatusOK {
			t.Errorf("expected active session 2 to succeed (200), got %d", renewRecNew.Code)
		}

		// Verify database contains only 1 active session for user
		ctx := context.Background()
		totalSessions, countErr := testDB.NewSelect().
			Model((*models.Session)(nil)).
			Where("user_id = ?", testUser.ID).
			Count(ctx)
		if countErr != nil {
			t.Fatalf("failed to count sessions in DB: %v", countErr)
		}
		if totalSessions != 1 {
			t.Errorf("expected exactly 1 active session in DB, got %d", totalSessions)
		}
	})
}

func TestClearSessionCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	handlers.ClearSessionCookie(rec)

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == handlers.SessionCookieName {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatalf("expected '%s' cookie to be present in response headers", handlers.SessionCookieName)
	}
	if sessionCookie.Value != "" {
		t.Errorf("cookie value: got %q, want empty string", sessionCookie.Value)
	}
	if sessionCookie.MaxAge >= 0 {
		t.Errorf("cookie maxAge: got %d, want < 0", sessionCookie.MaxAge)
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("cookie HttpOnly: got %v, want true", sessionCookie.HttpOnly)
	}
	if !sessionCookie.Secure {
		t.Errorf("cookie Secure: got %v, want true", sessionCookie.Secure)
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite: got %v, want %v", sessionCookie.SameSite, http.SameSiteStrictMode)
	}
	if sessionCookie.Path != "/" {
		t.Errorf("cookie Path: got %q, want '/'", sessionCookie.Path)
	}
}
