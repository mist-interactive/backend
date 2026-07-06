package handlers_test

import (
	"bytes"
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func strPtr(s string) *string {
	return &s
}

func TestGetProfile_Integration(t *testing.T) {
	ctx := context.Background()

	testUser, userCleanup := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, testUser, testDB)
	t.Cleanup(userCleanup)

	sessionToken := "profile_get_session_token_999"
	dbSession := &models.Session{
		SessionToken: sessionToken,
		UserID:       testUser.ID,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	if _, err := testDB.NewInsert().Model(dbSession).Exec(ctx); err != nil {
		t.Fatalf("failed to add session to db: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	req.AddCookie(&http.Cookie{
		Name:  "session_id",
		Value: sessionToken,
	})
	rec := httptest.NewRecorder()

	profileHandler := &handlers.ProfileHandler{DB: testDB}
	authGuard := handlers.AuthRequired(testDB)
	pipeline := authGuard(http.HandlerFunc(profileHandler.GetProfile))
	pipeline.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp handlers.ProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal ProfileResponse JSON: %v", err)
	}

	if resp.ID != testUser.ID {
		t.Errorf("mismatch ID: expected %d, got %d", testUser.ID, resp.ID)
	}
	if resp.Username != testUser.Username {
		t.Errorf("mismatch Username: expected %s, got %s", testUser.Username, resp.Username)
	}
	if resp.Email != testUser.Email {
		t.Errorf("mismatch Email: expected %s, got %s", testUser.Email, resp.Email)
	}
}

func TestUpdateProfile_Integration(t *testing.T) {
	ctx := context.Background()

	mainUser, mainCleanup := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, mainUser, testDB)
	t.Cleanup(mainCleanup)

	sessionToken := "profile_patch_session_token_777"
	dbSession := &models.Session{
		SessionToken: sessionToken,
		UserID:       mainUser.ID,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	if _, err := testDB.NewInsert().Model(dbSession).Exec(ctx); err != nil {
		t.Fatalf("failed to add session to db: %v", err)
	}

	conflictUser, conflictCleanup := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, conflictUser, testDB)
	t.Cleanup(conflictCleanup)

	unregisteredUser, _ := testutil.MakeTestUser(t, testDB)

	profileHandler := &handlers.ProfileHandler{DB: testDB}
	authGuard := handlers.AuthRequired(testDB)
	pipeline := authGuard(http.HandlerFunc(profileHandler.UpdateProfile))

	tests := []struct {
		name           string
		payload        handlers.UpdateProfileInput
		expectedStatus int
		validate       func(t *testing.T, resBody []byte)
	}{
		{
			name: "Success - Update both username and email cleanly",
			payload: handlers.UpdateProfileInput{
				Username: strPtr(unregisteredUser.Username),
				Email:    strPtr(unregisteredUser.Email),
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, resBody []byte) {
				var resp handlers.ProfileResponse
				json.Unmarshal(resBody, &resp)
				if resp.Username != unregisteredUser.Username || resp.Email != unregisteredUser.Email {
					t.Errorf("expected fields to be modified, got body: %s", string(resBody))
				}
			},
		},
		{
			name: "Failure - Username value does not meet min length constraint",
			payload: handlers.UpdateProfileInput{
				Username: strPtr("ab"),
			},
			expectedStatus: http.StatusBadRequest,
			validate:       nil,
		},
		{
			name: "Failure - Email format fails parsing syntax rules",
			payload: handlers.UpdateProfileInput{
				Email: strPtr("garbage_email_string"),
			},
			expectedStatus: http.StatusBadRequest,
			validate:       nil,
		},
		{
			name: "Failure - Target username conflicts with another account registration",
			payload: handlers.UpdateProfileInput{
				Username: strPtr(conflictUser.Username),
			},
			expectedStatus: http.StatusConflict,
			validate:       nil,
		},
		{
			name: "Failure - Target email conflicts with another account registration",
			payload: handlers.UpdateProfileInput{
				Email: strPtr(conflictUser.Email),
			},
			expectedStatus: http.StatusConflict,
			validate:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPatch, "/api/profile", bytes.NewBuffer(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{
				Name:  "session_id",
				Value: sessionToken,
			})

			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil && rec.Code == http.StatusOK {
				tc.validate(t, rec.Body.Bytes())
			}
		})
	}
}
