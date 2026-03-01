package commands

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCommand implements SlashCommand for testing.
type stubCommand struct {
	name string
	desc string
}

func (s *stubCommand) Name() string                                          { return s.name }
func (s *stubCommand) Description() string                                   { return s.desc }
func (s *stubCommand) Arguments() []ArgumentDef                              { return nil }
func (s *stubCommand) Complete(_ context.Context, _ []string) []Candidate    { return nil }
func (s *stubCommand) Execute(_ context.Context, _ []string) (Result, error) { return Result{}, nil }

// --- Task 1: Type Tests ---

func TestActionConstantsAreDistinct(t *testing.T) {
	actions := []Action{ActionNone, ActionQuit, ActionOpenConfig}
	seen := make(map[Action]bool)
	for _, a := range actions {
		assert.False(t, seen[a], "duplicate Action constant: %d", a)
		seen[a] = true
	}
}

func TestActionNoneIsZero(t *testing.T) {
	assert.Equal(t, Action(0), ActionNone)
}

func TestCandidateFields(t *testing.T) {
	c := Candidate{Value: "test", Description: "a test candidate"}
	assert.Equal(t, "test", c.Value)
	assert.Equal(t, "a test candidate", c.Description)
}

func TestArgumentDefFields(t *testing.T) {
	a := ArgumentDef{
		Name:        "file",
		Description: "file path",
		Required:    true,
		Static:      []string{"a.go", "b.go"},
	}
	assert.Equal(t, "file", a.Name)
	assert.Equal(t, "file path", a.Description)
	assert.True(t, a.Required)
	assert.Equal(t, []string{"a.go", "b.go"}, a.Static)
}

func TestResultFields(t *testing.T) {
	r := Result{Output: "done", Action: ActionQuit}
	assert.Equal(t, "done", r.Output)
	assert.Equal(t, ActionQuit, r.Action)
}

// --- Task 2: Registry Tests ---

func TestNewRegistryIsEmpty(t *testing.T) {
	reg := NewRegistry()
	assert.Empty(t, reg.All())
}

func TestRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	cmd := &stubCommand{name: "test", desc: "a test command"}

	err := reg.Register(cmd)
	require.NoError(t, err)

	got, ok := reg.Get("test")
	assert.True(t, ok)
	assert.Equal(t, "test", got.Name())
	assert.Equal(t, "a test command", got.Description())
}

func TestRegisterDuplicateReturnsError(t *testing.T) {
	reg := NewRegistry()
	cmd := &stubCommand{name: "dup", desc: "first"}

	err := reg.Register(cmd)
	require.NoError(t, err)

	err = reg.Register(&stubCommand{name: "dup", desc: "second"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegisterNilReturnsError(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestUnregister(t *testing.T) {
	reg := NewRegistry()
	cmd := &stubCommand{name: "rm", desc: "removable"}

	require.NoError(t, reg.Register(cmd))

	err := reg.Unregister("rm")
	require.NoError(t, err)

	_, ok := reg.Get("rm")
	assert.False(t, ok)
}

func TestUnregisterNotFoundReturnsError(t *testing.T) {
	reg := NewRegistry()

	err := reg.Unregister("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestGetNotFound(t *testing.T) {
	reg := NewRegistry()

	_, ok := reg.Get("missing")
	assert.False(t, ok)
}

func TestAllReturnsSorted(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "charlie", desc: "c"}))
	require.NoError(t, reg.Register(&stubCommand{name: "alpha", desc: "a"}))
	require.NoError(t, reg.Register(&stubCommand{name: "bravo", desc: "b"}))

	all := reg.All()
	require.Len(t, all, 3)
	assert.Equal(t, "alpha", all[0].Name())
	assert.Equal(t, "bravo", all[1].Name())
	assert.Equal(t, "charlie", all[2].Name())
}

func TestMatchPrefixFiltering(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "help", desc: "show help"}))
	require.NoError(t, reg.Register(&stubCommand{name: "history", desc: "show history"}))
	require.NoError(t, reg.Register(&stubCommand{name: "quit", desc: "quit"}))

	matches := reg.Match("h")
	require.Len(t, matches, 2)
	assert.Equal(t, "help", matches[0].Value)
	assert.Equal(t, "history", matches[1].Value)

	// More specific prefix narrows results
	matches = reg.Match("he")
	require.Len(t, matches, 1)
	assert.Equal(t, "help", matches[0].Value)
}

func TestMatchEmptyReturnsAll(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "alpha", desc: "a"}))
	require.NoError(t, reg.Register(&stubCommand{name: "bravo", desc: "b"}))

	matches := reg.Match("")
	require.Len(t, matches, 2)
	assert.Equal(t, "alpha", matches[0].Value)
	assert.Equal(t, "bravo", matches[1].Value)
}

func TestMatchCaseInsensitive(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "Help", desc: "show help"}))

	matches := reg.Match("he")
	require.Len(t, matches, 1)
	assert.Equal(t, "Help", matches[0].Value)

	matches = reg.Match("HE")
	require.Len(t, matches, 1)
	assert.Equal(t, "Help", matches[0].Value)
}

func TestMatchReturnsSorted(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "model", desc: "switch model"}))
	require.NoError(t, reg.Register(&stubCommand{name: "map", desc: "map things"}))
	require.NoError(t, reg.Register(&stubCommand{name: "merge", desc: "merge"}))

	matches := reg.Match("m")
	require.Len(t, matches, 3)
	assert.Equal(t, "map", matches[0].Value)
	assert.Equal(t, "merge", matches[1].Value)
	assert.Equal(t, "model", matches[2].Value)
}

func TestMatchCandidateHasDescription(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubCommand{name: "help", desc: "show help info"}))

	matches := reg.Match("h")
	require.Len(t, matches, 1)
	assert.Equal(t, "show help info", matches[0].Description)
}

func TestConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "cmd" + string(rune('A'+n%26))
			_ = reg.Register(&stubCommand{name: name, desc: "desc"})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.All()
			reg.Match("cmd")
			reg.Get("cmdA")
		}()
	}

	wg.Wait()
	// Test passes if no data race is detected (run with -race)
}
