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

func TestGetHistory_Integration(t *testing.T) {
	//setup
	sessionToken := "history_get_session_token_abc"
	p1, p2 := testutil.MakeTwoPlayers(t, testDB)
	testutil.CreateTestSession(t, testDB, p1.ID, sessionToken)
	mockMatches := []models.MatchRecord{
		{
			Player1:   p1.ID,
			Player2:   p2.ID,
			Result:    "player1_win",
			StartedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			Player1:   p2.ID,
			Player2:   p1.ID,
			Result:    "player2_win",
			StartedAt: time.Now().Add(-1 * time.Hour),
		},
	}
	testutil.SeedMockMatches(t, testDB, mockMatches)
	// Tests
	req := httptest.NewRequest(http.MethodGet, "/api/match-history", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionToken})
	rec := httptest.NewRecorder()

	matchHandler := &handlers.MatchHistoryHandler{DB: testDB}
	authGuard := handlers.AuthRequired(testDB)
	pipeline := authGuard(http.HandlerFunc(matchHandler.GetHistory))
	pipeline.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { //got a response
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	var history []models.MatchRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil { //response correctly formatted
		t.Fatalf("failed to parse match history response: %v", err)
	}
	if len(history) != 2 { //response of expected length
		t.Errorf("expected 2 match history entries, got %d", len(history))
	}
}

func TestInsertingHistory_Integration(t *testing.T) {
	ctx := context.Background()
	apiKey, err := handlers.GetAPIKey()
	if err != nil {
		t.Fatalf("Failed to read API key from disk: %v", err)
	}
	p1, p2 := testutil.MakeTwoPlayers(t, testDB)
	matchHandler := &handlers.MatchHistoryHandler{DB: testDB}
	serverGuard := handlers.GameServerAuth(apiKey)
	pipeline := serverGuard(http.HandlerFunc(matchHandler.PostHistory))
	tests := []struct {
		name           string
		apiKeyHeader   string
		payload        handlers.MatchInput
		expectedStatus int
		validateStats  func(t *testing.T)
	}{
		{
			name:         "Success - Player 1 Wins atomically increments stats",
			apiKeyHeader: apiKey,
			payload: handlers.MatchInput{
				Player1:   p1.ID,
				Player2:   p2.ID,
				Result:    "player1_win",
				StartedAt: time.Now().Add(-1 * time.Hour),
			},
			expectedStatus: http.StatusCreated,
			validateStats: func(t *testing.T) {
				u1 := new(models.User)
				if err := testDB.NewSelect().Model(u1).Where("id = ?", p1.ID).Scan(ctx); err != nil {
					t.Fatal(err)
				}
				if u1.TotalWins != p1.TotalWins+1 {
					t.Errorf("expected player 1 wins to increase to %d, got %d", p1.TotalWins+1, u1.TotalWins)
				}
				u2 := new(models.User)
				if err := testDB.NewSelect().Model(u2).Where("id = ?", p2.ID).Scan(ctx); err != nil {
					t.Fatal(err)
				}
				if u2.TotalLosses != p2.TotalLosses+1 {
					t.Errorf("expected player 2 losses to increase to %d, got %d", p2.TotalLosses+1, u2.TotalLosses)
				}
				p1.TotalWins = u1.TotalWins
				p2.TotalLosses = u2.TotalLosses
			},
		},
		{
			name:         "Success - Draw skips profile stats modifications",
			apiKeyHeader: apiKey,
			payload: handlers.MatchInput{
				Player1:   p1.ID,
				Player2:   p2.ID,
				Result:    "draw",
				StartedAt: time.Now().Add(-1 * time.Hour),
			},
			expectedStatus: http.StatusCreated,
			validateStats: func(t *testing.T) {
				u1 := new(models.User)
				testDB.NewSelect().Model(u1).Where("id = ?", p1.ID).Scan(ctx)
				if u1.TotalWins != p1.TotalWins || u1.TotalLosses != p1.TotalLosses {
					t.Error("draw should not alter player 1 win/loss metrics")
				}
			},
		},
		{
			name:         "Failure - Invalid API key drops request instantly",
			apiKeyHeader: "wrong_api_key_imposter",
			payload: handlers.MatchInput{
				Player1:   p1.ID,
				Player2:   p2.ID,
				Result:    "player1_win",
				StartedAt: time.Now().Add(-1 * time.Hour),
			},
			expectedStatus: http.StatusUnauthorized,
			validateStats:  nil,
		},
		{
			name:         "Failure - Malformed payload enum option drops request",
			apiKeyHeader: apiKey,
			payload: handlers.MatchInput{
				Player1:   p1.ID,
				Player2:   p2.ID,
				Result:    "invalid_string_outcome",
				StartedAt: time.Now().Add(-1 * time.Hour),
			},
			expectedStatus: http.StatusBadRequest,
			validateStats:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPost, "/api/match-history", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			if tc.apiKeyHeader != "" {
				req.Header.Set("X-API-Key", tc.apiKeyHeader)
			}

			rec := httptest.NewRecorder()
			pipeline.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("[%s] expected HTTP status %d, got %d. Response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if rec.Code == http.StatusCreated {
				var createdMatch models.MatchRecord
				if err := json.Unmarshal(rec.Body.Bytes(), &createdMatch); err != nil {
					t.Fatalf("failed to decode match response JSON: %v", err)
				}

				if createdMatch.ID <= 0 {
					t.Errorf("expected a valid auto-incremented match ID, got %d", createdMatch.ID)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().
						Model((*models.MatchRecord)(nil)).
						Where("id = ?", createdMatch.ID).
						Exec(ctx)
				})

				dbMatch := new(models.MatchRecord)
				err := testDB.NewSelect().
					Model(dbMatch).
					Where("id = ?", createdMatch.ID).
					Scan(ctx)

				if err != nil {
					t.Fatalf("match was reported created but could not be found in the database: %v", err)
				}

				if dbMatch.Player1 != tc.payload.Player1 {
					t.Errorf("database player_one mismatch: expected %d, got %d", tc.payload.Player1, dbMatch.Player1)
				}
				if dbMatch.Player2 != tc.payload.Player2 {
					t.Errorf("database player_two mismatch: expected %d, got %d", tc.payload.Player2, dbMatch.Player2)
				}
				if dbMatch.Result != tc.payload.Result {
					t.Errorf("database result mismatch: expected %s, got %s", tc.payload.Result, dbMatch.Result)
				}
			}

			if tc.validateStats != nil && rec.Code == http.StatusCreated {
				tc.validateStats(t)
			}
		})
	}
}
