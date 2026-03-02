package tools

import (
	"fmt"
	"strings"
	"sync"
)

// Operation represents the type of file modification.
type Operation int

const (
	// OpCreated indicates a new file was created.
	OpCreated Operation = iota
	// OpModified indicates an existing file was modified.
	OpModified
	// OpDeleted indicates a file was deleted.
	OpDeleted
)

// String returns the human-readable name of an Operation.
func (o Operation) String() string {
	switch o {
	case OpCreated:
		return "created"
	case OpModified:
		return "modified"
	case OpDeleted:
		return "deleted"
	default:
		return fmt.Sprintf("Operation(%d)", o)
	}
}

// FileChange records a single file modification made during an agent turn.
type FileChange struct {
	// Path is the file path relative to the working directory.
	Path string
	// Operation is the type of change (created, modified, deleted).
	Operation Operation
	// Diff holds the unified diff text for the change.
	Diff string
	// Tool is the name of the tool that made the change.
	Tool string
}

// DiffTracker accumulates file changes across an agent turn and provides
// a consolidated summary. All methods are safe for concurrent use.
type DiffTracker struct {
	mu      sync.Mutex
	changes []FileChange
}

// NewDiffTracker creates a new DiffTracker with an empty change list.
func NewDiffTracker() *DiffTracker {
	return &DiffTracker{}
}

// Record adds a file change to the tracker.
func (dt *DiffTracker) Record(change FileChange) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.changes = append(dt.changes, change)
}

// Changes returns a copy of all recorded file changes.
func (dt *DiffTracker) Changes() []FileChange {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	cp := make([]FileChange, len(dt.changes))
	copy(cp, dt.changes)
	return cp
}

// Reset clears all recorded changes. Call at the start of a new turn.
func (dt *DiffTracker) Reset() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.changes = nil
}

// Summarize generates a human-readable summary of all changes in the current
// turn. Returns an empty string if no changes were recorded.
func (dt *DiffTracker) Summarize() string {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if len(dt.changes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Turn Summary: %d file(s) changed\n\n", len(dt.changes)))

	for _, c := range dt.changes {
		sb.WriteString(fmt.Sprintf("- **%s** %s (via %s)\n", c.Path, c.Operation, c.Tool))
		if c.Diff != "" {
			sb.WriteString("```diff\n")
			sb.WriteString(c.Diff)
			if !strings.HasSuffix(c.Diff, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("```\n")
		}
	}

	return sb.String()
}
