package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbBackend/handlers"
)

func TestLogin(t *testing.T) {
	testUser := makeTestUser(t)
	setCleanup(t, testUser)
	registerUser(t, testUser)
	handler := &handlers.AuthHandler{DB: testDB}

	tests := []struct {
		name           string
		requestBody    handlers.LoginRequest
		expectedStatus int
		expectJSON     bool
	}{
		{
			name: "Success - Correct credentials",
			requestBody: handlers.LoginRequest{
				Username: testUser.Username,
				Password: "password123",
			},
			expectedStatus: http.StatusOK,
			expectJSON:     true,
		},
		{
			name: "Failure - Correct user but incorrect password",
			requestBody: handlers.LoginRequest{
				Username: testUser.Username,
				Password: "wrongpassword",
			},
			expectedStatus: http.StatusForbidden,
			expectJSON:     false,
		},
		{
			name: "Failure - Nonexistent user",
			requestBody: handlers.LoginRequest{
				Username: "completely_different_" + testUser.Username,
				Password: "somepassword",
			},
			expectedStatus: http.StatusNotFound,
			expectJSON:     false,
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
