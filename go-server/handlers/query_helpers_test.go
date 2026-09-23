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
	t.Run("Standard bounded integer", func(t *testing.T) {
		const def, min, max = 50, 1, 100
		tests := []struct {
			name  string
			query string
			want  int
		}{
			{"Missing key returns default", "", 50},
			{"Empty value returns default", "limit=", 50},
			{"Valid value within bounds returns parsed value", "limit=25", 25},
			{"Non-numeric value returns default", "limit=invalid", 50},
			{"Value below minVal returns default", "limit=0", 50},
			{"Negative value below minVal returns default", "limit=-10", 50},
			{"Value above maxVal is clamped to maxVal", "limit=500", 100},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
				got := handlers.ParseQueryInt(r, "limit", def, min, max)
				if got != tc.want {
					t.Errorf("handlers.ParseQueryInt() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("Unbounded maxVal allows large values", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/?offset=1000", nil)
		if got := handlers.ParseQueryInt(r, "offset", 0, 0, 0); got != 1000 {
			t.Errorf("handlers.ParseQueryInt() = %v, want 1000", got)
		}
	})
}

// Unit test: TestParsePagination verifies limit and offset extraction,
// default values, and boundary clamping into a Pagination struct without a database.
func TestParsePagination(t *testing.T) {
	const defLimit, maxLimit = 50, 100
	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"Default pagination with empty query", "", 50, 0},
		{"Custom valid limit and offset", "limit=20&offset=15", 20, 15},
		{"Clamped limit exceeding maxLimit", "limit=200&offset=5", 100, 5},
		{"Negative offset falls back to 0", "limit=10&offset=-5", 10, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			got := handlers.ParsePagination(r, defLimit, maxLimit)
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
		username   string
		wantUserID int64
		wantErrIs  error
	}{
		{
			name:       "Omitted username returns authenticated caller user ID",
			username:   "",
			wantUserID: caller.ID,
		},
		{
			name:       "Valid username returns target user ID",
			username:   target.Username,
			wantUserID: target.ID,
		},
		{
			name:      "Non-existent username returns sql.ErrNoRows",
			username:  "nonexistent_user_9999",
			wantErrIs: sql.ErrNoRows,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetURL := "/api/protected/matches"
			if tc.username != "" {
				targetURL += "?username=" + tc.username
			}
			r := httptest.NewRequest(http.MethodGet, targetURL, nil)
			ctxWithUser := handlers.ContextWithUserID(r.Context(), caller.ID)
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
