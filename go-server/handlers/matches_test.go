package handlers_test

import (
	"bytes"
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func TestMatchPatch(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 3)
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
		setup          func(t *testing.T) (int64, models.MatchPatchInput)
		expectedStatus int
		validate       func(t *testing.T, matchID int64)
	}{
		{
			name: "Success: Unordered scores mapped correctly and player 2 win inferred",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: users[1].ID, Score: 7},
						{PlayerID: users[0].ID, Score: 3},
					},
				}
			},
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T, matchID int64) {
				var m models.MatchRecord
				if err := testDB.NewSelect().Model(&m).Where("id = ?", matchID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if m.Status != models.StatusFinished {
					t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
				}
				if m.Result == nil || *m.Result != models.ResultPlayer2Win {
					t.Errorf("expected result %s, got %v", models.ResultPlayer2Win, m.Result)
				}
				if m.Player1Score == nil || *m.Player1Score != 3 {
					t.Errorf("expected player1_score 3, got %v", m.Player1Score)
				}
				if m.Player2Score == nil || *m.Player2Score != 7 {
					t.Errorf("expected player2_score 7, got %v", m.Player2Score)
				}
				if m.FinishedAt == nil {
					t.Errorf("expected finished_at timestamp to be set")
				}
			},
		},
		{
			name: "Success: Equal scores infer draw",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: users[0].ID, Score: 4},
						{PlayerID: users[1].ID, Score: 4},
					},
				}
			},
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T, matchID int64) {
				var m models.MatchRecord
				if err := testDB.NewSelect().Model(&m).Where("id = ?", matchID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if m.Status != models.StatusFinished {
					t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
				}
				if m.Result == nil || *m.Result != models.ResultDraw {
					t.Errorf("expected result %s, got %v", models.ResultDraw, m.Result)
				}
				if m.Player1Score == nil || *m.Player1Score != 4 {
					t.Errorf("expected player1_score 4, got %v", m.Player1Score)
				}
				if m.Player2Score == nil || *m.Player2Score != 4 {
					t.Errorf("expected player2_score 4, got %v", m.Player2Score)
				}
			},
		},
		{
			name: "Failure: Duplicate player ID in scores",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				return 999, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: users[0].ID, Score: 5},
						{PlayerID: users[0].ID, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Non-participant player ID in scores",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: users[0].ID, Score: 5},
						{PlayerID: users[2].ID, Score: 2}, // non-participant in this match
					},
				}
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Match already finished",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				result := models.ResultPlayer1Win
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: users[0].ID, Score: 5},
						{PlayerID: users[1].ID, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matchID, input := tc.setup(t)
			jsonBytes, _ := json.Marshal(input)
			req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/internal/matches/%d", matchID), bytes.NewReader(jsonBytes))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", fmt.Sprintf("%d", matchID))
			rec := httptest.NewRecorder()

			handler.MatchPatch(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, matchID)
			}
		})
	}
}

