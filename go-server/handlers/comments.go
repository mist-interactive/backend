package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log/slog"
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

// ProfileCommentCreate handles POST /api/protected/profile/{username}/comments.
// Any authenticated user can post a comment on another user's profile wall, or on their own wall.
func (h *Handler) ProfileCommentCreate(w http.ResponseWriter, r *http.Request) {
	callerID, _ := UserIDFromContext(r.Context())

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

	input, err := DecodeAndValidate[models.CommentCreateInput](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	comment := &models.Comment{
		OwnerID:  targetUser.ID,
		PosterID: callerID,
		Content:  input.Content,
	}

	err = h.DB.NewInsert().
		Model(comment).
		Returning("*").
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Creating profile comment")
		return
	}

	resp := models.CommentResponse{
		ID:              comment.ID,
		OwnerID:         comment.OwnerID,
		PosterID:        comment.PosterID,
		PosterUsername:  targetUser.Username, //by default, use a known username/avatar
		PosterAvatarURL: targetUser.AvatarURL,
		Content:         comment.Content,
		CreatedAt:       comment.CreatedAt,
	}

	if callerID != targetUser.ID { //if it's not a self-comment, update with the correct values
		caller, err := h.getUserByID(r.Context(), callerID)
		if err != nil {
			HandleDBError(w, err, "Fetching poster details")
			return
		}
		resp.PosterUsername = caller.Username
		resp.PosterAvatarURL = caller.AvatarURL
	}

	slog.Info("profile comment created", "comment_id", comment.ID, "owner", targetUser.Username, "poster", resp.PosterUsername)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}
