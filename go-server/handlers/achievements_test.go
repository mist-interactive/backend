package handlers_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// use a mock Notifier instead of a real websocket hub to avoid dependency on unrelated package
type mockMatchNotifier struct {
	lastPayload *models.MatchFinishedPayload
}

func (m *mockMatchNotifier) NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error {
	return nil
}
func (m *mockMatchNotifier) NotifyFriendResponse(targetUserID int64, item models.FriendshipItemResponse) error {
	return nil
}
func (m *mockMatchNotifier) NotifyFriendDeleted(targetUserID int64, friendshipID int64) error {
	return nil
}
func (m *mockMatchNotifier) MatchFinished(payload models.MatchFinishedPayload) error { //the only notifier function that does anything, allows us to check that payload was sent correctly to websocket hub
	m.lastPayload = &payload
	return nil
}

func assertUserBadge(t *testing.T, badges []models.UserBadgeResponse, badgeID string, wantUnlocked bool, wantProgress int, wantTarget int) {
	t.Helper()
	for _, b := range badges {
		if b.ID == badgeID {
			if b.Unlocked != wantUnlocked {
				t.Errorf("badge %q unlocked: got %v, want %v", badgeID, b.Unlocked, wantUnlocked)
			}
			if wantUnlocked && b.UnlockedAt == nil {
				t.Errorf("badge %q unlocked_at: expected non-nil timestamp", badgeID)
			}
			if b.Progress != wantProgress {
				t.Errorf("badge %q progress: got %d, want %d", badgeID, b.Progress, wantProgress)
			}
			if b.Target != wantTarget {
				t.Errorf("badge %q target: got %d, want %d", badgeID, b.Target, wantTarget)
			}
			wantPct := 0
			if wantTarget > 0 {
				wantPct = (wantProgress * 100) / wantTarget
			}
			if wantUnlocked {
				wantPct = 100
			}
			if b.ProgressPct != wantPct {
				t.Errorf("badge %q progress_pct: got %d, want %d", badgeID, b.ProgressPct, wantPct)
			}
			return
		}
	}
	t.Errorf("badge %q not found in badges response", badgeID)
}

