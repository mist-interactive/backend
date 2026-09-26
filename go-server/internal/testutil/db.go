package testutil

import (
	"context"
	"dbBackend/db"
	"log"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func SetupTestDB() (*bun.DB, func()) {
	testDB, err := db.InitDB()
	if err != nil {
		log.Fatalf("Integration test suite failed to connect to database: %v", err)
	}

	migrationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.RunMigrations(migrationCtx, testDB); err != nil {
		cancel()
		log.Fatalf("Integration test suite failed to migrate database schema: %v", err)
	}
	cancel()

	cleanup := func() {
		testDB.Close()
	}
	return testDB, cleanup
}

// NonexistentID queries the maximum ID currently in the table for model T and returns max + 1.
// This is guaranteed not to exist in the database and works for any Bun model with an 'id' column.
func NonexistentID[T any](t testing.TB, db *bun.DB) int64 {
	t.Helper()
	var maxID int64
	err := db.NewSelect().
		Model((*T)(nil)).
		ColumnExpr("COALESCE(MAX(id), 0)").
		Scan(context.Background(), &maxID)
	if err != nil {
		t.Fatalf("failed to query nonexistent ID for %T: %v", new(T), err)
	}
	return maxID + 1
}

