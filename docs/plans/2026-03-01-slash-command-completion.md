# Slash Command Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an extensible slash command registry with TUI completion popup, supporting built-in commands, argument completions, and skill-contributed commands.

**Architecture:** A `commands.Registry` holds `SlashCommand` implementations registered by built-ins and skills. A `CompletionOverlay` Bubble Tea component queries the registry and renders a dropdown above the input area. The commands package avoids Bubble Tea dependency by using an `Action` enum in `Result` that the TUI interprets.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, testify

---

### Task 1: Command Types

**Files:**
- Create: `internal/commands/commands.go`
- Test: `internal/commands/commands_test.go`

**Step 1: Write the failing test**

```go
// internal/commands/commands_test.go
package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionConstants(t *testing.T) {
	assert.Equal(t, Action(0), ActionNone)
	assert.NotEqual(t, ActionNone, ActionQuit)
	assert.NotEqual(t, ActionQuit, ActionOpenConfig)
}

func TestCandidateFields(t *testing.T) {
	c := Candidate{Value: "model", Description: "Switch LLM model"}
	assert.Equal(t, "model", c.Value)
	assert.Equal(t, "Switch LLM model", c.Description)
}

func TestArgumentDefFields(t *testing.T) {
	a := ArgumentDef{
		Name:        "name",
		Description: "Model name",
		Required:    true,
		Static:      []string{"gpt-4", "claude-3"},
	}
	assert.Equal(t, "name", a.Name)
	assert.True(t, a.Required)
	assert.Len(t, a.Static, 2)
}

func TestResultFields(t *testing.T) {
	r := Result{Output: "done", Action: ActionQuit}
	assert.Equal(t, "done", r.Output)
	assert.Equal(t, ActionQuit, r.Action)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/... -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/commands/commands.go
package commands

import "context"

// Action represents a TUI-level action that a command can request.
// The TUI interprets these; the commands package has no Bubble Tea dependency.
type Action int

const (
	ActionNone       Action = iota
	ActionQuit              // Signal the TUI to quit
	ActionOpenConfig        // Signal the TUI to open config overlay
)

// Candidate is a single completion suggestion shown in the popup.
type Candidate struct {
	Value       string
	Description string
}

// ArgumentDef describes one positional argument for a slash command.
type ArgumentDef struct {
	Name        string
	Description string
	Required    bool
	Static      []string // pre-known values; nil means use Complete() for dynamic
}

// Result is returned by SlashCommand.Execute.
type Result struct {
	Output string // text to display in the viewport
	Action Action // optional TUI-level action
}

// SlashCommand is the interface that built-in and skill-contributed commands implement.
type SlashCommand interface {
	Name() string
	Description() string
	Arguments() []ArgumentDef
	Complete(ctx context.Context, args []string) []Candidate
	Execute(ctx context.Context, args []string) (Result, error)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/commands/... -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add command types for slash command completion
```

---

### Task 2: Command Registry

**Files:**
- Modify: `internal/commands/commands.go` (add Registry type)
- Test: `internal/commands/commands_test.go` (add registry tests)

**Step 1: Write the failing tests**

