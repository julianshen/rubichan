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
		if !strings.HasPrefix(command, prefix) {
			continue
		}
		// Ensure the match is at a word boundary.
		if len(command) > len(prefix) && command[len(prefix)] != ' ' {
			continue
		}
		// Reject commands that contain shell separators (;, &&, ||, |)
		// after the prefix — these could chain arbitrary commands.
		// Also reject output redirection (>, >>).
		if containsShellSeparatorOrRedirect(command) {
			continue
		}
		return true
	}

	return false
}

// containsShellSeparatorOrRedirect returns true if the command contains any
// shell control operator (;, &&, ||, |) or output redirection (>, >>) outside
// of quoted strings. This prevents pattern-matched allowlist entries from
// being exploited by appending chained commands.
func containsShellSeparatorOrRedirect(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if r == ';' || r == '>' {
			return true
		}
		if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			return true
		}
		if r == '|' {
			return true
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
