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
// polls the sidebar for completion signals.
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

// taskState tracks one running task.
type taskState struct {
	surfaceID string
	command   string
	done      bool
	status    string // "done", "error", "timeout"
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

	// Launch each task: split pane, send command + enter.
	states := make([]*taskState, 0, len(in.Tasks))
	for _, task := range in.Tasks {
		resp, err := t.client.Call("surface.split", map[string]string{"direction": task.Direction})
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("surface.split failed: %s", err), IsError: true}, nil
		}
		if !resp.OK {
			return ToolResult{Content: fmt.Sprintf("surface.split error: %s", resp.Error), IsError: true}, nil
		}
		var surf cmux.Surface
		if err := json.Unmarshal(resp.Result, &surf); err != nil {
			return ToolResult{Content: fmt.Sprintf("decode surface: %s", err), IsError: true}, nil
		}

		// Send command text.
		if resp, err := t.client.Call("surface.send_text", map[string]string{
			"surface_id": surf.ID,
			"text":       task.Command,
		}); err != nil || !resp.OK {
			msg := fmt.Sprintf("send_text failed for surface %s", surf.ID)
			if err != nil {
				msg = fmt.Sprintf("%s: %s", msg, err)
			} else {
				msg = fmt.Sprintf("%s: %s", msg, resp.Error)
			}
			return ToolResult{Content: msg, IsError: true}, nil
		}

		// Send Enter.
		if resp, err := t.client.Call("surface.send_key", map[string]string{
			"surface_id": surf.ID,
			"key":        "Enter",
		}); err != nil || !resp.OK {
			msg := fmt.Sprintf("send_key failed for surface %s", surf.ID)
			if err != nil {
				msg = fmt.Sprintf("%s: %s", msg, err)
			} else {
				msg = fmt.Sprintf("%s: %s", msg, resp.Error)
			}
			return ToolResult{Content: msg, IsError: true}, nil
		}

		states = append(states, &taskState{surfaceID: surf.ID, command: task.Command})
	}

	// Poll sidebar-state every 2 s until all done or timeout.
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		allDone := true
		for _, s := range states {
			if !s.done {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			for _, s := range states {
				if !s.done {
					s.status = "timeout"
					s.done = true
				}
			}
			break
		}

		select {
		case <-ctx.Done():
			return ToolResult{Content: "context cancelled", IsError: true}, nil
		case <-ticker.C:
			resp, err := t.client.Call("sidebar-state", map[string]any{})
			if err != nil {
				continue
			}
			if !resp.OK {
				continue
			}
			var state cmux.SidebarState
			if err := json.Unmarshal(resp.Result, &state); err != nil {
				continue
			}
			for _, entry := range state.Logs {
				for _, s := range states {
					if s.done {
						continue
					}
					if !strings.Contains(entry.Message, s.surfaceID) {
						continue
					}
					if strings.Contains(entry.Message, "[DONE]") {
						s.done = true
						s.status = "done"
					} else if strings.Contains(entry.Message, "[ERROR]") {
						s.done = true
						s.status = "error"
					}
				}
			}
		}
	}

	// Build summary.
	var sb strings.Builder
	for _, s := range states {
		sb.WriteString(fmt.Sprintf("surface %s (%s): %s\n", s.surfaceID, s.command, s.status))
	}
	return ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
}
