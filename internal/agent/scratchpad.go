package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ScratchpadAccess defines the interface for tools to interact with the
// agent's scratchpad. This breaks the import cycle between tools/ and agent/.
type ScratchpadAccess interface {
	Set(tag, content string)
	Get(tag string) string
	Delete(tag string)
	All() map[string]string
}

// Scratchpad provides structured note-taking that survives context compaction.
// Notes are stored as tag → content pairs and rendered into the system prompt.
type Scratchpad struct {
	mu    sync.RWMutex
	notes map[string]string
}

// NewScratchpad creates an empty Scratchpad.
func NewScratchpad() *Scratchpad {
	return &Scratchpad{notes: make(map[string]string)}
}

// Set stores a note under the given tag, replacing any existing note.
func (s *Scratchpad) Set(tag, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes[tag] = content
}

// Get retrieves the note for the given tag. Returns "" if not found.
func (s *Scratchpad) Get(tag string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notes[tag]
}

// Delete removes a note by tag.
func (s *Scratchpad) Delete(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notes, tag)
}

// All returns a copy of all notes.
func (s *Scratchpad) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.notes))
	for k, v := range s.notes {
		cp[k] = v
	}
	return cp
}

// Render formats all notes as a markdown section for system prompt injection.
// Returns "" if there are no notes.
func (s *Scratchpad) Render() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.notes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Agent Notes\n\n")

	// Sort tags for deterministic output.
	tags := make([]string, 0, len(s.notes))
	for tag := range s.notes {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	for _, tag := range tags {
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", tag, s.notes[tag]))
	}

	return sb.String()
}
