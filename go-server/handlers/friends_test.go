package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"fmt"
	"net/http"
	"testing"

	"github.com/uptrace/bun"
)

// mockEventNotifier records dispatched real-time notifications in memory,
// allowing integration tests to verify event delivery without depending on the WebSocket Hub.
type mockEventNotifier struct {
	friendRequestsRecv []struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}
	friendResponsesRecv []struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}
	friendDeletedRecv []struct {
		targetUserID    int64
		friendshipID    int64
		deleterUsername string
	}
	mutualPresenceRecv []struct {
		userAID   int64
		userBID   int64
		usernameA string
		usernameB string
	}
	returnErr bool
}

//---- Helpers ----

func (m *mockEventNotifier) NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error {
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	m.friendRequestsRecv = append(m.friendRequestsRecv, struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}{targetUserID, item})
	return nil
}

func (m *mockEventNotifier) NotifyFriendResponse(targetUserID int64, item models.FriendshipItemResponse) error {
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	m.friendResponsesRecv = append(m.friendResponsesRecv, struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}{targetUserID, item})
	return nil
}

func (m *mockEventNotifier) NotifyFriendDeleted(targetUserID int64, friendshipID int64, deleterUsername string) error {
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	m.friendDeletedRecv = append(m.friendDeletedRecv, struct {
		targetUserID    int64
		friendshipID    int64
		deleterUsername string
	}{targetUserID, friendshipID, deleterUsername})
	return nil
}

func (m *mockEventNotifier) NotifyMutualPresence(userAID, userBID int64, usernameA, usernameB string) error {
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	m.mutualPresenceRecv = append(m.mutualPresenceRecv, struct {
		userAID   int64
		userBID   int64
		usernameA string
		usernameB string
	}{userAID, userBID, usernameA, usernameB})
	return nil
}

func (m *mockEventNotifier) MatchFinished(payload models.MatchFinishedPayload) error {
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	return nil
}

// setupFriendsTestRouter initializes an isolated test router mounting the protected friends endpoints
// (/api/protected/friends) guarded by JWT authentication.
func setupFriendsTestRouter(t *testing.T, notifier handlers.EventNotifier) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", notifier)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("POST /friends", h.FriendRequestPost)
	protected.HandleFunc("GET /friends", h.FriendsListGet)
	protected.HandleFunc("PATCH /friends/{id}", h.FriendRequestAnswer)
	protected.HandleFunc("DELETE /friends/{id}", h.FriendDelete)

	return h, mux, privateKey
}

// friendsTestEnv bundles the test HTTP router, authenticated test users, and mock notifier.
type friendsTestEnv struct {
	handler *handlers.Handler
	router  http.Handler
	privKey *rsa.PrivateKey
	mock    *mockEventNotifier
	users   []AuthUser
}

// setupFriendsTestEnv sets up an isolated test router, seeds n users with auth headers,
// registers automatic DB cleanup, and wires up an optional in-memory event notifier (or nil).
func setupFriendsTestEnv(t *testing.T, numUsers int, mock *mockEventNotifier) *friendsTestEnv {
	t.Helper()

	var notifier handlers.EventNotifier
	if mock != nil {
		notifier = mock
	}

	h, router, privKey := setupFriendsTestRouter(t, notifier)
	rawUsers := testutil.MakeNTestUsers(t, testDB, numUsers)
	cleanupFriendships(t, testutil.UserIDs(rawUsers))

	return &friendsTestEnv{
		handler: h,
		router:  router,
		privKey: privKey,
		mock:    mock,
		users:   makeAuthUsers(t, rawUsers, privKey),
	}
}

// cleanupFriendships registers a t.Cleanup callback that deletes all friendship records
// involving any of the specified user IDs to ensure database isolation between tests.
func cleanupFriendships(t *testing.T, ids []int64) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.Friendship)(nil)).
			Where("user_id IN (?) OR friend_id IN (?)", bun.List(ids), bun.List(ids)).
			Exec(context.Background())
	})
}