```go
func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	cmd := &stubCommand{name: "test", desc: "A test command"}

	err := r.Register(cmd)
	assert.NoError(t, err)

	got, ok := r.Get("test")
	assert.True(t, ok)
	assert.Equal(t, "test", got.Name())
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	cmd := &stubCommand{name: "test", desc: "A test command"}

	assert.NoError(t, r.Register(cmd))
	err := r.Register(cmd)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegistryRegisterNil(t *testing.T) {
	r := NewRegistry()
	err := r.Register(nil)
	assert.Error(t, err)
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	cmd := &stubCommand{name: "test", desc: "A test command"}
	_ = r.Register(cmd)

	err := r.Unregister("test")
	assert.NoError(t, err)

	_, ok := r.Get("test")
	assert.False(t, ok)
}

func TestRegistryUnregisterNotFound(t *testing.T) {
	r := NewRegistry()
	err := r.Unregister("nope")
	assert.Error(t, err)
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubCommand{name: "alpha", desc: "A"})
	_ = r.Register(&stubCommand{name: "beta", desc: "B"})

	all := r.All()
	assert.Len(t, all, 2)
}

func TestRegistryMatchPrefix(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubCommand{name: "model", desc: "Switch model"})
	_ = r.Register(&stubCommand{name: "mode", desc: "Switch mode"})
	_ = r.Register(&stubCommand{name: "clear", desc: "Clear"})

	matches := r.Match("mo")
	assert.Len(t, matches, 2)

	matches = r.Match("cl")
	assert.Len(t, matches, 1)
	assert.Equal(t, "clear", matches[0].Value)
}

func TestRegistryMatchEmpty(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubCommand{name: "quit", desc: "Quit"})
	_ = r.Register(&stubCommand{name: "help", desc: "Help"})

	matches := r.Match("")
	assert.Len(t, matches, 2)
}

func TestRegistryMatchCaseInsensitive(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubCommand{name: "model", desc: "Switch model"})

	matches := r.Match("MO")
	assert.Len(t, matches, 1)
}

func TestRegistryGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nope")
	assert.False(t, ok)
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			_ = r.Register(&stubCommand{name: fmt.Sprintf("cmd-%d", i), desc: "test"})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		r.Match("cmd")
		r.All()
	}
	<-done
}

// --- stub command for testing ---

type stubCommand struct {
	name string
	desc string
}

func (s *stubCommand) Name() string                                              { return s.name }
func (s *stubCommand) Description() string                                       { return s.desc }
func (s *stubCommand) Arguments() []ArgumentDef                                  { return nil }
func (s *stubCommand) Complete(_ context.Context, _ []string) []Candidate        { return nil }
func (s *stubCommand) Execute(_ context.Context, _ []string) (Result, error)     { return Result{}, nil }
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/... -v -run TestRegistry`
Expected: FAIL — NewRegistry undefined

**Step 3: Write minimal implementation**

Add to `internal/commands/commands.go`:

```go
// Registry manages a collection of slash commands. All methods are safe for
// concurrent use. Mirrors the pattern of tools.Registry.
type Registry struct {
	mu   sync.RWMutex
	cmds map[string]SlashCommand
}

// NewRegistry creates a new empty command registry.
func NewRegistry() *Registry {
	return &Registry{cmds: make(map[string]SlashCommand)}
}

// Register adds a command to the registry.
func (r *Registry) Register(cmd SlashCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cmd == nil {
		return fmt.Errorf("cannot register nil command")
	}
	if _, exists := r.cmds[cmd.Name()]; exists {
		return fmt.Errorf("command already registered: %s", cmd.Name())
	}
	r.cmds[cmd.Name()] = cmd
	return nil
}

// Unregister removes a command by name.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cmds[name]; !exists {
		return fmt.Errorf("command not registered: %s", name)
	}
	delete(r.cmds, name)
	return nil
}

// Get retrieves a command by name.
func (r *Registry) Get(name string) (SlashCommand, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cmd, ok := r.cmds[name]
	return cmd, ok
}

// All returns candidates for all registered commands, sorted by name.
func (r *Registry) All() []Candidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := make([]Candidate, 0, len(r.cmds))
	for _, cmd := range r.cmds {
		candidates = append(candidates, Candidate{
			Value:       cmd.Name(),
			Description: cmd.Description(),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Value < candidates[j].Value
	})
	return candidates
}

// Match returns candidates whose names have the given prefix (case-insensitive),
// sorted by name.
func (r *Registry) Match(prefix string) []Candidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(prefix)
	var candidates []Candidate
	for _, cmd := range r.cmds {
		if strings.HasPrefix(strings.ToLower(cmd.Name()), lower) {
			candidates = append(candidates, Candidate{
				Value:       cmd.Name(),
				Description: cmd.Description(),
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Value < candidates[j].Value
	})
	return candidates
}
```

Add imports: `"fmt"`, `"sort"`, `"strings"`, `"sync"`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/commands/... -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add command registry with concurrent-safe Register/Unregister/Match/Get
```

---

### Task 3: Built-in Commands

**Files:**
- Create: `internal/commands/builtin.go`
- Test: `internal/commands/builtin_test.go`

Built-in commands need dependencies injected at construction time. We define callback interfaces to avoid coupling to `agent` or `tui` packages.

**Step 1: Write the failing tests**

```go
// internal/commands/builtin_test.go
package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuitCommand(t *testing.T) {
	cmd := NewQuitCommand()
	assert.Equal(t, "quit", cmd.Name())
	assert.Contains(t, cmd.Description(), "Exit")

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionQuit, result.Action)
}

