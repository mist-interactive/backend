package handlers_test

import (
	"context"
	"crypto/rsa"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type mockEventNotifier struct {
	mu                  sync.Mutex
	friendRequestsRecv  []struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}
	friendResponsesRecv []struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}
	returnErr bool
}

func (m *mockEventNotifier) NotifyFriendRequest(targetUserID int64, item models.FriendshipItemResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.returnErr {
		return fmt.Errorf("mock notifier error")
	}
	m.friendResponsesRecv = append(m.friendResponsesRecv, struct {
		targetUserID int64
		item         models.FriendshipItemResponse
	}{targetUserID, item})
	return nil
}

// setupFriendsTestRouter initializes the handler and returns an http.Handler with JWTGuard mounted.
func setupFriendsTestRouter(t *testing.T, notifiers ...handlers.EventNotifier) (*handlers.Handler, http.Handler, *rsa.PrivateKey) {
	t.Helper()

	privateKey, publicKey := getTestKeys(t)
	var notifier handlers.EventNotifier
	if len(notifiers) > 0 {
		notifier = notifiers[0]
	}
	h := handlers.NewHandler(testDB, privateKey, publicKey, "", notifier)
	mux := http.NewServeMux()

	protected := handlers.NewGroup(mux, "/api/protected", h.JWTGuard)
	protected.HandleFunc("POST /friends", h.FriendRequestPost)
	protected.HandleFunc("GET /friends", h.FriendsListGet)
	protected.HandleFunc("PATCH /friends/{id}", h.FriendRequestAnswer)
	protected.HandleFunc("DELETE /friends/{id}", h.FriendDelete)

	return h, mux, privateKey
}

// createPendingFriendship creates a pending request between sender and target, returning the friendship ID.
func createPendingFriendship(t *testing.T, router http.Handler, senderAuth, targetUsername string) int64 {
	t.Helper()
	rec := doTestRequest(router, http.MethodPost, "/api/protected/friends", senderAuth, models.FriendRequest{Target: targetUsername})
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create pending friendship: %s", rec.Body.String())
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode friendship creation response: %v", err)
	}
	return resp.ID
}

// test cases for POST friends/
func TestFriendRequestPost_Integration(t *testing.T) {
	_, router, privKey := setupFriendsTestRouter(t)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	bobAuth := makeAuthHeader(t, bob, privKey)

	tests := []struct {
		name           string
		authHeader     string
		target         string
		expectedStatus int
		validate       func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:           "Success: Send friend request to valid user",
			authHeader:     aliceAuth,
			target:         bob.Username,
			expectedStatus: http.StatusCreated,
			validate: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var resp map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp["status"] != string(models.StatusPending) {
					t.Errorf("expected status pending, got %v", resp["status"])
				}

				// Verify database persistence
				f := new(models.Friendship)
				err := testDB.NewSelect().
					Model(f).
					Where("user_id = ? AND friend_id = ?", alice.ID, bob.ID).
					Scan(context.Background())
				if err != nil {
					t.Fatalf("friendship not found in database: %v", err)
				}
				if f.Status != models.StatusPending {
					t.Errorf("database status expected %s, got %s", models.StatusPending, f.Status)
				}
			},
		},
		{
			name:           "Failure: Cannot friend oneself",
			authHeader:     aliceAuth,
			target:         alice.Username,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Target user does not exist",
			authHeader:     aliceAuth,
			target:         "nonexistent_user_xyz",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Duplicate friend request",
			authHeader:     aliceAuth,
			target:         bob.Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "Failure: Reverse duplicate friend request",
			authHeader:     bobAuth,
			target:         alice.Username,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "Failure: Unauthorized when missing token",
			authHeader:     "",
			target:         bob.Username,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodPost, "/api/protected/friends", tc.authHeader, models.FriendRequest{Target: tc.target})

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t, rec)
			}
		})
	}
}

