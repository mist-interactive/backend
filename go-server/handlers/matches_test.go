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
)

func TestMatchCreate_Integration(t *testing.T) {
	ctx := context.Background()

	//make test users, insert them into the DB, and create a match between them
	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, user1, testDB)
	t.Cleanup(cleanup1)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, user2, testDB)
	t.Cleanup(cleanup2)

	matchHandler := &handlers.MatchHandler{DB: testDB}

	t.Run("Successfully creates match", func(t *testing.T) {
		var createdMatchID int64
		t.Cleanup(testutil.CleanupMatchByID(t, testDB, &createdMatchID))
		payload := map[string]any{
			"player_one": user1.ID,
			"player_two": user2.ID,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/matches", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		matchHandler.MatchesCreate(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected status 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]int64
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response payload JSON: %v", err)
		}

		matchID := resp["id"]
		if matchID == 0 {
			t.Fatal("expected valid non-zero match ID in response body")
		}
		createdMatchID = matchID

		// Verify in DB
		match := new(models.MatchRecord)
		err := testDB.NewSelect().Model(match).Where("id = ?", matchID).Scan(ctx)
		if err != nil {
			t.Fatalf("failed to query created match from DB: %v", err)
		}

		if match.Player1 != user1.ID || match.Player2 != user2.ID {
			t.Errorf("match players mismatch: got (%d, %d), expected (%d, %d)", match.Player1, match.Player2, user1.ID, user2.ID)
		}
		if match.Status != models.StatusInProgress {
			t.Errorf("expected status %s, got %s", models.StatusInProgress, match.Status)
		}
	})

	t.Run("Fails when player tries to play themselves", func(t *testing.T) {
		payload := map[string]any{
			"player_one": user1.ID,
			"player_two": user1.ID,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/matches", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		matchHandler.MatchesCreate(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected status 409 Conflict, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMatchPatch_Integration(t *testing.T) {
	ctx := context.Background()

	user1, cleanup1 := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, user1, testDB)
	t.Cleanup(cleanup1)

	user2, cleanup2 := testutil.MakeTestUser(t, testDB)
	testutil.RegisterUser(t, user2, testDB)
	t.Cleanup(cleanup2)

	match, matchCleanup := testutil.MakeTestMatch(t, testDB, user1.ID, user2.ID)
	t.Cleanup(matchCleanup)

	matchHandler := &handlers.MatchHandler{DB: testDB}

	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/matches/{id}", matchHandler.MatchesPatch)

	t.Run("Successfully updates match state", func(t *testing.T) {
		payload := map[string]string{
			"result": "player1_win",
			"status": "finished",
		}
		body, _ := json.Marshal(payload)

		url := fmt.Sprintf("/api/matches/%d", match.ID)
		req := httptest.NewRequest(http.MethodPatch, url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204 No Content, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify update in DB
		updatedMatch := new(models.MatchRecord)
		err := testDB.NewSelect().Model(updatedMatch).Where("id = ?", match.ID).Scan(ctx)
		if err != nil {
			t.Fatalf("failed to fetch updated match from DB: %v", err)
		}

		if updatedMatch.Status != "finished" {
			t.Errorf("expected status 'finished', got '%s'", updatedMatch.Status)
		}
		if updatedMatch.Result == nil || string(*updatedMatch.Result) != "player1_win" {
			t.Errorf("expected result 'player1_win', got '%s'", *updatedMatch.Result)
		}
	})

	t.Run("Fails with invalid match ID path value", func(t *testing.T) {
		payload := map[string]string{
			"result": "player1_win",
			"status": "finished",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/api/matches/invalid_id", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
