package models_test

import (
	"dbBackend/models"
	"testing"
)

func TestCommentCreateInput_Sanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Trims leading and trailing whitespace",
			input:    "   Hello world!   ",
			expected: "Hello world!",
		},
		{
			name:     "Preserves internal whitespace and newlines",
			input:    "  First line\nSecond line  ",
			expected: "First line\nSecond line",
		},
		{
			name:     "Reduces whitespace-only string to empty",
			input:    "    \t\n   ",
			expected: "",
		},
		{
			name:     "No-op on already clean string",
			input:    "Clean comment",
			expected: "Clean comment",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comment := &models.CommentCreateInput{Content: tc.input}
			comment.Sanitize()
			if comment.Content != tc.expected {
				t.Errorf("Sanitize: got %q, want %q", comment.Content, tc.expected)
			}
		})
	}
}
