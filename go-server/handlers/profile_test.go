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

	"github.com/uptrace/bun"
)

func TestProfileGet(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 2)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	// Seed 1 finished win for user 0
	winP1 := models.ResultPlayer1Win
	p1Score, p2Score := 5, 2
	match := &models.MatchRecord{
		Player1:      users[0].ID,
		Player2:      users[1].ID,
		Player1Score: &p1Score,
		Player2Score: &p2Score,
		Status:       models.StatusFinished,
		Result:       &winP1,
	}
	if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
		t.Fatalf("failed to insert test match: %v", err)
	}

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		authUserID     int64
		expectedStatus int
		validate       func(t *testing.T, body []byte)
	}{
		{
			name:           "Success: returns own profile with email and stats",
			authUserID:     users[0].ID,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var p models.UserProfile
				if err := json.Unmarshal(body, &p); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if p.Username != users[0].Username {
					t.Errorf("username: got %v, want %v", p.Username, users[0].Username)
				}
				if p.Email == nil || *p.Email != users[0].Email {
					t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
				}
				if p.Stats.GamesPlayed != 1 {
					t.Errorf("stats.games_played: got %v, want 1", p.Stats.GamesPlayed)
				}
				if p.Stats.Wins != 1 {
					t.Errorf("stats.wins: got %v, want 1", p.Stats.Wins)
				}
				if p.Stats.Losses != 0 {
					t.Errorf("stats.losses: got %v, want 0", p.Stats.Losses)
				}
				if p.Stats.WinRate != 100.0 {
					t.Errorf("stats.win_rate: got %v, want 100.0", p.Stats.WinRate)
				}
			},
		},
		{
			name:           "Failure: unauthenticated returns 401",
			authUserID:     0,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/protected/profile", nil)
			if tc.authUserID != 0 {
				req = req.WithContext(handlers.ContextWithUserID(req.Context(), tc.authUserID))
			}
			rec := httptest.NewRecorder()

			handler.ProfileGet(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec.Body.Bytes())
			}
		})
	}
}

func TestProfileGetByUsername(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 2)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		callerID       int64
		targetUsername string
		expectedStatus int
		validate       func(t *testing.T, body []byte)
	}{
		{
			name:           "Success: viewing other user profile strips email",
			callerID:       users[0].ID,
			targetUsername: users[1].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var p models.UserProfile
				if err := json.Unmarshal(body, &p); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if p.Username != users[1].Username {
					t.Errorf("username: got %v, want %v", p.Username, users[1].Username)
				}
				if p.Email != nil {
					t.Errorf("expected email to be omitted for other user, but got: %v", *p.Email)
				}
				// Verify JSON string itself doesn't contain "email"
				var raw map[string]any
				if err := json.Unmarshal(body, &raw); err != nil {
					t.Fatalf("failed to unmarshal raw map: %v", err)
				}
				if _, exists := raw["email"]; exists {
					t.Errorf("expected 'email' key to be completely omitted from JSON, but found: %v", raw["email"])
				}
			},
		},
		{
			name:           "Success: viewing self by username includes email",
			callerID:       users[0].ID,
			targetUsername: users[0].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var p models.UserProfile
				if err := json.Unmarshal(body, &p); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if p.Email == nil || *p.Email != users[0].Email {
					t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
				}
			},
		},
		{
			name:           "Failure: non-existent username returns 404",
			callerID:       users[0].ID,
			targetUsername: "nonexistent_user_999",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: unauthenticated caller returns 401",
			callerID:       0,
			targetUsername: users[0].Username,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/protected/profile/"+tc.targetUsername, nil)
			req.SetPathValue("username", tc.targetUsername)
			if tc.callerID != 0 {
				req = req.WithContext(handlers.ContextWithUserID(req.Context(), tc.callerID))
			}
			rec := httptest.NewRecorder()

			handler.ProfileGetByUsername(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec.Body.Bytes())
			}
		})
	}
}

