package handlers_test

import (
	"bytes"
	"dbBackend/handlers"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTryRegister_Integration(t *testing.T) {
	h := &handlers.RegisterHandler{DB: testDB}
	tests := []struct {
		name           string
		setup          func(t *testing.T) handlers.RegisterRequest
		expectedStatus int
	}{
		{
			name: "Success: new user was registered ",
			setup: func(t *testing.T) handlers.RegisterRequest {
				u := makeTestUser(t)
				setCleanup(t, u)

				return handlers.RegisterRequest{
					Username: u.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Failure: try to register a copy of a user",
			setup: func(t *testing.T) handlers.RegisterRequest {
				u := makeTestUser(t)
				registerUser(t, u)
				setCleanup(t, u)

				return handlers.RegisterRequest{
					Username: u.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Failure: try to register a user with an email already in use",
			setup: func(t *testing.T) handlers.RegisterRequest {
				u := makeTestUser(t)
				registerUser(t, u)
				setCleanup(t, u)
				newU := makeTestUser(t)

				return handlers.RegisterRequest{
					Username: newU.Username,
					Email:    u.Email,
					Password: "password123",
				}
			},
			expectedStatus: http.StatusConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userRequest := tc.setup(t)
			jsonBytes, _ := json.Marshal(userRequest)
			req := httptest.NewRequest("POST", "/api/register", bytes.NewBuffer(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.TryRegister(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. %s", tc.name, tc.expectedStatus, rec.Code, rec.Body)
			}
		})
	}
}
