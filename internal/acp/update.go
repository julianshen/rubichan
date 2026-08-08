package acp

// ToolCallStatus is the lifecycle of a tool call as reported to the client.
type ToolCallStatus string

const (
	ToolCallPending    ToolCallStatus = "pending"
	ToolCallInProgress ToolCallStatus = "in_progress"
	ToolCallCompleted  ToolCallStatus = "completed"
	ToolCallFailed     ToolCallStatus = "failed"
)

// SessionNotification is the params of a session/update notification.
//
// Update is the tagged union of turn events. The tag lives in a field named
// sessionUpdate rather than in the JSON-RPC method, so every variant arrives
// under the same method name and a client demultiplexes on the field. Getting
// the tag wrong yields an update the client silently ignores rather than one it
// rejects, which is why the variants are built here instead of at call sites.
type SessionNotification struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// NewSessionNotification addresses an update to a session.
func NewSessionNotification(sessionID string, update any) SessionNotification {
	return SessionNotification{SessionID: sessionID, Update: update}
}

// agentMessageChunk is one piece of the agent's reply.
type agentMessageChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       ContentBlock `json:"content"`
}

// AgentMessageChunk carries a fragment of assistant text to the client. This is
// how the reply itself reaches the far side: session/prompt's result carries
// only a stop reason.
func AgentMessageChunk(text string) any {
	return agentMessageChunk{
		SessionUpdate: "agent_message_chunk",
		Content:       ContentBlock{Type: ContentText, Text: text},
	}
}

// toolCall announces a tool the agent has begun.
type toolCall struct {
	SessionUpdate string         `json:"sessionUpdate"`
	ToolCallID    string         `json:"toolCallId"`
	Title         string         `json:"title"`
	Status        ToolCallStatus `json:"status"`
}

// ToolCall announces a tool invocation as pending. The title is what a client
// shows a user, so it is the tool's name rather than its id.
func ToolCall(id, title string) any {
	return toolCall{
		SessionUpdate: "tool_call",
		ToolCallID:    id,
		Title:         title,
		Status:        ToolCallPending,
	}
}

// toolCallUpdate moves an announced tool call to a new status.
type toolCallUpdate struct {
	SessionUpdate string         `json:"sessionUpdate"`
	ToolCallID    string         `json:"toolCallId"`
	Status        ToolCallStatus `json:"status"`
}

// ToolCallUpdate reports a tool call's new status against the id ToolCall
// announced. A client matches on that id, so an update carrying a different one
// creates a second tool call in the UI rather than advancing the first.
func ToolCallUpdate(id string, status ToolCallStatus) any {
	return toolCallUpdate{
		SessionUpdate: "tool_call_update",
		ToolCallID:    id,
		Status:        status,
	}
}
