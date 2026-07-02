package handlers_test

import (
	"context"
	"crypto/rand"
	"dbBackend/db"
	"dbBackend/models"
	"encoding/hex"
	"fmt"
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

func makeTestUser(t *testing.T) *models.User {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var uniqueName string = ""
	maxAttempts := 3
	for range maxAttempts {
		candidate := fmt.Sprintf("test_user_%s", generateRandomString(8))
		exists, err := testDB.NewSelect().
			Model((*models.User)(nil)).
			Where("username = ?", candidate).
			Exists(ctx)
		if err != nil {
			t.Fatalf("Error checking database while finding test username: %v", err)
		}
		if !exists {
			uniqueName = candidate
			break
		}
	}
	if uniqueName == "" {
		t.Fatal("Failed to generate unique username")
	}

	return &models.User{
		Username: uniqueName,
		Email:    uniqueName + "@testing.internal",
		PWHash:   "$2b$12$SX55NTDU0FL4DrpQm5kq.OLKcDrrMnS6siaY3Z80.8ki5zagqx08m",
	}
}

func registerUser(t *testing.T, u *models.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := testDB.NewInsert().Model(u).Exec(ctx)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}
}

func setCleanup(t *testing.T, u *models.User) {
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		_, err := testDB.NewDelete().
			Model((*models.User)(nil)).
			Where("username = ?", u.Username).
			Exec(cleanupCtx)
		if err != nil {
			t.Logf("Warning: Failed to clean up test user %s: %v", u.Username, err)
		}
	})
}

func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes)
}
