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
)

func TestMatchPatch(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)", user1.ID, user2.ID, user1.ID, user2.ID).
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user2.ID, Score: 7},
						{PlayerID: user1.ID, Score: 3},
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 4},
						{PlayerID: user2.ID, Score: 4},
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
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: user1.ID, Score: 2},
					},
				}
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Non-participant player ID in scores",
			setup: func(t *testing.T) (int64, models.MatchPatchInput) {
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: 999999, Score: 2},
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				return match.ID, models.MatchPatchInput{
					Scores: []models.PlayerScoreInput{
						{PlayerID: user1.ID, Score: 5},
						{PlayerID: user2.ID, Score: 2},
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
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	user3, cleanup3 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup3)
	testutil.RegisterUser(t, user3, testDB)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?, ?) OR player_two IN (?, ?, ?)",
				user1.ID, user2.ID, user3.ID, user1.ID, user2.ID, user3.ID).
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", user1.ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != user2.ID {
					t.Errorf("expected opponent_id %d, got %d", user2.ID, resp.OpponentID)
				}
				if resp.OpponentUsername != user2.Username {
					t.Errorf("expected opponent username %s, got %s", user2.Username, resp.OpponentUsername)
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", user2.ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != user1.ID {
					t.Errorf("expected opponent_id %d, got %d", user1.ID, resp.OpponentID)
				}
				if resp.OpponentUsername != user1.Username {
					t.Errorf("expected opponent username %s, got %s", user1.Username, resp.OpponentUsername)
				}
			},
		},
		{
			name: "Success: Returns latest ongoing match when finished match also exists",
			setup: func(t *testing.T) string {
				result := models.ResultPlayer1Win
				finishedMatch := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
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
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(activeMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert active match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", activeMatch.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", user1.ID)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, body []byte) {
				var resp models.ActiveMatchResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.OpponentID != user2.ID {
					t.Errorf("expected opponent_id %d, got %d", user2.ID, resp.OpponentID)
				}
			},
		},
		{
			name: "Failure: No match in progress (only finished match)",
			setup: func(t *testing.T) string {
				result := models.ResultPlayer2Win
				finishedMatch := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(finishedMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finishedMatch.ID).Exec(ctx)
				})
				return fmt.Sprintf("%d", user1.ID)
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "Failure: User has no match history",
			setup: func(t *testing.T) string {
				return fmt.Sprintf("%d", user3.ID)
			},
			expectedStatus: http.StatusNotFound,
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
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	user3, cleanup3 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup3)
	testutil.RegisterUser(t, user3, testDB)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?, ?) OR player_two IN (?, ?, ?)",
				user1.ID, user2.ID, user3.ID, user1.ID, user2.ID, user3.ID).
			Exec(ctx)
	})

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
			player1:        user1.Username,
			player2:        user2.Username,
			expectedStatus: http.StatusCreated,
		},
		{
			name: "Failure: Blocked when a player already has an active match",
			setup: func(t *testing.T) {
				match := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user3.ID,
					Status:  models.StatusInProgress,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert active match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
			},
			player1:        user1.Username,
			player2:        user2.Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Success: Allowed when prior match is finished",
			setup: func(t *testing.T) {
				result := models.ResultPlayer1Win
				finished := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(finished).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finished.ID).Exec(ctx)
				})
			},
			player1:        user1.Username,
			player2:        user2.Username,
			expectedStatus: http.StatusCreated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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

			if rec.Code == http.StatusCreated {
				var resp struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil && resp.ID > 0 {
					t.Cleanup(func() {
						_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", resp.ID).Exec(ctx)
					})
				}
			}

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMatchHeartbeat(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?, ?) OR player_two IN (?, ?)",
				user1.ID, user2.ID, user1.ID, user2.ID).
			Exec(ctx)
	})

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	tests := []struct {
		name           string
		setup          func(t *testing.T) (int64, time.Time)
		expectedStatus int
		validate       func(t *testing.T, matchID int64, prevHeartbeat time.Time)
	}{
		{
			name: "Success: Heartbeat updates last_heartbeat_at for active match",
			setup: func(t *testing.T) (int64, time.Time) {
				past := time.Now().Add(-10 * time.Second).UTC().Truncate(time.Microsecond)
				match := &models.MatchRecord{
					Player1:         user1.ID,
					Player2:         user2.ID,
					Status:          models.StatusInProgress,
					LastHeartbeatAt: past,
				}
				if _, err := testDB.NewInsert().Model(match).Exec(ctx); err != nil {
					t.Fatalf("failed to insert active match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
				return match.ID, past
			},
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T, matchID int64, prevHeartbeat time.Time) {
				var updated models.MatchRecord
				if err := testDB.NewSelect().Model(&updated).Where("id = ?", matchID).Scan(ctx); err != nil {
					t.Fatalf("failed to fetch updated match: %v", err)
				}
				if !updated.LastHeartbeatAt.After(prevHeartbeat) {
					t.Errorf("expected last_heartbeat_at (%v) to be after previous timestamp (%v)",
						updated.LastHeartbeatAt, prevHeartbeat)
				}
			},
		},
		{
			name: "Failure: Match already finished",
			setup: func(t *testing.T) (int64, time.Time) {
				result := models.ResultPlayer1Win
				finishedMatch := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusFinished,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(finishedMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert finished match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finishedMatch.ID).Exec(ctx)
				})
				return finishedMatch.ID, time.Time{}
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Failure: Match already abandoned",
			setup: func(t *testing.T) (int64, time.Time) {
				result := models.ResultAborted
				abandonedMatch := &models.MatchRecord{
					Player1: user1.ID,
					Player2: user2.ID,
					Status:  models.StatusAbandoned,
					Result:  &result,
				}
				if _, err := testDB.NewInsert().Model(abandonedMatch).Exec(ctx); err != nil {
					t.Fatalf("failed to insert abandoned match: %v", err)
				}
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", abandonedMatch.ID).Exec(ctx)
				})
				return abandonedMatch.ID, time.Time{}
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			matchID, prevHeartbeat := tc.setup(t)
			req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/internal/matches/%d/heartbeat", matchID), nil)
			req.SetPathValue("id", fmt.Sprintf("%d", matchID))
			rec := httptest.NewRecorder()

			handler.MatchHeartbeat(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Server response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, matchID, prevHeartbeat)
			}
		})
	}
}

