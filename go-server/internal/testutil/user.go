package testutil

import (
	"context"
	"crypto/rand"
	"dbBackend/models"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func MakeTestUser(t *testing.T, testDB *bun.DB) (*models.User, func()) {
	t.Helper()
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
		}, func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cleanupCancel()
			_, err := testDB.NewDelete().
				Model((*models.User)(nil)).
				Where("username = ?", uniqueName).
				Exec(cleanupCtx)
			if err != nil {
				t.Logf("Warning: Failed to clean up test user %s: %v", uniqueName, err)
			}
		}
}

func RegisterUser(t *testing.T, u *models.User, testDB *bun.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := testDB.NewInsert().Model(u).Exec(ctx)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}
}

// MakeNTestUsers generates and registers n unique test users in the database,
// automatically scheduling individual cleanup on test completion via t.Cleanup.
func MakeNTestUsers(t *testing.T, testDB *bun.DB, n int) []*models.User {
	t.Helper()
	users := make([]*models.User, n)
	for i := range n {
		u, cleanup := MakeTestUser(t, testDB)
		t.Cleanup(cleanup)
		RegisterUser(t, u, testDB)
		users[i] = u
	}
	return users
}

// UserIDs extracts a slice of user primary key IDs from a slice of User models.
func UserIDs(users []*models.User) []int64 {
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	return ids
}

func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(bytes)
}
