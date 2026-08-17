package testutil

import (
	"context"
	"dbBackend/models"
	"testing"

	"github.com/uptrace/bun"
)

func MakeTestMatch(t *testing.T, db *bun.DB, player1ID, player2ID int64) (*models.MatchRecord, func()) {
	t.Helper()
	ctx := context.Background()

	match := &models.MatchRecord{
		Player1: player1ID,
		Player2: player2ID,
		Status:  models.StatusInProgress,
	}

	_, err := db.NewInsert().Model(match).Exec(ctx)
	if err != nil {
		t.Fatalf("failed to insert test match into DB: %v", err)
	}

	cleanup := func() {
		_, _ = db.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("id = ?", match.ID).
			Exec(context.Background())
	}

	return match, cleanup
}

func CleanupMatchByID(t *testing.T, db *bun.DB, matchID *int64) func() {
	return func() {
		if matchID == nil || *matchID == 0 {
			return
		}
		_, _ = db.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("id = ?", *matchID).
			Exec(context.Background())
	}
}
