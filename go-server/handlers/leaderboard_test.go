package handlers_test

import (
	"context"
	"dbBackend/handlers"
	"dbBackend/internal/testutil"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

func setupLeaderboardTestRouter(t *testing.T) (*handlers.Handler, http.Handler) {
	t.Helper()
	privKey, pubKey := getTestKeys(t)
	h := handlers.NewHandler(testDB, privKey, pubKey, "", nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

// TestLeaderboardGet_SortingAndTieBreakers verifies that the leaderboard endpoint
// orders entries monotonically by Total XP (default) and Win Rate, correctly applying tie-breakers.
func TestLeaderboardGet_SortingAndTieBreakers(t *testing.T) {
	_, router := setupLeaderboardTestRouter(t)

	tests := []struct {
		name     string
		queryURL string
		sortBy   string
	}{
		{
			name:     "Success: Default sorting orders monotonically by Total XP with tie-breakers",
			queryURL: "/api/leaderboard?limit=50",
			sortBy:   "xp",
		},
		{
			name:     "Success: Explicit XP sorting orders monotonically by Total XP with tie-breakers",
			queryURL: "/api/leaderboard?sort=xp&limit=50",
			sortBy:   "xp",
		},
		{
			name:     "Success: Win rate sorting orders monotonically by WinRate with tie-breakers",
			queryURL: "/api/leaderboard?sort=win_rate&limit=50",
			sortBy:   "win_rate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doTestRequest(router, http.MethodGet, tc.queryURL, "", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("[%s] expected status 200, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}

			entries := testutil.DecodeJSON[[]models.LeaderboardEntry](t, rec)
			if len(entries) == 0 {
				t.Fatalf("[%s] expected non-empty leaderboard entries", tc.name)
			}

			for i := range entries {
				// Assert consecutive 1-based ranks
				wantRank := i + 1
				if entries[i].Rank != wantRank {
					t.Errorf("[%s] entries[%d].Rank: got %d, want %d", tc.name, i, entries[i].Rank, wantRank)
				}

				// Assert games played equals wins + losses
				if entries[i].GamesPlayed != entries[i].Wins+entries[i].Losses {
					t.Errorf("[%s] entries[%d] games_played (%d) != wins (%d) + losses (%d)",
						tc.name, i, entries[i].GamesPlayed, entries[i].Wins, entries[i].Losses)
				}

				// Assert win rate calculation
				var wantWinRate float64
				if entries[i].GamesPlayed > 0 {
					wantWinRate = math.Round((float64(entries[i].Wins) / float64(entries[i].GamesPlayed)) * 100)
				}
				if entries[i].WinRate != wantWinRate {
					t.Errorf("[%s] entries[%d].WinRate: got %v, want %v", tc.name, i, entries[i].WinRate, wantWinRate)
				}

				// Assert monotonic ordering with next entry
				if i < len(entries)-1 {
					curr := entries[i]
					next := entries[i+1]

					switch tc.sortBy {
					case "xp":
						if curr.TotalXP < next.TotalXP {
							t.Errorf("[%s] TotalXP violation at index %d: curr %d < next %d", tc.name, i, curr.TotalXP, next.TotalXP)
						} else if curr.TotalXP == next.TotalXP {
							if curr.Wins < next.Wins {
								t.Errorf("[%s] Wins tie-breaker violation at index %d: curr %d < next %d", tc.name, i, curr.Wins, next.Wins)
							} else if curr.Wins == next.Wins && curr.GamesPlayed < next.GamesPlayed {
								t.Errorf("[%s] GamesPlayed tie-breaker violation at index %d: curr %d < next %d", tc.name, i, curr.GamesPlayed, next.GamesPlayed)
							}
						}
					case "win_rate":
						if curr.WinRate < next.WinRate {
							t.Errorf("[%s] WinRate violation at index %d: curr %v < next %v", tc.name, i, curr.WinRate, next.WinRate)
						} else if curr.WinRate == next.WinRate {
							if curr.Wins < next.Wins {
								t.Errorf("[%s] Wins tie-breaker violation at index %d: curr %d < next %d", tc.name, i, curr.Wins, next.Wins)
							} else if curr.Wins == next.Wins && curr.TotalXP < next.TotalXP {
								t.Errorf("[%s] TotalXP tie-breaker violation at index %d: curr %d < next %d", tc.name, i, curr.TotalXP, next.TotalXP)
							}
						}
					}
				}
			}
		})
	}
}

// TestLeaderboardGet_Pagination verifies limit and offset behavior, ensuring that
// consecutive paginated slices preserve 1-based ranks and match a combined page query.
func TestLeaderboardGet_Pagination(t *testing.T) {
	_, router := setupLeaderboardTestRouter(t)

	// Fetch combined first 4 entries
	recCombined := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=4&offset=0", "", nil)
	if recCombined.Code != http.StatusOK {
		t.Fatalf("combined request expected status 200, got %d", recCombined.Code)
	}
	combined := testutil.DecodeJSON[[]models.LeaderboardEntry](t, recCombined)
	if len(combined) < 4 {
		t.Fatalf("expected at least 4 entries in database, got %d", len(combined))
	}

	// Fetch page 1 (limit=2, offset=0)
	recPage1 := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=2&offset=0", "", nil)
	if recPage1.Code != http.StatusOK {
		t.Fatalf("page 1 request expected status 200, got %d", recPage1.Code)
	}
	page1 := testutil.DecodeJSON[[]models.LeaderboardEntry](t, recPage1)
	if len(page1) != 2 {
		t.Fatalf("page 1 expected 2 entries, got %d", len(page1))
	}

	// Fetch page 2 (limit=2, offset=2)
	recPage2 := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=2&offset=2", "", nil)
	if recPage2.Code != http.StatusOK {
		t.Fatalf("page 2 request expected status 200, got %d", recPage2.Code)
	}
	page2 := testutil.DecodeJSON[[]models.LeaderboardEntry](t, recPage2)
	if len(page2) != 2 {
		t.Fatalf("page 2 expected 2 entries, got %d", len(page2))
	}

	// Verify ranks
	if page1[0].Rank != 1 || page1[1].Rank != 2 {
		t.Errorf("page 1 ranks: got [%d, %d], want [1, 2]", page1[0].Rank, page1[1].Rank)
	}
	if page2[0].Rank != 3 || page2[1].Rank != 4 {
		t.Errorf("page 2 ranks: got [%d, %d], want [3, 4]", page2[0].Rank, page2[1].Rank)
	}

	// Verify matching users
	if page1[0].UserID != combined[0].UserID || page1[1].UserID != combined[1].UserID {
		t.Errorf("page 1 entries do not match combined[0:2]")
	}
	if page2[0].UserID != combined[2].UserID || page2[1].UserID != combined[3].UserID {
		t.Errorf("page 2 entries do not match combined[2:4]")
	}
}

// TestLeaderboardGet_CacheAndInvalidation verifies that the leaderboard serves from memory cache
// and refreshes properly when InvalidateLeaderboardCache is called.
func TestLeaderboardGet_CacheAndInvalidation(t *testing.T) {
	h, router := setupLeaderboardTestRouter(t)

	// Prime the cache
	rec1 := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=100", "", nil)
	if rec1.Code != http.StatusOK {
		t.Fatalf("prime request expected status 200, got %d", rec1.Code)
	}
	initialEntries := testutil.DecodeJSON[[]models.LeaderboardEntry](t, rec1)
	initialCount := len(initialEntries)

	// Create a new user directly in the database (bypassing the register handler, so cache is not invalidated)
	users := testutil.MakeNTestUsers(t, testDB, 1)
	newUser := users[0]

	// Read leaderboard again without invalidation: should serve cached response
	rec2 := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=100", "", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("cached request expected status 200, got %d", rec2.Code)
	}
	cachedEntries := testutil.DecodeJSON[[]models.LeaderboardEntry](t, rec2)
	if len(cachedEntries) != initialCount {
		t.Errorf("expected cached count %d, got %d", initialCount, len(cachedEntries))
	}
	for _, e := range cachedEntries {
		if e.UserID == newUser.ID {
			t.Errorf("new user %d was found in cached leaderboard before invalidation", newUser.ID)
		}
	}

	// Invalidate the cache
	h.InvalidateLeaderboardCache()

	// Read leaderboard after invalidation: should rebuild from database and include new user
	rec3 := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=100", "", nil)
	if rec3.Code != http.StatusOK {
		t.Fatalf("invalidated request expected status 200, got %d", rec3.Code)
	}
	freshEntries := testutil.DecodeJSON[[]models.LeaderboardEntry](t, rec3)
	var foundNewUser bool
	for _, e := range freshEntries {
		if e.UserID == newUser.ID {
			foundNewUser = true
			break
		}
	}
	if !foundNewUser {
		t.Errorf("new user %d was not found in refreshed leaderboard after invalidation", newUser.ID)
	}
}

