package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// setupMessagesTestRouter mounts the messages endpoints for testing.
// Protected routes are guarded by JWTGuard; internal routes are mounted directly
// to focus strictly on endpoint business logic (per test scope).
func setupMessagesTestRouter(t *testing.T) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", nil)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("GET /messages/{friend_name}", h.MessagesGetHistory)
	protected.HandleFunc("PATCH /messages/{friend_name}/read", h.MessageSetRead)

	mux.HandleFunc("POST /api/internal/messages", h.MessageCreate)

	return h, mux, privateKey
}

// messagesTestEnv bundles the test HTTP router, RSA private key, and pre-authenticated test users.
type messagesTestEnv struct {
	handler *handlers.Handler
	router  http.Handler
	privKey *rsa.PrivateKey
	users   []AuthUser
}

// setupMessagesTestEnv initializes a clean test router and creates n authenticated test users,
// scheduling database teardown for both messages and users via t.Cleanup.
func setupMessagesTestEnv(t *testing.T, numUsers int) *messagesTestEnv {
	t.Helper()

	h, router, privKey := setupMessagesTestRouter(t)
	rawUsers := testutil.MakeNTestUsers(t, testDB, numUsers)
	ids := testutil.UserIDs(rawUsers)
	cleanupMessages(t, ids)

	return &messagesTestEnv{
		handler: h,
		router:  router,
		privKey: privKey,
		users:   makeAuthUsers(t, rawUsers, privKey),
	}
}

