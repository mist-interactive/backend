package models

import (
	"time"

	"github.com/uptrace/bun"
)

type MatchStatus string
type MatchResult string
type MatchOutcome string

const (
	StatusInProgress MatchStatus = "in_progress"
	StatusFinished   MatchStatus = "finished"
	StatusAbandoned  MatchStatus = "abandoned"

	ResultPlayer1Win MatchResult = "player1_win"
	ResultPlayer2Win MatchResult = "player2_win"
	ResultDraw       MatchResult = "draw"
	ResultAborted    MatchResult = "aborted"

	OutcomeWin     MatchOutcome = "win"
	OutcomeLoss    MatchOutcome = "loss"
	OutcomeAborted MatchOutcome = "aborted"
)

type MatchRecord struct {
	bun.BaseModel `bun:"table:matches"`

	ID           int64        `json:"id" bun:"id,pk,autoincrement"`
	Player1      int64        `json:"player_one" bun:"player_one,notnull"`
	Player2      int64        `json:"player_two" bun:"player_two,notnull"`
	Player1Score *int         `json:"player_one_score" bun:"player_one_score"`
	Player2Score *int         `json:"player_two_score" bun:"player_two_score"`
	Status       MatchStatus  `json:"status" bun:"status,notnull"`
	Result       *MatchResult `json:"result" bun:"result"`
	StartedAt    time.Time    `json:"started_at" bun:"started_at,default:current_timestamp"`
	FinishedAt   *time.Time   `json:"finished_at" bun:"finished_at"`
}

type MatchCreateInput struct {
	Player1 string `json:"player_one" validate:"required,min=3,max=50"`
	Player2 string `json:"player_two" validate:"required,min=3,max=50"`
}

type PlayerScoreInput struct {
	PlayerID int64 `json:"player_id" validate:"required"`
	Score    int   `json:"score" validate:"min=0"`
}

type MatchPatchInput struct {
	Scores []PlayerScoreInput `json:"scores" validate:"required,len=2"`
	Status *MatchStatus       `json:"status,omitempty" validate:"omitempty,oneof=finished abandoned"`
}

type ActiveMatchResponse struct {
	MatchID          int64     `json:"match_id" bun:"match_id"`
	OpponentID       int64     `json:"opponent_id" bun:"opponent_id"`
	OpponentUsername string    `json:"opponent" bun:"opponent"`
	StartedAt        time.Time `json:"started_at" bun:"started_at"`
}

type MatchFinishedPayload struct {
	MatchID      int64       `json:"match_id"`
	Player1      int64       `json:"player_one"`
	Player2      int64       `json:"player_two"`
	Player1Score int         `json:"player_one_score"`
	Player2Score int         `json:"player_two_score"`
	Status       MatchStatus `json:"status"`
	Result       MatchResult `json:"result"`
	WinnerID     *int64      `json:"winner_id,omitempty"`
}

type MatchHistoryResponse struct {
	ID                int64         `json:"id" bun:"id"`
	OpponentID        int64         `json:"opponent_id" bun:"opponent_id"`
	OpponentUsername  string        `json:"opponent" bun:"opponent"`
	OpponentAvatarURL *string       `json:"opponent_avatar_url" bun:"opponent_avatar_url"`
	UserScore         *int          `json:"user_score" bun:"user_score"`
	OpponentScore     *int          `json:"opponent_score" bun:"opponent_score"`
	Status            MatchStatus   `json:"status" bun:"status"`
	Result            *MatchResult  `json:"result" bun:"result"`
	Outcome           *MatchOutcome `json:"outcome" bun:"outcome"`
	StartedAt         time.Time     `json:"started_at" bun:"started_at"`
	FinishedAt        *time.Time    `json:"finished_at" bun:"finished_at"`
}