// createPendingFriendship sends a POST /api/protected/friends request to create a pending
// friend request from sender to target, returning the created friendship record ID.
func createPendingFriendship(t *testing.T, router http.Handler, senderAuth, targetUsername string) int64 {
	t.Helper()
	rec := doTestRequest(router, http.MethodPost, "/api/protected/friends", senderAuth, models.FriendRequest{Target: targetUsername})
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create pending friendship: %s", rec.Body.String())
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	resp = testutil.DecodeJSON[struct {
		ID int64 `json:"id"`
	}](t, rec)
	return resp.ID
}

// ---- Tests ----

// TestFriendRequestPost_Integration tests POST /api/protected/friends for sending friend requests,
// verifying successful creation with pending status, database persistence, and rejection of self-requests,
// non-existent targets, and duplicate or reverse-duplicate requests.
func TestFriendRequestPost_Integration(t *testing.T) {
	env := setupFriendsTestEnv(t, 2, nil)
	alice := env.users[0]
	bob := env.users[1]

	tests := []struct {
		name           string
		authHeader     string
		target         string
		expectedStatus int
		validate       func(t *testing.T)
	}{
		{
			name:           "Success: Send friend request to valid user",
			authHeader:     alice.Auth,
			target:         bob.Username,
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T) {
				t.Helper()
				f := new(models.Friendship)
				err := testDB.NewSelect().
					Model(f).
					Where("user_id = ? AND friend_id = ?", alice.ID, bob.ID).
					Scan(context.Background())
				if err != nil {
					t.Fatalf("friendship not found in database: %v", err)
				}
				if f.Status != models.StatusPending {
					t.Errorf("database status: got %s, want %s", f.Status, models.StatusPending)
				}
			},
		},
		{
			name:           "Failure: Cannot friend oneself",
			authHeader:     alice.Auth,
			target:         alice.Username,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Target user does not exist",
			authHeader:     alice.Auth,
			target:         "nonexistent_user_xyz",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Duplicate friend request",
			authHeader:     alice.Auth,
			target:         bob.Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "Failure: Reverse duplicate friend request",
			authHeader:     bob.Auth,
			target:         alice.Username,
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(env.router, http.MethodPost, "/api/protected/friends", tc.authHeader, models.FriendRequest{Target: tc.target})

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] status: got %d, want %d. Response: %q",
					tc.name, rec.Code, tc.expectedStatus, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t)
			}
		})
	}
}

// TestFriendsListGet_Integration tests GET /api/protected/friends for retrieving the caller's friend list,
// verifying empty list responses, correct is_incoming directional flags for pending requests,
// and reciprocal visibility once a friendship is accepted.
func TestFriendsListGet_Integration(t *testing.T) {
	env := setupFriendsTestEnv(t, 2, nil)
	alice := env.users[0]
	bob := env.users[1]

	var friendshipID int64

	tests := []struct {
		name           string
		authHeader     string
		setup          func(t *testing.T)
		expectedStatus int
		validate       func(t *testing.T, items []models.FriendshipItemResponse)
	}{
		{
			name:           "Success: Empty friends list for new user",
			authHeader:     alice.Auth,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 0 {
					t.Errorf("friends count: got %d, want 0", len(items))
				}
			},
		},
		{
			name:       "Success: Sender sees pending request as outgoing (is_incoming=false)",
			authHeader: alice.Auth,
			setup: func(t *testing.T) {
				t.Helper()
				friendshipID = createPendingFriendship(t, env.router, alice.Auth, bob.Username)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || items[0].IsIncoming || items[0].Status != models.StatusPending {
					t.Errorf("sender items: got %+v, want is_incoming=false, status=pending", items)
				}
			},
		},
		{
			name:           "Success: Recipient sees pending request as incoming (is_incoming=true)",
			authHeader:     bob.Auth,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || !items[0].IsIncoming || items[0].Status != models.StatusPending || items[0].Username != alice.Username {
					t.Errorf("recipient items: got %+v, want is_incoming=true, username=%s", items, alice.Username)
				}
			},
		},
		{
			name:       "Success: Both users see accepted friendship status after accepting",
			authHeader: alice.Auth,
			setup: func(t *testing.T) {
				t.Helper()
				path := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
				rec := doTestRequest(env.router, http.MethodPatch, path, bob.Auth, models.FriendRequestAnswer{Status: models.StatusAccepted})
				if rec.Code != http.StatusOK {
					t.Fatalf("failed to accept friendship: %s", rec.Body.String())
				}
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || items[0].Status != models.StatusAccepted {
					t.Errorf("items: got %+v, want status=accepted", items)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			rec := doTestRequest(env.router, http.MethodGet, "/api/protected/friends", tc.authHeader, nil)
			if rec.Code != tc.expectedStatus {
				t.Fatalf("[%s] status: got %d, want %d. Body: %s", tc.name, rec.Code, tc.expectedStatus, rec.Body.String())
			}
			items := testutil.DecodeJSON[[]models.FriendshipItemResponse](t, rec)
			if tc.validate != nil {
				tc.validate(t, items)
			}
		})
	}
}

