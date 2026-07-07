package testutil

import (
	"context"
	"dbBackend/models"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func CreateTestSession(t *testing.T, db *bun.DB, userID int64, token string) {
	ctx := context.Background()
	dbSession := &models.Session{
		SessionToken: token,
		UserID:       userID,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}

	if _, err := db.NewInsert().Model(dbSession).Exec(ctx); err != nil {
		t.Fatalf("testutil: failed to insert test session row: %v", err)
	}
}

func MakeTwoPlayers(t *testing.T, db *bun.DB) (*models.User, *models.User) {
	p1, p1Cleanup := MakeTestUser(t, db)
	RegisterUser(t, p1, db)
	t.Cleanup(p1Cleanup)

	p2, p2Cleanup := MakeTestUser(t, db)
	RegisterUser(t, p2, db)
	t.Cleanup(p2Cleanup)

	return p1, p2
}

func SeedMockMatches(t *testing.T, db *bun.DB, matches []models.MatchRecord) {
	ctx := context.Background()
	if _, err := db.NewInsert().Model(&matches).Exec(ctx); err != nil {
		t.Fatalf("testutil: failed to seed mock match records: %v", err)
	}
	matchIDs := make([]int64, len(matches))
	for i, m := range matches {
		matchIDs[i] = m.ID
	}
	t.Cleanup(func() {
		_, err := db.NewDelete().
			Model((*models.MatchRecord)(nil)).
			Where("id IN (?)", bun.List(matchIDs)).
			Exec(context.Background())
		if err != nil {
			t.Logf("teardown warning: failed to purge test matches: %v", err)
		}
	})
}
