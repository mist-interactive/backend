package realtime

import (
	"context"
	"dbBackend/models"

	"github.com/uptrace/bun"
)

// this is how the ws system interacts with the DB. if split off to own service, just define a new DataStore that comms over http
type DataStore interface {
	//SaveMessage(ctx context.Context, senderID, recipientID int64, content string) (*models.Message, error) //TODO
	GetFriendsList(ctx context.Context, userID int64) ([]int64, error)
}

type BunDataStore struct {
	DB *bun.DB
}

func NewBunDataStore(db *bun.DB) *BunDataStore {
	return &BunDataStore{DB: db}
}

func (s *BunDataStore) GetFriendsList(ctx context.Context, userID int64) ([]int64, error) {
	var friends []int64

	// Extracts the OTHER user's ID for all accepted relationships
	err := s.DB.NewSelect().
		TableExpr("friendships").
		Where("(user_id = ? OR friend_id = ?) AND status = ?", userID, userID, models.StatusAccepted). //this filters the table: only accepted friendships relating to query user remain
		ColumnExpr("CASE WHEN user_id = ? THEN friend_id ELSE user_id END", userID).                   //select the other part of the friendship relation as output
		Scan(ctx, &friends)
	if err != nil {
		return nil, err
	}
	return friends, nil
}
