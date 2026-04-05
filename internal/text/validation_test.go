package text

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEmptyResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", true},
		{"single space", " ", true},
		{"multiple spaces", "   ", true},
		{"tabs", "\t\t", true},
		{"newlines", "\n\n", true},
		{"mixed whitespace", "  \n\t  ", true},
		{"single word", "hello", false},
		{"word with spaces", " hello ", false},
		{"meaningful with newline", "text\n", false},
		{"meaningful with tab", "\ttext", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEmptyResponse(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
