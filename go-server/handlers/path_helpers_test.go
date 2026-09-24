package handlers_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbBackend/handlers"
)

// Unit test: TestParsePathInt64 verifies path parameter extraction,
// int64 parsing, and error handling for missing or malformed values.
func TestParsePathInt64(t *testing.T) {
	tests := []struct {
		name      string
		paramKey  string
		setKey    string
		setValue  string
		wantVal   int64
		expectErr bool
	}{
		{
			name:      "Valid positive integer",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "42",
			wantVal:   42,
			expectErr: false,
		},
		{
			name:      "Valid MaxInt64",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "9223372036854775807",
			wantVal:   math.MaxInt64,
			expectErr: false,
		},
		{
			name:      "Missing path parameter",
			paramKey:  "id",
			setKey:    "other",
			setValue:  "42",
			wantVal:   0,
			expectErr: true,
		},
		{
			name:      "Empty path parameter value",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "",
			wantVal:   0,
			expectErr: true,
		},
		{
			name:      "Non-numeric string value",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "not-a-number",
			wantVal:   0,
			expectErr: true,
		},
		{
			name:      "Floating-point string value",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "12.34",
			wantVal:   0,
			expectErr: true,
		},
		{
			name:      "Overflow value beyond int64",
			paramKey:  "id",
			setKey:    "id",
			setValue:  "99999999999999999999999999999999",
			wantVal:   0,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.setKey != "" {
				req.SetPathValue(tc.setKey, tc.setValue)
			}

			gotVal, err := handlers.ParsePathInt64(req, tc.paramKey)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for case %q, got nil (val: %d)", tc.name, gotVal)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for case %q: %v", tc.name, err)
				}
				if gotVal != tc.wantVal {
					t.Errorf("got %d, want %d", gotVal, tc.wantVal)
				}
			}
		})
	}
}