// TestCalculateProgression verifies calculation of cumulative XP, player levels,
// progress percentages toward the next level, and tier rank titles across win/loss stats.
func TestCalculateProgression(t *testing.T) {
	tests := []struct {
		name  string
		stats models.UserStats
		want  models.ProgressionInfo
	}{
		{
			name:  "Success: Zero matches yields Level 1 Rookie with 0 XP",
			stats: models.UserStats{GamesPlayed: 0, Wins: 0, Losses: 0},
			want:  models.ProgressionInfo{TotalXP: 0, Level: 1, CurrentLevelXP: 0, XPPerLevel: 200, ProgressPercent: 0, RankTitle: "Rookie"},
		},
		{
			name:  "Success: 1 win gives 100 XP (50% through Level 1 Rookie)",
			stats: models.UserStats{GamesPlayed: 1, Wins: 1, Losses: 0},
			want:  models.ProgressionInfo{TotalXP: 100, Level: 1, CurrentLevelXP: 100, XPPerLevel: 200, ProgressPercent: 50, RankTitle: "Rookie"},
		},
		{
			name:  "Success: 2 wins gives 200 XP (Level 2 Contender with 0% progress)",
			stats: models.UserStats{GamesPlayed: 2, Wins: 2, Losses: 0},
			want:  models.ProgressionInfo{TotalXP: 200, Level: 2, CurrentLevelXP: 0, XPPerLevel: 200, ProgressPercent: 0, RankTitle: "Contender"},
		},
		{
			name:  "Success: 3 wins and 4 losses gives 440 XP (Level 3 Contender with 20% progress)",
			stats: models.UserStats{GamesPlayed: 7, Wins: 3, Losses: 4}, // 3*100 + 4*35 = 440 -> lvl 3 (40/200 = 20%)
			want:  models.ProgressionInfo{TotalXP: 440, Level: 3, CurrentLevelXP: 40, XPPerLevel: 200, ProgressPercent: 20, RankTitle: "Contender"},
		},
		{
			name:  "Success: 8 wins gives 800 XP (Level 5 Veteran)",
			stats: models.UserStats{GamesPlayed: 8, Wins: 8, Losses: 0},
			want:  models.ProgressionInfo{TotalXP: 800, Level: 5, CurrentLevelXP: 0, XPPerLevel: 200, ProgressPercent: 0, RankTitle: "Veteran"},
		},
		{
			name:  "Success: 12 wins gives 1200 XP (Level 7 Grandmaster)",
			stats: models.UserStats{GamesPlayed: 12, Wins: 12, Losses: 0},
			want:  models.ProgressionInfo{TotalXP: 1200, Level: 7, CurrentLevelXP: 0, XPPerLevel: 200, ProgressPercent: 0, RankTitle: "Grandmaster"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := handlers.CalculateProgression(tc.stats)
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestEvaluateAndGrantBadges verifies match-event badge unlocking, ensuring qualifying
// achievements are persisted, duplicate earns are prevented, and milestone thresholds trigger correctly.
func TestEvaluateAndGrantBadges(t *testing.T) {
	ctx := context.Background()
	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	users := testutil.MakeNTestUsers(t, testDB, 1)
	u1 := users[0]

	t.Run("Success: First win unlocks First Blood only", func(t *testing.T) {
		newlyEarned, err := handler.EvaluateAndGrantBadges(ctx, u1.ID, models.TriggerMatch, models.EvalContext{
			Stats:    models.UserStats{GamesPlayed: 1, Wins: 1, Losses: 0},
			WonMatch: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(newlyEarned) != 1 {
			t.Fatalf("expected 1 newly earned badge, got %d", len(newlyEarned))
		}
		if newlyEarned[0].ID != "first_win" {
			t.Errorf("got badge %s, want first_win", newlyEarned[0].ID)
		}
	})

	t.Run("Success: Second win does not grant duplicate First Blood", func(t *testing.T) {
		newlyEarned, err := handler.EvaluateAndGrantBadges(ctx, u1.ID, models.TriggerMatch, models.EvalContext{
			Stats:    models.UserStats{GamesPlayed: 2, Wins: 2, Losses: 0},
			WonMatch: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(newlyEarned) != 0 {
			t.Errorf("expected 0 newly earned badges on second win, got %d", len(newlyEarned))
		}
	})

	t.Run("Success: Third win unlocks Dominator", func(t *testing.T) {
		newlyEarned, err := handler.EvaluateAndGrantBadges(ctx, u1.ID, models.TriggerMatch, models.EvalContext{
			Stats:    models.UserStats{GamesPlayed: 3, Wins: 3, Losses: 0},
			WonMatch: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(newlyEarned) != 1 {
			t.Fatalf("expected 1 newly earned badge (Dominator), got %d", len(newlyEarned))
		}
		if newlyEarned[0].ID != "dominator" {
			t.Errorf("got badge %s, want dominator", newlyEarned[0].ID)
		}
	})

	t.Run("Success: Fifth match unlocks Arena Veteran", func(t *testing.T) {
		newlyEarned, err := handler.EvaluateAndGrantBadges(ctx, u1.ID, models.TriggerMatch, models.EvalContext{
			Stats:    models.UserStats{GamesPlayed: 5, Wins: 3, Losses: 2},
			WonMatch: false,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(newlyEarned) != 1 {
			t.Fatalf("expected 1 newly earned badge (Veteran), got %d", len(newlyEarned))
		}
		if newlyEarned[0].ID != "veteran" {
			t.Errorf("got badge %s, want veteran", newlyEarned[0].ID)
		}
	})
}

// TestFriendshipPermanentAchievement verifies that accepting a friend request awards the First Friend
// badge to both users, and that the achievement remains permanently unlocked even if the friendship is deleted.
func TestFriendshipPermanentAchievement(t *testing.T) {
	ctx := context.Background()
	privateKey, publicKey := getTestKeys(t)
	handler := handlers.NewHandler(testDB, privateKey, publicKey, "", nil)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	u1, u2 := users[0], users[1]

	// 1. Send friend request from u1 to u2
	f := &models.Friendship{
		UserID:   u1.ID,
		FriendID: u2.ID,
		Status:   models.StatusPending,
	}
	if _, err := testDB.NewInsert().Model(f).Exec(ctx); err != nil {
		t.Fatalf("failed to insert friend request: %v", err)
	}

	// 2. u2 accepts the request via FriendRequestAnswer
	body, _ := json.Marshal(models.FriendRequestAnswer{Status: models.StatusAccepted})
	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/protected/friends/%d", f.ID), bytes.NewBuffer(body))
	req.SetPathValue("id", fmt.Sprintf("%d", f.ID))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(handlers.ContextWithUserID(req.Context(), u2.ID))
	rec := httptest.NewRecorder()

	handler.FriendRequestAnswer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("FriendRequestAnswer expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify both users unlocked first_friend
	badges1, _ := handler.BuildUserBadges(ctx, u1.ID, models.EvalContext{HasFriends: true})
	badges2, _ := handler.BuildUserBadges(ctx, u2.ID, models.EvalContext{HasFriends: true})

	assertUserBadge(t, badges1, "first_friend", true, 1, 1)
	assertUserBadge(t, badges2, "first_friend", true, 1, 1)

	// 3. Delete friendship between u1 and u2
	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/protected/friends/%d", f.ID), nil)
	delReq.SetPathValue("id", fmt.Sprintf("%d", f.ID))
	delReq = delReq.WithContext(handlers.ContextWithUserID(delReq.Context(), u2.ID))
	delRec := httptest.NewRecorder()

	handler.FriendDelete(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("FriendDelete expected status 204, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// 4. Verify that despite friendship being removed, first_friend remains UNLOCKED with full progress (permanence requirement)
	badgesAfterDelete, _ := handler.BuildUserBadges(ctx, u2.ID, models.EvalContext{HasFriends: false})
	assertUserBadge(t, badgesAfterDelete, "first_friend", true, 1, 1)
}

// TestMatchPatchWithEarnedBadgesNotification verifies that concluding a match via MatchPatch
// evaluates badge criteria and includes newly earned badges in the realtime MatchFinished notification.
func TestMatchPatchWithEarnedBadgesNotification(t *testing.T) {
	ctx := context.Background()
	notifier := &mockMatchNotifier{}
	privateKey, publicKey := getTestKeys(t)
	handler := handlers.NewHandler(testDB, privateKey, publicKey, "secret_key", notifier)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	u1, u2 := users[0], users[1]

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)", u1.ID, u2.ID, u1.ID, u2.ID).
			Exec(ctx)
	})

	match := &models.MatchRecord{
		Player1: u1.ID,
		Player2: u2.ID,
		Status:  models.StatusInProgress,
	}
	if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
		t.Fatalf("failed to insert in-progress match: %v", err)
	}

	patchBody, _ := json.Marshal(models.MatchPatchInput{
		Scores: []models.PlayerScoreInput{
			{PlayerID: u1.ID, Score: 10},
			{PlayerID: u2.ID, Score: 5},
		},
	})

	req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", match.ID), bytes.NewBuffer(patchBody))
	req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.MatchPatch(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("MatchPatch expected status 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if notifier.lastPayload == nil {
		t.Fatal("expected MatchFinished notification to be dispatched, but it was nil")
	}

	// Player 1 won their first game -> should have First Blood in player_one_earned_badges
	if len(notifier.lastPayload.Player1EarnedBadges) != 1 {
		t.Fatalf("expected Player 1 to earn 1 badge, got %d", len(notifier.lastPayload.Player1EarnedBadges))
	}
	if notifier.lastPayload.Player1EarnedBadges[0].ID != "first_win" {
		t.Errorf("got badge ID %s, want first_win", notifier.lastPayload.Player1EarnedBadges[0].ID)
	}

	// Player 2 lost their first game -> 0 badges earned
	if len(notifier.lastPayload.Player2EarnedBadges) != 0 {
		t.Errorf("expected Player 2 to earn 0 badges, got %d", len(notifier.lastPayload.Player2EarnedBadges))
	}
}

func setupProfileTestRouterWithNotifier(t *testing.T) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()
	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", nil)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /profile", h.ProfileGet)
	protected.HandleFunc("GET /profile/{username}", h.ProfileGetByUsername)

	return h, mux, privateKey
}

// TestProfileEndpointReturnsProgressionAndBadges verifies that GET /profile returns enriched
// player progression data and the full badge catalog with accurate unlock statuses and progress percentages.
func TestProfileEndpointReturnsProgressionAndBadges(t *testing.T) {
	ctx := context.Background()
	h, router, privKey := setupProfileTestRouterWithNotifier(t)

	users := testutil.MakeNTestUsers(t, testDB, 2)
	u1, u2 := users[0], users[1]
	auth1 := makeAuthHeader(t, u1, privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)", u1.ID, u2.ID, u1.ID, u2.ID).
			Exec(ctx)
	})

	// Add 1 win for u1
	res := models.ResultPlayer1Win
	m := &models.MatchRecord{
		Player1:    u1.ID,
		Player2:    u2.ID,
		Status:     models.StatusFinished,
		Result:     &res,
		FinishedAt: testutil.Ptr(time.Now()),
	}
	if _, err := testDB.NewInsert().Model(m).Exec(ctx); err != nil {
		t.Fatalf("failed to insert test match: %v", err)
	}

	// In the application flow, finishing a match evaluates and unlocks badges:
	_, err := h.EvaluateAndGrantBadges(ctx, u1.ID, models.TriggerMatch, models.EvalContext{
		Stats:    models.UserStats{GamesPlayed: 1, Wins: 1, Losses: 0},
		WonMatch: true,
	})
	if err != nil {
		t.Fatalf("failed to grant test badge: %v", err)
	}

	t.Run("Success: GET /profile returns valid progression and badges", func(t *testing.T) {
		rec := doTestRequest(router, http.MethodGet, "/api/protected/profile", auth1, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		profile := testutil.DecodeJSON[models.UserProfile](t, rec)

		if profile.Progression == nil {
			t.Fatal("expected profile.Progression to be non-nil")
		}
		if profile.Progression.TotalXP != 100 {
			t.Errorf("Progression.TotalXP: got %d, want 100", profile.Progression.TotalXP)
		}
		if profile.Progression.Level != 1 {
			t.Errorf("Progression.Level: got %d, want 1", profile.Progression.Level)
		}
		if profile.Progression.RankTitle != "Rookie" {
			t.Errorf("Progression.RankTitle: got %s, want Rookie", profile.Progression.RankTitle)
		}

		if len(profile.Badges) != len(models.BadgeCatalog) {
			t.Fatalf("expected %d badges in catalog, got %d", len(models.BadgeCatalog), len(profile.Badges))
		}

		// First Blood unlocked from match win (target 1, progress 1/1, 100%)
		assertUserBadge(t, profile.Badges, "first_win", true, 1, 1)

		// Dominator requires 3 wins; user has 1 win -> locked, progress 1/3 (33%)
		assertUserBadge(t, profile.Badges, "dominator", false, 1, 3)

		// Arena Veteran requires 5 matches; user has 1 match -> locked, progress 1/5 (20%)
		assertUserBadge(t, profile.Badges, "veteran", false, 1, 5)
	})
}
