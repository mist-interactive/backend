package handlers

import (
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

type FriendRequest struct {
	Target string `json:"target" validate:"required,min=3,max=50"` //same length constraints as when registering
}

type FriendRequestAnswer struct {
	Status models.FriendshipStatus `json:"status" validate:"required,oneof=accepted blocked"`
}

// The backend returns a list of these.
// FriendshipID is mainly for management: accept/reject/block/delete existing friendship: do via the friendship-id
// IsIncoming tells whether a pending friend request was sent by this user, or opposite party
// other fields are the same as profile-get so you don't need multiple db calls
type FriendshipItemResponse struct {
	FriendshipID int64                   `json:"friendship_id"`
	UserID       int64                   `json:"user_id"`
	Username     string                  `json:"username"`
	AvatarURL    *string                 `json:"avatar_url"`
	Status       models.FriendshipStatus `json:"status"`
	IsIncoming   bool                    `json:"is_incoming"`
	UnreadCount  int64                   `json:"unread_count"`
}

func (h *Handler) FriendRequestPost(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[FriendRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	//first get target user data
	targetUser, err := h.getUserByUsername(r.Context(), input.Target)
	if err != nil {
		HandleDBError(w, err, fmt.Sprintf("User '%s'", input.Target))
		return
	}
	if targetUser.ID == userID {
		http.Error(w, "Cannot friend yourself", http.StatusBadRequest)
		return
	}
	//Use fetched user data to populate friendship entry
	f := models.Friendship{
		UserID:   userID,
		FriendID: targetUser.ID,
		Status:   models.StatusPending,
	}
	err = h.DB.NewInsert().
		Model(&f).
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Adding friend request")
		return
	}
	//send back the id of the created entry and status of the request
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":     f.ID,
		"status": f.Status,
	})
}

func (h *Handler) FriendsListGet(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	//initialize a list of structs, so JSON understands to encode it as a list
	friends := make([]FriendshipItemResponse, 0)

	//make a single DB call to get all friends, *and* their profiles, so
	err := h.DB.NewSelect().
		TableExpr("friendships AS f").
		ColumnExpr("f.id AS friendship_id"). //Columnexpr are used later, this is just declaring naming of variables
		ColumnExpr("u.id AS user_id").
		ColumnExpr("u.username AS username").
		ColumnExpr("u.avatar_url AS avatar_url").
		ColumnExpr("f.status AS status").
		ColumnExpr("(f.friend_id = ?) AS is_incoming", userID).                                                                                           //check if id'd user is the recipient of the request, store result in is_incoming
		ColumnExpr("(SELECT COUNT(*) FROM messages AS m WHERE m.sender_id = u.id AND m.recipient_id = ? AND m.is_read = FALSE) AS unread_count", userID). //count how many messages from this friend to id'd user are unread, store in unread_count
		Join("JOIN users AS u ON (f.user_id = ? AND f.friend_id = u.id) OR (f.friend_id = ? AND f.user_id = u.id)", userID, userID).                      //current user is either user or friend in the records. this checks both
		Where("f.user_id = ? OR f.friend_id = ?", userID, userID).
		Where("f.status != ? OR f.user_id = ?", models.StatusBlocked, userID). // Hide blocks unless current user is the blocker
		Scan(r.Context(), &friends)
	if err != nil {
		HandleDBError(w, err, "Friends list")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(friends) //response is all the
}

func (h *Handler) FriendRequestAnswer(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[FriendRequestAnswer](r) //this forces input.Status to be either 'accepted' or 'blocked'
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	idStr := r.PathValue("id")
	friendshipID, err := strconv.ParseInt(idStr, 10, 64)
	now := time.Now()
	log.Printf("Patching request id %d from user %d\n", friendshipID, userID)
	f := models.Friendship{}
	err = h.DB.NewUpdate().
		Model(&f).
		Where("id = ?", friendshipID).
		Where("friend_id = ? AND status = ?", userID, models.StatusPending). //you can only change status of a request made to you that is still pending
		Set("status = ?", input.Status).
		Set("updated_at = ?", now).
		Returning("*").
		Scan(r.Context())
	if err != nil {
		HandleDBError(w, err, "Answering friend request")
		return
	}
}

func (h *Handler) FriendDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := r.PathValue("id")
	friendshipID, err := strconv.ParseInt(idStr, 10, 64)
	res, err := h.DB.NewDelete().
		Model((*models.Friendship)(nil)).
		Where("id = ?", friendshipID).
		Where("friend_id = ? OR user_id = ?", userID, userID). //you can delete a friendship from either side
		Exec(r.Context())
	if err != nil {
		HandleDBError(w, err, "Deleting friendship")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		http.Error(w, "Friendship not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getUserByUsername(ctx context.Context, target string) (*models.User, error) {
	user := new(models.User)
	err := h.DB.NewSelect().
		Model(user).
		Where("username = ?", target).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return user, nil
}
