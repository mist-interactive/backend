package handlers_test

import (
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"fmt"
	"net/http"
	"testing"

	"github.com/uptrace/bun"
)

func TestProfileCommentsGet(t *testing.T) {
	ctx := context.Background()
	privKey, pubKey := getTestKeys(t)
	router := http.NewServeMux()
	handlers.NewHandler(testDB, privKey, pubKey, "", nil).RegisterRoutes(router)

	users := testutil.MakeNTestUsers(t, testDB, 3)
	ids := testutil.UserIDs(users)
	userAuth := makeAuthHeader(t, users[1], privKey)

	t.Cleanup(func() {
		_, _ = testDB.NewDelete().
			Model((*models.Comment)(nil)).
			Where("owner_id IN (?) OR poster_id IN (?)", bun.List(ids), bun.List(ids)).
			Exec(ctx)
	})

	// Seed comments (c1, c2, c3 in insertion order)
	c1 := &models.Comment{OwnerID: users[0].ID, PosterID: users[1].ID, Content: "First comment"}
	c2 := &models.Comment{OwnerID: users[0].ID, PosterID: users[2].ID, Content: "Second comment"}
	c3 := &models.Comment{OwnerID: users[0].ID, PosterID: users[1].ID, Content: "Third comment"}
	for _, c := range []*models.Comment{c1, c2, c3} {
		if _, err := testDB.NewInsert().Model(c).Exec(ctx); err != nil {
			t.Fatalf("failed to insert seed comment: %v", err)
		}
	}

	userMap := map[int64]string{users[0].ID: users[0].Username, users[1].ID: users[1].Username, users[2].ID: users[2].Username}

	assertComments := func(t *testing.T, resp models.PaginatedCommentsResponse, wantHasMore bool, want ...*models.Comment) {
		t.Helper()
		if resp.HasMore != wantHasMore {
			t.Errorf("has_more: got %v, want %v", resp.HasMore, wantHasMore)
		}
		if len(resp.Comments) != len(want) {
			t.Fatalf("comments count: got %d, want %d", len(resp.Comments), len(want))
		}
		for i, w := range want {
			got := resp.Comments[i]
			if got.ID != w.ID || got.Content != w.Content || got.PosterID != w.PosterID {
				t.Errorf("comment[%d]: got (id=%d content=%q poster_id=%d), want (id=%d content=%q poster_id=%d)",
					i, got.ID, got.Content, got.PosterID, w.ID, w.Content, w.PosterID)
			}
			if got.PosterUsername != userMap[got.PosterID] {
				t.Errorf("comment[%d]: got poster_username %q, want %q", i, got.PosterUsername, userMap[got.PosterID])
			}
		}
	}

	tests := []struct {
		name           string
		queryURL       string
		expectedStatus int
		wantHasMore    bool
		wantComments   []*models.Comment
	}{
		{
			name:           "Success: Retrieve all comments newest first with has_more=false",
			queryURL:       "/api/protected/profile/" + users[0].Username + "/comments",
			expectedStatus: http.StatusOK,
			wantHasMore:    false,
			wantComments:   []*models.Comment{c3, c2, c1},
		},
		{
			name:           "Success: Limit triggers has_more=true",
			queryURL:       "/api/protected/profile/" + users[0].Username + "/comments?limit=2",
			expectedStatus: http.StatusOK,
			wantHasMore:    true,
			wantComments:   []*models.Comment{c3, c2},
		},
		{
			name:           "Success: Cursor pagination via last_shown_id returns older comments",
			queryURL:       fmt.Sprintf("/api/protected/profile/%s/comments?last_shown_id=%d&limit=2", users[0].Username, c3.ID),
			expectedStatus: http.StatusOK,
			wantHasMore:    false,
			wantComments:   []*models.Comment{c2, c1},
		},
		{
			name:           "Success: Empty wall returns empty array and has_more=false",
			queryURL:       "/api/protected/profile/" + users[1].Username + "/comments",
			expectedStatus: http.StatusOK,
			wantHasMore:    false,
			wantComments:   []*models.Comment{},
		},
		{
			name:           "Failure: Target profile not found",
			queryURL:       "/api/protected/profile/non_existent_user_9999/comments",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodGet, tc.queryURL, userAuth, nil)
			if rec.Code != tc.expectedStatus {
				t.Fatalf("[%s] expected status %d, got %d. Body: %s", tc.name, tc.expectedStatus, rec.Code, rec.Body.String())
			}
			if tc.expectedStatus == http.StatusOK {
				resp := testutil.DecodeJSON[models.PaginatedCommentsResponse](t, rec)
				assertComments(t, resp, tc.wantHasMore, tc.wantComments...)
			}
		})
	}
}
