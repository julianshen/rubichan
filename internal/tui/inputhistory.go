package tui

// InputHistory stores submitted prompts in a bounded list for recall
// via Ctrl+P (previous) and Ctrl+N (next).
type InputHistory struct {
	entries []string
	cursor  int    // -1 = composing new input (not browsing)
	draft   string // saves in-progress text when browsing
	maxSize int
}

// NewInputHistory creates a new history with the given max size.
// A maxSize less than 1 is clamped to 1.
func NewInputHistory(maxSize int) *InputHistory {
	if maxSize < 1 {
		maxSize = 1
	}
	return &InputHistory{
		cursor:  -1,
		maxSize: maxSize,
	}
}

// Add appends a prompt to history, resetting the cursor.
// Duplicate consecutive entries are suppressed.
func (h *InputHistory) Add(entry string) {
	if entry == "" {
		return
	}
	// Skip duplicates of the most recent entry.
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == entry {
		h.cursor = -1
		h.draft = ""
		return
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}
	h.cursor = -1
	h.draft = ""
}

// Previous moves the cursor back in history. If this is the first call
// (cursor == -1), currentInput is saved as draft. Returns the history
// entry and true, or ("", false) if already at the beginning.
func (h *InputHistory) Previous(currentInput string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.cursor == -1 {
		h.draft = currentInput
		h.cursor = len(h.entries) - 1
		return h.entries[h.cursor], true
	}
	if h.cursor <= 0 {
		return "", false
	}
	h.cursor--
	return h.entries[h.cursor], true
}

// Next moves the cursor forward. If past the last entry, restores the
// draft and returns it. Returns ("", false) if not browsing.
func (h *InputHistory) Next() (string, bool) {
	if h.cursor == -1 {
		return "", false
	}
	h.cursor++
	if h.cursor >= len(h.entries) {
		// Restore draft
		h.cursor = -1
		return h.draft, true
	}
	return h.entries[h.cursor], true
}

// Len returns the number of entries in history.
func (h *InputHistory) Len() int {
	return len(h.entries)
}
