package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// TaskSpawner creates and runs child agents. Defined here to break the
// import cycle between tools/ and agent/.
type TaskSpawner interface {
	Spawn(ctx context.Context, cfg TaskSpawnConfig, prompt string) (*TaskSpawnResult, error)
}

// TaskSpawnConfig captures the configuration for spawning a subagent.
// Mirrors agent.SubagentConfig but lives in tools/ to avoid the cycle.
type TaskSpawnConfig struct {
	Name         string
	SystemPrompt string
	Tools        []string
	MaxTurns     int
	MaxTokens    int
	Model        string
	Depth        int
	MaxDepth     int
}

// TaskSpawnResult is the output of a subagent execution.
// Mirrors agent.SubagentResult but lives in tools/ to avoid the cycle.
type TaskSpawnResult struct {
	Name         string
	Output       string
	ToolsUsed    []string
	TurnCount    int
	InputTokens  int
	OutputTokens int
	Error        error
}

// TaskAgentDef describes a pre-configured subagent template for use by
// the TaskTool. Mirrors the fields needed from agent.AgentDef.
type TaskAgentDef struct {
	Name         string
	SystemPrompt string
	Tools        []string
	MaxTurns     int
	MaxDepth     int
	Model        string
}

// TaskAgentDefLookup retrieves named agent definitions for the TaskTool.
// Defined here to break the import cycle between tools/ and agent/.
type TaskAgentDefLookup interface {
	GetAgentDef(name string) (*TaskAgentDef, bool)
}

// BackgroundTaskManager handles background task lifecycle.
// Defined here so that the agent package can provide an adapter without
// creating an import cycle (tools/ does not import agent/).
type BackgroundTaskManager interface {
	SubmitBackground(name string, cancel context.CancelFunc) string
	CompleteBackground(taskID string, output string, err error)
}

// TaskTool delegates tasks to subagents via the TaskSpawner interface.
type TaskTool struct {
	spawner   TaskSpawner
	agentDefs TaskAgentDefLookup
	depth     int
	bgManager BackgroundTaskManager
}

// NewTaskTool creates a TaskTool that delegates to the given spawner.
func NewTaskTool(spawner TaskSpawner, defs TaskAgentDefLookup, depth int) *TaskTool {
	return &TaskTool{spawner: spawner, agentDefs: defs, depth: depth}
}

// SetBackgroundManager attaches a BackgroundTaskManager for background mode.
func (t *TaskTool) SetBackgroundManager(mgr BackgroundTaskManager) {
	t.bgManager = mgr
}

func (t *TaskTool) Name() string { return "task" }
func (t *TaskTool) Description() string {
	return "Delegate a task to a subagent for autonomous execution"
}

func (t *TaskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string", "description": "The task description for the subagent"},
			"agent_type": {"type": "string", "description": "Named agent definition to use (default: general)"},
			"max_turns": {"type": "integer", "description": "Override maximum turns for the subagent"},
			"background": {"type": "boolean", "description": "Run the subagent asynchronously in the background"}
		},
		"required": ["prompt"]
	}`)
}

type taskInput struct {
	Prompt     string `json:"prompt"`
	AgentType  string `json:"agent_type"`
	MaxTurns   int    `json:"max_turns"`
	Background bool   `json:"background"`
}

func (t *TaskTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var ti taskInput
	if err := json.Unmarshal(input, &ti); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if ti.Prompt == "" {
		return ToolResult{Content: "prompt is required", IsError: true}, nil
	}

	cfg := TaskSpawnConfig{
		Name:  "general",
		Depth: t.depth,
	}

	if ti.AgentType != "" && t.agentDefs != nil {
		def, ok := t.agentDefs.GetAgentDef(ti.AgentType)
		if ok {
			cfg.Name = def.Name
			cfg.SystemPrompt = def.SystemPrompt
			cfg.Tools = def.Tools
			cfg.MaxTurns = def.MaxTurns
			cfg.MaxDepth = def.MaxDepth
			cfg.Model = def.Model
		}
	}

	if ti.MaxTurns > 0 {
		cfg.MaxTurns = ti.MaxTurns
	}

	// Background mode: submit to BackgroundTaskManager and return immediately.
	if ti.Background && t.bgManager != nil {
		bgCtx, cancel := context.WithCancel(context.Background())
		taskID := t.bgManager.SubmitBackground(cfg.Name, cancel)
		go func() {
			result, err := t.spawner.Spawn(bgCtx, cfg, ti.Prompt)
			var output string
			var spawnErr error
			if err != nil {
				spawnErr = err
			} else if result != nil {
				output = result.Output
				spawnErr = result.Error
			}
			t.bgManager.CompleteBackground(taskID, output, spawnErr)
		}()
		return ToolResult{
			Content: fmt.Sprintf("Background task %s started (agent: %s)", taskID, cfg.Name),
		}, nil
	}

	result, err := t.spawner.Spawn(ctx, cfg, ti.Prompt)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("subagent failed: %v", err), IsError: true}, nil
	}

	content := result.Output
	if result.Error != nil {
		content = fmt.Sprintf("subagent error: %v\n%s", result.Error, result.Output)
	}

	return ToolResult{
		Content: content,
		DisplayContent: fmt.Sprintf("[subagent:%s] %d turns, %d input / %d output tokens\n%s",
			result.Name, result.TurnCount, result.InputTokens, result.OutputTokens, content),
	}, nil
}
