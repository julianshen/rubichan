package shell

import (
	"os"
	"strings"
)

// CommandHistory manages readline history with persistence.
type CommandHistory struct {
	entries  []string
	maxSize  int
	position int // cursor for Previous/Next navigation; -1 means past-the-end
}

// NewCommandHistory creates a history with the given maximum size.
func NewCommandHistory(maxSize int) *CommandHistory {
	return &CommandHistory{
		maxSize:  maxSize,
		position: -1,
	}
}

// Add appends an entry to the history. Consecutive duplicates are suppressed.
func (h *CommandHistory) Add(line string) {
	// Suppress consecutive duplicates
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
		return
	}

	h.entries = append(h.entries, line)

	// Evict oldest if over capacity
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}

	// Reset navigation position
	h.position = -1
}

// Entries returns all history entries in order (oldest first).
func (h *CommandHistory) Entries() []string {
	if len(h.entries) == 0 {
		return nil
	}
	result := make([]string, len(h.entries))
	copy(result, h.entries)
	return result
}

// Previous moves backward in history and returns the entry.
// Returns ("", false) when at the beginning.
func (h *CommandHistory) Previous() (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}

	if h.position == -1 {
		// Start from the end
		h.position = len(h.entries) - 1
	} else if h.position > 0 {
		h.position--
	} else {
		// Already at the beginning
		return "", false
	}

	return h.entries[h.position], true
}

// Next moves forward in history and returns the entry.
// Returns ("", false) when past the end.
func (h *CommandHistory) Next() (string, bool) {
	if len(h.entries) == 0 || h.position == -1 {
		return "", false
	}

	if h.position < len(h.entries)-1 {
		h.position++
		return h.entries[h.position], true
	}

	// Past the end
	h.position = -1
	return "", false
}

// Save writes the history to a file at the given path.
func (h *CommandHistory) Save(path string) error {
	content := strings.Join(h.entries, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// Load reads history from a file at the given path.
func (h *CommandHistory) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	h.entries = nil
	for _, line := range lines {
		if line != "" {
			h.entries = append(h.entries, line)
		}
	}

	// Apply max size
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}

	h.position = -1
	return nil
}