// test cases for GET friends/
func TestFriendsListGet_Integration(t *testing.T) {
	_, router, privKey := setupFriendsTestRouter(t)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	bobAuth := makeAuthHeader(t, bob, privKey)

	var friendshipID int64

	tests := []struct {
		name           string
		authHeader     string
		setup          func(t *testing.T)
		expectedStatus int
		validate       func(t *testing.T, items []models.FriendshipItemResponse)
	}{
		{
			name:           "Empty friends list for new user",
			authHeader:     aliceAuth,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 0 {
					t.Errorf("expected 0 friends, got %d", len(items))
				}
			},
		},
		{
			name:       "Sender sees pending request as outgoing (is_incoming=false)",
			authHeader: aliceAuth,
			setup: func(t *testing.T) {
				t.Helper()
				friendshipID = createPendingFriendship(t, router, aliceAuth, bob.Username)
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || items[0].IsIncoming || items[0].Status != models.StatusPending {
					t.Errorf("unexpected sender items: %+v", items)
				}
			},
		},
		{
			name:           "Recipient sees pending request as incoming (is_incoming=true)",
			authHeader:     bobAuth,
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || !items[0].IsIncoming || items[0].Status != models.StatusPending || items[0].Username != alice.Username {
					t.Errorf("unexpected recipient items: %+v", items)
				}
			},
		},
		{
			name:       "Both users see accepted friendship status after accepting",
			authHeader: aliceAuth,
			setup: func(t *testing.T) {
				t.Helper()
				rec := doTestRequest(router, http.MethodPatch, fmt.Sprintf("/api/protected/friends/%d", friendshipID), bobAuth, models.FriendRequestAnswer{Status: models.StatusAccepted})
				if rec.Code != http.StatusOK {
					t.Fatalf("failed to accept friendship: %s", rec.Body.String())
				}
			},
			expectedStatus: http.StatusOK,
			validate: func(t *testing.T, items []models.FriendshipItemResponse) {
				t.Helper()
				if len(items) != 1 || items[0].Status != models.StatusAccepted {
					t.Errorf("expected accepted status, got %+v", items)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			rec := doTestRequest(router, http.MethodGet, "/api/protected/friends", tc.authHeader, nil)
			if rec.Code != tc.expectedStatus {
				t.Fatalf("[%s] expected status %d, got %d. Body: %s", tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}
			var items []models.FriendshipItemResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}
			if tc.validate != nil {
				tc.validate(t, items)
			}
		})
	}
}

// test answering a friends request (PATCH friends/{id})
func TestFriendRequestAnswer_Integration(t *testing.T) {
	_, router, privKey := setupFriendsTestRouter(t)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	bobAuth := makeAuthHeader(t, bob, privKey)

	friendshipID := createPendingFriendship(t, router, aliceAuth, bob.Username)

	tests := []struct {
		name           string
		authHeader     string
		friendshipID   int64
		statusAnswer   models.FriendshipStatus
		expectedStatus int
	}{
		{
			name:           "Failure: Sender cannot accept their own outgoing request",
			authHeader:     aliceAuth,
			friendshipID:   friendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Invalid status option",
			authHeader:     bobAuth,
			friendshipID:   friendshipID,
			statusAnswer:   "invalid_status",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Failure: Non-existent friendship ID",
			authHeader:     bobAuth,
			friendshipID:   999999999,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Success: Recipient accepts pending request",
			authHeader:     bobAuth,
			friendshipID:   friendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/protected/friends/%d", tc.friendshipID)
			rec := doTestRequest(router, http.MethodPatch, path, tc.authHeader, models.FriendRequestAnswer{Status: tc.statusAnswer})

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Response: %q",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}
		})
	}

	// Verify friendship as accepted in the database
	f := new(models.Friendship)
	err := testDB.NewSelect().Model(f).Where("id = ?", friendshipID).Scan(context.Background())
	if err != nil {
		t.Fatalf("failed to query friendship from DB: %v", err)
	}
	if f.Status != models.StatusAccepted {
		t.Errorf("expected DB status %s, got %s", models.StatusAccepted, f.Status)
	}
}