// TestLeaderboardGet_ExcludesDeletedUsers verifies that anonymized or deleted accounts
// are omitted from the public leaderboard.
func TestLeaderboardGet_ExcludesDeletedUsers(t *testing.T) {
	ctx := context.Background()
	h, router := setupLeaderboardTestRouter(t)

	users := testutil.MakeNTestUsers(t, testDB, 1)
	delUser := users[0]

	// Mark user as deleted
	_, err := testDB.NewUpdate().
		Table("users").
		Where("id = ?", delUser.ID).
		Set("password_hash = ?", "deleted").
		Set("username = ?", fmt.Sprintf("deleted_user_%d", delUser.ID)).
		Exec(ctx)
	if err != nil {
		t.Fatalf("failed to anonymize user: %v", err)
	}

	// Invalidate cache so fresh data is read
	h.InvalidateLeaderboardCache()

	rec := doTestRequest(router, http.MethodGet, "/api/leaderboard?limit=100", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	entries := testutil.DecodeJSON[[]models.LeaderboardEntry](t, rec)
	for _, e := range entries {
		if e.UserID == delUser.ID {
			t.Errorf("deleted user %d was unexpectedly included in leaderboard", delUser.ID)
		}
	}
}

type rebuildQueryCounter struct {
	count atomic.Int64
}

func (c *rebuildQueryCounter) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

func (c *rebuildQueryCounter) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if strings.Contains(event.Query, "LEFT JOIN matches") {
		c.count.Add(1)
	}
}