func TestExitCommand(t *testing.T) {
	cmd := NewExitCommand()
	assert.Equal(t, "exit", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionQuit, result.Action)
}

func TestClearCommand(t *testing.T) {
	var cleared bool
	cmd := NewClearCommand(func() { cleared = true })

	assert.Equal(t, "clear", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, cleared)
	assert.Equal(t, ActionNone, result.Action)
}

func TestModelCommand(t *testing.T) {
	var setTo string
	cmd := NewModelCommand(func(name string) { setTo = name })

	assert.Equal(t, "model", cmd.Name())
	assert.Len(t, cmd.Arguments(), 1)
	assert.True(t, cmd.Arguments()[0].Required)

	result, err := cmd.Execute(context.Background(), []string{"gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", setTo)
	assert.Contains(t, result.Output, "gpt-4")
}

func TestModelCommandNoArg(t *testing.T) {
	cmd := NewModelCommand(func(_ string) {})

	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}

func TestConfigCommand(t *testing.T) {
	cmd := NewConfigCommand()
	assert.Equal(t, "config", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActionOpenConfig, result.Action)
}

func TestHelpCommand(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&stubCommand{name: "quit", desc: "Exit the app"})
	_ = reg.Register(&stubCommand{name: "clear", desc: "Clear history"})
	cmd := NewHelpCommand(reg)

	assert.Equal(t, "help", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "/quit")
	assert.Contains(t, result.Output, "/clear")
	assert.Contains(t, result.Output, "Exit the app")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/... -v -run "Test(Quit|Exit|Clear|Model|Config|Help)Command"`
Expected: FAIL — constructors undefined

**Step 3: Write minimal implementation**

```go
// internal/commands/builtin.go
package commands

import (
	"context"
	"fmt"
	"strings"
)

// --- Quit ---

type quitCommand struct{}

func NewQuitCommand() SlashCommand       { return &quitCommand{} }
func (*quitCommand) Name() string        { return "quit" }
func (*quitCommand) Description() string { return "Exit the application" }
func (*quitCommand) Arguments() []ArgumentDef {
	return nil
}
func (*quitCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (*quitCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionQuit}, nil
}

// --- Exit (alias for quit) ---

type exitCommand struct{}

func NewExitCommand() SlashCommand       { return &exitCommand{} }
func (*exitCommand) Name() string        { return "exit" }
func (*exitCommand) Description() string { return "Exit the application" }
func (*exitCommand) Arguments() []ArgumentDef {
	return nil
}
func (*exitCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (*exitCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionQuit}, nil
}

// --- Clear ---

type clearCommand struct {
	onClear func()
}

func NewClearCommand(onClear func()) SlashCommand {
	return &clearCommand{onClear: onClear}
}
func (*clearCommand) Name() string        { return "clear" }
func (*clearCommand) Description() string { return "Clear conversation history" }
func (*clearCommand) Arguments() []ArgumentDef {
	return nil
}
func (*clearCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (c *clearCommand) Execute(_ context.Context, _ []string) (Result, error) {
	if c.onClear != nil {
		c.onClear()
	}
	return Result{}, nil
}

// --- Model ---

type modelCommand struct {
	onSwitch func(name string)
}

func NewModelCommand(onSwitch func(name string)) SlashCommand {
	return &modelCommand{onSwitch: onSwitch}
}
func (*modelCommand) Name() string        { return "model" }
func (*modelCommand) Description() string { return "Switch to a different model" }
func (*modelCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{{Name: "name", Description: "Model name to switch to", Required: true}}
}
func (*modelCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (c *modelCommand) Execute(_ context.Context, args []string) (Result, error) {
	if len(args) == 0 {
		return Result{}, fmt.Errorf("model name required")
	}
	if c.onSwitch != nil {
		c.onSwitch(args[0])
	}
	return Result{Output: fmt.Sprintf("Model switched to %s", args[0])}, nil
}

// --- Config ---

type configCommand struct{}

func NewConfigCommand() SlashCommand     { return &configCommand{} }
func (*configCommand) Name() string        { return "config" }
func (*configCommand) Description() string { return "Edit configuration" }
func (*configCommand) Arguments() []ArgumentDef {
	return nil
}
func (*configCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (*configCommand) Execute(_ context.Context, _ []string) (Result, error) {
	return Result{Action: ActionOpenConfig}, nil
}

// --- Help ---

type helpCommand struct {
	registry *Registry
}

func NewHelpCommand(registry *Registry) SlashCommand {
	return &helpCommand{registry: registry}
}
func (*helpCommand) Name() string        { return "help" }
func (*helpCommand) Description() string { return "Show available commands" }
func (*helpCommand) Arguments() []ArgumentDef {
	return nil
}
func (*helpCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}
func (c *helpCommand) Execute(_ context.Context, _ []string) (Result, error) {
	all := c.registry.All()
	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, cmd := range all {
		b.WriteString(fmt.Sprintf("  /%-12s %s\n", cmd.Value, cmd.Description))
	}
	return Result{Output: b.String()}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/commands/... -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add built-in slash command implementations
```

---

### Task 4: Completion Overlay

**Files:**
- Create: `internal/tui/completion.go`
- Test: `internal/tui/completion_test.go`

**Step 1: Write the failing tests**

```go
// internal/tui/completion_test.go
package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/julianshen/rubichan/internal/commands"
)

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
	assert.False(t, co.Visible())
	assert.Empty(t, co.Candidates())
}

func TestCompletionOverlayUpdateShowsOnSlash(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.True(t, co.Visible())
	assert.Equal(t, 6, len(co.Candidates())) // all commands
}

func TestCompletionOverlayFilters(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/mo")
	assert.True(t, co.Visible())
	assert.Equal(t, 1, len(co.Candidates()))
	assert.Equal(t, "model", co.Candidates()[0].Value)
}

func TestCompletionOverlayFiltersMultiple(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/e")
	assert.True(t, co.Visible())
	assert.Equal(t, 1, len(co.Candidates()))
	assert.Equal(t, "exit", co.Candidates()[0].Value)
}

func TestCompletionOverlayHidesOnEmpty(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/mo")
	assert.True(t, co.Visible())

	co.Update("")
	assert.False(t, co.Visible())
}

func TestCompletionOverlayHidesOnNoSlash(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("hello")
	assert.False(t, co.Visible())
}

func TestCompletionOverlayNavigateDown(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.Equal(t, 0, co.Selected())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, co.Selected())
}

func TestCompletionOverlayNavigateUp(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, co.Selected())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, co.Selected())
}