// TestFriendRequestAnswer_Integration tests PATCH /api/protected/friends/{id} for responding to pending requests,
// verifying that only the target recipient can answer, invalid statuses and non-existent IDs are rejected,
// and accepted or blocked status updates persist correctly in the database.
func TestFriendRequestAnswer_Integration(t *testing.T) {
	env := setupFriendsTestEnv(t, 3, nil)
	alice := env.users[0]
	bob := env.users[1]
	charlie := env.users[2]

	friendshipID := createPendingFriendship(t, env.router, alice.Auth, bob.Username)
	friendshipID2 := createPendingFriendship(t, env.router, alice.Auth, charlie.Username)
	nonexistentFriendshipID := testutil.NonexistentID[models.Friendship](t, testDB)

	tests := []struct {
		name           string
		authHeader     string
		friendshipID   int64
		statusAnswer   models.FriendshipStatus
		expectedStatus int
	}{
		{
			name:           "Failure: Sender cannot accept their own outgoing request",
			authHeader:     alice.Auth,
			friendshipID:   friendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Invalid status option",
			authHeader:     bob.Auth,
			friendshipID:   friendshipID,
			statusAnswer:   "invalid_status",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Non-existent friendship ID",
			authHeader:     bob.Auth,
			friendshipID:   nonexistentFriendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Success: Recipient accepts pending request",
			authHeader:     bob.Auth,
			friendshipID:   friendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Success: Recipient blocks friendship",
			authHeader:     charlie.Auth,
			friendshipID:   friendshipID2,
			statusAnswer:   models.StatusBlocked,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/protected/friends/%d", tc.friendshipID)
			rec := doTestRequest(env.router, http.MethodPatch, path, tc.authHeader, models.FriendRequestAnswer{Status: tc.statusAnswer})

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] status: got %d, want %d. Response: %q",
					tc.name, rec.Code, tc.expectedStatus, rec.Body.String())
			}
		})
	}

	// Verify friendship 1 marked as accepted in DB
	f1 := new(models.Friendship)
	if err := testDB.NewSelect().Model(f1).Where("id = ?", friendshipID).Scan(context.Background()); err != nil {
		t.Fatalf("failed to query friendship 1 from DB: %v", err)
	}
	if f1.Status != models.StatusAccepted {
		t.Errorf("friendship 1 DB status: got %s, want %s", f1.Status, models.StatusAccepted)
	}

	// Verify friendship 2 marked as blocked in DB
	f2 := new(models.Friendship)
	if err := testDB.NewSelect().Model(f2).Where("id = ?", friendshipID2).Scan(context.Background()); err != nil {
		t.Fatalf("failed to query friendship 2 from DB: %v", err)
	}
	if f2.Status != models.StatusBlocked {
		t.Errorf("friendship 2 DB status: got %s, want %s", f2.Status, models.StatusBlocked)
	}
}

