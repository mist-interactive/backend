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
		return nil
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
	return nil
}
