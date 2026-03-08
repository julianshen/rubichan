package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLineHandlesQuotes(t *testing.T) {
	args, err := ParseLine(`/ralph-loop "finish the feature" --completion-promise "ALL DONE"`)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"/ralph-loop",
		"finish the feature",
		"--completion-promise",
		"ALL DONE",
	}, args)
}

func TestParseLineRejectsUnterminatedQuote(t *testing.T) {
	_, err := ParseLine(`/ralph-loop "unfinished`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated")
}