// cleanupMessages registers a t.Cleanup callback that deletes all message records
// involving any of the specified user IDs before user records are deleted.
func cleanupMessages(t *testing.T, ids []int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = testDB.NewDelete().
			Model((*models.Message)(nil)).
			Where("sender_id IN (?) OR recipient_id IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})
}

// seedTestMessage directly inserts a message record into the database for testing.
func seedTestMessage(t *testing.T, ctx context.Context, senderID, recipientID int64, content string, isRead bool) *models.Message {
	t.Helper()
	msg := &models.Message{
		SenderID:    senderID,
		RecipientID: recipientID,
		Content:     content,
		IsRead:      isRead,
	}

	if _, err := testDB.NewInsert().Model(msg).Returning("*").Exec(ctx); err != nil {
		t.Fatalf("failed to insert seed message: %v", err)
	}
	return msg
}

// sendTestRequest dispatches an HTTP request, supporting either structured JSON payloads
// or raw strings (useful for testing malformed JSON payloads).
func sendTestRequest(router http.Handler, method, path, auth string, payload any, raw string) *httptest.ResponseRecorder {
	if raw != "" {
		req := httptest.NewRequest(method, path, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	return doTestRequest(router, method, path, auth, payload)
}

// assertMessagePersisted verifies the returned created message response and confirms its database row.
func assertMessagePersisted(t *testing.T, ctx context.Context, rec *httptest.ResponseRecorder, senderID, recipientID int64, content string) {
	t.Helper()
	created := testutil.DecodeJSON[models.Message](t, rec)
	if created.ID == 0 {
		t.Errorf("expected non-zero message ID, got 0")
	}
	if created.SenderID != senderID {
		t.Errorf("SenderID: got %d, want %d", created.SenderID, senderID)
	}
	if created.RecipientID != recipientID {
		t.Errorf("RecipientID: got %d, want %d", created.RecipientID, recipientID)
	}
	if created.Content != content {
		t.Errorf("content: got %q, want %q", created.Content, content)
	}
	if created.IsRead {
		t.Errorf("expected IsRead to be false, got true")
	}

	var dbMsg models.Message
	if err := testDB.NewSelect().Model(&dbMsg).Where("id = ?", created.ID).Scan(ctx); err != nil {
		t.Fatalf("failed to query created message from database: %v", err)
	}
	if dbMsg.Content != content {
		t.Errorf("database content: got %q, want %q", dbMsg.Content, content)
	}
}

// assertMessageReadStates verifies the is_read boolean flags for the specified message IDs.
func assertMessageReadStates(t *testing.T, ctx context.Context, wantReadIDs, wantUnreadIDs []int64) {
	t.Helper()
	allIDs := append(append([]int64(nil), wantReadIDs...), wantUnreadIDs...)
	var msgs []models.Message
	if err := testDB.NewSelect().Model(&msgs).Where("id IN (?)", bun.List(allIDs)).Scan(ctx); err != nil {
		t.Fatalf("failed to query messages: %v", err)
	}

	statusMap := make(map[int64]bool, len(msgs))
	for _, m := range msgs {
		statusMap[m.ID] = m.IsRead
	}

	for _, id := range wantReadIDs {
		if !statusMap[id] {
			t.Errorf("message %d: got is_read=false, want true", id)
		}
	}
	for _, id := range wantUnreadIDs {
		if statusMap[id] {
			t.Errorf("message %d: got is_read=true, want false", id)
		}
	}
}

// assertMessagesResponse validates decoded message IDs or verifies that the response is an empty array.
func assertMessagesResponse(t *testing.T, rec *httptest.ResponseRecorder, wantIDs []int64, wantEmpty bool) {
	t.Helper()
	if wantEmpty {
		if trimmed := strings.TrimSpace(rec.Body.String()); trimmed != "[]" {
			t.Errorf("expected empty JSON array '[]', got %q", trimmed)
		}
		return
	}

	messages := testutil.DecodeJSON[[]models.Message](t, rec)
	if len(messages) != len(wantIDs) {
		t.Fatalf("expected %d messages, got %d", len(wantIDs), len(messages))
	}

	for i, wantID := range wantIDs {
		if messages[i].ID != wantID {
			t.Errorf("message[%d] ID: got %d, want %d", i, messages[i].ID, wantID)
		}
	}
}

// TestMessagesGetHistory verifies GET /api/protected/messages/{friend_name}.
func TestMessagesGetHistory(t *testing.T) {
	ctx := context.Background()
	env := setupMessagesTestEnv(t, 3)
	alice := env.users[0]
	bob := env.users[1]
	charlie := env.users[2]

	// Seed bilateral conversation between Alice and Bob
	m1 := seedTestMessage(t, ctx, alice.ID, bob.ID, "Hello Bob", false)
	m2 := seedTestMessage(t, ctx, bob.ID, alice.ID, "Hey Alice", false)
	m3 := seedTestMessage(t, ctx, alice.ID, bob.ID, "How are you doing?", false)

	// Seed separate conversation between Bob and Charlie (must not leak into Alice-Bob history)
	seedTestMessage(t, ctx, bob.ID, charlie.ID, "Bob and Charlie private message", false)

	nonexistentUser := testutil.NonexistentUsername(t, testDB)

	tests := []struct {
		name           string
		authUser       AuthUser
		friendUsername string
		expectedStatus int
		wantIDs        []int64
		wantEmptyArray bool
	}{
		{
			name:           "Success: Alice retrieves bilateral history with Bob chronologically",
			authUser:       alice,
			friendUsername: bob.Username,
			expectedStatus: http.StatusOK,
			wantIDs:        []int64{m1.ID, m2.ID, m3.ID},
		},
		{
			name:           "Success: Bob retrieves same bilateral history with Alice chronologically",
			authUser:       bob,
			friendUsername: alice.Username,
			expectedStatus: http.StatusOK,
			wantIDs:        []int64{m1.ID, m2.ID, m3.ID},
		},
		{
			name:           "Success: Empty history returns empty JSON array",
			authUser:       alice,
			friendUsername: charlie.Username,
			expectedStatus: http.StatusOK,
			wantEmptyArray: true,
		},
		{
			name:           "Failure: Non-existent friend username returns 400 Bad Request",
			authUser:       alice,
			friendUsername: nonexistentUser,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/protected/messages/" + tc.friendUsername
			rec := doTestRequest(env.router, http.MethodGet, path, tc.authUser.Auth, nil)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.expectedStatus == http.StatusOK {
				assertMessagesResponse(t, rec, tc.wantIDs, tc.wantEmptyArray)
			}
		})
	}
}

// TestMessageCreate verifies POST /api/internal/messages.
func TestMessageCreate(t *testing.T) {
	env := setupMessagesTestEnv(t, 2)
	alice := env.users[0]
	bob := env.users[1]
	nonexistentUser := testutil.NonexistentUsername(t, testDB)

	tests := []struct {
		name           string
		payload        any
		rawPayload     string
		expectedStatus int
		verifyDB       bool
	}{
		{
			name: "Success: Valid message persisted and returned",
			payload: models.MessageCreateInput{
				SenderID:  alice.ID,
				Recipient: bob.Username,
				Content:   "Hello through internal API",
			},
			expectedStatus: http.StatusCreated,
			verifyDB:       true,
		},
		{
			name: "Failure: Recipient user does not exist",
			payload: models.MessageCreateInput{
				SenderID:  alice.ID,
				Recipient: nonexistentUser,
				Content:   "Are you there?",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Missing sender_id",
			payload: models.MessageCreateInput{
				SenderID:  0,
				Recipient: bob.Username,
				Content:   "No sender",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Empty content",
			payload: models.MessageCreateInput{
				SenderID:  alice.ID,
				Recipient: bob.Username,
				Content:   "",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Recipient username too short (< 3 chars)",
			payload: models.MessageCreateInput{
				SenderID:  alice.ID,
				Recipient: "ab",
				Content:   "Valid content",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "Failure: Content exceeds 2000 characters limit",
			payload: models.MessageCreateInput{
				SenderID:  alice.ID,
				Recipient: bob.Username,
				Content:   strings.Repeat("A", 2001),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Malformed JSON body",
			rawPayload:     "not-a-valid-json-struct",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := sendTestRequest(env.router, http.MethodPost, "/api/internal/messages", "", tc.payload, tc.rawPayload)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.verifyDB {
				assertMessagePersisted(t, context.Background(), rec, alice.ID, bob.ID, "Hello through internal API")
			}
		})
	}
}

// TestMessageSetRead verifies PATCH /api/protected/messages/{friend_name}/read.
func TestMessageSetRead(t *testing.T) {
	ctx := context.Background()
	env := setupMessagesTestEnv(t, 2)
	alice := env.users[0]
	bob := env.users[1]

	// Bob sends 3 messages to Alice (Alice is recipient, all unread)
	m1 := seedTestMessage(t, ctx, bob.ID, alice.ID, "Unread 1", false)
	m2 := seedTestMessage(t, ctx, bob.ID, alice.ID, "Unread 2", false)
	m3 := seedTestMessage(t, ctx, bob.ID, alice.ID, "Unread 3", false)

	// Alice sends a message to Bob (Alice is sender, unread)
	m4 := seedTestMessage(t, ctx, alice.ID, bob.ID, "Outgoing from Alice", false)

	nonexistentUser := testutil.NonexistentUsername(t, testDB)

	tests := []struct {
		name           string
		authUser       AuthUser
		friendUsername string
		payload        any
		rawPayload     string
		expectedStatus int
		verifyEffects  func(t *testing.T)
	}{
		{
			name:           "Success: Alice marks Bob's messages as read up to m2",
			authUser:       alice,
			friendUsername: bob.Username,
			payload:        models.MessageSetReadInput{ReadUpTo: m2.ID},
			expectedStatus: http.StatusNoContent,
			verifyEffects: func(t *testing.T) {
				assertMessageReadStates(t, ctx, []int64{m1.ID, m2.ID}, []int64{m3.ID, m4.ID})
			},
		},
		{
			name:           "Success: Idempotent when called again with same ReadUpTo",
			authUser:       alice,
			friendUsername: bob.Username,
			payload:        models.MessageSetReadInput{ReadUpTo: m2.ID},
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "Failure: Friend username not found",
			authUser:       alice,
			friendUsername: nonexistentUser,
			payload:        models.MessageSetReadInput{ReadUpTo: m2.ID},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Missing read_up_to field (zero value fails required validation)",
			authUser:       alice,
			friendUsername: bob.Username,
			payload:        models.MessageSetReadInput{ReadUpTo: 0},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Malformed JSON payload",
			authUser:       alice,
			friendUsername: bob.Username,
			rawPayload:     "not-a-valid-json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/protected/messages/" + tc.friendUsername + "/read"
			rec := sendTestRequest(env.router, http.MethodPatch, path, tc.authUser.Auth, tc.payload, tc.rawPayload)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.verifyEffects != nil {
				tc.verifyEffects(t)
			}
		})
	}
}
