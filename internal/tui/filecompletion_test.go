package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestFileCompletionSource_IndexFromGitLsFiles(t *testing.T) {
	src := NewFileCompletionSource("")
	output := "cmd/rubichan/main.go\ninternal/tui/model.go\ninternal/tui/view.go\nREADME.md\n"
	src.SetFiles(strings.Split(strings.TrimSpace(output), "\n"))
	assert.True(t, src.Indexed())
	assert.Equal(t, 4, len(src.Files()))
}

func TestFileCompletionSource_MatchPrefix(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{
		"cmd/rubichan/main.go",
		"internal/tui/model.go",
		"internal/tui/view.go",
		"internal/agent/agent.go",
	})
	matches := src.Match("internal/tui")
	assert.Len(t, matches, 2)
	assert.Equal(t, "internal/tui/model.go", matches[0])
	assert.Equal(t, "internal/tui/view.go", matches[1])
}

func TestFileCompletionSource_MatchFuzzy(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{
		"cmd/rubichan/main.go",
		"internal/tui/model.go",
		"internal/tui/view.go",
	})
	// Matching by filename portion
	matches := src.Match("model")
	assert.Len(t, matches, 1)
	assert.Equal(t, "internal/tui/model.go", matches[0])
}

func TestFileCompletionSource_EmptyRepo(t *testing.T) {
	src := NewFileCompletionSource("")
	assert.False(t, src.Indexed())
	assert.Empty(t, src.Match("anything"))
	// Also works with empty file list
	src.SetFiles([]string{})
	assert.True(t, src.Indexed())
	assert.Empty(t, src.Match("test"))
}

func TestFileCompletionSource_MatchLimit(t *testing.T) {
	src := NewFileCompletionSource("")
	files := make([]string, 100)
	for i := range files {
		files[i] = "src/file.go"
	}
	src.SetFiles(files)
	matches := src.Match("src/")
	assert.LessOrEqual(t, len(matches), maxFileCompletionCandidates)
}

func TestFileCompletionOverlay_ActivatesOnAt(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go", "internal/tui/view.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@internal")
	assert.True(t, fo.Visible())
	assert.Greater(t, len(fo.Candidates()), 0)
}

func TestFileCompletionOverlay_HidesOnSpace(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@internal/tui/model.go ")
	assert.False(t, fo.Visible())
}

func TestFileCompletionOverlay_TabAccepts(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@internal")
	accepted, value := fo.HandleTab()
	assert.True(t, accepted)
	assert.Equal(t, "internal/tui/model.go", value)
}

func TestFileCompletionOverlay_EscapeDismisses(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@internal")
	assert.True(t, fo.Visible())
	fo.HandleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.False(t, fo.Visible())
}

func TestFileCompletionOverlay_NoAtPrefix(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"model.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("model")
	assert.False(t, fo.Visible())
}

func TestFileCompletionOverlay_UpDownNavigation(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"a.go", "ab.go", "abc.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@a")
	assert.Equal(t, 0, fo.Selected())
	fo.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, fo.Selected())
	fo.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, fo.Selected())
	// Wrap around
	fo.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 2, fo.Selected())
}

func TestFileCompletionOverlay_ViewRendersWhenVisible(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"model.go", "view.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@")
	assert.NotEmpty(t, fo.View())
	fo.Update("nothing")
	assert.Empty(t, fo.View())
}

func TestFileCompletionOverlay_AtOnlyShowsAll(t *testing.T) {
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"a.go", "b.go", "c.go"})
	fo := NewFileCompletionOverlay(src, 80)
	fo.Update("@")
	assert.True(t, fo.Visible())
	assert.Len(t, fo.Candidates(), 3)
}

func TestModelAtMentionTriggersFileCompletion(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go", "internal/tui/view.go"})
	m.SetFileCompletionSource(src)

	// Type @ to trigger file completion
	m.input.SetValue("@internal")
	m.syncCompletion()
	assert.True(t, m.fileCompletion.Visible())
	assert.Len(t, m.fileCompletion.Candidates(), 2)

	// View should include the file overlay
	v := m.View()
	assert.Contains(t, v, "model.go")
}

func TestModelFileCompletionTabAccept(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
	src := NewFileCompletionSource("")
	src.SetFiles([]string{"internal/tui/model.go"})
	m.SetFileCompletionSource(src)

	m.input.SetValue("check @internal")
	m.syncCompletion()
	assert.True(t, m.fileCompletion.Visible())

	// Tab should accept the file path
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Contains(t, m.input.Value(), "internal/tui/model.go")
}
