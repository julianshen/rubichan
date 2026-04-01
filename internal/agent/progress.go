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
		trimmed := make([]ProgressEntry, p.maxEntries)
		copy(trimmed, p.entries[len(p.entries)-p.maxEntries:])
		p.entries = trimmed
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
	sb.WriteString("| Turn | Action | Detail | Result |\n")
	sb.WriteString("|------|--------|--------|--------|\n")

	for _, e := range p.entries {
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n",
			e.Turn,
			escapeTableCell(e.Action),
			escapeTableCell(e.Detail),
			escapeTableCell(e.Result),
		))
	}

	return sb.String()
}

// escapeTableCell sanitizes a string for use inside a markdown table cell.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
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
		return "ran command", truncateResult(cmd, 80)
	case "search":
		pattern := jsonStr(parsed["pattern"])
		return "searched", pattern
	case "git_status":
		path := jsonStr(parsed["path"])
		return "git status", path
	case "git_diff":
		path := jsonStr(parsed["path"])
		return "git diff", path
	case "git_log":
		path := jsonStr(parsed["path"])
		return "git log", path
	case "git_show":
		return "git show", jsonStr(parsed["rev"])
	case "git_blame":
		return "git blame", jsonStr(parsed["path"])
	case "process":
		op := jsonStr(parsed["operation"])
		switch op {
		case "exec":
			return "started process", truncateResult(jsonStr(parsed["command"]), 60)
		case "kill":
			return "killed process", jsonStr(parsed["id"])
		default:
			return "process " + op, jsonStr(parsed["id"])
		}
	case "notes":
		op := jsonStr(parsed["action"])
		return "notes " + op, jsonStr(parsed["tag"])
	case "task":
		desc := jsonStr(parsed["description"])
		return "spawned task", truncateResult(desc, 60)
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

// truncateResult returns a prefix of s up to maxLen runes, appending "..." if truncated.
func truncateResult(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