// TestFriendDelete_Integration tests DELETE /api/protected/friends/{id} for removing friendships,
// verifying that non-participants cannot delete others' friendships, invalid non-numeric IDs return 400,
// and either participant (sender or recipient) can successfully delete the record.
func TestFriendDelete_Integration(t *testing.T) {
	env := setupFriendsTestEnv(t, 3, nil)
	alice := env.users[0]
	bob := env.users[1]
	charlie := env.users[2]

	friendshipID1 := createPendingFriendship(t, env.router, alice.Auth, bob.Username)
	friendshipID2 := createPendingFriendship(t, env.router, alice.Auth, charlie.Username)
	nonexistentFriendshipID := testutil.NonexistentID[models.Friendship](t, testDB)

	tests := []struct {
		name           string
		authHeader     string
		targetID       string
		expectedStatus int
		validate       func(t *testing.T)
	}{
		{
			name:           "Failure: Third party cannot delete friendship",
			authHeader:     bob.Auth,
			targetID:       fmt.Sprintf("%d", friendshipID2),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Delete non-existent friendship ID",
			authHeader:     alice.Auth,
			targetID:       fmt.Sprintf("%d", nonexistentFriendshipID),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Non-numeric friendship ID returns bad request",
			authHeader:     alice.Auth,
			targetID:       "not-a-number",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Success: Sender participant deletes friendship",
			authHeader:     alice.Auth,
			targetID:       fmt.Sprintf("%d", friendshipID1),
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T) {
				t.Helper()
				exists, err := testDB.NewSelect().
					Model((*models.Friendship)(nil)).
					Where("id = ?", friendshipID1).
					Exists(context.Background())
				if err != nil {
					t.Fatalf("error querying DB: %v", err)
				}
				if exists {
					t.Errorf("expected friendship %d to be deleted from DB", friendshipID1)
				}
			},
		},
		{
			name:           "Success: Recipient participant deletes friendship",
			authHeader:     charlie.Auth,
			targetID:       fmt.Sprintf("%d", friendshipID2),
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T) {
				t.Helper()
				exists, err := testDB.NewSelect().
					Model((*models.Friendship)(nil)).
					Where("id = ?", friendshipID2).
					Exists(context.Background())
				if err != nil {
					t.Fatalf("error querying DB: %v", err)
				}
				if exists {
					t.Errorf("expected friendship %d to be deleted from DB", friendshipID2)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/protected/friends/%s", tc.targetID)
			rec := doTestRequest(env.router, http.MethodDelete, path, tc.authHeader, nil)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] status: got %d, want %d. Response: %s",
					tc.name, rec.Code, tc.expectedStatus, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t)
			}
		})
	}
}

// TestFriendRequestPost_DispatchesNotification verifies that POST /api/protected/friends dispatches
// a TypeFriendRequestRecv real-time event notification to the target recipient.
func TestFriendRequestPost_DispatchesNotification(t *testing.T) {
	mock := &mockEventNotifier{}
	env := setupFriendsTestEnv(t, 2, mock)
	alice := env.users[0]
	bob := env.users[1]

	rec := doTestRequest(env.router, http.MethodPost, "/api/protected/friends", alice.Auth, models.FriendRequest{Target: bob.Username})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 Created: %s", rec.Code, rec.Body.String())
	}

	if len(mock.friendRequestsRecv) != 1 {
		t.Fatalf("notifications count: got %d, want 1", len(mock.friendRequestsRecv))
	}
	n := mock.friendRequestsRecv[0]
	if n.targetUserID != bob.ID {
		t.Errorf("targetUserID: got %d, want %d", n.targetUserID, bob.ID)
	}
	if n.item.UserID != alice.ID || n.item.Username != alice.Username {
		t.Errorf("sender info: got (id=%d username=%s), want (id=%d username=%s)",
			n.item.UserID, n.item.Username, alice.ID, alice.Username)
	}
	if n.item.Status != models.StatusPending || !n.item.IsIncoming {
		t.Errorf("payload status/incoming: got status=%s is_incoming=%v, want status=pending is_incoming=true",
			n.item.Status, n.item.IsIncoming)
	}
}