// TestLeaderboardGet_ConcurrentAccess verifies thread-safety of the in-memory cache,
// ensuring that concurrent readers and cache invalidators do not produce data races or panics.
func TestLeaderboardGet_ConcurrentAccess(t *testing.T) {
	counter := &rebuildQueryCounter{}
	privKey, pubKey := getTestKeys(t)
	h := handlers.NewHandler(testDB.WithQueryHook(counter), privKey, pubKey, "", nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	router := mux

	stopInvalidator := make(chan struct{})
	invalidatorDone := make(chan struct{})

	// Launch a cache invalidator in a loop before readers start
	go func() {
		defer close(invalidatorDone)
		for {
			select {
			case <-stopInvalidator:
				return
			default:
				h.InvalidateLeaderboardCache()
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	var wg sync.WaitGroup //make a place to collect a group of goroutines
	numReaders := 20
	requestsPerReader := 10

	// Launch concurrent readers (tracked by WaitGroup)
	for i := range numReaders { //this makes numReaders goroutines
		readerID := i
		wg.Go(func() { //this spawns a new goroutine, and registers it to the waitgroup
			for j := range requestsPerReader {
				endpoint := "/api/leaderboard?limit=10"
				if (readerID+j)%2 == 0 {
					endpoint = "/api/leaderboard?sort=win_rate&limit=10"
				}
				rec := doTestRequest(router, http.MethodGet, endpoint, "", nil)
				if rec.Code != http.StatusOK {
					t.Errorf("reader %d request %d failed with status %d", readerID, j, rec.Code)
					return
				}
				var entries []models.LeaderboardEntry
				if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
					t.Errorf("reader %d request %d received invalid JSON: %v", readerID, j, err)
					return
				}
			}
		})
	}

	// 3. Wait until all readers finish, then cancel the invalidator
	wg.Wait()
	close(stopInvalidator)
	<-invalidatorDone //this ensures the invalidator has fully finished before the test ends

	t.Logf("Total cache rebuild queries executed: %d (across %d requests)", counter.count.Load(), numReaders*requestsPerReader) //check that some actual concurrency was happening, should expect something between 1 and 200
}
