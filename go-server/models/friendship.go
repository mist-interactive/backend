package models

import (
	"time"

	"github.com/uptrace/bun"
)

type FriendshipStatus string

// these are the allowed options for status column
const (
	StatusPending  FriendshipStatus = "pending"
	StatusAccepted FriendshipStatus = "accepted"
	StatusBlocked  FriendshipStatus = "blocked"
)

type Friendship struct {
	bun.BaseModel `bun:"table:friendships"`
	ID            int64            `json:"friendship_id" bun:"id,notnull"`
	UserID        int64            `json:"user_id" bun:"user_id,notnull"`
	FriendID      int64            `json:"friend_id" bun:"friend_id,notnull"`
	Status        FriendshipStatus `json:"status" bun:"status,notnull"`
	CreatedAt     time.Time        `json:"created_at" bun:"created_at,default:current_timestamp"`
	UpdatedAt     time.Time        `json:"updated_at" bun:"updated_at,default:current_timestamp"`
}