func TestCompletionOverlayNavigateUpWraps(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.Equal(t, 0, co.Selected())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	// Should wrap to last item
	assert.Equal(t, len(co.Candidates())-1, co.Selected())
}

func TestCompletionOverlayNavigateDownWraps(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	last := len(co.Candidates()) - 1
	for i := 0; i < last; i++ {
		co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, last, co.Selected())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 0, co.Selected())
}

func TestCompletionOverlayTabAccept(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/cl")
	accepted, value := co.HandleTab()
	assert.True(t, accepted)
	assert.Equal(t, "clear", value)
}

func TestCompletionOverlayTabNoCandidate(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/zzz")
	accepted, _ := co.HandleTab()
	assert.False(t, accepted)
}

func TestCompletionOverlayEscapeDismisses(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	assert.True(t, co.Visible())

	co.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, co.Visible())
}

func TestCompletionOverlaySelectedValue(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	val := co.SelectedValue()
	assert.NotEmpty(t, val)
}

func TestCompletionOverlayViewNotEmpty(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	view := co.View()
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "clear")
	assert.Contains(t, view, "quit")
}

func TestCompletionOverlayViewHidden(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	view := co.View()
	assert.Empty(t, view)
}

func TestCompletionOverlaySelectedResetOnFilterChange(t *testing.T) {
	reg := newTestRegistry()
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	co.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, co.Selected())

	// Typing more should reset selection to 0
	co.Update("/cl")
	assert.Equal(t, 0, co.Selected())
}

