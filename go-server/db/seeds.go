package db

import (
	"context"
	"dbBackend/models"
	"log"
	"math/rand"
	"time"

	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

func SeedDevDatabase(ctx context.Context, db *bun.DB) error {
	exists, err := db.NewSelect().Model((*models.User)(nil)).Exists(ctx) //first check if db is already filled, and exit early if so
	if err != nil {
		return err
	}
	if exists {
		log.Println("Database already populated, skipping dev seeding.")
		if err := SeedDevComments(ctx, db); err != nil {
			return err
		}
		return SeedDevAchievements(ctx, db)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	names := []string{"nraatika", "mhirvasm", "jpelline", "anpollan", "zfarah", "loser1", "loser2", "loser3", "loser4", "loser5"}

	//build and bulk insert 10 users
	users := make([]models.User, len(names))
	for i, name := range names {
		users[i] = models.User{Username: name, Email: name + "@student.hive.fi", PWHash: string(hash)}
	}
	err = db.NewInsert().Model(&users).Scan(ctx)
	if err != nil {
		return err
	}

	//add 10 accepted friendships into DB
	friendships := make([]models.Friendship, len(users))
	for i := range users {
		friendships[i] = models.Friendship{
			UserID:   users[i].ID,
			FriendID: users[(i+1)%len(users)].ID, // connects user 0->1, 1->2, ... 9->0
			Status:   models.StatusAccepted,
		}
	}
	_, err = db.NewInsert().Model(&friendships).Exec(ctx)
	if err != nil {
		return err
	}

	winP1 := models.ResultPlayer1Win
	winP2 := models.ResultPlayer2Win
	var matches []models.MatchRecord

	// 1. Two matches each between the first 5 named users (1 win, 1 loss each: 20 matches)
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			// Match A: user i wins
			p1ScoreA, p2ScoreA := 5, 3
			matches = append(matches, models.MatchRecord{
				Player1:      users[i].ID,
				Player2:      users[j].ID,
				Player1Score: &p1ScoreA,
				Player2Score: &p2ScoreA,
				Status:       models.StatusFinished,
				Result:       &winP1,
			})

			// Match B: user j wins
			p1ScoreB, p2ScoreB := 2, 5
			matches = append(matches, models.MatchRecord{
				Player1:      users[i].ID,
				Player2:      users[j].ID,
				Player1Score: &p1ScoreB,
				Player2Score: &p2ScoreB,
				Status:       models.StatusFinished,
				Result:       &winP2,
			})
		}
	}

	// 2. <id> matches each against all losers (all wins for named users: 75 matches total)
	// Named user 1: 1 match per loser (5 matches)
	// Named user 2: 2 matches per loser (10 matches)
	// Named user 3: 3 matches per loser (15 matches)
	// Named user 4: 4 matches per loser (20 matches)
	// Named user 5: 5 matches per loser (25 matches)
	for i := 0; i < 5; i++ {
		matchCount := i + 1
		for loserIdx := 5; loserIdx < 10; loserIdx++ {
			for m := 0; m < matchCount; m++ {
				loserScore := (i + loserIdx + m) % 4
				winnerScore := 5

				// Alternate between home (Player1) and away (Player2) for variety
				if (i+loserIdx+m)%2 == 0 {
					p1Score := winnerScore
					p2Score := loserScore
					matches = append(matches, models.MatchRecord{
						Player1:      users[i].ID,
						Player2:      users[loserIdx].ID,
						Player1Score: &p1Score,
						Player2Score: &p2Score,
						Status:       models.StatusFinished,
						Result:       &winP1,
					})
				} else {
					p1Score := loserScore
					p2Score := winnerScore
					matches = append(matches, models.MatchRecord{
						Player1:      users[loserIdx].ID,
						Player2:      users[i].ID,
						Player1Score: &p1Score,
						Player2Score: &p2Score,
						Status:       models.StatusFinished,
						Result:       &winP2,
					})
				}
			}
		}
	}

	// Deterministic shuffle so matches are interleaved across the timeline
	rng := rand.New(rand.NewSource(42))
	rng.Shuffle(len(matches), func(a, b int) {
		matches[a], matches[b] = matches[b], matches[a]
	})

	// Spread across thirty days
	now := time.Now()
	totalSpan := 30 * 24 * time.Hour
	step := totalSpan / time.Duration(len(matches))

	for k := range matches {
		startedAt := now.Add(-totalSpan + time.Duration(k)*step)
		duration := time.Duration(5+((k*7)%11)) * time.Minute
		finishedAt := startedAt.Add(duration)

		matches[k].StartedAt = startedAt
		matches[k].FinishedAt = &finishedAt
	}

	_, err = db.NewInsert().Model(&matches).Exec(ctx)
	if err != nil {
		return err
	}

	log.Printf("Successfully seeded %d users, %d friendships, and %d matches!", len(users), len(friendships), len(matches))
	if err := SeedDevComments(ctx, db); err != nil {
		return err
	}
	return SeedDevAchievements(ctx, db)
}

