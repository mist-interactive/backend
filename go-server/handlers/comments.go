package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
)

// ProfileCommentsGet handles GET /api/protected/profile/{username}/comments.
// It retrieves the list of comments posted on the specified user's profile wall,
// ordered newest-first (descending ID). Supports cursor-based pagination via:
//   - limit: maximum number of items to return (default 50, max 100)
//   - last_shown_id: optional comment ID cursor; returns comments older than this ID
func (h *Handler) ProfileCommentsGet(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		http.Error(w, "No username provided", http.StatusBadRequest)
		return
	}

	targetUser, err := h.getUserByUsername(r.Context(), username)
	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User '%s'", username))
		return
	}

	limit := ParseQueryInt(r, "limit", 50, 1, 100)
	lastShownID := int64(ParseQueryInt(r, "last_shown_id", 0, 1, 0))

	q := h.DB.NewSelect().
		TableExpr("comments AS c").
		ColumnExpr("c.id, c.owner_id, c.poster_id, c.content, c.created_at").
		ColumnExpr("u.username AS poster_username, u.avatar_url AS poster_avatar_url").
		Join("JOIN users AS u ON u.id = c.poster_id").
		Where("c.owner_id = ?", targetUser.ID)

	if lastShownID > 0 { //if lastShownId was prsent and clean, add it to query
		q = q.Where("c.id < ?", lastShownID)
	}

	var comments []models.CommentResponse
	// Query limit+1 items to determine has_more without an extra COUNT(*) query
	err = q.Order("c.created_at DESC", "c.id DESC").
		Limit(limit+1).
		Scan(r.Context(), &comments)
	if err != nil {
		HandleDBError(w, err, "Fetching profile comments")
		return
	}

	hasMore := false
	if len(comments) > limit { //if it found limit+1 entries, cut response to limit and set hasMore = true
		hasMore = true
		comments = comments[:limit]
	}

	if comments == nil { //return empty array instead of null, for easier handling on frontend
		comments = []models.CommentResponse{}
	}

	resp := models.PaginatedCommentsResponse{
		Comments: comments,
		HasMore:  hasMore,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
