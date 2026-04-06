// internal/tui/turnrenderer.go
package tui

import (
	"context"
	"strings"
	"time"
)

// TurnRenderer encapsulates all turn rendering logic.
// It is a pure function: takes immutable turn data, returns rendered string.
// Zero state, zero side effects — easy to test and parallelize.
type TurnRenderer struct{}

// RenderOptions controls rendering behavior (streaming state, width, etc.)
type RenderOptions struct {
	Width          int  // viewport width in characters
	IsStreaming    bool // whether turn is still streaming (affects UI state)
	CollapsedTools bool // whether tool results are collapsed
	HighlightError bool // highlight error messages in red
	MaxToolLines   int  // truncate tool output beyond this many lines (0 = no limit)
}

// Turn represents the complete state of a rendered turn (immutable).
// Extracted from Model's streaming state for rendering.
type Turn struct {
	ID            string             // unique turn identifier (for caching)
	AssistantText string             // raw assistant message text
	ThinkingText  string             // raw thinking block content (empty if not present)
	ToolCalls     []RenderedToolCall // all tool calls in this turn
	Status        string             // "streaming", "done", or "error"
	ErrorMsg      string             // error message (if status == "error")
	StartTime     time.Time          // when turn started (for elapsed time display)
}

// RenderedToolCall represents a single tool invocation and its result.
type RenderedToolCall struct {
	ID        string // tool call ID
	Name      string // tool name (e.g., "file", "shell")
	Args      string // formatted tool arguments
	Result    string // raw tool output
	IsError   bool   // whether this tool call failed
	Collapsed bool   // true = show collapsed summary; false = show full output
	LineCount int    // total lines in Result (before truncation)
}

// Render produces the complete text representation of a turn.
// This is the main entry point used by Model.View().
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error) {
	var output strings.Builder

	// Render thinking block if present
	if turn.ThinkingText != "" {
		output.WriteString("🧠 Thinking...\n")
		output.WriteString(turn.ThinkingText)
		output.WriteString("\n\n")
	}

	// Render assistant message
	if turn.AssistantText != "" {
		output.WriteString(turn.AssistantText)
		output.WriteString("\n")
	}

	return output.String(), nil
}