func TestUserActiveMatchGet(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)
	endpoint := handlers.InjectPathIDContext(handler.UserActiveMatchGet)

	tests := []struct {
		name           string
		setup          func(t *testing.T) string
		expectedStatus int
		validate       func(t *testing.T, body []byte)
	}{
		{
			name: "Success: User is player 1 in ongoing match",
			setup: func(t *testing.T) string {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", users[0].ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != users[1].ID {
					t.Errorf("expected opponent_id %d, got %d", users[1].ID, resp.OpponentID)
				}
				if resp.OpponentUsername != users[1].Username {
					t.Errorf("expected opponent username %s, got %s", users[1].Username, resp.OpponentUsername)
				}
				if resp.MatchID == 0 {
					t.Errorf("expected valid match_id, got 0")
				}
				if resp.StartedAt.IsZero() {
					t.Errorf("expected valid started_at timestamp")
				}
			},
		},
		{
			name: "Success: User is player 2 in ongoing match",
			setup: func(t *testing.T) string {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", users[1].ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != users[0].ID {
					t.Errorf("expected opponent_id %d, got %d", users[0].ID, resp.OpponentID)
				}
				if resp.OpponentUsername != users[0].Username {
					t.Errorf("expected opponent username %s, got %s", users[0].Username, resp.OpponentUsername)
				}
			},
		},
		{
			name: "Success: Returns latest ongoing match when finished match also exists",
			setup: func(t *testing.T) string {
				result := models.ResultPlayer1Win
				finishedMatch := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(finishedMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finishedMatch.ID).Exec(ctx)
				})

				activeMatch := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(activeMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert active match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", activeMatch.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", users[0].ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != users[1].ID {
					t.Errorf("expected opponent_id %d, got %d", users[1].ID, resp.OpponentID)
				}
			},
		},
		{
			name: "Failure: No match in progress (only finished match)",
			setup: func(t *testing.T) string {
				result := models.ResultPlayer2Win
				finishedMatch := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(finishedMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finishedMatch.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", users[0].ID)
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "Failure: User has no match history",
			setup: func(t *testing.T) string {
				return fmt.Sprintf("%d", users[2].ID)
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "Failure: Stale match in progress is swept to abandoned, returning 404",
			setup: func(t *testing.T) string {
				staleHeartbeat := time.Now().Add(-2 * time.Minute)
				staleMatch := &models.MatchRecord{
					Player1:         users[0].ID,
					Player2:         users[1].ID,
					Status:          models.StatusInProgress,
					LastHeartbeatAt: staleHeartbeat,
				}
				if _, err := testDB.NewInsert().Model(staleMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert stale match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", staleMatch.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", users[0].ID)
			},
			expectedStatus: http.StatusNotFound,
			validate: func(t *testing.T, body []byte) {
				count, err := testDB.NewSelect().
					Model((*models.MatchRecord)(nil)).
					Where("player_one = ? AND status = ?", users[0].ID, models.StatusAbandoned).
					Count(ctx)
				if err != nil {
					t.Fatalf("failed to query db for swept match: %v", err)
				}
				if count == 0 {
					t.Errorf("expected match to be swept to abandoned, but none found")
				}
			},
		},
		{
			name: "Failure: Invalid user ID in path",
			setup: func(t *testing.T) string {
				return "invalid-id"
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idStr := tc.setup(t)
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/internal/users/%s/active-match", idStr), nil)
			req.SetPathValue("id", idStr)
			rec := httptest.NewRecorder()

			endpoint(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec.Body.Bytes())
			}
		})
	}
}

func TestMatchCreate(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	resetMatches := func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	}
	t.Cleanup(resetMatches)

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		setup          func(t *testing.T)
		player1        string
		player2        string
		expectedStatus int
	}{
		{
			name:           "Success: Match created between available players",
			player1:        users[0].Username,
			player2:        users[1].Username,
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Failure: Blocked when a player already has an active match",
			setup: func(t *testing.T) {
				match := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[2].ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert active match: %v", err)
				}
			},
			player1:        users[0].Username,
			player2:        users[1].Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Success: Allowed when prior match is finished",
			setup: func(t *testing.T) {
				finished := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
					Status:  models.StatusFinished,
					Result:  testutil.Ptr(models.ResultPlayer1Win),
				}
				if _, err := testDB.NewInsert().Model(finished).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
			},
			player1:        users[0].Username,
			player2:        users[1].Username,
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Success: Allowed when prior match is stale (swept to abandoned)",
			setup: func(t *testing.T) {
				stale := &models.MatchRecord{
					Player1:         users[0].ID,
					Player2:         users[1].ID,
					Status:          models.StatusInProgress,
					LastHeartbeatAt: time.Now().Add(-2 * time.Minute),
				}
				if _, err := testDB.NewInsert().Model(stale).Exec(ctx); err != nil {
					t.Fatalf("failed to insert stale match: %v", err)
				}
			},
			player1:        users[0].Username,
			player2:        users[2].Username,
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetMatches()
			if tc.setup != nil {
				tc.setup(t)
			}
			body, _ := json.Marshal(models.MatchCreateInput{
				Player1: tc.player1,
				Player2: tc.player2,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/internal/matches", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.MatchCreate(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMatchHeartbeat(t *testing.T) {
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
		status         models.MatchStatus
		result         *models.MatchResult
		expectedStatus int
		verifyUpdate   bool
	}{
		{
			name:           "Success: Heartbeat updates last_heartbeat_at for active match",
			status:         models.StatusInProgress,
			expectedStatus: http.StatusNoContent,
			verifyUpdate:   true,
		},
		{
			name:           "Failure: Match already finished",
			status:         models.StatusFinished,
			result:         testutil.Ptr(models.ResultPlayer1Win),
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "Failure: Match already abandoned",
			status:         models.StatusAbandoned,
			result:         testutil.Ptr(models.ResultAborted),
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			past := time.Now().Add(-10 * time.Second)
			match := &models.MatchRecord{
				Player1:         users[0].ID,
				Player2:         users[1].ID,
				Status:          tc.status,
				Result:          tc.result,
				LastHeartbeatAt: past,
			}
			if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
				t.Fatalf("failed to insert match: %v", err)
			}

			req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/internal/matches/%d/heartbeat", match.ID), nil)
			req.SetPathValue("id", fmt.Sprintf("%d", match.ID))
			rec := httptest.NewRecorder()

			handler.MatchHeartbeat(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.verifyUpdate {
				var updated models.MatchRecord
				if err := testDB.NewSelect().Model(&updated).Where("id = ?", match.ID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if !updated.LastHeartbeatAt.After(past) {
					t.Errorf("expected last_heartbeat_at (%v) to be after previous timestamp (%v)",
						updated.LastHeartbeatAt, past)
				}
			}
		})
	}
}

