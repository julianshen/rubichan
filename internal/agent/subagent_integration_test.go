package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
)

// echoProvider returns whatever text is in the last user message.
type echoProvider struct{}

func (e *echoProvider) Stream(_ context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 3)
	// Find the last user message text.
	var text string
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			for _, block := range msg.Content {
				if block.Type == "text" {
					text = block.Text
				}
			}
		}
	}
	ch <- provider.StreamEvent{Type: "text_delta", Text: "Echo: " + text}
	ch <- provider.StreamEvent{Type: "done", InputTokens: 10, OutputTokens: 5}
	close(ch)
	return ch, nil
}

// testSpawnerAdapter bridges agent.DefaultSubagentSpawner to tools.TaskSpawner,
// converting between the type-specific config/result structs.
// NOTE: Intentionally duplicates spawnerAdapter in cmd/rubichan/main.go —
// cannot import package main. Keep both in sync.
type testSpawnerAdapter struct {
	spawner *agent.DefaultSubagentSpawner
}

func (a *testSpawnerAdapter) Spawn(ctx context.Context, cfg tools.TaskSpawnConfig, prompt string) (*tools.TaskSpawnResult, error) {
	result, err := a.spawner.Spawn(ctx, agent.SubagentConfig{
		Name:          cfg.Name,
		SystemPrompt:  cfg.SystemPrompt,
		Tools:         cfg.Tools,
		MaxTurns:      cfg.MaxTurns,
		MaxTokens:     cfg.MaxTokens,
		Model:         cfg.Model,
		Depth:         cfg.Depth,
		MaxDepth:      cfg.MaxDepth,
		InheritSkills: cfg.InheritSkills,
		ExtraSkills:   cfg.ExtraSkills,
		DisableSkills: cfg.DisableSkills,
	}, prompt)
	if err != nil {
		return nil, err
	}
	return &tools.TaskSpawnResult{
		Name:         result.Name,
		Output:       result.Output,
		ToolsUsed:    result.ToolsUsed,
		TurnCount:    result.TurnCount,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		Error:        result.Error,
	}, nil
}

// testDefLookupAdapter bridges agent.AgentDefRegistry to tools.TaskAgentDefLookup.
// NOTE: Intentionally duplicates agentDefLookupAdapter in cmd/rubichan/main.go.
type testDefLookupAdapter struct {
	reg *agent.AgentDefRegistry
}

func (a *testDefLookupAdapter) GetAgentDef(name string) (*tools.TaskAgentDef, bool) {
	def, ok := a.reg.Get(name)
	if !ok {
		return nil, false
	}
	return &tools.TaskAgentDef{
		Name:          def.Name,
		SystemPrompt:  def.SystemPrompt,
		Tools:         def.Tools,
		MaxTurns:      def.MaxTurns,
		MaxDepth:      def.MaxDepth,
		Model:         def.Model,
		InheritSkills: def.InheritSkills,
		ExtraSkills:   def.ExtraSkills,
		DisableSkills: def.DisableSkills,
	}, true
}

func TestSubagentIntegration(t *testing.T) {
	cfg := &config.Config{
		Agent:    config.AgentConfig{MaxTurns: 3},
		Provider: config.ProviderConfig{Model: "echo"},
	}

	reg := tools.NewRegistry()
	ep := &echoProvider{}

	defReg := agent.NewAgentDefRegistry()
	err := defReg.Register(&agent.AgentDef{
		Name:        "general",
		Description: "General agent",
	})
	require.NoError(t, err)

	spawner := &agent.DefaultSubagentSpawner{
		Provider:    ep,
		ParentTools: reg,
		Config:      cfg,
		AgentDefs:   defReg,
	}

	adapter := &testSpawnerAdapter{spawner: spawner}
	defAdapter := &testDefLookupAdapter{reg: defReg}
	taskTool := tools.NewTaskTool(adapter, defAdapter, 0)

	// Execute the task tool directly.
	input := json.RawMessage(`{"prompt":"find all test files"}`)
	result, err := taskTool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Echo: find all test files")
}
