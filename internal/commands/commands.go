package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Action represents the side-effect a slash command requests from the host.
type Action int

const (
	// ActionNone indicates no special action is needed.
	ActionNone Action = iota
	// ActionQuit requests the host to terminate.
	ActionQuit
	// ActionOpenConfig requests the host to open the configuration editor.
	ActionOpenConfig
)

// Candidate represents a completion suggestion.
type Candidate struct {
	Value       string
	Description string
}

// ArgumentDef describes a single argument accepted by a slash command.
type ArgumentDef struct {
	Name        string
	Description string
	Required    bool
	Static      []string
}

// Result is the outcome of executing a slash command.
type Result struct {
	Output string
	Action Action
}

// SlashCommand defines the interface for a user-invokable slash command.
type SlashCommand interface {
	Name() string
	Description() string
	Arguments() []ArgumentDef
	Complete(ctx context.Context, args []string) []Candidate
	Execute(ctx context.Context, args []string) (Result, error)
}

// Registry manages a collection of slash commands. All methods are safe for
// concurrent use.
type Registry struct {
	mu   sync.RWMutex
	cmds map[string]SlashCommand
}

// NewRegistry creates a new empty command registry.
func NewRegistry() *Registry {
	return &Registry{cmds: make(map[string]SlashCommand)}
}

// Register adds a command to the registry. Returns an error if a command
// with the same name is already registered or if cmd is nil.
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

// Unregister removes a command from the registry by name. Returns an error
// if the command is not registered.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cmds[name]; !exists {
		return fmt.Errorf("command not registered: %s", name)
	}
	delete(r.cmds, name)
	return nil
}

// Get retrieves a command by name. Returns the command and true if found,
// or nil and false if not found.
func (r *Registry) Get(name string) (SlashCommand, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cmd, ok := r.cmds[name]
	return cmd, ok
}

// All returns all registered commands sorted by name.
func (r *Registry) All() []SlashCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cmds := make([]SlashCommand, 0, len(r.cmds))
	for _, cmd := range r.cmds {
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name() < cmds[j].Name()
	})
	return cmds
}

// Match returns completion candidates for commands whose names match the
// given prefix (case-insensitive). Results are sorted by name.
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
