package testutil

import (
	"context"
	"dbBackend/db"
	"log"
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
