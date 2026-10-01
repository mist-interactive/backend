package realtime

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalAndValidate(t *testing.T) {
	tests := []struct {
		name      string
		rawJSON   string
		expectErr bool
	}{
		{
			name:      "Valid payload passes validation",
			rawJSON:   `{"username": "bob", "status": "pending"}`,
			expectErr: false,
		},
		{
			name:      "Malformed JSON returns error",
			rawJSON:   `{"username": "bob",`,
			expectErr: true,
		},
		{
			name:      "Struct validation failure on required field",
			rawJSON:   `{"username": ""}`,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalAndValidate[MatchInvitePayload](json.RawMessage(tc.rawJSON))
			if (err != nil) != tc.expectErr {
				t.Errorf("got err=%v, want expectErr=%v", err, tc.expectErr)
			}
		})
	}
}

func TestEncodeMessage(t *testing.T) {
	t.Run("Encodes message successfully into envelope", func(t *testing.T) {
		payload := DMPayload{Username: "alice", Content: "hello"}
		bytes, err := EncodeMessage(TypeDMSend, payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var envelope WebsocketMessage
		if err := json.Unmarshal(bytes, &envelope); err != nil {
			t.Fatalf("failed to decode envelope: %v", err)
		}
		if envelope.Type != TypeDMSend {
			t.Errorf("type: got %s, want %s", envelope.Type, TypeDMSend)
		}
		parsed := parsePayload[DMPayload](t, envelope.Payload)
		if parsed.Username != "alice" || parsed.Content != "hello" {
			t.Errorf("payload mismatch: %+v", parsed)
		}
	})

	t.Run("Fails to encode unmarshalable payload", func(t *testing.T) {
		unmarshalable := make(chan int)
		_, err := EncodeMessage(TypeError, unmarshalable)
		if err == nil {
			t.Errorf("expected error for unmarshalable payload, got nil")
		}
	})
}

func TestBind(t *testing.T) {
	t.Run("Valid payload unmarshals and calls wrapped handler", func(t *testing.T) {
		var called bool
		handler := bind(func(c *Client, payload MatchInvitePayload) error {
			called = true
			if payload.Username != "bob" {
				t.Errorf("username: got %q, want bob", payload.Username)
			}
			return nil
		})

		err := handler(&Client{}, json.RawMessage(`{"username": "bob", "status": "pending"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Errorf("expected wrapped handler to be called")
		}
	})

	t.Run("Invalid payload returns error and does not call handler", func(t *testing.T) {
		var called bool
		handler := bind(func(c *Client, payload MatchInvitePayload) error {
			called = true
			return nil
		})

		err := handler(&Client{}, json.RawMessage(`{"username": ""}`))
		if err == nil {
			t.Errorf("expected validation error, got nil")
		}
		if called {
			t.Errorf("expected handler not to be called on validation error")
		}
	})
}