func TestCompletionOverlayMaxVisible(t *testing.T) {
	// Register many commands to test capping
	reg := commands.NewRegistry()
	for i := 0; i < 20; i++ {
		_ = reg.Register(&testCmd{name: fmt.Sprintf("cmd%02d", i), desc: "test"})
	}
	co := NewCompletionOverlay(reg, 80)

	co.Update("/")
	view := co.View()
	// Should show at most maxVisibleCandidates items
	lines := strings.Count(view, "\n")
	// View has border lines too, but should not have 20+ content lines
	assert.Less(t, lines, 15)
}

// testCmd is a minimal SlashCommand for testing.
type testCmd struct {
	name string
	desc string
}

func (c *testCmd) Name() string                                                 { return c.name }
func (c *testCmd) Description() string                                          { return c.desc }
func (c *testCmd) Arguments() []commands.ArgumentDef                            { return nil }
func (c *testCmd) Complete(_ context.Context, _ []string) []commands.Candidate  { return nil }
func (c *testCmd) Execute(_ context.Context, _ []string) (commands.Result, error) {
	return commands.Result{}, nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -v -run TestCompletionOverlay`
Expected: FAIL — NewCompletionOverlay undefined

**Step 3: Write minimal implementation**

```go
// internal/tui/completion.go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/commands"
)

const maxVisibleCandidates = 8

