package agentsdk

import "encoding/json"

// TurnEvent represents a streaming event emitted during an agent turn.
type TurnEvent struct {
	Type           string             // "text_delta", "thinking_delta", "input_json_delta", "tool_call", "tool_result", "tool_progress", "ui_request", "ui_update", "ui_response", "message_start", "context_overflow", "error", "done", "subagent_done"
	Text           string             // text content for text_delta and input_json_delta events
	Model          string             // populated for message_start events
	MessageID      string             // populated for message_start events
	ToolCall       *ToolCallEvent     // populated for tool_call events
	ToolResult     *ToolResultEvent   // populated for tool_result events
	ToolProgress   *ToolProgressEvent // populated for tool_progress events
	UIRequest      *UIRequest         // populated for ui_request events
	UIUpdate       *UIUpdate          // populated for ui_update events
	UIResponse     *UIResponse        // populated for ui_response events
	Error          error              // populated for error events
	InputTokens    int                // populated for done events: total input tokens used
	OutputTokens   int                // populated for done events: total output tokens used
	DiffSummary    string             // populated for done events: markdown-formatted cumulative file change summary
	SubagentResult *SubagentResult    // populated for subagent_done events
	ContextBudget  *ContextBudget     // populated for done events: per-component context usage breakdown
	ExitReason     TurnExitReason     // populated for done events: why the turn stopped
}

// ToolCallEvent contains details about a tool being called.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent contains details about a tool execution result.
type ToolResultEvent struct {
	ID             string
	Name           string
	Content        string
	DisplayContent string // shown to user; falls back to Content if empty
	IsError        bool
}

// ToolProgressEvent contains a streaming progress chunk from a tool execution.
type ToolProgressEvent struct {
	ID      string
	Name    string
	Stage   EventStage
	Content string
	IsError bool
}
