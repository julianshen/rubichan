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

func TestNewWikiFormDefaults(t *testing.T) {
	wf := NewWikiForm("/tmp/project")
	assert.Equal(t, ".", wf.Path)
	assert.Equal(t, "raw-md", wf.Format)
	assert.Equal(t, "docs/wiki", wf.OutDir)
	assert.Equal(t, "5", wf.ConcurrencyStr)
}

func TestWikiFormForm(t *testing.T) {
	wf := NewWikiForm("/tmp/project")
	f := wf.Form()
	assert.NotNil(t, f)
}

func TestWikiFormConcurrency(t *testing.T) {
	wf := NewWikiForm("/tmp")
	wf.ConcurrencyStr = "10"
	assert.Equal(t, 10, wf.Concurrency())

	wf.ConcurrencyStr = "invalid"
	assert.Equal(t, 5, wf.Concurrency())
}
