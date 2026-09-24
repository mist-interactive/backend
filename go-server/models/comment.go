package models

import (
	"strings"
	"time"

	"github.com/uptrace/bun"
)

type Comment struct {
	bun.BaseModel `bun:"table:comments"`

	ID        int64     `json:"id" bun:"id,pk,autoincrement"`
	OwnerID   int64     `json:"owner_id" bun:"owner_id,notnull"`
	PosterID  int64     `json:"poster_id" bun:"poster_id,notnull"`
	Content   string    `json:"content" bun:"content,notnull" validate:"required,min=1,max=1000"`
	CreatedAt time.Time `json:"created_at" bun:"created_at,default:current_timestamp"`
}

type CommentCreateInput struct {
	Content string `json:"content" validate:"required,min=1,max=1000"`
}

// function runs before validation, so input of only whitespace will fail min=1 requirement
func (c *CommentCreateInput) Sanitize() {
	c.Content = strings.TrimSpace(c.Content)
}

type CommentResponse struct {
	ID              int64     `json:"id" bun:"id"`
	OwnerID         int64     `json:"owner_id" bun:"owner_id"`
	PosterID        int64     `json:"poster_id" bun:"poster_id"`
	PosterUsername  string    `json:"poster_username" bun:"poster_username"`
	PosterAvatarURL *string   `json:"poster_avatar_url" bun:"poster_avatar_url"`
	Content         string    `json:"content" bun:"content"`
	CreatedAt       time.Time `json:"created_at" bun:"created_at"`
}

type PaginatedCommentsResponse struct {
	Comments []CommentResponse `json:"comments"`
	HasMore  bool              `json:"has_more"`
}