// test cases for DELETE friends/{id}
func TestFriendDelete_Integration(t *testing.T) {
	_, router, privKey := setupFriendsTestRouter(t)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)
	charlie := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	charlieAuth := makeAuthHeader(t, charlie, privKey)

	friendshipID := createPendingFriendship(t, router, aliceAuth, bob.Username)

	tests := []struct {
		name           string
		authHeader     string
		targetID       int64
		expectedStatus int
		validate       func(t *testing.T)
	}{
		{
			name:           "Failure: Third party cannot delete friendship",
			authHeader:     charlieAuth,
			targetID:       friendshipID,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Failure: Delete non-existent friendship ID",
			authHeader:     aliceAuth,
			targetID:       999999999,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "Success: Participant deletes friendship",
			authHeader:     aliceAuth,
			targetID:       friendshipID,
			expectedStatus: http.StatusNoContent,
			validate: func(t *testing.T) {
				t.Helper()
				exists, err := testDB.NewSelect().
					Model((*models.Friendship)(nil)).
					Where("id = ?", friendshipID).
					Exists(context.Background())
				if err != nil {
					t.Fatalf("error querying DB: %v", err)
				}
				if exists {
					t.Errorf("expected friendship %d to be deleted from DB, but it still exists", friendshipID)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/protected/friends/%d", tc.targetID)
			rec := doTestRequest(router, http.MethodDelete, path, tc.authHeader, nil)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d. Response: %s",
					tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}

			if tc.validate != nil {
				tc.validate(t)
			}
		})
	}
}

func TestFriendRequestPost_Notification(t *testing.T) {
	mock := &mockEventNotifier{}
	_, router, privKey := setupFriendsTestRouter(t, mock)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)

	tests := []struct {
		name           string
		authHeader     string
		targetUsername string
		expectedStatus int
		expectNotify   bool
	}{
		{
			name:           "Success: Dispatches notification to target user with sender profile",
			authHeader:     aliceAuth,
			targetUsername: bob.Username,
			expectedStatus: http.StatusCreated,
			expectNotify:   true,
		},
		{
			name:           "Failure: Self-request does not dispatch notification",
			authHeader:     aliceAuth,
			targetUsername: alice.Username,
			expectedStatus: http.StatusBadRequest,
			expectNotify:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock.mu.Lock()
			mock.friendRequestsRecv = nil
			mock.mu.Unlock()

			rec := doTestRequest(router, http.MethodPost, "/api/protected/friends", tc.authHeader, models.FriendRequest{Target: tc.targetUsername})
			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			mock.mu.Lock()
			notifications := append([]struct {
				targetUserID int64
				item         models.FriendshipItemResponse
			}(nil), mock.friendRequestsRecv...)
			mock.mu.Unlock()

			if tc.expectNotify {
				if len(notifications) != 1 {
					t.Fatalf("expected 1 notification, got %d", len(notifications))
				}
				n := notifications[0]
				if n.targetUserID != bob.ID {
					t.Errorf("got targetUserID %d, want %d", n.targetUserID, bob.ID)
				}
				if n.item.UserID != alice.ID {
					t.Errorf("got sender user_id %d, want %d", n.item.UserID, alice.ID)
				}
				if n.item.Username != alice.Username {
					t.Errorf("got sender username %s, want %s", n.item.Username, alice.Username)
				}
				if n.item.Status != models.StatusPending {
					t.Errorf("got status %s, want %s", n.item.Status, models.StatusPending)
				}
				if !n.item.IsIncoming {
					t.Errorf("got is_incoming %v, want true", n.item.IsIncoming)
				}
			} else {
				if len(notifications) != 0 {
					t.Errorf("expected 0 notifications, got %d", len(notifications))
				}
			}
		})
	}
}

