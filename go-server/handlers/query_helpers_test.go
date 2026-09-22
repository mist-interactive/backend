package handlers_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbBackend/handlers"
	"dbBackend/internal/testutil"
)

// Unit test: TestParseQueryInt verifies integer query parameter extraction,
// default fallbacks, and boundary clamping using in-memory requests without a database.
func TestParseQueryInt(t *testing.T) {
	tests := []struct {
		name       string
		targetURL  string
		key        string
		defaultVal int
		minVal     int
		maxVal     int
		want       int
	}{
		{
			name:       "Missing key returns default",
			targetURL:  "/test",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "Empty value returns default",
			targetURL:  "/test?limit=",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "Valid value within bounds returns parsed value",
			targetURL:  "/test?limit=25",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       25,
		},
		{
			name:       "Non-numeric value returns default",
			targetURL:  "/test?limit=invalid",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "Value below minVal returns default",
			targetURL:  "/test?limit=0",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "Negative value below minVal returns default",
			targetURL:  "/test?limit=-10",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       50,
		},
		{
			name:       "Value above maxVal is clamped to maxVal",
			targetURL:  "/test?limit=500",
			key:        "limit",
			defaultVal: 50,
			minVal:     1,
			maxVal:     100,
			want:       100,
		},
		{
			name:       "Unbounded maxVal (0) allows large values",
			targetURL:  "/test?offset=1000",
			key:        "offset",
			defaultVal: 0,
			minVal:     0,
			maxVal:     0,
			want:       1000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.targetURL, nil)
			got := handlers.ParseQueryInt(r, tc.key, tc.defaultVal, tc.minVal, tc.maxVal)
			if got != tc.want {
				t.Errorf("handlers.ParseQueryInt() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Unit test: TestParsePagination verifies limit and offset extraction,
// default values, and boundary clamping into a Pagination struct without a database.
func TestParsePagination(t *testing.T) {
	tests := []struct {
		name         string
		targetURL    string
		defaultLimit int
		maxLimit     int
		wantLimit    int
		wantOffset   int
	}{
		{
			name:         "Default pagination with empty query",
			targetURL:    "/test",
			defaultLimit: 50,
			maxLimit:     100,
			wantLimit:    50,
			wantOffset:   0,
		},
		{
			name:         "Custom valid limit and offset",
			targetURL:    "/test?limit=20&offset=15",
			defaultLimit: 50,
			maxLimit:     100,
			wantLimit:    20,
			wantOffset:   15,
		},
		{
			name:         "Clamped limit exceeding maxLimit",
			targetURL:    "/test?limit=200&offset=5",
			defaultLimit: 50,
			maxLimit:     100,
			wantLimit:    100,
			wantOffset:   5,
		},
		{
			name:         "Negative offset falls back to 0",
			targetURL:    "/test?limit=10&offset=-5",
			defaultLimit: 50,
			maxLimit:     100,
			wantLimit:    10,
			wantOffset:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.targetURL, nil)
			got := handlers.ParsePagination(r, tc.defaultLimit, tc.maxLimit)
			if got.Limit != tc.wantLimit {
				t.Errorf("handlers.ParsePagination().Limit = %v, want %v", got.Limit, tc.wantLimit)
			}
			if got.Offset != tc.wantOffset {
				t.Errorf("handlers.ParsePagination().Offset = %v, want %v", got.Offset, tc.wantOffset)
			}
		})
	}
}

// Integration test: TestResolveTargetUserID_Integration verifies target user ID resolution
// against a live PostgreSQL test database, checking caller context fallback,
// target username lookup, and non-existent user handling.
func TestResolveTargetUserID_Integration(t *testing.T) {
	users := testutil.MakeNTestUsers(t, testDB, 2)
	caller := users[0]
	target := users[1]

	h := &handlers.Handler{DB: testDB}

	tests := []struct {
		name       string
		targetURL  string
		callerID   int64
		wantUserID int64
		wantErrIs  error
	}{
		{
			name:       "Omitted username returns authenticated caller user ID",
			targetURL:  "/api/protected/matches",
			callerID:   caller.ID,
			wantUserID: caller.ID,
			wantErrIs:  nil,
		},
		{
			name:       "Valid username returns target user ID",
			targetURL:  "/api/protected/matches?username=" + target.Username,
			callerID:   caller.ID,
			wantUserID: target.ID,
			wantErrIs:  nil,
		},
		{
			name:       "Non-existent username returns sql.ErrNoRows",
			targetURL:  "/api/protected/matches?username=nonexistent_user_9999",
			callerID:   caller.ID,
			wantUserID: 0,
			wantErrIs:  sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.targetURL, nil)
			ctxWithUser := handlers.ContextWithUserID(r.Context(), tc.callerID)
			r = r.WithContext(ctxWithUser)

			gotID, err := h.ResolveTargetUserID(r)
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("ResolveTargetUserID() error = %v, wantErrIs %v", err, tc.wantErrIs)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveTargetUserID() unexpected error: %v", err)
			}
			if gotID != tc.wantUserID {
				t.Errorf("ResolveTargetUserID() = %v, want %v", gotID, tc.wantUserID)
			}
		})
	}
}