func TestSweepStaleMatches(t *testing.T) {
	ctx := context.Background()
	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	t.Run("Sweeps only stale in-progress matches", func(t *testing.T) {
		staleMatch := &models.MatchRecord{
			Player1:         users[0].ID,
			Player2:         users[1].ID,
			Status:          models.StatusInProgress,
			LastHeartbeatAt: time.Now().Add(-2 * time.Minute),
		}
		freshMatch := &models.MatchRecord{
			Player1:         users[1].ID,
			Player2:         users[2].ID,
			Status:          models.StatusInProgress,
			LastHeartbeatAt: time.Now(),
		}
		finishedMatch := &models.MatchRecord{
			Player1:         users[0].ID,
			Player2:         users[2].ID,
			Status:          models.StatusFinished,
			Result:          testutil.Ptr(models.ResultPlayer1Win),
			LastHeartbeatAt: time.Now().Add(-2 * time.Minute),
		}

		matches := []*models.MatchRecord{staleMatch, freshMatch, finishedMatch}
		if _, err := testDB.NewInsert().Model(&matches).Exec(ctx); err != nil {
			t.Fatalf("failed to insert test matches: %v", err)
		}

		swept, err := handler.SweepStaleMatches(ctx, 60*time.Second)
		if err != nil {
			t.Fatalf("SweepStaleMatches failed: %v", err)
		}

		if len(swept) != 1 {
			t.Fatalf("got %d swept matches, want 1", len(swept))
		}
		if swept[0].ID != staleMatch.ID {
			t.Errorf("got swept match ID %d, want %d", swept[0].ID, staleMatch.ID)
		}
		if swept[0].Status != models.StatusAbandoned {
			t.Errorf("got status %v, want %v", swept[0].Status, models.StatusAbandoned)
		}
		if swept[0].Result == nil || *swept[0].Result != models.ResultAborted {
			t.Errorf("got result %v, want %v", swept[0].Result, models.ResultAborted)
		}

		// Verify DB state for all 3 matches
		assertStatus := func(id int64, want models.MatchStatus) {
			var m models.MatchRecord
			if err := testDB.NewSelect().Model(&m).Where("id = ?", id).Scan(ctx); err != nil {
				t.Fatalf("failed to query match %d: %v", id, err)
			}
			if m.Status != want {
				t.Errorf("match %d status: got %v, want %v", id, m.Status, want)
			}
		}

		assertStatus(staleMatch.ID, models.StatusAbandoned)
		assertStatus(freshMatch.ID, models.StatusInProgress)
		assertStatus(finishedMatch.ID, models.StatusFinished)
	})
}
