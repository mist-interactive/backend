package handlers_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"dbBackend/handlers"
	"dbBackend/models"
)

func TestDecodeAndValidate_RegisterRequest(t *testing.T) {
	tests := []struct {
		name        string
		jsonBody    string
		expectError bool
	}{
		{
			name:        "Valid Request",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "Password123"}`,
			expectError: false,
		},
		{
			name:        "Invalid JSON Syntax",
			jsonBody:    `{"username": "paavo", "email":`,
			expectError: true,
		},
		{
			name:        "Username Too Short",
			jsonBody:    `{"username": "p", "email": "paavo@pesusieni.fi", "password": "Password123"}`,
			expectError: true,
		},
		{
			name:        "Invalid Email Format",
			jsonBody:    `{"username": "paavo", "email": "invalid-email-no-at", "password": "Password123"}`,
			expectError: true,
		},
		{
			name:        "Password Too Short",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "P1!"}`,
			expectError: true,
		},
		{
			name:        "Password Complexity Fails (Only Lowercase)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "justlowercase"}`,
			expectError: true,
		},
		{
			name:        "Password Complexity Passes (Lower + Numbers)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "lowercase123"}`,
			expectError: false,
		},
		{
			name:        "Password Complexity Passes (Upper + Special)",
			jsonBody:    `{"username": "paavo", "email": "paavo@pesusieni.fi", "password": "UPPERCASE!!!"}`,
			expectError: false,
		},
		{
			name:        "Username contains invalid characters (only [a-zA-Z0-9_-] allowed)",
			jsonBody:    `{"username": "paavo on paras!", "email": "paavo@pesusieni.fi", "password": "UPPERCASE!!!"}`,
			expectError: true,
		},
		{
			name:        "Username contains tilde (cannot register deleted user format)",
			jsonBody:    `{"username": "deleted_user~42", "email": "paavo@pesusieni.fi", "password": "Password123"}`,
			expectError: true,
		},
		{
			name:        "Email with single-label internal domain rejected",
			jsonBody:    `{"username": "paavo", "email": "deleted_user~42@internal", "password": "Password123"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/register", strings.NewReader(tt.jsonBody))
			req.Header.Set("Content-Type", "application/json")
			_, err := handlers.DecodeAndValidate[models.RegisterRequest](req)
			if tt.expectError && err == nil {
				t.Errorf("expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("did not expect an error but got: %v", err)
			}
		})
	}
}

func TestDecodeAndValidate_LoginRequest(t *testing.T) {
	tests := []struct {
		name        string
		jsonBody    string
		expectError bool
	}{
		{
			name:        "Valid Request with Username",
			jsonBody:    `{"username": "paavo", "password": "password123"}`,
			expectError: false,
		},
		{
			name:        "Valid Request with Email in Username field",
			jsonBody:    `{"username": "paavo@student.hive.fi", "password": "password123"}`,
			expectError: false,
		},
		{
			name:        "Invalid JSON Syntax",
			jsonBody:    `{"username": "paavo", "password":`,
			expectError: true,
		},
		{
			name:        "Missing Identifier",
			jsonBody:    `{"password": "password123"}`,
			expectError: true,
		},
		{
			name:        "Identifier Too Short",
			jsonBody:    `{"username": "p", "password": "password123"}`,
			expectError: true,
		},
		{
			name:        "Password Too Short",
			jsonBody:    `{"username": "paavo", "password": "123"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/login", strings.NewReader(tt.jsonBody))
			req.Header.Set("Content-Type", "application/json")
			_, err := handlers.DecodeAndValidate[models.LoginRequest](req)
			if tt.expectError && err == nil {
				t.Errorf("expected an error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("did not expect an error but got: %v", err)
			}
		})
	}
}
