package a2atools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/a2a"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// DispatchTool picks the best-matching known agent for a task description
// (via Registry.Match's deterministic tag/word overlap, not an LLM call)
// and calls it.
type DispatchTool struct {
	cfg        *config.Config
	registry   *a2a.Registry
	httpClient *http.Client
}

// NewDispatchTool creates a DispatchTool over reg.
func NewDispatchTool(cfg *config.Config, reg *a2a.Registry) *DispatchTool {
	return &DispatchTool{cfg: cfg, registry: reg}
}

func (d *DispatchTool) Name() string { return "a2a_dispatch" }

func (d *DispatchTool) Description() string {
	return "Find the best-matching known A2A agent for a task description (by skill tags, " +
		"not an LLM call) and call it. Returns an error if no agent matches."
}

func (d *DispatchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_description": {"type": "string", "description": "What needs to be done"}
		},
		"required": ["task_description"]
	}`)
}

type dispatchInput struct {
	TaskDescription string `json:"task_description"`
}

func (d *DispatchTool) Execute(ctx context.Context, input json.RawMessage) (agentsdk.ToolResult, error) {
	var in dispatchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	matches := d.registry.Match(in.TaskDescription)
	if len(matches) == 0 {
		return agentsdk.ToolResult{Content: "no known agent matches this task description", IsError: true}, nil
	}
	best := matches[0]

	url, apiKey, err := resolveAgentURL(d.cfg, best)
	if err != nil {
		return agentsdk.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	client := a2a.NewClient(url, apiKey, d.httpClient)
	task, err := client.Send(ctx, a2a.Message{Role: "user", Parts: []a2a.Part{{Kind: "text", Text: in.TaskDescription}}})
	if err != nil {
		return agentsdk.ToolResult{Content: fmt.Sprintf("dispatch to %s failed: %v", best, err), IsError: true}, nil
	}
	if task.State != a2a.TaskCompleted {
		return agentsdk.ToolResult{Content: fmt.Sprintf("%s ended in state %q: %s", best, task.State, task.Error), IsError: true}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] ", best)
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			b.WriteString(p.Text)
		}
	}
	return agentsdk.ToolResult{Content: b.String()}, nil
}