// TestFriendRequestAnswer_DispatchesNotification verifies that PATCH /api/protected/friends/{id} dispatches
// a TypeFriendRequestResponse real-time event notification to the original requester.
func TestFriendRequestAnswer_DispatchesNotification(t *testing.T) {
	mock := &mockEventNotifier{}
	env := setupFriendsTestEnv(t, 2, mock)
	alice := env.users[0]
	bob := env.users[1]

	friendshipID := createPendingFriendship(t, env.router, alice.Auth, bob.Username)

	// Clear out any notification recorded during creation
	mock.friendRequestsRecv = nil
	mock.friendResponsesRecv = nil
	mock.mutualPresenceRecv = nil

	path := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
	rec := doTestRequest(env.router, http.MethodPatch, path, bob.Auth, models.FriendRequestAnswer{Status: models.StatusAccepted})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 OK: %s", rec.Code, rec.Body.String())
	}

	if len(mock.friendResponsesRecv) != 1 {
		t.Fatalf("responses count: got %d, want 1", len(mock.friendResponsesRecv))
	}
	r := mock.friendResponsesRecv[0]
	if r.targetUserID != alice.ID {
		t.Errorf("targetUserID: got %d, want %d", r.targetUserID, alice.ID)
	}
	if r.item.UserID != bob.ID || r.item.Username != bob.Username {
		t.Errorf("responder info: got (id=%d username=%s), want (id=%d username=%s)",
			r.item.UserID, r.item.Username, bob.ID, bob.Username)
	}
	if r.item.Status != models.StatusAccepted || r.item.IsIncoming {
		t.Errorf("payload status/incoming: got status=%s is_incoming=%v, want status=accepted is_incoming=false",
			r.item.Status, r.item.IsIncoming)
	}

	if len(mock.mutualPresenceRecv) != 1 {
		t.Fatalf("mutual presence count: got %d, want 1", len(mock.mutualPresenceRecv))
	}
	mp := mock.mutualPresenceRecv[0]
	if mp.userAID != alice.ID || mp.userBID != bob.ID || mp.usernameA != alice.Username || mp.usernameB != bob.Username {
		t.Errorf("mutual presence: got (userAID=%d, userBID=%d, userA=%s, userB=%s), want (userAID=%d, userBID=%d, userA=%s, userB=%s)",
			mp.userAID, mp.userBID, mp.usernameA, mp.usernameB, alice.ID, bob.ID, alice.Username, bob.Username)
	}
}

// TestFriendRequestAnswer_BlockedDoesNotDispatchPresence verifies that blocking a friend request
// dispatches a response event but does NOT dispatch mutual presence updates.
func TestFriendRequestAnswer_BlockedDoesNotDispatchPresence(t *testing.T) {
	mock := &mockEventNotifier{}
	env := setupFriendsTestEnv(t, 2, mock)
	alice := env.users[0]
	bob := env.users[1]

	friendshipID := createPendingFriendship(t, env.router, alice.Auth, bob.Username)

	mock.friendRequestsRecv = nil
	mock.friendResponsesRecv = nil
	mock.mutualPresenceRecv = nil

	path := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
	rec := doTestRequest(env.router, http.MethodPatch, path, bob.Auth, models.FriendRequestAnswer{Status: models.StatusBlocked})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 OK: %s", rec.Code, rec.Body.String())
	}

	if len(mock.friendResponsesRecv) != 1 {
		t.Fatalf("responses count: got %d, want 1", len(mock.friendResponsesRecv))
	}
	if len(mock.mutualPresenceRecv) != 0 {
		t.Errorf("mutual presence count: got %d, want 0 on blocked request", len(mock.mutualPresenceRecv))
	}
}

// TestFriendDelete_DispatchesNotification verifies that DELETE /api/protected/friends/{id} dispatches
// a TypeFriendDeleted real-time event notification to the other participant.
func TestFriendDelete_DispatchesNotification(t *testing.T) {
	mock := &mockEventNotifier{}
	env := setupFriendsTestEnv(t, 2, mock)
	alice := env.users[0]
	bob := env.users[1]

	friendshipID := createPendingFriendship(t, env.router, alice.Auth, bob.Username)

	// Clear out any notification recorded during creation
	mock.friendRequestsRecv = nil
	mock.friendDeletedRecv = nil

	// Bob (the recipient) deletes the friendship
	path := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
	rec := doTestRequest(env.router, http.MethodDelete, path, bob.Auth, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 No Content: %s", rec.Code, rec.Body.String())
	}

	if len(mock.friendDeletedRecv) != 1 {
		t.Fatalf("deletions count: got %d, want 1", len(mock.friendDeletedRecv))
	}
	d := mock.friendDeletedRecv[0]
	if d.targetUserID != alice.ID {
		t.Errorf("targetUserID: got %d, want %d", d.targetUserID, alice.ID)
	}
	if d.friendshipID != friendshipID {
		t.Errorf("friendshipID: got %d, want %d", d.friendshipID, friendshipID)
	}
	if d.deleterUsername != bob.Username {
		t.Errorf("username: got %q, want %q", d.deleterUsername, bob.Username)
	}
}