var (
	completionBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#555555"}).
		Padding(0, 1)
	completionSelectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
		Background(lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#5A56E0"})
	completionNormalStyle = lipgloss.NewStyle()
	completionDescStyle   = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
)

// CompletionOverlay renders a dropdown of matching slash commands above
// the input area. It queries the command registry for matches and supports
// keyboard navigation.
type CompletionOverlay struct {
	registry   *commands.Registry
	candidates []commands.Candidate
	selected   int
	visible    bool
	dismissed  bool
	width      int
}

// NewCompletionOverlay creates a new overlay backed by the given registry.
func NewCompletionOverlay(registry *commands.Registry, width int) *CompletionOverlay {
	return &CompletionOverlay{
		registry: registry,
		width:    width,
	}
}

// Update refreshes the overlay state based on current input text.
// Call this after every keystroke in the input area.
func (co *CompletionOverlay) Update(input string) {
	if co.dismissed {
		if !strings.HasPrefix(input, "/") {
			co.dismissed = false
		}
		co.visible = false
		co.candidates = nil
		return
	}

	if !strings.HasPrefix(input, "/") {
		co.visible = false
		co.candidates = nil
		return
	}

	prefix := strings.TrimPrefix(input, "/")
	// If there's a space, we're past the command name — hide for now
	// (argument completion is a later enhancement).
	if strings.Contains(prefix, " ") {
		co.visible = false
		co.candidates = nil
		return
	}

	co.candidates = co.registry.Match(prefix)
	co.visible = len(co.candidates) > 0
	co.selected = 0
}

// HandleKey processes navigation keys. Returns true if the key was consumed.
func (co *CompletionOverlay) HandleKey(msg tea.KeyMsg) bool {
	if !co.visible {
		return false
	}

	switch msg.Type {
	case tea.KeyUp:
		co.selected--
		if co.selected < 0 {
			co.selected = len(co.candidates) - 1
		}
		return true
	case tea.KeyDown:
		co.selected++
		if co.selected >= len(co.candidates) {
			co.selected = 0
		}
		return true
	case tea.KeyEsc:
		co.visible = false
		co.dismissed = true
		return true
	}
	return false
}

// HandleTab accepts the currently selected candidate. Returns true and the
// command name if a candidate was accepted, false otherwise.
func (co *CompletionOverlay) HandleTab() (bool, string) {
	if !co.visible || len(co.candidates) == 0 {
		return false, ""
	}
	value := co.candidates[co.selected].Value
	co.visible = false
	return true, value
}

// Visible returns whether the overlay should be rendered.
func (co *CompletionOverlay) Visible() bool { return co.visible }

// Candidates returns the current filtered candidates.
func (co *CompletionOverlay) Candidates() []commands.Candidate { return co.candidates }

// Selected returns the currently highlighted index.
func (co *CompletionOverlay) Selected() int { return co.selected }

// SelectedValue returns the value of the currently selected candidate.
func (co *CompletionOverlay) SelectedValue() string {
	if co.selected >= 0 && co.selected < len(co.candidates) {
		return co.candidates[co.selected].Value
	}
	return ""
}

// View renders the completion popup. Returns empty string when not visible.
func (co *CompletionOverlay) View() string {
	if !co.visible || len(co.candidates) == 0 {
		return ""
	}

	visible := co.candidates
	if len(visible) > maxVisibleCandidates {
		visible = visible[:maxVisibleCandidates]
	}

	var rows []string
	for i, c := range visible {
		name := fmt.Sprintf("/%-12s", c.Value)
		desc := completionDescStyle.Render(c.Description)
		row := fmt.Sprintf("%s %s", name, desc)
		if i == co.selected {
			row = completionSelectedStyle.Render(row)
		} else {
			row = completionNormalStyle.Render(row)
		}
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	styled := completionBorderStyle.Width(co.width - 4).Render(content)
	return styled
}

// SetWidth updates the rendering width.
func (co *CompletionOverlay) SetWidth(w int) {
	co.width = w
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -v -run TestCompletionOverlay`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add CompletionOverlay TUI component for slash command popup
```

---

### Task 5: Wire Overlay into TUI Model

**Files:**
- Modify: `internal/tui/model.go` — add `completion *CompletionOverlay`, `cmdRegistry *commands.Registry` fields; update `NewModel` signature
- Modify: `internal/tui/update.go` — intercept keys when overlay visible, update overlay on input changes
- Modify: `internal/tui/view.go` — render overlay above input
- Modify: `internal/tui/model_test.go` — update existing tests for new NewModel signature, add overlay integration tests
- Modify: `cmd/rubichan/main.go` — create registry, register built-ins, pass to NewModel

This is the largest task. It refactors `handleCommand()` to delegate to the registry and wires the overlay into the key handling and view rendering.

**Step 1: Write the failing tests**

Add to `internal/tui/model_test.go`:

```go
func TestNewModelWithRegistry(t *testing.T) {
	reg := commands.NewRegistry()
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	assert.NotNil(t, m.completion)
	assert.NotNil(t, m.cmdRegistry)
}

func TestModelHandleCommandViaRegistry(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	_ = reg.Register(commands.NewClearCommand(nil))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	cmd := m.handleCommand("/help")
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "/help")
	assert.Contains(t, m.content.String(), "/clear")
}

func TestModelCompletionOverlayShowsOnSlash(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	// Type "/" into input
	m.input.SetValue("/")
	// Trigger an update to sync the overlay
	m.syncCompletion()

	assert.True(t, m.completion.Visible())
}

func TestModelCompletionViewRendered(t *testing.T) {
	reg := commands.NewRegistry()
	_ = reg.Register(commands.NewHelpCommand(reg))
	_ = reg.Register(commands.NewQuitCommand())
	m := NewModel(nil, "rubichan", "claude-3", 50, "", nil, reg)

	m.input.SetValue("/")
	m.syncCompletion()

	view := m.View()
	assert.Contains(t, view, "help")
	assert.Contains(t, view, "quit")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -v -run "TestNewModelWithRegistry|TestModelHandleCommandViaRegistry|TestModelCompletion"`
Expected: FAIL — NewModel wrong number of args

**Step 3: Implementation changes**

**`model.go` changes:**
- Add `cmdRegistry *commands.Registry` and `completion *CompletionOverlay` fields to `Model`
- Update `NewModel` signature to accept `*commands.Registry`
- Refactor `handleCommand()` to delegate to `cmdRegistry.Get()` and interpret `Result.Action`
- Add `syncCompletion()` helper

**`update.go` changes:**
- In `handleKeyMsg`, when `StateInput`:
  - Before forwarding to input area, check if overlay is visible
  - Tab → accept completion, fill input
  - Up/Down when overlay visible → navigate overlay instead of viewport scroll
  - Escape when overlay visible → dismiss
  - After forwarding any key to input area, call `syncCompletion()`

**`view.go` changes:**
- Between status bar and input line, render `m.completion.View()` if non-empty

**`cmd/rubichan/main.go` changes:**
- Create `commands.Registry`, register built-ins, pass to `NewModel`

**Step 4: Update ALL existing tests** that call `NewModel(nil, "rubichan", "claude-3", 50, "", nil)` to pass `nil` as the 7th arg (or create a helper):

All existing `NewModel(...)` calls in `model_test.go` gain an extra `nil` parameter. This is a mechanical change. A helper function can reduce duplication:

```go
func newTestModel() *Model {
	return NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
}
```

**Step 5: Run full test suite**

Run: `go test ./internal/tui/... -v`
Expected: PASS

Run: `go test ./... -count=1`
Expected: PASS (main.go also updated)

**Step 6: Commit**

```
[BEHAVIORAL] Wire completion overlay into TUI model and refactor handleCommand to use registry
```

---

### Task 6: Manifest Extension for Skill Commands

**Files:**
- Modify: `internal/skills/manifest.go` — add `Commands []CommandDef` to `SkillManifest`
- Modify: `internal/skills/manifest_test.go` — add parsing tests for commands field

**Step 1: Write the failing tests**

Add to `internal/skills/manifest_test.go`:

```go
func TestParseManifestWithCommands(t *testing.T) {
	data := []byte(`
name: kubernetes
version: "1.0.0"
description: Kubernetes skill
types: [tool]
implementation:
  backend: starlark
  entrypoint: main.star
commands:
  - name: pods
    description: "List Kubernetes pods"
    arguments:
      - name: namespace
        description: "Target namespace"
        required: false
  - name: deploy
    description: "Deploy a resource"
    arguments:
      - name: resource
        description: "Resource to deploy"
        required: true
`)
	m, err := ParseManifest(data)
	require.NoError(t, err)
	assert.Len(t, m.Commands, 2)
	assert.Equal(t, "pods", m.Commands[0].Name)
	assert.Len(t, m.Commands[0].Arguments, 1)
	assert.False(t, m.Commands[0].Arguments[0].Required)
	assert.Equal(t, "deploy", m.Commands[1].Name)
	assert.True(t, m.Commands[1].Arguments[0].Required)
}

func TestParseManifestWithoutCommands(t *testing.T) {
	data := []byte(`
name: simple
version: "1.0.0"
description: Simple skill
types: [prompt]
`)
	m, err := ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Commands)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/... -v -run TestParseManifestWithCommands`
Expected: FAIL — Commands field does not exist

**Step 3: Write minimal implementation**

Add to `SkillManifest` struct in `manifest.go`:

```go
Commands      []CommandDef         `yaml:"commands"`
```

Add the `CommandDef` and `CommandArgDef` types:

```go
// CommandDef declares a slash command contributed by a skill.
type CommandDef struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Arguments   []CommandArgDef `yaml:"arguments"`
}

// CommandArgDef describes an argument for a skill-contributed command.
type CommandArgDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/... -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add Commands field to SkillManifest for skill-contributed slash commands
```

---

### Task 7: SkillBackend Commands() and Runtime Wiring

**Files:**
- Modify: `internal/skills/types.go` — add `Commands()` to `SkillBackend` interface
- Modify: `internal/skills/runtime.go` — register/unregister commands on activate/deactivate
- Modify: All `SkillBackend` implementations to add `Commands()` method (starlark, goplugin, process, mcp, noopPromptBackend, test stubs)
- Test: `internal/skills/runtime_test.go` — add test for command registration on activate

This task adds the `Commands()` method returning `[]commands.SlashCommand`. Initially all backends return `nil` (no commands). The runtime wires registration/unregistration.

**Step 1: Write the failing test**

Add to `internal/skills/runtime_test.go`:

```go
func TestRuntimeActivateRegistersCommands(t *testing.T) {
	// Create a command registry
	cmdReg := commands.NewRegistry()

	// Create a backend that provides a command
	backend := &mockBackendWithCommands{
		cmds: []commands.SlashCommand{
			commands.NewQuitCommand(), // reuse for simplicity
		},
	}

	loader := NewLoader(t.TempDir(), t.TempDir())
	s := newTestStore(t)
	defer s.Close()
	toolReg := tools.NewRegistry()

	rt := NewRuntime(loader, s, toolReg, nil,
		func(_ SkillManifest, _ string) (SkillBackend, error) { return backend, nil },
		func(_ string, _ []Permission) PermissionChecker { return &noopChecker{} },
	)
	rt.SetCommandRegistry(cmdReg)

	// Add a skill manually
	rt.skills["test-skill"] = &Skill{
		Manifest: &SkillManifest{
			Name:    "test-skill",
			Version: "1.0.0",
			Description: "test",
			Types:   []SkillType{SkillTypeTool},
			Implementation: ImplementationConfig{
				Backend:    BackendStarlark,
				Entrypoint: "main.star",
			},
		},
		State:  SkillStateInactive,
		Source: SourceUser,
	}

	err := rt.Activate("test-skill")
	require.NoError(t, err)

	// The command should be registered
	_, ok := cmdReg.Get("quit")
	assert.True(t, ok)

	// Deactivate should unregister
	err = rt.Deactivate("test-skill")
	require.NoError(t, err)

	_, ok = cmdReg.Get("quit")
	assert.False(t, ok)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/... -v -run TestRuntimeActivateRegistersCommands`
Expected: FAIL — Commands() not in interface, SetCommandRegistry undefined

**Step 3: Write implementation**

- Add `Commands() []commands.SlashCommand` to `SkillBackend` interface in `types.go`
- Add `cmdRegistry *commands.Registry` field and `SetCommandRegistry()` method to `Runtime` in `runtime.go`
- In `Activate()`, after tool registration, register commands from `backend.Commands()`
- In `Deactivate()`, unregister commands from `backend.Commands()`
- Add `Commands()` returning `nil` to all existing backends: starlark engine, goplugin, process, mcp, noopPromptBackend, wiki backend, apple-dev backend, and all test mocks

**Step 4: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add Commands() to SkillBackend and wire registration in Runtime
```

---

### Task 8: Startup Wiring in main.go

**Files:**
- Modify: `cmd/rubichan/main.go` — create commands.Registry, register built-ins, pass to TUI and skill runtime

**Step 1: Write the wiring code**

In `runInteractive()`, after creating the tool registry and before creating the TUI model:

```go
// Create command registry with built-in slash commands.
cmdRegistry := commands.NewRegistry()
```

After creating the agent and before `tui.NewModel`:

```go
// Register built-in slash commands.
// Closures capture model fields that are set after NewModel.
_ = cmdRegistry.Register(commands.NewQuitCommand())
_ = cmdRegistry.Register(commands.NewExitCommand())
_ = cmdRegistry.Register(commands.NewHelpCommand(cmdRegistry))
_ = cmdRegistry.Register(commands.NewConfigCommand())
```

After model creation, register commands that need model references:

```go
// These commands need callbacks that reference the model.
_ = cmdRegistry.Register(commands.NewClearCommand(func() {
    if model.agent != nil {
        model.agent.ClearConversation()
    }
    model.content.Reset()
    model.viewport.SetContent("")
}))
_ = cmdRegistry.Register(commands.NewModelCommand(func(name string) {
    if model.agent != nil {
        model.agent.SetModel(name)
    }
    model.modelName = name
    model.statusBar.SetModel(name)
}))
```

Pass `cmdRegistry` to `tui.NewModel` and `rt.SetCommandRegistry()`.

**Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

**Step 3: Manual smoke test**

Run: `go run ./cmd/rubichan`
- Type `/` — completion popup should appear
- Type `/mo` — should filter to "model"
- Press Tab — should fill to "/model "
- Press Escape — popup should dismiss
- Type `/help` + Enter — should show all commands from registry

**Step 4: Commit**

```
[BEHAVIORAL] Wire command registry and completion overlay into main.go startup
```

---

### Task 9: Update Existing Tests

**Files:**
- Modify: `internal/tui/model_test.go` — update all `NewModel` calls to include registry parameter
- Modify: any other test files that construct `Model` directly

**Step 1: Mechanical update**

Add a helper at the top of `model_test.go`:

```go
func newTestModel() *Model {
	return NewModel(nil, "rubichan", "claude-3", 50, "", nil, nil)
}
```

Replace all `NewModel(nil, "rubichan", "claude-3", 50, "", nil)` calls with `newTestModel()` or add the extra `nil` arg.

**Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

**Step 3: Commit**

```
[STRUCTURAL] Update existing tests for new NewModel signature
```

---

### Task 10: Final Verification

**Step 1: Run full test suite with coverage**

Run: `go test -cover ./...`
Expected: All packages PASS, coverage >90% for new code

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No warnings

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No files listed

**Step 4: Commit any fixes**

If any issues found, fix and commit.
