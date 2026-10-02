package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

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
	protected.HandleFunc("DELETE /profile", h.ProfileDelete)

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

func TestProfileDelete(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupProfileTestRouter(t)

	t.Run("anonymizes user profile, purges sessions, and clears cookie", func(t *testing.T) {
		users := makeAuthUsers(t, testutil.MakeNTestUsers(t, testDB, 2), privKey)
		targetUser := users[0]
		viewerUser := users[1]

		// Register explicit user cleanup by ID because ProfileDelete changes targetUser.Username
		// to "deleted_user_<id>", which causes MakeNTestUsers' username-based cleanup to miss it.
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = testDB.NewDelete().
				Model((*models.User)(nil)).
				Where("id = ?", targetUser.ID).
				Exec(cleanupCtx)
		})

		// Populate bio and avatar to verify they get wiped upon deletion
		avatarURL := "https://example.com/avatars/user.jpg"
		targetUser.Bio = "Hello world, I will be deleted"
		targetUser.AvatarURL = &avatarURL
		if _, err := testDB.NewUpdate().
			Model(targetUser.User).
			Column("bio", "avatar_url").
			WherePK().
			Exec(ctx); err != nil {
			t.Fatalf("failed to update target user with bio and avatar: %v", err)
		}

		// Seed active sessions in DB to verify they are purged
		sessions := []*models.Session{
			{
				UserID:       targetUser.ID,
				SessionToken: "active_token_1_" + targetUser.Username,
				ExpiresAt:    time.Now().Add(24 * time.Hour),
			},
			{
				UserID:       targetUser.ID,
				SessionToken: "active_token_2_" + targetUser.Username,
				ExpiresAt:    time.Now().Add(48 * time.Hour),
			},
		}
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = testDB.NewDelete().
				Model((*models.Session)(nil)).
				Where("user_id = ?", targetUser.ID).
				Exec(cleanupCtx)
		})
		if _, err := testDB.NewInsert().Model(&sessions).Exec(ctx); err != nil {
			t.Fatalf("failed to insert active sessions: %v", err)
		}

		// Execute DELETE /api/protected/profile
		rec := doTestRequest(router, http.MethodDelete, "/api/protected/profile", targetUser.Auth, nil)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204 No Content, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body, got %q", rec.Body.String())
		}

		// Verify ClearSessionCookie was called
		var sessionCookie *http.Cookie
		for _, c := range rec.Result().Cookies() {
			if c.Name == handlers.SessionCookieName {
				sessionCookie = c
				break
			}
		}
		if sessionCookie == nil {
			t.Errorf("expected %s cookie in response", handlers.SessionCookieName)
		} else {
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
		}

		// Verify DB user record is anonymized
		var anonymizedUser models.User
		if err := testDB.NewSelect().
			Model(&anonymizedUser).
			Where("id = ?", targetUser.ID).
			Scan(ctx); err != nil {
			t.Fatalf("failed to query anonymized user: %v", err)
		}

		expectedUsername := fmt.Sprintf("deleted_user_%d", targetUser.ID)
		expectedEmail := fmt.Sprintf("deleted_user_%d@internal", targetUser.ID)
		if anonymizedUser.Username != expectedUsername {
			t.Errorf("username: got %q, want %q", anonymizedUser.Username, expectedUsername)
		}
		if anonymizedUser.Email != expectedEmail {
			t.Errorf("email: got %q, want %q", anonymizedUser.Email, expectedEmail)
		}
		if anonymizedUser.PWHash != "deleted" {
			t.Errorf("password_hash: got %q, want 'deleted'", anonymizedUser.PWHash)
		}
		if anonymizedUser.Bio != "" {
			t.Errorf("bio: got %q, want empty string", anonymizedUser.Bio)
		}
		if anonymizedUser.AvatarURL != nil {
			t.Errorf("avatar_url: got %v, want nil", anonymizedUser.AvatarURL)
		}

		// Verify all active sessions were purged
		sessionCount, err := testDB.NewSelect().
			Model((*models.Session)(nil)).
			Where("user_id = ?", targetUser.ID).
			Count(ctx)
		if err != nil {
			t.Fatalf("failed to count sessions after deletion: %v", err)
		}
		if sessionCount != 0 {
			t.Errorf("session count: got %d, want 0", sessionCount)
		}

		// Verify old username returns 404
		oldProfileRec := doTestRequest(router, http.MethodGet, "/api/protected/profile/"+targetUser.Username, viewerUser.Auth, nil)
		if oldProfileRec.Code != http.StatusNotFound {
			t.Errorf("expected status 404 for deleted username, got %d", oldProfileRec.Code)
		}

		// Verify viewing anonymized username returns profile without email
		anonProfileRec := doTestRequest(router, http.MethodGet, "/api/protected/profile/"+expectedUsername, viewerUser.Auth, nil)
		if anonProfileRec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for anonymized profile, got %d", anonProfileRec.Code)
		}
		anonProfile := testutil.DecodeJSON[models.UserProfile](t, anonProfileRec)
		if anonProfile.Username != expectedUsername {
			t.Errorf("anonymized profile username: got %q, want %q", anonProfile.Username, expectedUsername)
		}
		if anonProfile.Email != nil {
			t.Errorf("anonymized profile email: got %v, want nil", anonProfile.Email)
		}
		if anonProfile.Bio != "" {
			t.Errorf("anonymized profile bio: got %q, want empty string", anonProfile.Bio)
		}
		if anonProfile.AvatarURL != nil {
			t.Errorf("anonymized profile avatarUrl: got %v, want nil", anonProfile.AvatarURL)
		}
	})

	t.Run("deleted username is released and can be registered by new user", func(t *testing.T) {
		users := makeAuthUsers(t, testutil.MakeNTestUsers(t, testDB, 1), privKey)
		targetUser := users[0]
		originalUsername := targetUser.Username

		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = testDB.NewDelete().
				Model((*models.User)(nil)).
				Where("id = ?", targetUser.ID).
				Exec(cleanupCtx)
		})

		rec := doTestRequest(router, http.MethodDelete, "/api/protected/profile", targetUser.Auth, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204 No Content, got %d", rec.Code)
		}

		// The original username should now be free to use
		newUser := &models.User{
			Username: originalUsername,
			Email:    "reclaimed_" + originalUsername + "@testing.internal",
			PWHash:   "$2b$12$SX55NTDU0FL4DrpQm5kq.OLKcDrrMnS6siaY3Z80.8ki5zagqx08m",
		}
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = testDB.NewDelete().
				Model((*models.User)(nil)).
				Where("id = ?", newUser.ID).
				Exec(cleanupCtx)
		})

		if _, err := testDB.NewInsert().Model(newUser).Exec(ctx); err != nil {
			t.Fatalf("failed to insert new user with released username %q: %v", originalUsername, err)
		}
		if newUser.ID == targetUser.ID {
			t.Errorf("expected different ID for new user, got same ID %d", newUser.ID)
		}
	})
}
