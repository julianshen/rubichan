package tui

import (
	"context"
	"fmt"
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

func TestWikiFormSetForm(t *testing.T) {
	wf := NewWikiForm("/tmp")
	original := wf.Form()
	assert.NotNil(t, original)

	// SetForm replaces the underlying form.
	wf.SetForm(original)
	assert.Equal(t, original, wf.Form())
}

func TestWikiCommandComplete(t *testing.T) {
	cmd := NewWikiCommand(WikiCommandConfig{})
	candidates := cmd.Complete(context.Background(), nil)
	assert.Nil(t, candidates)
}

func TestSetWikiConfig(t *testing.T) {
	reg := commands.NewRegistry()
	m := &Model{cmdRegistry: reg}
	m.SetWikiConfig(WikiCommandConfig{WorkDir: "/tmp/proj"})

	assert.Equal(t, "/tmp/proj", m.wikiCfg.WorkDir)
	// /wiki command should be registered.
	cmd, ok := reg.Get("wiki")
	assert.True(t, ok)
	assert.Equal(t, "wiki", cmd.Name())
}

func TestHandleCommandOpenWiki(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)
	m.SetWikiConfig(WikiCommandConfig{WorkDir: "/tmp"})

	cmd := m.handleCommand("/wiki")
	assert.NotNil(t, cmd) // form Init() returns a Cmd
	assert.Equal(t, StateWikiOverlay, m.state)
	assert.NotNil(t, m.activeOverlay)
}

func TestHandleCommandOpenWikiAlreadyRunning(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "test", "model", 10, "", nil, reg)
	m.SetWikiConfig(WikiCommandConfig{WorkDir: "/tmp"})
	m.wikiRunning = true

	cmd := m.handleCommand("/wiki")
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "already running")
	assert.Equal(t, StateInput, m.state)
}

func TestWikiDoneMsgClearsState(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiRunning = true
	m.statusBar.SetWikiProgress("analyzing")

	_, _ = m.Update(wikiDoneMsg{Err: nil})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "Wiki generation complete!")
}

func TestWikiDoneMsgWithError(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiRunning = true

	_, _ = m.Update(wikiDoneMsg{Err: fmt.Errorf("disk full")})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "disk full")
}

func TestWikiDoneMsgWithCancellation(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiRunning = true

	_, _ = m.Update(wikiDoneMsg{Err: context.Canceled})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "Wiki generation cancelled.")
	assert.NotContains(t, m.content.String(), "Wiki generation failed")
}

func TestWikiDoneMsgWithDeadlineExceeded(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiRunning = true

	_, _ = m.Update(wikiDoneMsg{Err: context.DeadlineExceeded})
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "Wiki generation cancelled.")
	assert.NotContains(t, m.content.String(), "Wiki generation failed")
}

func TestViewWikiOverlay(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	overlay, _ := NewWikiOverlay("/tmp")
	m.activeOverlay = overlay
	m.state = StateWikiOverlay

	view := m.View()
	// Wiki form overlay should render (huh form output).
	assert.NotEmpty(t, view)
	// Should not contain the normal header since overlay takes over.
	assert.NotContains(t, view, "test · model")
}

func TestStartWikiGenerationPathTraversal(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiCfg = WikiCommandConfig{WorkDir: "/tmp/proj"}

	wf := NewWikiForm("/tmp/proj")
	wf.Path = "../../etc"
	m.wikiRunning = true

	cmd := m.startWikiGeneration(wf)
	assert.Nil(t, cmd)
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "escapes working directory")
}

func TestStartWikiGenerationOutDirTraversal(t *testing.T) {
	m := NewModel(nil, "test", "model", 10, "", nil, nil)
	m.wikiCfg = WikiCommandConfig{WorkDir: "/tmp/proj"}

	wf := NewWikiForm("/tmp/proj")
	wf.Path = "."
	wf.OutDir = "../../../tmp/evil"
	m.wikiRunning = true

	cmd := m.startWikiGeneration(wf)
	assert.Nil(t, cmd)
	assert.False(t, m.wikiRunning)
	assert.Contains(t, m.content.String(), "escapes working directory")
}

func TestStatusBarWikiProgress(t *testing.T) {
	sb := NewStatusBar(120)
	sb.SetWikiProgress("analyzing")
	view := sb.View()
	assert.Contains(t, view, "Wiki: analyzing")

	sb.ClearWikiProgress()
	view = sb.View()
	assert.NotContains(t, view, "Wiki:")
}
