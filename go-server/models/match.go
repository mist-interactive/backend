package models

import (
	"time"

	"github.com/uptrace/bun"
)

type MatchRecord struct {
	bun.BaseModel `bun:"table:matches"`

	ID         int64      `json:"id" bun:"id,pk,autoincrement"`
	Player1    int64      `json:"player_one" bun:"player_one,notnull"`
	Player2    int64      `json:"player_two" bun:"player_two,notnull"`
	Result     string     `json:"result" bun:"result,notnull"`
	StartedAt  time.Time  `json:"started_at" bun:"started_at,default:current_timestamp"`
	FinishedAt *time.Time `json:"finished_at" bun:"finished_at"`
}
