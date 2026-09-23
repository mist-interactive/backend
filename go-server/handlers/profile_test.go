package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/uptrace/bun"
)

func setupProfileTestRouter(t *testing.T) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", nil)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /profile", h.ProfileGet)
	protected.HandleFunc("PATCH /profile", h.ProfilePatch)
	protected.HandleFunc("GET /profile/{username}", h.ProfileGetByUsername)

	return h, mux, privateKey
}

func assertStats(t *testing.T, got models.UserStats, wantGames, wantWins, wantLosses int, wantRate float64) {
	t.Helper()
	if got.GamesPlayed != wantGames {
		t.Errorf("stats.games_played: got %d, want %d", got.GamesPlayed, wantGames)
	}
	if got.Wins != wantWins {
		t.Errorf("stats.wins: got %d, want %d", got.Wins, wantWins)
	}
	if got.Losses != wantLosses {
		t.Errorf("stats.losses: got %d, want %d", got.Losses, wantLosses)
	}
	if got.WinRate != wantRate {
		t.Errorf("stats.win_rate: got %v, want %v", got.WinRate, wantRate)
	}
}

func TestProfileGet(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupProfileTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	ids := testutil.UserIDs(users)
	userAuth := makeAuthHeader(t, users[0], privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	// Seed 1 finished win for user 0
	match := &models.MatchRecord{
		Player1:      users[0].ID,
		Player2:      users[1].ID,
		Player1Score: testutil.Ptr(5),
		Player2Score: testutil.Ptr(2),
		Status:       models.StatusFinished,
		Result:       testutil.Ptr(models.ResultPlayer1Win),
	}
	if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
		t.Fatalf("failed to insert test match: %v", err)
	}

	rec := doTestRequest(router, http.MethodGet, "/api/protected/profile", userAuth, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	p := testutil.DecodeJSON[models.UserProfile](t, rec)
	if p.Username != users[0].Username {
		t.Errorf("username: got %v, want %v", p.Username, users[0].Username)
	}
	if p.Email == nil || *p.Email != users[0].Email {
		t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
	}
	assertStats(t, p.Stats, 1, 1, 0, 100.0)
}

func TestProfileGetByUsername(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupProfileTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	ids := testutil.UserIDs(users)
	userAuth := makeAuthHeader(t, users[0], privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	tests := []struct {
		name           string
		targetUsername string
		expectedStatus int
		validate       func(t *testing.T, rec *http.Response, body string)
	}{
		{
			name:           "Success: viewing other user profile strips email",
			targetUsername: users[1].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *http.Response, body string) {
				var p models.UserProfile
				if err := json.Unmarshal([]byte(body), &p); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				if p.Username != users[1].Username {
					t.Errorf("username: got %v, want %v", p.Username, users[1].Username)
				}
				if p.Email != nil {
					t.Errorf("expected email to be omitted for other user, got: %v", *p.Email)
				}
				var raw map[string]any
				if err := json.Unmarshal([]byte(body), &raw); err != nil {
					t.Fatalf("failed to unmarshal raw map: %v", err)
				}
				if _, exists := raw["email"]; exists {
					t.Errorf("expected 'email' key to be completely omitted from JSON, but found: %v", raw["email"])
				}
			},
		},
		{
			name:           "Success: viewing self by username includes email",
			targetUsername: users[0].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *http.Response, body string) {
				var p models.UserProfile
				if err := json.Unmarshal([]byte(body), &p); err != nil {
					t.Fatalf("failed to unmarshal: %v", err)
				}
				if p.Email == nil || *p.Email != users[0].Email {
					t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
				}
			},
		},
		{
			name:           "Failure: non-existent username returns 404",
			targetUsername: "nonexistent_user_999",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodGet, "/api/protected/profile/"+tc.targetUsername, userAuth, nil)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec.Result(), rec.Body.String())
			}
		})
	}
}

func TestProfileStats_Calculations(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupProfileTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	// Insert diverse matches for users[0]:
	// 1. Win as Player 1 (Finished) -> Win
	// 2. Win as Player 2 (Finished) -> Win
	// 3. Loss as Player 1 (Finished) -> Loss
	// 4. InProgress match -> Ignored
	// 5. Abandoned match -> Ignored
	matches := []*models.MatchRecord{
		{Player1: users[0].ID, Player2: users[1].ID, Player1Score: testutil.Ptr(5), Player2Score: testutil.Ptr(3), Status: models.StatusFinished, Result: testutil.Ptr(models.ResultPlayer1Win)},
		{Player1: users[1].ID, Player2: users[0].ID, Player1Score: testutil.Ptr(1), Player2Score: testutil.Ptr(5), Status: models.StatusFinished, Result: testutil.Ptr(models.ResultPlayer2Win)},
		{Player1: users[0].ID, Player2: users[1].ID, Player1Score: testutil.Ptr(3), Player2Score: testutil.Ptr(5), Status: models.StatusFinished, Result: testutil.Ptr(models.ResultPlayer2Win)},
		{Player1: users[0].ID, Player2: users[2].ID, Status: models.StatusInProgress},
		{Player1: users[0].ID, Player2: users[2].ID, Status: models.StatusAbandoned, Result: testutil.Ptr(models.ResultAborted)},
	}
	if _, err := testDB.NewInsert().Model(&matches).Exec(ctx); err != nil {
		t.Fatalf("failed to insert test matches: %v", err)
	}

	tests := []struct {
		name       string
		user       *models.User
		wantGames  int
		wantWins   int
		wantLosses int
		wantRate   float64
	}{
		{
			name:       "User with matches calculates accurate stats",
			user:       users[0],
			wantGames:  3,
			wantWins:   2,
			wantLosses: 1,
			wantRate:   67.0,
		},
		{
			name:       "User with zero matches returns zeroed stats",
			user:       users[2],
			wantGames:  0,
			wantWins:   0,
			wantLosses: 0,
			wantRate:   0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := makeAuthHeader(t, tc.user, privKey)
			rec := doTestRequest(router, http.MethodGet, "/api/protected/profile", auth, nil)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
			}

			p := testutil.DecodeJSON[models.UserProfile](t, rec)
			assertStats(t, p.Stats, tc.wantGames, tc.wantWins, tc.wantLosses, tc.wantRate)
		})
	}
}

func TestProfilePatch(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupProfileTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	ids := testutil.UserIDs(users)
	userAuth := makeAuthHeader(t, users[0], privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	tests := []struct {
		name           string
		body           models.ProfilePatchInput
		expectedStatus int
		validate       func(t *testing.T, p models.UserProfile)
	}{
		{
			name:           "Success: update bio returns profile with stats",
			body:           models.ProfilePatchInput{Bio: testutil.Ptr("New bio content")},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, p models.UserProfile) {
				if p.Bio != "New bio content" {
					t.Errorf("bio: got %v, want 'New bio content'", p.Bio)
				}
				if p.Email == nil || *p.Email != users[0].Email {
					t.Errorf("email: got %v, want %v", p.Email, users[0].Email)
				}
				assertStats(t, p.Stats, 0, 0, 0, 0.0)
			},
		},
		{
			name:           "Failure: empty patch returns 400",
			body:           models.ProfilePatchInput{},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodPatch, "/api/protected/profile", userAuth, tc.body)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				p := testutil.DecodeJSON[models.UserProfile](t, rec)
				tc.validate(t, p)
			}
		})
	}
}
