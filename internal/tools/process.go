package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// processInput represents the input for the process tool.
type processInput struct {
	Operation string `json:"operation"`
	Command   string `json:"command,omitempty"`
	ProcessID string `json:"process_id,omitempty"`
	Input     string `json:"input,omitempty"`
}

// ProcessTool wraps a ProcessManager as a Tool, dispatching operations
// (exec, read_output, write_stdin, kill, list) to the underlying manager.
type ProcessTool struct {
	manager *ProcessManager
}

// NewProcessTool creates a ProcessTool backed by the given ProcessManager.
func NewProcessTool(manager *ProcessManager) *ProcessTool {
	return &ProcessTool{manager: manager}
}

func (p *ProcessTool) Name() string {
	return "process"
}

func (p *ProcessTool) Description() string {
	return "Manage long-running processes. Supports operations: exec (start a process), " +
		"read_output (get recent output), write_stdin (send input), kill (terminate), " +
		"and list (show all processes)."
}

func (p *ProcessTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["exec", "read_output", "write_stdin", "kill", "list"],
				"description": "The operation to perform"
			},
			"command": {
				"type": "string",
				"description": "The shell command to execute (required for exec)"
			},
			"process_id": {
				"type": "string",
				"description": "The ID of the target process (required for read_output, write_stdin, kill)"
			},
			"input": {
				"type": "string",
				"description": "Data to send to process stdin (required for write_stdin)"
			}
		},
		"required": ["operation"]
	}`)
}

func (p *ProcessTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var in processInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	switch in.Operation {
	case "exec":
		return p.execOp(ctx, in)
	case "read_output":
		return p.readOutputOp(in)
	case "write_stdin":
		return p.writeStdinOp(in)
	case "kill":
		return p.killOp(in)
	case "list":
		return p.listOp()
	default:
		return ToolResult{
			Content: fmt.Sprintf("unknown operation: %s", in.Operation),
			IsError: true,
		}, nil
	}
}

func (p *ProcessTool) execOp(ctx context.Context, in processInput) (ToolResult, error) {
	if in.Command == "" {
		return ToolResult{Content: "command is required for exec", IsError: true}, nil
	}

	id, output, err := p.manager.Exec(ctx, in.Command)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("exec failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("process_id: %s\n%s", id, output)}, nil
}

func (p *ProcessTool) readOutputOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for read_output", IsError: true}, nil
	}

	output, status, err := p.manager.ReadOutput(in.ProcessID)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("read_output failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("status: %s\n%s", status, output)}, nil
}

func (p *ProcessTool) writeStdinOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for write_stdin", IsError: true}, nil
	}
	if in.Input == "" {
		return ToolResult{Content: "input is required for write_stdin", IsError: true}, nil
	}

	err := p.manager.WriteStdin(in.ProcessID, in.Input)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("write_stdin failed: %s", err), IsError: true}, nil
	}

	// Brief pause to let the process respond, then include a snippet.
	time.Sleep(50 * time.Millisecond)
	output, status, readErr := p.manager.ReadOutput(in.ProcessID)
	if readErr != nil || output == "" {
		return ToolResult{Content: fmt.Sprintf("sent %d bytes to process %s", len(in.Input), in.ProcessID)}, nil
	}

	return ToolResult{Content: fmt.Sprintf("sent %d bytes to process %s\nstatus: %s\n%s", len(in.Input), in.ProcessID, status, output)}, nil
}

func (p *ProcessTool) killOp(in processInput) (ToolResult, error) {
	if in.ProcessID == "" {
		return ToolResult{Content: "process_id is required for kill", IsError: true}, nil
	}

	err := p.manager.Kill(in.ProcessID)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("kill failed: %s", err), IsError: true}, nil
	}

	return ToolResult{Content: fmt.Sprintf("process %s terminated", in.ProcessID)}, nil
}

func (p *ProcessTool) listOp() (ToolResult, error) {
	procs := p.manager.List()
	if len(procs) == 0 {
		return ToolResult{Content: "no managed processes"}, nil
	}

	var b strings.Builder
	for i, proc := range procs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s  %s  %s", proc.ID, proc.Status, proc.Command)
	}
	return ToolResult{Content: b.String()}, nil
}