func TestSweepStaleMatches(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	user3, cleanup3 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup3)
	testutil.RegisterUser(t, user3, testDB)

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	t.Run("Sweeps only stale in-progress matches", func(t *testing.T) {
		staleHeartbeat := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Microsecond)
		staleMatch := &models.MatchRecord{
			Player1:         user1.ID,
			Player2:         user2.ID,
			Status:          models.StatusInProgress,
			LastHeartbeatAt: staleHeartbeat,
		}
		if _, err := testDB.NewInsert().Model(staleMatch).Exec(ctx); err != nil {
			t.Fatalf("failed to insert stale match: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", staleMatch.ID).Exec(ctx)
		})

		freshHeartbeat := time.Now().UTC().Truncate(time.Microsecond)
		freshMatch := &models.MatchRecord{
			Player1:         user2.ID,
			Player2:         user3.ID,
			Status:          models.StatusInProgress,
			LastHeartbeatAt: freshHeartbeat,
		}
		if _, err := testDB.NewInsert().Model(freshMatch).Exec(ctx); err != nil {
			t.Fatalf("failed to insert fresh match: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", freshMatch.ID).Exec(ctx)
		})

		result := models.ResultPlayer1Win
		finishedMatch := &models.MatchRecord{
			Player1:         user1.ID,
			Player2:         user3.ID,
			Status:          models.StatusFinished,
			Result:          &result,
			LastHeartbeatAt: staleHeartbeat,
		}
		if _, err := testDB.NewInsert().Model(finishedMatch).Exec(ctx); err != nil {
			t.Fatalf("failed to insert finished match: %v", err)
		}
		t.Cleanup(func() {
			_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", finishedMatch.ID).Exec(ctx)
		})

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
		var dbStale models.MatchRecord
		if err := testDB.NewSelect().Model(&dbStale).Where("id = ?", staleMatch.ID).Scan(ctx); err != nil {
			t.Fatalf("failed to query stale match: %v", err)
		}
		if dbStale.Status != models.StatusAbandoned {
			t.Errorf("db stale match status: got %v, want %v", dbStale.Status, models.StatusAbandoned)
		}

		var dbFresh models.MatchRecord
		if err := testDB.NewSelect().Model(&dbFresh).Where("id = ?", freshMatch.ID).Scan(ctx); err != nil {
			t.Fatalf("failed to query fresh match: %v", err)
		}
		if dbFresh.Status != models.StatusInProgress {
			t.Errorf("db fresh match status: got %v, want %v", dbFresh.Status, models.StatusInProgress)
		}

		var dbFinished models.MatchRecord
		if err := testDB.NewSelect().Model(&dbFinished).Where("id = ?", finishedMatch.ID).Scan(ctx); err != nil {
			t.Fatalf("failed to query finished match: %v", err)
		}
		if dbFinished.Status != models.StatusFinished {
			t.Errorf("db finished match status: got %v, want %v", dbFinished.Status, models.StatusFinished)
		}
	})
}

