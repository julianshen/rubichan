package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/cmux"
)

// CmuxOrchestrateTool runs multiple commands in parallel pane splits and
// polls the sidebar for completion signals. Delegates to cmux.Orchestrator.
type CmuxOrchestrateTool struct {
	client cmux.Caller
}

// NewCmuxOrchestrate creates a CmuxOrchestrateTool backed by client.
func NewCmuxOrchestrate(client cmux.Caller) *CmuxOrchestrateTool {
	return &CmuxOrchestrateTool{client: client}
}

func (t *CmuxOrchestrateTool) Name() string { return "cmux_orchestrate" }

func (t *CmuxOrchestrateTool) Description() string {
	return "Run multiple commands across split panes in parallel. " +
		"Each task gets its own pane. Polls sidebar logs for [DONE]/[ERROR] completion markers."
}

func (t *CmuxOrchestrateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"tasks": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"direction": {
							"type": "string",
							"enum": ["left", "right", "up", "down"],
							"description": "Split direction for this task's pane"
						},
						"command": {
							"type": "string",
							"description": "Shell command to run in the pane"
						}
					},
					"required": ["direction", "command"]
				},
				"description": "Tasks to run in parallel"
			},
			"timeout": {
				"type": "string",
				"description": "Overall timeout, e.g. '5m', '30s'. Defaults to '5m'."
			}
		},
		"required": ["tasks"]
	}`)
}

type orchestrateTask struct {
	Direction string `json:"direction"`
	Command   string `json:"command"`
}

type orchestrateInput struct {
	Tasks   []orchestrateTask `json:"tasks"`
	Timeout string            `json:"timeout"`
}

func (t *CmuxOrchestrateTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var in orchestrateInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}
	if len(in.Tasks) == 0 {
		return ToolResult{Content: "tasks must not be empty", IsError: true}, nil
	}

	timeout := 5 * time.Minute
	if in.Timeout != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("invalid timeout %q: %s", in.Timeout, err), IsError: true}, nil
		}
		timeout = d
	}

	orch := cmux.NewOrchestrator(t.client)
	orch.SetPollRate(2 * time.Second)

	for _, task := range in.Tasks {
		if _, err := orch.Dispatch(task.Direction, task.Command); err != nil {
			return ToolResult{Content: fmt.Sprintf("dispatch failed: %s", err), IsError: true}, nil
		}
	}

	results, err := orch.Wait(timeout)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("orchestration failed: %s", err), IsError: true}, nil
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("surface %s (%s): %s\n", r.SurfaceID, r.Command, r.Status))
	}
	return ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
}
