package handlers

import (
	"context"
	"dbBackend/models"
	"log/slog"
	"math"
	"time"
)

// EvaluateAndGrantBadges evaluates all catalog badges relevant to the trigger event against the provided context.
// Any qualifying badges are recorded in the database, and only newly unlocked badges (first-time earns) are returned.
func (h *Handler) EvaluateAndGrantBadges(ctx context.Context, userID int64, trigger models.TriggerType, evalCtx models.EvalContext) ([]models.BadgeDefinition, error) {
	var newlyEarned []models.BadgeDefinition

	for _, badge := range models.BadgeCatalog {
		if badge.Trigger != trigger {
			continue
		}

		current, target := badge.Evaluate(evalCtx) //call the badge's evaluate function with current stats, and see if it passes (current >= target)
		if target > 0 && current >= target {
			res, err := h.DB.NewInsert().
				Model(&models.UserAchievement{
					UserID:        userID,
					AchievementID: badge.ID,
				}).
				On("CONFLICT (user_id, achievement_id) DO NOTHING"). //conflict means (user, badge) was already in DB -> ignore that
				Exec(ctx)
			if err != nil {
				slog.Error("failed to grant achievement", "user_id", userID, "achievement_id", badge.ID, "error", err)
				continue
			}

			if rows, _ := res.RowsAffected(); rows == 1 { // if a row was returned, a new badge was added to db -> add to return
				newlyEarned = append(newlyEarned, badge)
				slog.Info("achievement unlocked", "user_id", userID, "achievement_id", badge.ID, "name", badge.Name)
			}
		}
	}

	return newlyEarned, nil
}

// BuildUserBadges formats the catalog badges with their unlock status and live progress for client display.
func (h *Handler) BuildUserBadges(ctx context.Context, userID int64, evalCtx models.EvalContext) ([]models.UserBadgeResponse, error) {
	var records []models.UserAchievement
	err := h.DB.NewSelect().
		Model(&records).
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	unlockedMap := make(map[string]time.Time, len(records))
	for _, r := range records {
		unlockedMap[r.AchievementID] = r.UnlockedAt
	}

	badges := make([]models.UserBadgeResponse, len(models.BadgeCatalog))
	for i, badge := range models.BadgeCatalog {
		current, target := badge.Evaluate(evalCtx)

		// Clamp progress for display (0 <= progress <= target)
		progress := max(0, min(current, target))

		progressPct := 0
		if target > 0 {
			progressPct = (progress * 100) / target
		}

		b := models.UserBadgeResponse{
			ID:          badge.ID,
			Name:        badge.Name,
			Description: badge.Description,
			Type:        badge.Type,
			Target:      target,
			Progress:    progress,
			ProgressPct: progressPct,
			Unlocked:    false,
		}

		// If once unlocked in database, it remains permanently unlocked with full completion
		if t, ok := unlockedMap[badge.ID]; ok {
			b.Unlocked = true
			b.UnlockedAt = &t
			b.Progress = target
			b.ProgressPct = 100
		}

		badges[i] = b
	}

	return badges, nil
}

// CalculateProgression computes total XP, level, and rank title from cumulative user stats.
func CalculateProgression(stats models.UserStats) models.ProgressionInfo {
	totalXp := (stats.Wins * 100) + (stats.Losses * 35)
	xpPerLevel := 200
	level := (totalXp / xpPerLevel) + 1
	currentLevelXp := totalXp % xpPerLevel
	progressPercent := int(math.Min(100, math.Floor((float64(currentLevelXp)/float64(xpPerLevel))*100)))

	var rankTitle string
	switch {
	case level <= 1:
		rankTitle = "Rookie"
	case level <= 3:
		rankTitle = "Contender"
	case level <= 5:
		rankTitle = "Veteran"
	default:
		rankTitle = "Grandmaster"
	}

	return models.ProgressionInfo{
		TotalXP:         totalXp,
		Level:           level,
		CurrentLevelXP:  currentLevelXp,
		XPPerLevel:      xpPerLevel,
		ProgressPercent: progressPercent,
		RankTitle:       rankTitle,
	}
}
