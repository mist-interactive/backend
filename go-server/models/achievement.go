package models

import (
	"slices"
	"time"

	"github.com/uptrace/bun"
)

// UserAchievement represents a record of an achievement unlocked by a user.
type UserAchievement struct {
	bun.BaseModel `bun:"table:user_achievements"`

	ID            int64     `bun:"id,pk,autoincrement" json:"id"`
	UserID        int64     `bun:"user_id,notnull" json:"user_id"`
	AchievementID string    `bun:"achievement_id,type:varchar(50),notnull" json:"achievement_id"`
	UnlockedAt    time.Time `bun:"unlocked_at,nullzero,notnull,default:current_timestamp" json:"unlocked_at"`
}

// BadgeType categorizes the visual icon and theme of the badge on the frontend.
type BadgeType string

const (
	BadgeTypeTrophy BadgeType = "trophy"
	BadgeTypeFriend BadgeType = "friend"
	BadgeTypeShield BadgeType = "shield"
	BadgeTypeTarget BadgeType = "target"
)

// TriggerType specifies which kind of event should trigger evaluation of a badge.
type TriggerType string

const (
	TriggerMatch  TriggerType = "match"
	TriggerSocial TriggerType = "social"
)

// EvalContext contains information needed to evaluate if achievements should be granted.
type EvalContext struct {
	Stats      UserStats
	WonMatch   bool
	HasFriends bool
}

// BadgeDefinition defines an achievement, including a function to evaluate if it should be granted, when the above struct of the current state is passed to it.
type BadgeDefinition struct {
	ID          string                                        `json:"id"`
	Name        string                                        `json:"name"`
	Description string                                        `json:"description"`
	Type        BadgeType                                     `json:"type"`
	Trigger     TriggerType                                   `json:"trigger"`
	Evaluate    func(c EvalContext) (current int, target int) `json:"-"` //we pass in EvalContext, and get back two integers: current state, and target, so frontend can show progression (target is 5 wins, current state 3, as an exmaple)
}

// Milestone defines a step in a tiered achievement series.
type Milestone struct {
	ID          string
	Name        string
	Description string
	Target      int
}

var standaloneBadges = []BadgeDefinition{
	{
		ID:          "first_friend",
		Name:        "Wingman",
		Description: "Add your first friend",
		Type:        BadgeTypeFriend,
		Trigger:     TriggerSocial,
		Evaluate: func(c EvalContext) (int, int) {
			if c.HasFriends {
				return 1, 1
			}
			return 0, 1
		},
	},
}

// series of "win x matches" achievements
var winMilestones = []Milestone{
	{
		ID:          "first_win",
		Name:        "First Blood",
		Description: "Win your first game",
		Target:      1,
	},
	{
		ID:          "dominator",
		Name:        "Dominator",
		Description: "Achieve 3 wins",
		Target:      3,
	},
	{
		ID:          "champion",
		Name:        "Champion",
		Description: "Achieve 5 wins",
		Target:      5,
	},
	{
		ID:          "legend",
		Name:        "Legend",
		Description: "Achieve 10 wins",
		Target:      10,
	},
}

// series of "play x matches" achievements
var matchMilestones = []Milestone{
	{
		ID:          "veteran",
		Name:        "Arena Veteran",
		Description: "Play at least 5 matches",
		Target:      5,
	},
	{
		ID:          "gladiator",
		Name:        "Gladiator",
		Description: "Play at least 10 matches",
		Target:      10,
	},
	{
		ID:          "warlord",
		Name:        "Warlord",
		Description: "Play at least 20 matches",
		Target:      20,
	},
}

// function to build a series of achievements. They'll all share a badgeType, you pass in what metric is being evaluated for the test (what returns the "current" value of the evaluate function), and the definitions
func buildSeries(badgeType BadgeType, metric func(EvalContext) int, milestones []Milestone) []BadgeDefinition {
	badges := make([]BadgeDefinition, len(milestones))
	for i, m := range milestones {
		badges[i] = BadgeDefinition{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Type:        badgeType,
			Trigger:     TriggerMatch,
			Evaluate: func(c EvalContext) (int, int) {
				return metric(c), m.Target
			},
		}
	}
	return badges
}

// BadgeCatalog holds the definitions for each achievement we grant, assembled from standalone badges and series.
var BadgeCatalog = slices.Concat(
	standaloneBadges,
	buildSeries(BadgeTypeTrophy, func(c EvalContext) int { return c.Stats.Wins }, winMilestones),
	buildSeries(BadgeTypeShield, func(c EvalContext) int { return c.Stats.GamesPlayed }, matchMilestones),
)

// FindBadge looks up a badge definition by its ID in the catalog.
func FindBadge(id string) (BadgeDefinition, bool) {
	for _, b := range BadgeCatalog {
		if b.ID == id {
			return b, true
		}
	}
	return BadgeDefinition{}, false
}

// UserBadgeResponse represents an achievement with its unlock status and progress for a specific user.
type UserBadgeResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Type        BadgeType  `json:"type"`
	Unlocked    bool       `json:"unlocked"`
	UnlockedAt  *time.Time `json:"unlocked_at,omitempty"`
	Progress    int        `json:"progress"`
	Target      int        `json:"target"`
	ProgressPct int        `json:"progress_pct"`
}

// ProgressionInfo represents user rank, XP, and level progression derived from match stats.
type ProgressionInfo struct {
	TotalXP         int    `json:"total_xp"`
	Level           int    `json:"level"`
	CurrentLevelXP  int    `json:"current_level_xp"`
	XPPerLevel      int    `json:"xp_per_level"`
	ProgressPercent int    `json:"progress_percent"`
	RankTitle       string `json:"rank_title"`
}
