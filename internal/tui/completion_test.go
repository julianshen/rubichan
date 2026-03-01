package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/commands"
)

// testCmd is a minimal SlashCommand for test purposes.
type testCmd struct {
	name string
	desc string
}

func (c *testCmd) Name() string                                                { return c.name }
func (c *testCmd) Description() string                                         { return c.desc }
func (c *testCmd) Arguments() []commands.ArgumentDef                           { return nil }
func (c *testCmd) Complete(_ context.Context, _ []string) []commands.Candidate { return nil }
func (c *testCmd) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{}, nil
}

func newTestRegistry() *commands.Registry {
	r := commands.NewRegistry()
	_ = r.Register(commands.NewQuitCommand())
	_ = r.Register(commands.NewExitCommand())
	_ = r.Register(commands.NewClearCommand(nil))
	_ = r.Register(commands.NewModelCommand(nil))
	_ = r.Register(commands.NewConfigCommand())
	_ = r.Register(commands.NewHelpCommand(r))
	return r
}

func TestCompletionOverlayNew(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	assert.NotNil(t, co)
	assert.False(t, co.Visible())
	assert.Empty(t, co.Candidates())
}

func TestCompletionOverlayUpdateShowsOnSlash(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")

	assert.True(t, co.Visible())
	assert.Len(t, co.Candidates(), 6)
}

func TestCompletionOverlayFilters(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/mo")

	assert.True(t, co.Visible())
	require.Len(t, co.Candidates(), 1)
	assert.Equal(t, "model", co.Candidates()[0].Value)
}

func TestCompletionOverlayHidesOnEmpty(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.True(t, co.Visible())

	co.Update("")
	assert.False(t, co.Visible())
}

func TestCompletionOverlayHidesOnNoSlash(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("hello")

	assert.False(t, co.Visible())
	assert.Empty(t, co.Candidates())
}

func TestCompletionOverlayNavigateDown(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.Equal(t, 0, co.Selected())

	consumed := co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.True(t, consumed)
	assert.Equal(t, 1, co.Selected())
}

func TestCompletionOverlayNavigateUp(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	// Move down first, then up
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, co.Selected())

	consumed := co.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.True(t, consumed)
	assert.Equal(t, 0, co.Selected())
}

func TestCompletionOverlayNavigateUpWraps(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.Equal(t, 0, co.Selected())

	consumed := co.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.True(t, consumed)
	assert.Equal(t, len(co.Candidates())-1, co.Selected())
}

func TestCompletionOverlayNavigateDownWraps(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	lastIdx := len(co.Candidates()) - 1

	// Navigate to last
	for i := 0; i < lastIdx; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, lastIdx, co.Selected())

	// One more down should wrap to 0
	consumed := co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.True(t, consumed)
	assert.Equal(t, 0, co.Selected())
}

func TestCompletionOverlayTabAccept(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/cl")
	require.True(t, co.Visible())

	accepted, value := co.HandleTab()
	assert.True(t, accepted)
	assert.Equal(t, "clear", value)
}

func TestCompletionOverlayTabNoCandidate(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/zzz")
	assert.False(t, co.Visible())

	accepted, value := co.HandleTab()
	assert.False(t, accepted)
	assert.Equal(t, "", value)
}

func TestCompletionOverlayEscapeDismisses(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.True(t, co.Visible())

	consumed := co.HandleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.True(t, consumed)
	assert.False(t, co.Visible())

	// Dismissed flag prevents re-show until input changes away from /
	co.Update("/c")
	assert.False(t, co.Visible(), "should stay hidden while dismissed")

	// Typing something without / clears dismissed
	co.Update("hello")
	assert.False(t, co.Visible())

	// Now slash should work again
	co.Update("/")
	assert.True(t, co.Visible())
}

func TestCompletionOverlaySelectedResetOnFilterChange(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, co.Selected())

	// Typing more resets selected to 0
	co.Update("/c")
	assert.Equal(t, 0, co.Selected())
}

func TestCompletionOverlayViewNotEmpty(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	view := co.View()

	assert.NotEmpty(t, view)
	// Should contain some command names
	assert.True(t, strings.Contains(view, "clear") || strings.Contains(view, "config"),
		"view should contain command names")
}

func TestCompletionOverlayViewHidden(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	view := co.View()
	assert.Equal(t, "", view)
}

func TestCompletionOverlayMaxVisible(t *testing.T) {
	reg := commands.NewRegistry()
	for i := 0; i < 20; i++ {
		cmd := &testCmd{
			name: strings.Repeat("a", i+1), // "a", "aa", "aaa", ...
			desc: "test command",
		}
		_ = reg.Register(cmd)
	}

	co := NewCompletionOverlay(reg, 80)
	co.Update("/")

	assert.True(t, co.Visible())
	assert.Equal(t, 20, len(co.Candidates()))

	view := co.View()
	// Count the number of rows with "test command" — should be capped at maxVisibleCandidates
	lines := strings.Split(view, "\n")
	commandLines := 0
	for _, line := range lines {
		if strings.Contains(line, "test command") {
			commandLines++
		}
	}
	assert.LessOrEqual(t, commandLines, maxVisibleCandidates,
		"view should show at most %d candidates", maxVisibleCandidates)
}

func TestCompletionOverlayScrollsWithSelection(t *testing.T) {
	// Register more commands than maxVisibleCandidates to test scrolling.
	reg := commands.NewRegistry()
	for i := 0; i < 12; i++ {
		cmd := &testCmd{
			name: fmt.Sprintf("cmd%02d", i), // cmd00, cmd01, ..., cmd11
			desc: fmt.Sprintf("command %d", i),
		}
		_ = reg.Register(cmd)
	}

	co := NewCompletionOverlay(reg, 80)
	co.Update("/")

	require.Equal(t, 12, len(co.Candidates()))

	// Navigate down past maxVisibleCandidates.
	for i := 0; i < 10; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, 10, co.Selected())

	// The view should still show the selected item.
	view := co.View()
	assert.Contains(t, view, "cmd10", "scrolled view should contain the selected item")

	// Tab-accept should return the correct (scrolled-to) value.
	accepted, val := co.HandleTab()
	assert.True(t, accepted)
	assert.Equal(t, "cmd10", val)
}

func TestCompletionOverlayBoundsClampOnFilterChange(t *testing.T) {
	// Start with many candidates, navigate down, then filter to fewer.
	reg := newTestRegistry() // 6 commands
	co := NewCompletionOverlay(reg, 80)
	co.Update("/")

	// Navigate to last (index 5).
	for i := 0; i < 5; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, 5, co.Selected())

	// Filter to fewer candidates — selected should be clamped.
	co.Update("/cl") // only "clear"
	assert.Equal(t, 0, co.Selected(), "selected should be clamped when candidates shrink")
}

func TestCompletionOverlaySelectedValue(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	// Candidates are sorted: clear, config, exit, help, model, quit
	assert.Equal(t, "clear", co.SelectedValue())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "config", co.SelectedValue())
}

func TestCompletionOverlayHidesOnSpace(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/model ")

	assert.False(t, co.Visible(), "should hide when space found after command name")
}