func TestProfileStats_Calculations(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	winP1 := models.ResultPlayer1Win
	winP2 := models.ResultPlayer2Win
	aborted := models.ResultAborted

	score5, score3, score1 := 5, 3, 1

	// Insert diverse matches for users[0]:
	// 1. Win as Player 1 (Finished) -> Win
	// 2. Win as Player 2 (Finished) -> Win
	// 3. Loss as Player 1 (Finished) -> Loss
	// 4. InProgress match -> Ignored
	// 5. Abandoned match -> Ignored
	matches := []*models.MatchRecord{
		{Player1: users[0].ID, Player2: users[1].ID, Player1Score: &score5, Player2Score: &score3, Status: models.StatusFinished, Result: &winP1},
		{Player1: users[1].ID, Player2: users[0].ID, Player1Score: &score1, Player2Score: &score5, Status: models.StatusFinished, Result: &winP2},
		{Player1: users[0].ID, Player2: users[1].ID, Player1Score: &score3, Player2Score: &score5, Status: models.StatusFinished, Result: &winP2},
		{Player1: users[0].ID, Player2: users[2].ID, Status: models.StatusInProgress},
		{Player1: users[0].ID, Player2: users[2].ID, Status: models.StatusAbandoned, Result: &aborted},
	}
	if _, err := testDB.NewInsert().Model(&matches).Exec(ctx); err != nil {
		t.Fatalf("failed to insert test matches: %v", err)
	}

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	t.Run("User with matches calculates accurate stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/protected/profile", nil)
		req = req.WithContext(handlers.ContextWithUserID(req.Context(), users[0].ID))
		rec := httptest.NewRecorder()

		handler.ProfileGet(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var p models.UserProfile
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("failed to unmarshal profile: %v", err)
		}

		// 3 finished games (2 wins, 1 loss) -> 67% win rate
		if p.Stats.GamesPlayed != 3 {
			t.Errorf("games_played: got %d, want 3", p.Stats.GamesPlayed)
		}
		if p.Stats.Wins != 2 {
			t.Errorf("wins: got %d, want 2", p.Stats.Wins)
		}
		if p.Stats.Losses != 1 {
			t.Errorf("losses: got %d, want 1", p.Stats.Losses)
		}
		if p.Stats.WinRate != 67.0 {
			t.Errorf("win_rate: got %v, want 67.0", p.Stats.WinRate)
		}
	})

	t.Run("User with zero matches returns zeroed stats", func(t *testing.T) {
		// users[2] only participated in non-finished matches with users[0], so 0 finished games
		req := httptest.NewRequest(http.MethodGet, "/api/protected/profile", nil)
		req = req.WithContext(handlers.ContextWithUserID(req.Context(), users[2].ID))
		rec := httptest.NewRecorder()

		handler.ProfileGet(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var p models.UserProfile
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("failed to unmarshal profile: %v", err)
		}

		if p.Stats.GamesPlayed != 0 || p.Stats.Wins != 0 || p.Stats.Losses != 0 || p.Stats.WinRate != 0.0 {
			t.Errorf("expected all zero stats, got %+v", p.Stats)
		}
	})
}

func TestProfilePatch(t *testing.T) {
	users := testutil.MakeNTestUsers(t, testDB, 1)
	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		callerID       int64
		body           any
		expectedStatus int
		validate       func(t *testing.T, body []byte)
	}{
		{
			name:     "Success: updates bio and returns updated profile with stats",
			callerID: users[0].ID,
			body: models.ProfilePatchInput{
				Bio: testutil.Ptr("New bio content"),
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var p models.UserProfile
				if err := json.Unmarshal(body, &p); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if p.Bio != "New bio content" {
					t.Errorf("bio: got %v, want 'New bio content'", p.Bio)
				}
				if p.Email == nil || *p.Email != users[0].Email {
					t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
				}
			},
		},
		{
			name:           "Failure: empty patch returns 400",
			callerID:       users[0].ID,
			body:           models.ProfilePatchInput{},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: unauthenticated caller returns 401",
			callerID:       0,
			body:           models.ProfilePatchInput{Bio: testutil.Ptr("test")},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPatch, "/api/protected/profile", bytes.NewBuffer(b))
			req.Header.Set("Content-Type", "application/json")
			if tc.callerID != 0 {
				req = req.WithContext(handlers.ContextWithUserID(req.Context(), tc.callerID))
			}
			rec := httptest.NewRecorder()

			handler.ProfilePatch(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec.Body.Bytes())
			}
		})
	}
}