func SeedDevComments(ctx context.Context, db *bun.DB) error {
	exists, err := db.NewSelect().Model((*models.Comment)(nil)).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		log.Println("Comments already populated, skipping comment seeding.")
		return nil
	}

	namedUsernames := []string{"nraatika", "mhirvasm", "jpelline", "anpollan", "zfarah"}
	var namedUsers []models.User
	err = db.NewSelect().
		Model(&namedUsers).
		Where("username IN (?)", bun.List(namedUsernames)).
		Order("id ASC").
		Scan(ctx)
	if err != nil {
		return err
	}
	if len(namedUsers) < 2 {
		log.Println("Not enough named users found to seed comments.")
		return nil
	}

	sampleComments := []string{
		"First!",
		"LLLoser",
		"GG",
		"you up?",
		"Lucky",
		"FU",
		"Ysvaaa",
	}

	var comments []models.Comment
	now := time.Now()
	commentIdx := 0

	for i := range namedUsers {
		for j := range namedUsers {
			createdAt := now.Add(-time.Duration(len(namedUsers)*(len(namedUsers)-1)-commentIdx) * 2 * time.Hour)
			comments = append(comments, models.Comment{
				OwnerID:   namedUsers[i].ID,
				PosterID:  namedUsers[j].ID,
				Content:   sampleComments[commentIdx%len(sampleComments)],
				CreatedAt: createdAt,
			})
			commentIdx++
		}
	}

	_, err = db.NewInsert().Model(&comments).Exec(ctx)
	if err != nil {
		return err
	}

	log.Printf("Successfully seeded %d comments across %d named users!", len(comments), len(namedUsers))
	return nil
}

func SeedDevAchievements(ctx context.Context, db *bun.DB) error {
	exists, err := db.NewSelect().Model((*models.UserAchievement)(nil)).Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		log.Println("Achievements already populated, skipping achievement seeding.")
		return nil
	}

	var users []models.User
	err = db.NewSelect().
		Model(&users).
		Order("id ASC").
		Scan(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		log.Println("No users found to seed achievements.")
		return nil
	}

	now := time.Now()
	var achievements []models.UserAchievement

	for _, u := range users {
		hasFriends, err := db.NewSelect().
			Table("friendships").
			Where("(user_id = ? OR friend_id = ?) AND status = ?", u.ID, u.ID, models.StatusAccepted).
			Exists(ctx)
		if err != nil {
			return err
		}

		var stats models.UserStats
		err = db.NewSelect().
			Table("matches").
			ColumnExpr("COUNT(*) FILTER (WHERE status = ?) AS games_played", models.StatusFinished).
			ColumnExpr("COUNT(*) FILTER (WHERE status = ? AND ((player_one = ? AND result = ?) OR (player_two = ? AND result = ?))) AS wins",
				models.StatusFinished, u.ID, models.ResultPlayer1Win, u.ID, models.ResultPlayer2Win).
			ColumnExpr("COUNT(*) FILTER (WHERE status = ? AND ((player_one = ? AND result = ?) OR (player_two = ? AND result = ?))) AS losses",
				models.StatusFinished, u.ID, models.ResultPlayer2Win, u.ID, models.ResultPlayer1Win).
			Where("player_one = ? OR player_two = ?", u.ID, u.ID).
			Scan(ctx, &stats)
		if err != nil {
			return err
		}

		evalCtx := models.EvalContext{
			Stats:      stats,
			HasFriends: hasFriends,
		}

		for _, badge := range models.BadgeCatalog {
			current, target := badge.Evaluate(evalCtx)
			if target > 0 && current >= target {
				achievements = append(achievements, models.UserAchievement{
					UserID:        u.ID,
					AchievementID: badge.ID,
					UnlockedAt:    now,
				})
			}
		}
	}

	if len(achievements) > 0 {
		_, err = db.NewInsert().
			Model(&achievements).
			On("CONFLICT (user_id, achievement_id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return err
		}
	}

	log.Printf("Successfully seeded %d achievements across %d users!", len(achievements), len(users))
	return nil
}

