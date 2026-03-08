package tui

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiCommandName(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Equal(t, "wiki", cmd.Name())
}

func TestWikiCommandDescription(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Equal(t, "Generate project documentation wiki", cmd.Description())
}

func TestWikiCommandExecuteReturnsOpenWiki(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{WorkDir: "/tmp"})
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, commands.ActionOpenWiki, result.Action)
}

func TestWikiCommandArguments(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	assert.Empty(t, cmd.Arguments())
}
