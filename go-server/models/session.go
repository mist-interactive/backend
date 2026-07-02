package models

import (
	"time"

	"github.com/uptrace/bun"
)

type Session struct {
	bun.BaseModel `bun:"table:sessions"`

	SessionID    int64     `bun:"id,pk,autoincrement" json:"id"`
	UserID       int64     `bun:"user_id,notnull" json:"user_id"`
	SessionToken string    `bun:"session_token,notnull,unique"`
	CreatedAt    time.Time `bun:"created_at,notnull,default:current_timestamp"`
	ExpiresAt    time.Time `bun:"expires_at,notnull"`

	User *User `bun:"rel:belongs-to,join:user_id=id"` //used to fetch the User data from the users table in the same DB request
}
