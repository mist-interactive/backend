package models

import (
	"time"

	"github.com/uptrace/bun"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID        int64     `bun:"id,pk,autoincrement" json:"id"`
	Username  string    `bun:"username,type:varchar(50),unique,notnull" json:"username"`
	PWHash    string    `bun:"password_hash,type:varchar(255),notnull" json:"-"`
	Email     string    `bun:"email,type:varchar(255),unique,notnull" json:"email"`
	Bio       string    `bun:"bio" json:"bio"`
	AvatarURL *string   `bun:"avatar_url" json:"avatarUrl"` //pointer so it can be null
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
