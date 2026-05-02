package agentsdk

import (
	"context"
	"encoding/json"
)

// Tool defines the interface for a tool that can be used by the AI agent.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

// EventStage describes the lifecycle stage of a streaming tool event.
type EventStage int

const (
	// EventBegin marks the start of a streaming tool execution.
	EventBegin EventStage = iota
	// EventDelta carries incremental output while the tool is running.
	EventDelta
	// EventEnd marks the end of a streaming tool execution.
	EventEnd
)

// String returns the stable wire/debug name for a streaming event stage.
func (s EventStage) String() string {
	switch s {
	case EventBegin:
		return "begin"
	case EventDelta:
		return "delta"
	case EventEnd:
		return "end"
	default:
		return "unknown"
	}
}

// ToolEvent is emitted by StreamingTool implementations during execution.
type ToolEvent struct {
	Stage   EventStage
	Content string
	IsError bool
}

// ToolEventEmitter handles streaming tool events.
type ToolEventEmitter func(ToolEvent)

// StreamingTool is an optional extension interface for tools that can emit
// real-time progress events while executing.
//
// Tools that don't implement this interface continue to run through Execute.
type StreamingTool interface {
	Tool
	ExecuteStream(ctx context.Context, input json.RawMessage, emit ToolEventEmitter) (ToolResult, error)
}

// SearchHinter is an optional interface that tools can implement to provide
// keyword hints for tool_search discovery. When a tool is deferred to save
// context, the search hint helps the LLM find it via keyword queries even
// when the tool name and description don't contain the exact search terms.
type SearchHinter interface {
	SearchHint() string
}

// ResultBudgeted is an optional interface that tools can implement to
// declare their preferred result size limit. When a tool's output exceeds
// this limit, the agent applies head+tail truncation before the aggregate
// per-message budget is enforced.
type ResultBudgeted interface {
	Tool
	// MaxResultChars returns the maximum number of characters this tool's
	// result should occupy in the conversation context. Zero or negative
	// means no per-tool limit (only the aggregate message budget applies).
	MaxResultChars() int
}

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	Content        string // sent to LLM conversation history
	DisplayContent string // shown to user; falls back to Content if empty
	IsError        bool
}

// Display returns the content intended for user display. It returns
// DisplayContent when set, otherwise falls back to Content.
func (r ToolResult) Display() string {
	if r.DisplayContent != "" {
		return r.DisplayContent
	}
	return r.Content
}
