package models_test

import (
	"dbBackend/models"
	"testing"
)

func TestLoginRequest_Sanitize(t *testing.T) {
	tests := []struct {
		name         string
		input        models.LoginRequest
		wantUsername string
	}{
		{
			name: "Trims leading and trailing whitespace from username",
			input: models.LoginRequest{
				Username: "  alice  ",
			},
			wantUsername: "alice",
		},
		{
			name: "Leaves already clean username unchanged",
			input: models.LoginRequest{
				Username: "bob@student.hive.fi",
			},
			wantUsername: "bob@student.hive.fi",
		},
		{
			name: "Handles whitespace-only values by trimming to empty string",
			input: models.LoginRequest{
				Username: "   \t\n  ",
			},
			wantUsername: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.input
			req.Sanitize()

			if req.Username != tc.wantUsername {
				t.Errorf("Username: got %q, want %q", req.Username, tc.wantUsername)
			}
		})
	}
}
