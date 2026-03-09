package tools

import (
	"strings"
	"sync"
)

// CommandAllowlist maintains a session-scoped set of approved shell commands.
// Commands can be approved individually (exact match) or by prefix pattern.
// Read-only commands are automatically allowed without explicit approval.
// All methods are safe for concurrent use.
type CommandAllowlist struct {
	mu       sync.RWMutex
	exact    map[string]bool
	patterns []string
}

// NewCommandAllowlist creates an empty allowlist.
func NewCommandAllowlist() *CommandAllowlist {
	return &CommandAllowlist{
		exact: make(map[string]bool),
	}
}

// Allow adds an exact command string to the allowlist.
func (al *CommandAllowlist) Allow(command string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.exact[command] = true
}

// AllowPattern adds a command prefix pattern. Any command starting with this
// prefix will be considered allowed.
func (al *CommandAllowlist) AllowPattern(prefix string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	for _, p := range al.patterns {
		if p == prefix {
			return
		}
	}
	al.patterns = append(al.patterns, prefix)
}

// IsAllowed checks whether a command is allowed. A command is allowed if:
//  1. It is a read-only command (auto-bypass), or
//  2. It was explicitly allowed via Allow (exact match), or
//  3. It matches a pattern added via AllowPattern (prefix match).
func (al *CommandAllowlist) IsAllowed(command string) bool {
	if IsReadOnlyCommand(command) {
		return true
	}

	al.mu.RLock()
	defer al.mu.RUnlock()

	if al.exact[command] {
		return true
	}

	for _, prefix := range al.patterns {
		if strings.HasPrefix(command, prefix) {
			// Ensure the match is at a word boundary: the command must either
			// equal the prefix exactly or the character after the prefix must
			// be a space. This prevents "git status; rm -rf /" from matching
			// a pattern of "git status".
			if len(command) == len(prefix) || command[len(prefix)] == ' ' {
				return true
			}
		}
	}

	return false
}

// Clear removes all explicit approvals and patterns, resetting the allowlist.
// Read-only commands continue to be auto-allowed.
func (al *CommandAllowlist) Clear() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.exact = make(map[string]bool)
	al.patterns = nil
}
