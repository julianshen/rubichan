package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSpawner struct {
	lastCfg    TaskSpawnConfig
	lastPrompt string
	result     *TaskSpawnResult
	err        error
}

func (f *fakeSpawner) Spawn(_ context.Context, cfg TaskSpawnConfig, prompt string) (*TaskSpawnResult, error) {
	f.lastCfg = cfg
	f.lastPrompt = prompt
	return f.result, f.err
}

type fakeAgentDefLookup struct {
	defs map[string]*TaskAgentDef
}

func (f *fakeAgentDefLookup) GetAgentDef(name string) (*TaskAgentDef, bool) {
	def, ok := f.defs[name]
	return def, ok
}

func TestTaskToolName(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	assert.Equal(t, "task", tool.Name())
}

func TestTaskToolDescription(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	assert.NotEmpty(t, tool.Description())
}

func TestTaskToolInputSchema(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	schema := tool.InputSchema()
	var parsed map[string]interface{}
	err := json.Unmarshal(schema, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "object", parsed["type"])
}

func TestTaskToolExecute(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{Name: "general", Output: "Found 3 matching files.", TurnCount: 2},
	}
	tool := NewTaskTool(spawner, &fakeAgentDefLookup{}, 0)
	input := json.RawMessage(`{"prompt":"Find all Go test files"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Found 3 matching files.")
	assert.Equal(t, "Find all Go test files", spawner.lastPrompt)
}

func TestTaskToolWithAgentType(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{Name: "explorer", Output: "done"},
	}
	defs := &fakeAgentDefLookup{
		defs: map[string]*TaskAgentDef{
			"explorer": {
				Name: "explorer", SystemPrompt: "You explore code.",
				Tools: []string{"file", "search"}, MaxTurns: 5,
			},
		},
	}
	tool := NewTaskTool(spawner, defs, 0)
	input := json.RawMessage(`{"prompt":"explore","agent_type":"explorer"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "explorer", spawner.lastCfg.Name)
	assert.Equal(t, []string{"file", "search"}, spawner.lastCfg.Tools)
	assert.Equal(t, 5, spawner.lastCfg.MaxTurns)
}

func TestTaskToolMaxTurnsOverride(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{Name: "general", Output: "ok"},
	}
	tool := NewTaskTool(spawner, nil, 0)
	input := json.RawMessage(`{"prompt":"test","max_turns":20}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, 20, spawner.lastCfg.MaxTurns)
}

func TestTaskToolMissingPrompt(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	input := json.RawMessage(`{}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "prompt")
}

func TestTaskToolInvalidJSON(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	input := json.RawMessage(`{invalid}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "invalid input")
}

func TestTaskToolSpawnError(t *testing.T) {
	spawner := &fakeSpawner{err: fmt.Errorf("depth exceeded")}
	tool := NewTaskTool(spawner, nil, 0)
	input := json.RawMessage(`{"prompt":"test"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "depth exceeded")
}

func TestTaskToolSubagentError(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{
			Name:   "general",
			Output: "partial output",
			Error:  fmt.Errorf("turn limit reached"),
		},
	}
	tool := NewTaskTool(spawner, nil, 0)
	input := json.RawMessage(`{"prompt":"test"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "subagent error")
	assert.Contains(t, result.Content, "turn limit reached")
	assert.Contains(t, result.Content, "partial output")
}

func TestTaskToolDisplayContent(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{
			Name:         "general",
			Output:       "result text",
			TurnCount:    3,
			InputTokens:  500,
			OutputTokens: 200,
		},
	}
	tool := NewTaskTool(spawner, nil, 0)
	input := json.RawMessage(`{"prompt":"test"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.DisplayContent, "[subagent:general]")
	assert.Contains(t, result.DisplayContent, "3 turns")
	assert.Contains(t, result.DisplayContent, "500 input")
	assert.Contains(t, result.DisplayContent, "200 output")
}

func TestTaskToolDepthPassthrough(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{Name: "general", Output: "ok"},
	}
	tool := NewTaskTool(spawner, nil, 2)
	input := json.RawMessage(`{"prompt":"test"}`)
	_, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, 2, spawner.lastCfg.Depth)
}

func TestTaskToolUnknownAgentType(t *testing.T) {
	spawner := &fakeSpawner{
		result: &TaskSpawnResult{Name: "general", Output: "ok"},
	}
	defs := &fakeAgentDefLookup{defs: map[string]*TaskAgentDef{}}
	tool := NewTaskTool(spawner, defs, 0)
	input := json.RawMessage(`{"prompt":"test","agent_type":"nonexistent"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	// Should fall back to "general" when agent type is not found.
	assert.Equal(t, "general", spawner.lastCfg.Name)
}
