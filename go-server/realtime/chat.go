package realtime

import (
	"context"
	"log"
)

func (c *Client) HandleSendMsg(payload DMSendPayload) error {
	if payload.Username == c.Username {
		return nil // Cannot DM yourself
	}
	savedMsg, err := c.Hub.store.SaveMessage(context.Background(), c.UserID, payload.Username, payload.Message)
	if err != nil {
		log.Printf("[WS] Failed to save DM from %s to %s: %v", c.Username, payload.Username, err)
		return err
	}
	deliveryBytes, err := EncodeMessage(TypeDMRecv, DMRecvPayload{
		MessageID: savedMsg.ID,
		Sender:    c.Username,
		Message:   savedMsg.Content,
		CreatedAt: savedMsg.CreatedAt,
	})
	if err != nil {
		return err
	}

	c.Hub.SendToUser(savedMsg.RecipientID, deliveryBytes)
	return nil
}
