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

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
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
				t.Cleanup(func() {
					_, _ = testDB.NewDelete().Model((*models.MatchRecord)(nil)).Where("id = ?", match.ID).Exec(ctx)
				})
			},
			player1:        users[0].Username,
			player2:        users[1].Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name: "Success: Allowed when prior match is finished",
			setup: func(t *testing.T) {
				result := models.ResultPlayer1Win
				finished := &models.MatchRecord{
					Player1: users[0].ID,
					Player2: users[1].ID,
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
			player1:        users[0].Username,
			player2:        users[1].Username,
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

func setupMatchesTestRouter(t *testing.T) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", nil)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /matches", h.MatchHistoryGet)

	return h, mux, privateKey
}

func TestMatchHistoryGet_Integration(t *testing.T) {
	ctx := context.Background()
	_, router, privKey := setupMatchesTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)

	userAuth := makeAuthHeader(t, users[0], privKey)
	unrelatedAuth := makeAuthHeader(t, users[2], privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("player_one IN (?) OR player_two IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	now := time.Now()

	// Seed matches for users[0] and users[1]:
	// Match 1: users[0] (p1) vs users[1] (p2), finished, users[0] win (5-2), 30 mins ago
	p1Score1, p2Score1 := 5, 2
	result1 := models.ResultPlayer1Win
	started1 := now.Add(-30 * time.Minute)
	finished1 := now.Add(-15 * time.Minute)
	m1 := &models.MatchRecord{
		Player1:      users[0].ID,
		Player2:      users[1].ID,
		Player1Score: &p1Score1,
		Player2Score: &p2Score1,
		Status:       models.StatusFinished,
		Result:       &result1,
		StartedAt:    started1,
		FinishedAt:   &finished1,
	}
	if _, err := testDB.NewInsert().Model(m1).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 1: %v", err)
	}

	// Match 2: users[1] (p1) vs users[0] (p2), finished, users[1] win (7-3) => users[0] loss, 20 mins ago
	p1Score2, p2Score2 := 7, 3
	result2 := models.ResultPlayer1Win
	started2 := now.Add(-20 * time.Minute)
	finished2 := now.Add(-5 * time.Minute)
	m2 := &models.MatchRecord{
		Player1:      users[1].ID,
		Player2:      users[0].ID,
		Player1Score: &p1Score2,
		Player2Score: &p2Score2,
		Status:       models.StatusFinished,
		Result:       &result2,
		StartedAt:    started2,
		FinishedAt:   &finished2,
	}
	if _, err := testDB.NewInsert().Model(m2).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 2: %v", err)
	}

	// Match 3: users[0] (p1) vs users[1] (p2), abandoned, aborted, 10 mins ago
	result3 := models.ResultAborted
	started3 := now.Add(-10 * time.Minute)
	finished3 := now.Add(-1 * time.Minute)
	m3 := &models.MatchRecord{
		Player1:    users[0].ID,
		Player2:    users[1].ID,
		Status:     models.StatusAbandoned,
		Result:     &result3,
		StartedAt:  started3,
		FinishedAt: &finished3,
	}
	if _, err := testDB.NewInsert().Model(m3).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 3: %v", err)
	}

	// Match 4: users[0] (p1) vs users[1] (p2), in_progress, started just now
	started4 := now
	m4 := &models.MatchRecord{
		Player1:   users[0].ID,
		Player2:   users[1].ID,
		Status:    models.StatusInProgress,
		StartedAt: started4,
	}
	if _, err := testDB.NewInsert().Model(m4).Exec(ctx); err != nil {
		t.Fatalf("failed to insert match 4: %v", err)
	}

	tests := []struct {
		name           string
		authHeader     string
		queryURL       string
		expectedStatus int
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:           "Failure: Missing Bearer token returns 401 Unauthorized",
			authHeader:     "",
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Failure: Invalid Bearer token returns 401 Unauthorized",
			authHeader:     "Bearer invalid.jwt.token",
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Success: Empty history returns empty JSON array",
			authHeader:     unrelatedAuth,
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 0 {
					t.Errorf("expected 0 matches, got %d", len(history))
				}
				if body := rec.Body.String(); body != "[]\n" && body != "[]" {
					t.Errorf("expected raw JSON '[]', got %q", body)
				}
			},
		},
		{
			name:           "Success: All matches ordered by started_at DESC with correct relative perspective",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 4 {
					t.Fatalf("expected 4 matches, got %d", len(history))
				}

				// Match 4: in_progress
				if history[0].ID != m4.ID {
					t.Errorf("expected match ID %d, got %d", m4.ID, history[0].ID)
				}
				if history[0].Status != models.StatusInProgress {
					t.Errorf("expected status %s, got %s", models.StatusInProgress, history[0].Status)
				}
				if history[0].Outcome != nil {
					t.Errorf("expected outcome nil, got %v", history[0].Outcome)
				}
				if history[0].UserScore != nil || history[0].OpponentScore != nil {
					t.Errorf("expected nil scores for in_progress match")
				}
				if history[0].OpponentUsername != users[1].Username {
					t.Errorf("expected opponent %s, got %s", users[1].Username, history[0].OpponentUsername)
				}

				// Match 3: aborted
				if history[1].ID != m3.ID {
					t.Errorf("expected match ID %d, got %d", m3.ID, history[1].ID)
				}
				if history[1].Outcome == nil || *history[1].Outcome != models.OutcomeAborted {
					t.Errorf("expected outcome %s, got %v", models.OutcomeAborted, history[1].Outcome)
				}
				if history[1].UserScore != nil || history[1].OpponentScore != nil {
					t.Errorf("expected nil scores for aborted match")
				}

				// Match 2: users[1] was player 1 (7), users[0] was player 2 (3) -> users[0] lost 3-7
				if history[2].ID != m2.ID {
					t.Errorf("expected match ID %d, got %d", m2.ID, history[2].ID)
				}
				if history[2].Outcome == nil || *history[2].Outcome != models.OutcomeLoss {
					t.Errorf("expected outcome %s, got %v", models.OutcomeLoss, history[2].Outcome)
				}
				if history[2].UserScore == nil || *history[2].UserScore != 3 {
					t.Errorf("expected user_score 3, got %v", history[2].UserScore)
				}
				if history[2].OpponentScore == nil || *history[2].OpponentScore != 7 {
					t.Errorf("expected opponent_score 7, got %v", history[2].OpponentScore)
				}

				// Match 1: users[0] was player 1 (5), users[1] was player 2 (2) -> users[0] won 5-2
				if history[3].ID != m1.ID {
					t.Errorf("expected match ID %d, got %d", m1.ID, history[3].ID)
				}
				if history[3].Outcome == nil || *history[3].Outcome != models.OutcomeWin {
					t.Errorf("expected outcome %s, got %v", models.OutcomeWin, history[3].Outcome)
				}
				if history[3].UserScore == nil || *history[3].UserScore != 5 {
					t.Errorf("expected user_score 5, got %v", history[3].UserScore)
				}
				if history[3].OpponentScore == nil || *history[3].OpponentScore != 2 {
					t.Errorf("expected opponent_score 2, got %v", history[3].OpponentScore)
				}
			},
		},
		{
			name:           "Success: Filter by status finished",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?status=finished",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 2 {
					t.Fatalf("expected 2 finished matches, got %d", len(history))
				}
				for _, m := range history {
					if m.Status != models.StatusFinished {
						t.Errorf("expected status %s, got %s", models.StatusFinished, m.Status)
					}
				}
			},
		},
		{
			name:           "Success: Filter by status abandoned",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?status=abandoned",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 1 {
					t.Fatalf("expected 1 abandoned match, got %d", len(history))
				}
				if history[0].ID != m3.ID {
					t.Errorf("expected match ID %d, got %d", m3.ID, history[0].ID)
				}
			},
		},
		{
			name:           "Success: Filter by status in_progress",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?status=in_progress",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 1 {
					t.Fatalf("expected 1 in_progress match, got %d", len(history))
				}
				if history[0].ID != m4.ID {
					t.Errorf("expected match ID %d, got %d", m4.ID, history[0].ID)
				}
			},
		},
		{
			name:           "Success: Pagination limit and offset",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?limit=1&offset=1",
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 1 {
					t.Fatalf("expected 1 match with limit=1, got %d", len(history))
				}
				// offset=1 should skip m4 and return m3
				if history[0].ID != m3.ID {
					t.Errorf("expected match ID %d, got %d", m3.ID, history[0].ID)
				}
			},
		},
		{
			name:           "Success: Query match history of another user by username reflects their perspective",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?username=" + users[1].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 4 {
					t.Fatalf("expected 4 matches, got %d", len(history))
				}

				// Match 2: users[1] was player 1 (7), users[0] was player 2 (3) -> from users[1]'s perspective: win 7-3
				if history[2].ID != m2.ID {
					t.Errorf("expected match ID %d, got %d", m2.ID, history[2].ID)
				}
				if history[2].OpponentUsername != users[0].Username {
					t.Errorf("expected opponent %s, got %s", users[0].Username, history[2].OpponentUsername)
				}
				if history[2].Outcome == nil || *history[2].Outcome != models.OutcomeWin {
					t.Errorf("expected outcome %s, got %v", models.OutcomeWin, history[2].Outcome)
				}
				if history[2].UserScore == nil || *history[2].UserScore != 7 {
					t.Errorf("expected user_score 7, got %v", history[2].UserScore)
				}
				if history[2].OpponentScore == nil || *history[2].OpponentScore != 3 {
					t.Errorf("expected opponent_score 3, got %v", history[2].OpponentScore)
				}

				// Match 1: users[0] was player 1 (5), users[1] was player 2 (2) -> from users[1]'s perspective: loss 2-5
				if history[3].ID != m1.ID {
					t.Errorf("expected match ID %d, got %d", m1.ID, history[3].ID)
				}
				if history[3].OpponentUsername != users[0].Username {
					t.Errorf("expected opponent %s, got %s", users[0].Username, history[3].OpponentUsername)
				}
				if history[3].Outcome == nil || *history[3].Outcome != models.OutcomeLoss {
					t.Errorf("expected outcome %s, got %v", models.OutcomeLoss, history[3].Outcome)
				}
				if history[3].UserScore == nil || *history[3].UserScore != 2 {
					t.Errorf("expected user_score 2, got %v", history[3].UserScore)
				}
				if history[3].OpponentScore == nil || *history[3].OpponentScore != 5 {
					t.Errorf("expected opponent_score 5, got %v", history[3].OpponentScore)
				}
			},
		},
		{
			name:           "Success: Query match history of existing user with zero matches returns empty JSON array",
			authHeader:     userAuth,
			queryURL:       "/api/protected/matches?username=" + users[2].Username,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var history []models.MatchHistoryResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if len(history) != 0 {
					t.Errorf("expected 0 matches, got %d", len(history))
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodGet, tc.queryURL, tc.authHeader, nil)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Body: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec)
			}
		})
	}
}

