package handlers

import (
	"dbBackend/models"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

type FriendRequest struct {
	Target int64 `json:"target_id" validate:"required"`
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
}

func (h *Handler) FriendRequestPost(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	input, err := DecodeAndValidate[FriendRequest](r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if input.Target == claims.UserID {
		http.Error(w, "Cannot friend yourself", http.StatusBadRequest)
		return
	}
	f := models.Friendship{
		UserID:   claims.UserID,
		FriendID: input.Target,
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
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
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
		ColumnExpr("(f.friend_id = ?) AS is_incoming", claims.UserID).
		Join("JOIN users AS u ON (f.user_id = ? AND f.friend_id = u.id) OR (f.friend_id = ? AND f.user_id = u.id)", claims.UserID, claims.UserID). //current user is either user or friend in the records. this checks both
		Where("f.user_id = ? OR f.friend_id = ?", claims.UserID, claims.UserID).
		Where("f.status != ? OR f.user_id = ?", models.StatusBlocked, claims.UserID). // Hide blocks unless current user is the blocker
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
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
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
	log.Printf("Patching request id %d from user %d\n", friendshipID, claims.UserID)
	f := models.Friendship{}
	err = h.DB.NewUpdate().
		Model(&f).
		Where("id = ?", friendshipID).
		Where("friend_id = ? AND status = ?", claims.UserID, models.StatusPending). //you can only change status of a request made to you that is still pending
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
	claims, ok := ClaimsFromContext(r.Context())
	if !ok || claims.UserID == 0 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	idStr := r.PathValue("id")
	friendshipID, err := strconv.ParseInt(idStr, 10, 64)
	res, err := h.DB.NewDelete().
		Model((*models.Friendship)(nil)).
		Where("id = ?", friendshipID).
		Where("friend_id = ? OR user_id = ?", claims.UserID, claims.UserID). //you can delete a friendship from either side
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