func TestFriendRequestAnswer_Notification(t *testing.T) {
	mock := &mockEventNotifier{}
	_, router, privKey := setupFriendsTestRouter(t, mock)

	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	aliceAuth := makeAuthHeader(t, alice, privKey)
	bobAuth := makeAuthHeader(t, bob, privKey)

	friendshipID := createPendingFriendship(t, router, aliceAuth, bob.Username)

	tests := []struct {
		name           string
		authHeader     string
		friendshipID   int64
		statusAnswer   models.FriendshipStatus
		expectedStatus int
		expectNotify   bool
	}{
		{
			name:           "Success: Recipient accepts request and dispatches response notification to requester",
			authHeader:     bobAuth,
			friendshipID:   friendshipID,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusOK,
			expectNotify:   true,
		},
		{
			name:           "Failure: Non-existent friendship does not dispatch notification",
			authHeader:     bobAuth,
			friendshipID:   999999999,
			statusAnswer:   models.StatusAccepted,
			expectedStatus: http.StatusNotFound,
			expectNotify:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock.mu.Lock()
			mock.friendResponsesRecv = nil
			mock.mu.Unlock()

			path := fmt.Sprintf("/api/protected/friends/%d", tc.friendshipID)
			rec := doTestRequest(router, http.MethodPatch, path, tc.authHeader, models.FriendRequestAnswer{Status: tc.statusAnswer})
			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tc.expectedStatus, rec.Code, rec.Body.String())
			}

			mock.mu.Lock()
			notifications := append([]struct {
				targetUserID int64
				item         models.FriendshipItemResponse
			}(nil), mock.friendResponsesRecv...)
			mock.mu.Unlock()

			if tc.expectNotify {
				if len(notifications) != 1 {
					t.Fatalf("expected 1 notification, got %d", len(notifications))
				}
				n := notifications[0]
				if n.targetUserID != alice.ID {
					t.Errorf("got targetUserID %d, want %d", n.targetUserID, alice.ID)
				}
				if n.item.UserID != bob.ID {
					t.Errorf("got responder user_id %d, want %d", n.item.UserID, bob.ID)
				}
				if n.item.Username != bob.Username {
					t.Errorf("got responder username %s, want %s", n.item.Username, bob.Username)
				}
				if n.item.Status != models.StatusAccepted {
					t.Errorf("got status %s, want %s", n.item.Status, models.StatusAccepted)
				}
				if n.item.IsIncoming {
					t.Errorf("got is_incoming %v, want false", n.item.IsIncoming)
				}
			} else {
				if len(notifications) != 0 {
					t.Errorf("expected 0 notifications, got %d", len(notifications))
				}
			}
		})
	}
}

func TestFriendNotification_GracefulDegradation(t *testing.T) {
	alice := testutil.CreateRegisteredUser(t, testDB)
	bob := testutil.CreateRegisteredUser(t, testDB)

	tests := []struct {
		name     string
		notifier handlers.EventNotifier
	}{
		{
			name:     "Success: nil notifier does not break friend request or answer",
			notifier: nil,
		},
		{
			name:     "Success: erroring notifier does not break friend request or answer",
			notifier: &mockEventNotifier{returnErr: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, router, privKey := setupFriendsTestRouter(t, tc.notifier)
			aliceAuth := makeAuthHeader(t, alice, privKey)
			bobAuth := makeAuthHeader(t, bob, privKey)

			// 1. Friend request POST
			recPost := doTestRequest(router, http.MethodPost, "/api/protected/friends", aliceAuth, models.FriendRequest{Target: bob.Username})
			if recPost.Code != http.StatusCreated {
				t.Fatalf("expected 201 Created even with failing/nil notifier, got %d: %s", recPost.Code, recPost.Body.String())
			}

			var resp struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(recPost.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			// 2. Friend request answer PATCH
			path := fmt.Sprintf("/api/protected/friends/%d", resp.ID)
			recPatch := doTestRequest(router, http.MethodPatch, path, bobAuth, models.FriendRequestAnswer{Status: models.StatusAccepted})
			if recPatch.Code != http.StatusOK {
				t.Fatalf("expected 200 OK even with failing/nil notifier, got %d: %s", recPatch.Code, recPatch.Body.String())
			}

			// Clean up friendship
			t.Cleanup(func() {
				_, _ = testDB.NewDelete().Model((*models.Friendship)(nil)).Where("id = ?", resp.ID).Exec(context.Background())
			})
		})
	}
}

