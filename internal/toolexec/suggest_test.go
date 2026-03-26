package toolexec_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestSuggestToolName(t *testing.T) {
	available := []string{"shell", "file", "search", "process", "tool_search"}

	tests := []struct {
		unknown  string
		expected string
	}{
		// Substring matches: unknown contains tool name.
		{"shell_exec", "shell"},
		{"run_shell", "shell"},
		{"file_write", "file"},
		{"file_read", "file"},
		{"code_search", "search"},

		// Keyword overlap.
		{"run_command", ""}, // no keyword overlap with available names
		{"write_file", "file"},
		{"read_file", "file"},

		// Exact name (shouldn't happen in practice, but tests completeness).
		{"shell", "shell"},

		// No match at all.
		{"foobar", ""},
		{"xyz_abc", ""},

		// Empty available list.
	}

	for _, tt := range tests {
		t.Run(tt.unknown, func(t *testing.T) {
			result := toolexec.SuggestToolName(tt.unknown, available)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSuggestToolNameEmptyAvailable(t *testing.T) {
	assert.Equal(t, "", toolexec.SuggestToolName("shell_exec", nil))
	assert.Equal(t, "", toolexec.SuggestToolName("shell_exec", []string{}))
}

func TestSuggestToolNameCaseInsensitive(t *testing.T) {
	available := []string{"shell", "file"}
	assert.Equal(t, "shell", toolexec.SuggestToolName("SHELL_EXEC", available))
	assert.Equal(t, "file", toolexec.SuggestToolName("FILE_WRITE", available))
}
