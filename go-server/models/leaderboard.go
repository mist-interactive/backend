package models

// LeaderboardSort defines valid sorting criteria for the leaderboard endpoint.
type LeaderboardSort string

const (
	LeaderboardSortXP      LeaderboardSort = "xp"
	LeaderboardSortWinRate LeaderboardSort = "win_rate"
)

// LeaderboardEntry represents a single player's position, profile info, and career statistics on the leaderboard.
type LeaderboardEntry struct {
	Rank        int     `json:"rank"`
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	AvatarURL   *string `json:"avatar_url"`
	GamesPlayed int     `json:"games_played"`
	Wins        int     `json:"wins"`
	Losses      int     `json:"losses"`
	WinRate     float64 `json:"win_rate"`
	TotalXP     int     `json:"total_xp"`
	Level       int     `json:"level"`
	RankTitle   string  `json:"rank_title"`
}
