package handlers_test

import (
	"context"
	"dbBackend/db"
	"log"
	"os"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

var testDB *bun.DB

func TestMain(m *testing.M) {
	var err error

	testDB, err = db.InitDB()
	if err != nil {
		log.Fatalf("Integration test suite failed to connect to database: %v", err)
	}

	migrationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.RunMigrations(migrationCtx, testDB); err != nil {
		cancel()
		log.Fatalf("Integration test suite failed to migrate database schema: %v", err)
	}
	cancel()

	exitCode := m.Run()
	testDB.Close()
	os.Exit(exitCode)
}
