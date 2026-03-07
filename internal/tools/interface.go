package tools

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

// ToolResult represents the result of executing a tool.
type ToolResult struct {
	Content        string // sent to LLM conversation history
	DisplayContent string // shown to user; falls back to Content if empty
	IsError        bool
}

// EventStage represents the lifecycle stage of a streaming tool event.
type EventStage int

const (
	// EventBegin signals the start of a streaming tool execution.
	EventBegin EventStage = iota
	// EventDelta carries incremental output during execution.
	EventDelta
	// EventEnd signals the completion of streaming execution.
	EventEnd
)

// ToolEvent represents a streaming event emitted during tool execution.
type ToolEvent struct {
	Stage   EventStage
	Content string
	IsError bool
}

// StreamingTool extends Tool with streaming execution capability.
// Tools that implement this interface emit real-time progress events
// during execution. Tools that don't implement it fall back to
// synchronous Execute().
type StreamingTool interface {
	Tool
	ExecuteStream(ctx context.Context, input json.RawMessage, emit func(ToolEvent)) (ToolResult, error)
}

type emitterContextKey struct{}

// WithEmitter returns a new context carrying the given emit function.
func WithEmitter(ctx context.Context, emit func(ToolEvent)) context.Context {
	return context.WithValue(ctx, emitterContextKey{}, emit)
}

// EmitterFromContext extracts the emit function from the context.
// Returns nil if no emitter was set.
func EmitterFromContext(ctx context.Context) func(ToolEvent) {
	if emit, ok := ctx.Value(emitterContextKey{}).(func(ToolEvent)); ok {
		return emit
	}
	return nil
}

// Display returns the content intended for user display. It returns
// DisplayContent when set, otherwise falls back to Content.
func (r ToolResult) Display() string {
	if r.DisplayContent != "" {
		return r.DisplayContent
	}
	return r.Content
}