func TestUserActiveMatchGet_SweepsStaleMatch(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	staleHeartbeat := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Microsecond)
	staleMatch := &models.MatchRecord{
		Player1:         user1.ID,
		Player2:         user2.ID,
		Status:          models.StatusInProgress,
		LastHeartbeatAt: staleHeartbeat,
	}
	if _, err := testDB.NewInsert().Model(staleMatch).Exec(ctx); err != nil {
		t.Fatalf("failed to insert stale match: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", staleMatch.ID).Exec(ctx)
	})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/internal/users/%d/active-match", user1.ID), nil)
	req = req.WithContext(handlers.ContextWithUserID(req.Context(), user1.ID))
	rec := httptest.NewRecorder()

	handler.UserActiveMatchGet(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d (not found after sweep), got %d. Body: %s",
			http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var dbMatch models.MatchRecord
	if err := testDB.NewSelect().Model(&dbMatch).Where("id = ?", staleMatch.ID).Scan(ctx); err != nil {
		t.Fatalf("failed to query db match: %v", err)
	}
	if dbMatch.Status != models.StatusAbandoned {
		t.Errorf("expected match status to be swept to %v, got %v", models.StatusAbandoned, dbMatch.Status)
	}
}

func TestMatchCreate_SweepsStaleMatchAllowsNewMatch(t *testing.T) {
	ctx := context.Background()
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup1)
	testutil.RegisterUser(t, user1, testDB)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup2)
	testutil.RegisterUser(t, user2, testDB)

	user3, cleanup3 := testutil.MakeTestUser(t, testDB)
	t.Cleanup(cleanup3)
	testutil.RegisterUser(t, user3, testDB)

	handler := handlers.NewHandler(testDB, nil, nil, "", nil)

	staleHeartbeat := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Microsecond)
	staleMatch := &models.MatchRecord{
		Player1:         user1.ID,
		Player2:         user2.ID,
		Status:          models.StatusInProgress,
		LastHeartbeatAt: staleHeartbeat,
	}
	if _, err := testDB.NewInsert().Model(staleMatch).Exec(ctx); err != nil {
		t.Fatalf("failed to insert stale match: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", staleMatch.ID).Exec(ctx)
	})

	body, _ := json.Marshal(models.MatchCreateInput{
		Player1: user1.Username,
		Player2: user3.Username,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/matches", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.MatchCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil && resp.ID > 0 {
		t.Cleanup(func() {
			_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", resp.ID).Exec(ctx)
		})
	}

	var prior models.MatchRecord
	if err := testDB.NewSelect().Model(&prior).Where("id = ?", staleMatch.ID).Scan(ctx); err != nil {
		t.Fatalf("failed to query prior match: %v", err)
	}
	if prior.Status != models.StatusAbandoned {
		t.Errorf("expected prior match to be swept to %v, got %v", models.StatusAbandoned, prior.Status)
	}
}


