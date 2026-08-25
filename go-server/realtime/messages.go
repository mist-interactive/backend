package realtime

import "encoding/json"

// definitions of message types the websocket handles
type MessageType string

const (
	//client->server
	//	TypeDM   MessageType = "direct_message"
	//	TypePing MessageType = "ping"
	//server->client
	TypeInitialPresence MessageType = "initial_presence"
	TypePresenceUpdate  MessageType = "presence_update"

// TypeError           MessageType = "error"
)

// message struct: a type, and then payload, which is any struct
type WebsocketMessage struct {
	Type    MessageType `json:"type"`
	Payload any         `json:"payload,omitempty"`
}

// helper to parse incoming messages: first JSON parse gets message type,
// second parse can then parse the RawMessage the correct struct for that message type
type IncomingMessage struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type InitialPresencePayload struct {
	OnlineUsers []string `json:"online_users"`
}

type PresenceUpdatePayload struct {
	Username     string `json:"username"`
	OnlineStatus bool   `json:"online_status"`
}
