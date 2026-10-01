package realtime

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHttpDataStoreGetActiveMatch(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		handler       http.HandlerFunc
		expectedError bool
		validate      func(t *testing.T, resp *models.ActiveMatchResponse)
	}{
		{
			name:       "Success: Returns active match when ongoing match exists (200 OK)",
			statusCode: http.StatusOK,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "test-api-key" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.URL.Path != "/api/internal/users/42/active-match" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				resp := models.ActiveMatchResponse{
					MatchID:          100,
					OpponentID:       43,
					OpponentUsername: "charlie",
					StartedAt:        time.Now().Truncate(time.Second),
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
			},
			expectedError: false,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp == nil {
					t.Fatalf("expected non-nil response, got nil")
				}
				if resp.MatchID != 100 {
					t.Errorf("expected match_id 100, got %d", resp.MatchID)
				}
				if resp.OpponentUsername != "charlie" {
					t.Errorf("expected opponent 'charlie', got %s", resp.OpponentUsername)
				}
			},
		},
		{
			name:       "Success: Returns nil when no active match in progress (404 Not Found)",
			statusCode: http.StatusNotFound,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedError: false,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp != nil {
					t.Errorf("expected nil response on 404, got %+v", resp)
				}
			},
		},
		{
			name:       "Failure: Returns error on server error (500 Internal Server Error)",
			statusCode: http.StatusInternalServerError,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedError: true,
			validate: func(t *testing.T, resp *models.ActiveMatchResponse) {
				if resp != nil {
					t.Errorf("expected nil response on error, got %+v", resp)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			t.Cleanup(func() {
				server.Close()
			})

			store := NewHttpDataStore(server.URL, "test-api-key")
			resp, err := store.GetActiveMatch(context.Background(), 42)

			if (err != nil) != tc.expectedError {
				t.Fatalf("unexpected error status: got err=%v, want expectedError=%v", err, tc.expectedError)
			}
			tc.validate(t, resp)
		})
	}
}

func slicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHttpDataStoreGetFriendsList(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		respBody  any
		wantIDs   []int64
		expectErr bool
	}{
		{
			name:   "Success: filters accepted friends",
			status: http.StatusOK,
			respBody: []models.FriendshipItemResponse{
				{UserID: 10, Status: models.StatusAccepted},
				{UserID: 11, Status: models.StatusPending},
				{UserID: 12, Status: models.StatusAccepted},
			},
			wantIDs:   []int64{10, 12},
			expectErr: false,
		},
		{
			name:      "Failure: 500 error",
			status:    http.StatusInternalServerError,
			respBody:  nil,
			wantIDs:   nil,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-API-Key") != "key" || r.URL.Path != "/api/internal/friends/42" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.status)
				if tc.respBody != nil {
					_ = json.NewEncoder(w).Encode(tc.respBody)
				}
			}))
			t.Cleanup(server.Close)

			store := NewHttpDataStore(server.URL, "key")
			got, err := store.GetFriendsList(context.Background(), 42)
			if (err != nil) != tc.expectErr {
				t.Fatalf("unexpected error status: got %v, want expectErr=%v", err, tc.expectErr)
			}
			if !tc.expectErr && !slicesEqual(got, tc.wantIDs) {
				t.Errorf("got %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestHttpDataStoreCreateMatch(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		respBody  any
		wantID    int64
		expectErr bool
	}{
		{
			name:      "Success: 201 Created returns match ID",
			status:    http.StatusCreated,
			respBody:  map[string]int64{"id": 99},
			wantID:    99,
			expectErr: false,
		},
		{
			name:      "Failure: 500 Internal Error",
			status:    http.StatusInternalServerError,
			respBody:  nil,
			wantID:    0,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/internal/matches" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.status)
				if tc.respBody != nil {
					_ = json.NewEncoder(w).Encode(tc.respBody)
				}
			}))
			t.Cleanup(server.Close)

			store := NewHttpDataStore(server.URL, "key")
			got, err := store.CreateMatch(context.Background(), "alice", "bob")
			if (err != nil) != tc.expectErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Errorf("match ID: got %d, want %d", got, tc.wantID)
			}
		})
	}
}

func TestHttpDataStoreSaveMessage(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		respBody  *models.Message
		expectErr bool
	}{
		{
			name:   "Success: 201 Created returns saved message",
			status: http.StatusCreated,
			respBody: &models.Message{
				ID:          55,
				SenderID:    1,
				RecipientID: 2,
				Content:     "hello",
			},
			expectErr: false,
		},
		{
			name:      "Failure: 400 Bad Request",
			status:    http.StatusBadRequest,
			respBody:  nil,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/internal/messages" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(tc.status)
				if tc.respBody != nil {
					_ = json.NewEncoder(w).Encode(tc.respBody)
				}
			}))
			t.Cleanup(server.Close)

			store := NewHttpDataStore(server.URL, "key")
			got, err := store.SaveMessage(context.Background(), 1, "bob", "hello")
			if (err != nil) != tc.expectErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.expectErr && (got == nil || got.ID != 55) {
				t.Errorf("got message %+v, want ID 55", got)
			}
		})
	}
}
