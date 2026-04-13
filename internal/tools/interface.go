package tools

import (
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// Type aliases — all existing code using tools.Tool etc. compiles unchanged.

// Tool defines the interface for a tool that can be used by the AI agent.
type Tool = agentsdk.Tool

// EventStage describes the lifecycle stage of a streaming tool event.
type EventStage = agentsdk.EventStage

// Re-export constants so tools.EventBegin etc. continue to work.
const (
	EventBegin = agentsdk.EventBegin
	EventDelta = agentsdk.EventDelta
	EventEnd   = agentsdk.EventEnd
)

// ToolEvent is emitted by StreamingTool implementations during execution.
type ToolEvent = agentsdk.ToolEvent

// ToolEventEmitter handles streaming tool events.
type ToolEventEmitter = agentsdk.ToolEventEmitter

// StreamingTool is an optional extension interface for tools that can emit
// real-time progress events while executing.
type StreamingTool = agentsdk.StreamingTool

// SearchHinter is an optional interface for tools that provide keyword hints
// for tool_search discovery of deferred tools.
type SearchHinter = agentsdk.SearchHinter

// ResultCapped is an optional interface for tools that declare a
// per-result byte cap so oversize output is truncated before entering
// the conversation.
type ResultCapped = agentsdk.ResultCapped

// ConcurrencySafeTool is an optional marker interface for tools that
// can be dispatched during streaming because they have no observable
// side effects.
type ConcurrencySafeTool = agentsdk.ConcurrencySafeTool

// ToolResult represents the result of executing a tool.
type ToolResult = agentsdk.ToolResult
