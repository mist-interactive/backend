package realtime

import (
	"bytes"
	"context"
	"dbBackend/models"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// this is how the ws system interacts with the DB. if split off to own service, just define a new DataStore that comms over http
type DataStore interface {
	//SaveMessage(ctx context.Context, senderID, recipientID int64, content string) (*models.Message, error) //TODO
	GetFriendsList(ctx context.Context, userID int64) ([]int64, error)
	SaveMessage(ctx context.Context, userID int64, recipient, content string) (*models.Message, error)
}

type HttpDataStore struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewHttpDataStore(baseURL, apiKey string) *HttpDataStore {
	return &HttpDataStore{BaseURL: baseURL, APIKey: apiKey, Client: &http.Client{Timeout: 5 * time.Second}}
}

func (s *HttpDataStore) GetFriendsList(ctx context.Context, userID int64) ([]int64, error) {
	url := fmt.Sprintf("%s/api/internal/friends/%d", s.BaseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", s.APIKey)
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach DB service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DB service returned status %d", resp.StatusCode)
	}
	var items []models.FriendshipItemResponse
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode friends list: %w", err)
	}
	var friendIDs []int64
	for _, item := range items {
		if item.Status == models.StatusAccepted {
			friendIDs = append(friendIDs, item.UserID)
		}
	}
	return friendIDs, nil
}

func (s *HttpDataStore) SaveMessage(ctx context.Context, userID int64, recipient, content string) (*models.Message, error) {
	input := models.MessageCreateInput{
		SenderID:  userID,
		Recipient: recipient,
		Content:   content,
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	url := s.BaseURL + "/api/internal/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.APIKey)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach DB service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to save message: status %d", resp.StatusCode)
	}

	savedMsg := new(models.Message)
	if err := json.NewDecoder(resp.Body).Decode(savedMsg); err != nil {
		return nil, fmt.Errorf("failed to decode saved message: %w", err)
	}

	return savedMsg, nil
}
