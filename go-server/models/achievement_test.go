package models_test

import (
	"context"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"math"
	"testing"
)

// check that returned badge progress equals the wanted states, used to test
func assertBadgeProgress(t *testing.T, badgeID string, evalCtx models.EvalContext, wantCurrent, wantTarget int) {
	t.Helper()
	badge, found := models.FindBadge(badgeID)
	if !found {
		t.Fatalf("badge %q not found in catalog", badgeID)
	}
	current, target := badge.Evaluate(evalCtx)
	if current != wantCurrent || target != wantTarget {
		t.Errorf("badge %q progress: got (%d/%d), want (%d/%d)", badgeID, current, target, wantCurrent, wantTarget)
	}
}

func TestUserAchievementDatabaseLifecycle(t *testing.T) {
	ctx := context.Background()
	user := testutil.MakeNTestUsers(t, testDB, 1)[0]

	// Pre-seed an achievement to test duplicate conflicts
	preExisting := &models.UserAchievement{
		UserID:        user.ID,
		AchievementID: "first_friend",
	}
	if _, err := testDB.NewInsert().Model(preExisting).Exec(ctx); err != nil {
		t.Fatalf("failed to insert pre-existing achievement: %v", err)
	}

	tests := []struct {
		name        string
		record      *models.UserAchievement
		onConflict  bool // should conflict status be ignored? true for insert actions
		expectError bool
		wantRows    int64 //this is an indicator of whether an achievement was granted in this check (1), or ignored as already achieved (0)
		validate    func(t *testing.T, a *models.UserAchievement)
	}{
		{
			name: "Success: Insert valid achievement and verify persistence",
			record: &models.UserAchievement{
				UserID:        user.ID,
				AchievementID: "first_win",
			},
			expectError: false,
			wantRows:    1,
			validate: func(t *testing.T, a *models.UserAchievement) { //check in db that achievement exists and matches expected values
				fetched := new(models.UserAchievement)
				err := testDB.NewSelect().
					Model(fetched).
					Where("user_id = ? AND achievement_id = ?", a.UserID, a.AchievementID).
					Scan(ctx)
				if err != nil {
					t.Fatalf("failed to fetch inserted achievement: %v", err)
				}
				if fetched.ID == 0 {
					t.Errorf("id: expected non-zero, got %d", fetched.ID)
				}
				if fetched.AchievementID != a.AchievementID {
					t.Errorf("achievement_id: got %s, want %s", fetched.AchievementID, a.AchievementID)
				}
				if fetched.UnlockedAt.IsZero() {
					t.Error("unlocked_at: expected non-zero timestamp")
				}
			},
		},
		{
			name: "Failure: Foreign key violation with non-existent user ID",
			record: &models.UserAchievement{
				UserID:        math.MaxInt64,
				AchievementID: "first_win",
			},
			expectError: true,
		},
		{
			name: "Failure: Unique constraint violation on duplicate", // we don't store duplicates in the DB
			record: &models.UserAchievement{
				UserID:        user.ID,
				AchievementID: "first_friend",
			},
			expectError: true,
		},
		{
			name: "Success: ON CONFLICT DO NOTHING ignores duplicate without error", // but we can safely ignore the duplicate warning if we want
			record: &models.UserAchievement{
				UserID:        user.ID,
				AchievementID: "first_friend",
			},
			onConflict:  true,
			expectError: false,
			wantRows:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query := testDB.NewInsert().Model(tc.record)
			if tc.onConflict {
				query = query.On("CONFLICT (user_id, achievement_id) DO NOTHING")
			}
			res, err := query.Exec(ctx)
			if tc.expectError {
				if err == nil {
					t.Errorf("[%s] expected database constraint error, got nil", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("[%s] unexpected database error: %v", tc.name, err)
			}
			if rows, _ := res.RowsAffected(); rows != tc.wantRows {
				t.Errorf("[%s] rows affected: got %d, want %d", tc.name, rows, tc.wantRows)
			}
			if tc.validate != nil {
				tc.validate(t, tc.record)
			}
		})
	}
}

func TestBadgeCatalogEvaluation(t *testing.T) {
	tests := []struct {
		name        string
		badgeID     string
		evalCtx     models.EvalContext
		wantCurrent int
		wantTarget  int
	}{
		{
			name:        "First Win with 0 wins",
			badgeID:     "first_win",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 0}},
			wantCurrent: 0,
			wantTarget:  1,
		},
		{
			name:        "First Win with 1 win",
			badgeID:     "first_win",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 1}},
			wantCurrent: 1,
			wantTarget:  1,
		},
		{
			name:        "First Friend without friends",
			badgeID:     "first_friend",
			evalCtx:     models.EvalContext{HasFriends: false},
			wantCurrent: 0,
			wantTarget:  1,
		},
		{
			name:        "First Friend with friends",
			badgeID:     "first_friend",
			evalCtx:     models.EvalContext{HasFriends: true},
			wantCurrent: 1,
			wantTarget:  1,
		},
		{
			name:        "Dominator with 2 wins",
			badgeID:     "dominator",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 2}},
			wantCurrent: 2,
			wantTarget:  3,
		},
		{
			name:        "Dominator with 3 wins",
			badgeID:     "dominator",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 3}},
			wantCurrent: 3,
			wantTarget:  3,
		},
		{
			name:        "Champion with 4 wins",
			badgeID:     "champion",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 4}},
			wantCurrent: 4,
			wantTarget:  5,
		},
		{
			name:        "Champion with 5 wins",
			badgeID:     "champion",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 5}},
			wantCurrent: 5,
			wantTarget:  5,
		},
		{
			name:        "Legend with 10 wins",
			badgeID:     "legend",
			evalCtx:     models.EvalContext{Stats: models.UserStats{Wins: 10}},
			wantCurrent: 10,
			wantTarget:  10,
		},
		{
			name:        "Veteran with 2 games played",
			badgeID:     "veteran",
			evalCtx:     models.EvalContext{Stats: models.UserStats{GamesPlayed: 2}},
			wantCurrent: 2,
			wantTarget:  5,
		},
		{
			name:        "Veteran with 5 games played",
			badgeID:     "veteran",
			evalCtx:     models.EvalContext{Stats: models.UserStats{GamesPlayed: 5}},
			wantCurrent: 5,
			wantTarget:  5,
		},
		{
			name:        "Gladiator with 10 games played",
			badgeID:     "gladiator",
			evalCtx:     models.EvalContext{Stats: models.UserStats{GamesPlayed: 10}},
			wantCurrent: 10,
			wantTarget:  10,
		},
		{
			name:        "Warlord with 20 games played",
			badgeID:     "warlord",
			evalCtx:     models.EvalContext{Stats: models.UserStats{GamesPlayed: 20}},
			wantCurrent: 20,
			wantTarget:  20,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertBadgeProgress(t, tc.badgeID, tc.evalCtx, tc.wantCurrent, tc.wantTarget)
		})
	}
}

func TestFindBadge(t *testing.T) {
	tests := []struct {
		name      string
		badgeID   string
		wantFound bool
	}{
		{"Existing standalone badge", "first_friend", true},
		{"Existing win series badge", "first_win", true},
		{"Existing match series badge", "warlord", true},
		{"Non-existent badge", "non_existent_badge_xyz", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			badge, found := models.FindBadge(tc.badgeID)
			if found != tc.wantFound {
				t.Errorf("FindBadge(%q) found: got %v, want %v", tc.badgeID, found, tc.wantFound)
			}
			if found && badge.ID != tc.badgeID {
				t.Errorf("FindBadge(%q) ID: got %s, want %s", tc.badgeID, badge.ID, tc.badgeID)
			}
		})
	}
}
