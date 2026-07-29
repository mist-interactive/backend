package models_test

import (
	"context"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSessionDatabaseLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tests := []struct {
		name        string
		setup       func(t *testing.T) *models.Session
		expectError bool
		validate    func(t *testing.T, token string)
	}{
		{
			name: "Success: Insert valid session and fetch user relation",
			setup: func(t *testing.T) *models.Session {
				u, cleanup := testutil.MakeTestUser(t, testDB)
				t.Cleanup(cleanup)
				testutil.RegisterUser(t, u, testDB)
				return &models.Session{
					UserID:       u.ID,
					SessionToken: "valid_session_token_123",
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}
			},
			expectError: false,
			validate: func(t *testing.T, token string) {
				fetched := new(models.Session)
				err := testDB.NewSelect().
					Model(fetched).
					Relation("User").
					Where("session_token = ?", token).
					Scan(ctx)
				assert.NoError(t, err)
				assert.NotNil(t, fetched.User)
				assert.NotEmpty(t, fetched.User.Username)
			},
		},
		{
			name: "Failure: Foreign key violation with non-existent user ID",
			setup: func(t *testing.T) *models.Session {
				return &models.Session{
					UserID:       math.MaxInt64,
					SessionToken: "invalid_user_session",
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}
			},
			expectError: true,
			validate:    nil,
		},
		{
			name: "Failure: Unique constraint violation on duplicate token",
			setup: func(t *testing.T) *models.Session {
				u, cleanup := testutil.MakeTestUser(t, testDB)
				t.Cleanup(cleanup)
				testutil.RegisterUser(t, u, testDB)
				token := "clashing_token_abc"
				preExisting := &models.Session{
					UserID:       u.ID,
					SessionToken: token,
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}
				_, err := testDB.NewInsert().Model(preExisting).Exec(ctx)
				assert.NoError(t, err)

				return &models.Session{
					UserID:       u.ID,
					SessionToken: token,
					ExpiresAt:    time.Now().Add(1 * time.Hour),
				}
			},
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := tt.setup(t)
			_, err := testDB.NewInsert().Model(session).Exec(ctx)
			if tt.expectError {
				assert.Error(t, err, "Expected database constraints to block this write operation")
			} else {
				assert.NoError(t, err, "Expected database write to compile successfully")
				if tt.validate != nil {
					tt.validate(t, session.SessionToken)
				}
			}
		})
	}
}
