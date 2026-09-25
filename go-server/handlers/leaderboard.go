package handlers

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"sync"
)

type leaderboardCache struct {
	mu        sync.RWMutex
	valid     bool
	byXP      []models.LeaderboardEntry
	byWinRate []models.LeaderboardEntry
}

// InvalidateLeaderboardCache marks the cached leaderboard as stale,
// prompting a lazy rebuild on the next request.
func (h *Handler) InvalidateLeaderboardCache() {
	h.leaderboard.mu.Lock()
	defer h.leaderboard.mu.Unlock()
	h.leaderboard.valid = false
}

type rawLeaderboardRow struct {
	UserID      int64   `bun:"user_id"`
	Username    string  `bun:"username"`
	AvatarURL   *string `bun:"avatar_url"`
	GamesPlayed int     `bun:"games_played"`
	Wins        int     `bun:"wins"`
	Losses      int     `bun:"losses"`
}

// LeaderboardGet handles GET /api/leaderboard.
// It returns a paginated ranking of users by XP (default) or win rate.
// Results are cached in memory and invalidated by state-changing event handlers.
func (h *Handler) LeaderboardGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := ParsePagination(r, 50, 100)
	sortBy := models.LeaderboardSort(r.URL.Query().Get("sort"))

	entries, err := h.getOrRefreshLeaderboard(ctx, sortBy)
	if err != nil {
		HandleDBError(w, err, "Leaderboard query")
		return
	}

	start := min(page.Offset, len(entries))
	end := min(page.Offset+page.Limit, len(entries))

	result := make([]models.LeaderboardEntry, end-start)
	copy(result, entries[start:end])

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) getOrRefreshLeaderboard(ctx context.Context, sort models.LeaderboardSort) ([]models.LeaderboardEntry, error) {
	// Fast path: read lock check; doesn't block others from reading, only writing
	h.leaderboard.mu.RLock()
	if h.leaderboard.valid {
		var cached []models.LeaderboardEntry
		if sort == models.LeaderboardSortWinRate {
			cached = h.leaderboard.byWinRate
		} else {
			cached = h.leaderboard.byXP
		}
		h.leaderboard.mu.RUnlock()
		return cached, nil
	}
	h.leaderboard.mu.RUnlock()

	// Slow path: acquire write lock to rebuild cache; blocks read and write
	h.leaderboard.mu.Lock()
	defer h.leaderboard.mu.Unlock()

	// Double-check under write lock in case another goroutine refreshed it
	if h.leaderboard.valid {
		if sort == models.LeaderboardSortWinRate {
			return h.leaderboard.byWinRate, nil
		}
		return h.leaderboard.byXP, nil
	}

	byXP, byWinRate, err := h.buildFreshLeaderboard(ctx)
	if err != nil {
		return nil, err
	}

	h.leaderboard.byXP = byXP
	h.leaderboard.byWinRate = byWinRate
	h.leaderboard.valid = true

	if sort == models.LeaderboardSortWinRate {
		return byWinRate, nil
	}
	return byXP, nil
}

func (h *Handler) buildFreshLeaderboard(ctx context.Context) ([]models.LeaderboardEntry, []models.LeaderboardEntry, error) {
	var rows []rawLeaderboardRow
	err := h.DB.NewSelect().
		TableExpr("users AS u").
		ColumnExpr("u.id AS user_id").
		ColumnExpr("u.username AS username").
		ColumnExpr("u.avatar_url AS avatar_url").
		ColumnExpr("COUNT(m.id) AS games_played").
		ColumnExpr("COUNT(CASE WHEN (m.player_one = u.id AND m.result = 'player1_win') OR (m.player_two = u.id AND m.result = 'player2_win') THEN 1 END) AS wins").
		ColumnExpr("COUNT(CASE WHEN (m.player_one = u.id AND m.result = 'player2_win') OR (m.player_two = u.id AND m.result = 'player1_win') THEN 1 END) AS losses").
		Join("LEFT JOIN matches AS m ON (m.player_one = u.id OR m.player_two = u.id) AND m.status = ?", models.StatusFinished).
		Where("u.password_hash != 'deleted'").
		GroupExpr("u.id, u.username, u.avatar_url").
		Scan(ctx, &rows)
	if err != nil {
		return nil, nil, err
	}

	entries := make([]models.LeaderboardEntry, len(rows))
	for i, r := range rows {
		winRate := 0.0
		if r.GamesPlayed > 0 {
			winRate = math.Round((float64(r.Wins) / float64(r.GamesPlayed)) * 100)
		}

		stats := models.UserStats{
			GamesPlayed: r.GamesPlayed,
			Wins:        r.Wins,
			Losses:      r.Losses,
			WinRate:     winRate,
		}
		progression := CalculateProgression(stats)

		entries[i] = models.LeaderboardEntry{
			UserID:      r.UserID,
			Username:    r.Username,
			AvatarURL:   r.AvatarURL,
			GamesPlayed: r.GamesPlayed,
			Wins:        r.Wins,
			Losses:      r.Losses,
			WinRate:     winRate,
			TotalXP:     progression.TotalXP,
			Level:       progression.Level,
			RankTitle:   progression.RankTitle,
		}
	}

	// 1. Sort by XP (Tie-breaker: Wins DESC, GamesPlayed DESC, UserID ASC)
	byXP := make([]models.LeaderboardEntry, len(entries))
	copy(byXP, entries)
	slices.SortFunc(byXP, func(a, b models.LeaderboardEntry) int {
		if a.TotalXP != b.TotalXP {
			return b.TotalXP - a.TotalXP
		}
		if a.Wins != b.Wins {
			return b.Wins - a.Wins
		}
		if a.GamesPlayed != b.GamesPlayed {
			return b.GamesPlayed - a.GamesPlayed
		}
		return int(a.UserID - b.UserID)
	})
	for i := range byXP {
		byXP[i].Rank = i + 1
	}

	// 2. Sort by Win Rate (Tie-breaker: Wins DESC, TotalXP DESC, UserID ASC)
	byWinRate := make([]models.LeaderboardEntry, len(entries))
	copy(byWinRate, entries)
	slices.SortFunc(byWinRate, func(a, b models.LeaderboardEntry) int {
		if a.WinRate != b.WinRate {
			if b.WinRate > a.WinRate {
				return 1
			}
			return -1
		}
		if a.Wins != b.Wins {
			return b.Wins - a.Wins
		}
		if a.TotalXP != b.TotalXP {
			return b.TotalXP - a.TotalXP
		}
		return int(a.UserID - b.UserID)
	})
	for i := range byWinRate {
		byWinRate[i].Rank = i + 1
	}

	return byXP, byWinRate, nil
}
