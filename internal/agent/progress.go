package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ProgressEntry records a single structured milestone during agent execution.
type ProgressEntry struct {
	Turn   int    // which turn this happened
	Action string // what was done: "wrote file", "ran command", "searched", etc.
	Detail string // specific detail: file path, command, pattern
	Result string // brief outcome: "ok", "error: ...", "42 matches"
}

// ProgressTracker automatically records structured milestones from tool
// results. Unlike the free-form scratchpad, entries are auto-populated and
// injected into the system prompt as a persistent "what happened so far"
// section that survives context compaction.
type ProgressTracker struct {
	mu         sync.Mutex
	entries    []ProgressEntry
	maxEntries int // cap to prevent unbounded growth (default 50)
}

// NewProgressTracker creates a ProgressTracker with the default capacity of 50.
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{maxEntries: 50}
}

// Record adds a progress entry. If entries exceed maxEntries, oldest are trimmed.
func (p *ProgressTracker) Record(turn int, action, detail, result string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.entries = append(p.entries, ProgressEntry{
		Turn:   turn,
		Action: action,
		Detail: detail,
		Result: result,
	})

	if len(p.entries) > p.maxEntries {
		p.entries = p.entries[len(p.entries)-p.maxEntries:]
	}
}

// Entries returns a copy of all recorded entries.
func (p *ProgressTracker) Entries() []ProgressEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	cp := make([]ProgressEntry, len(p.entries))
	copy(cp, p.entries)
	return cp
}

// Render returns a markdown-formatted progress summary for the system prompt.
// Returns "" if there are no entries.
func (p *ProgressTracker) Render() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("| # | Action | Detail | Result |\n")
	sb.WriteString("|---|--------|--------|--------|\n")

	for i, e := range p.entries {
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n",
			i+1,
			escapeTableCell(e.Action),
			escapeTableCell(e.Detail),
			escapeTableCell(e.Result),
		))
	}

	return sb.String()
}

// escapeTableCell replaces pipe characters that would break the markdown table.
func escapeTableCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// classifyToolAction maps a tool name and its JSON input to a human-readable
// (action, detail) pair for progress tracking.
func classifyToolAction(toolName string, input json.RawMessage) (action, detail string) {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(input, &parsed); err != nil {
		return toolName, ""
	}

	switch toolName {
	case "file":
		op := jsonStr(parsed["operation"])
		path := jsonStr(parsed["path"])
		switch op {
		case "write":
			return "wrote file", path
		case "read":
			return "read file", path
		case "patch":
			return "patched file", path
		default:
			return "file op", path
		}
	case "shell":
		cmd := jsonStr(parsed["command"])
		if len(cmd) > 80 {
			cmd = cmd[:80] + "..."
		}
		return "ran command", cmd
	case "search":
		pattern := jsonStr(parsed["pattern"])
		return "searched", pattern
	case "task":
		desc := jsonStr(parsed["description"])
		if len(desc) > 60 {
			desc = desc[:60] + "..."
		}
		return "spawned task", desc
	case "task_complete":
		return "completed task", jsonStr(parsed["summary"])
	default:
		return toolName, ""
	}
}

// jsonStr extracts a JSON string value from a RawMessage. Returns "" on error.
func jsonStr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// truncateResult returns a prefix of s up to maxLen bytes, appending "..." if truncated.
func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
