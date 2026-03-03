package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// TaskStatusProvider returns the status of background tasks.
type TaskStatusProvider interface {
	BackgroundTaskStatus() []BackgroundTaskInfo
}

// BackgroundTaskInfo describes a background task for the ListTasksTool.
type BackgroundTaskInfo struct {
	ID        string
	AgentName string
	Status    string
}

// ListTasksTool lists the status of background subagent tasks.
type ListTasksTool struct {
	provider TaskStatusProvider
}

// NewListTasksTool creates a ListTasksTool backed by the given provider.
func NewListTasksTool(provider TaskStatusProvider) *ListTasksTool {
	return &ListTasksTool{provider: provider}
}

func (t *ListTasksTool) Name() string        { return "list_tasks" }
func (t *ListTasksTool) Description() string { return "List background subagent task status" }

func (t *ListTasksTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ListTasksTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	statuses := t.provider.BackgroundTaskStatus()
	if len(statuses) == 0 {
		return ToolResult{Content: "No background tasks running."}, nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d background task(s):\n", len(statuses)))
	for _, s := range statuses {
		sb.WriteString(fmt.Sprintf("  - %s [%s] %s\n", s.ID, s.AgentName, s.Status))
	}
	return ToolResult{Content: sb.String()}, nil
}