// TestFriendDelete_MarksUnreadMessagesAsRead verifies that deleting a friendship marks any
// pending unread messages between the participants as read, preventing phantom unread badges.
func TestFriendDelete_MarksUnreadMessagesAsRead(t *testing.T) {
	mock := &mockEventNotifier{}
	env := setupFriendsTestEnv(t, 2, mock)
	alice := env.users[0]
	bob := env.users[1]

	friendshipID := createPendingFriendship(t, env.router, alice.Auth, bob.Username)

	// Accept the friendship
	pathAccept := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
	recAccept := doTestRequest(env.router, http.MethodPatch, pathAccept, bob.Auth, map[string]string{"status": "accepted"})
	if recAccept.Code != http.StatusOK {
		t.Fatalf("accept status: got %d, want 200 OK: %s", recAccept.Code, recAccept.Body.String())
	}

	// Insert unread messages between alice and bob
	msg1 := &models.Message{
		SenderID:    alice.ID,
		RecipientID: bob.ID,
		Content:     "Hello Bob",
		IsRead:      false,
	}
	msg2 := &models.Message{
		SenderID:    bob.ID,
		RecipientID: alice.ID,
		Content:     "Hello Alice",
		IsRead:      false,
	}
	_, err := env.handler.DB.NewInsert().Model(msg1).Exec(context.Background())
	if err != nil {
		t.Fatalf("failed to insert msg1: %v", err)
	}
	_, err = env.handler.DB.NewInsert().Model(msg2).Exec(context.Background())
	if err != nil {
		t.Fatalf("failed to insert msg2: %v", err)
	}

	// Bob deletes the friendship
	pathDel := fmt.Sprintf("/api/protected/friends/%d", friendshipID)
	recDel := doTestRequest(env.router, http.MethodDelete, pathDel, bob.Auth, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("delete status: got %d, want 204 No Content: %s", recDel.Code, recDel.Body.String())
	}

	// Verify both messages are now marked as read
	var unreadCount int
	unreadCount, err = env.handler.DB.NewSelect().
		Model((*models.Message)(nil)).
		Where("((sender_id = ? AND recipient_id = ?) OR (sender_id = ? AND recipient_id = ?)) AND is_read = FALSE",
			alice.ID, bob.ID, bob.ID, alice.ID).
		Count(context.Background())
	if err != nil {
		t.Fatalf("failed to count unread messages: %v", err)
	}
	if unreadCount != 0 {
		t.Errorf("unread messages count: got %d, want 0", unreadCount)
	}
}

// TestFriendNotification_GracefulDegradation verifies that HTTP handlers degrade gracefully when the event notifier
// is nil or returns an error, ensuring real-time notification failures do not abort successful HTTP operations.
func TestFriendNotification_GracefulDegradation(t *testing.T) {
	tests := []struct {
		name string
		mock *mockEventNotifier
	}{
		{
			name: "Success: nil notifier does not break friend request or answer",
			mock: nil,
		},
		{
			name: "Success: erroring notifier does not break friend request or answer",
			mock: &mockEventNotifier{returnErr: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := setupFriendsTestEnv(t, 2, tc.mock)
			alice := env.users[0]
			bob := env.users[1]

			// 1. Friend request POST
			recPost := doTestRequest(env.router, http.MethodPost, "/api/protected/friends", alice.Auth, models.FriendRequest{Target: bob.Username})
			if recPost.Code != http.StatusCreated {
				t.Fatalf("expected 201 Created even with failing/nil notifier, got %d: %s", recPost.Code, recPost.Body.String())
			}

			var resp struct {
				ID int64 `json:"id"`
			}
			resp = testutil.DecodeJSON[struct {
				ID int64 `json:"id"`
			}](t, recPost)

			// 2. Friend request answer PATCH
			path := fmt.Sprintf("/api/protected/friends/%d", resp.ID)
			recPatch := doTestRequest(env.router, http.MethodPatch, path, bob.Auth, models.FriendRequestAnswer{Status: models.StatusAccepted})
			if recPatch.Code != http.StatusOK {
				t.Fatalf("expected 200 OK even with failing/nil notifier, got %d: %s", recPatch.Code, recPatch.Body.String())
			}
		})
	}
}
