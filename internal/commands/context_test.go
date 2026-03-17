package commands_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCommand(t *testing.T) {
	budget := agentsdk.ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     2100,
		SkillPrompts:     3400,
		ToolDescriptions: 4200,
		Conversation:     32450,
	}

	cmd := commands.NewContextCommand(func() agentsdk.ContextBudget { return budget })
	assert.Equal(t, "context", cmd.Name())

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Context Usage:")
	assert.Contains(t, result.Output, "System prompt")
	assert.Contains(t, result.Output, "Skill prompts")
	assert.Contains(t, result.Output, "Tool definitions")
	assert.Contains(t, result.Output, "Conversation")
	assert.Contains(t, result.Output, "Remaining")
}

func TestContextCommandNilCallback(t *testing.T) {
	cmd := commands.NewContextCommand(nil)
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "not available")
}

func TestContextCommandZeroWindow(t *testing.T) {
	budget := agentsdk.ContextBudget{Total: 0, MaxOutputTokens: 0}
	cmd := commands.NewContextCommand(func() agentsdk.ContextBudget { return budget })
	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Context Usage:")
}

func TestCompactCommand(t *testing.T) {
	cr := agentsdk.CompactResult{
		BeforeTokens:   42150,
		AfterTokens:    28300,
		BeforeMsgCount: 45,
		AfterMsgCount:  22,
		StrategiesRun:  []string{"tool_result_clearing", "truncation"},
	}
	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return cr, nil
	})
	assert.Equal(t, "compact", cmd.Name())

	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "42,150")
	assert.Contains(t, res.Output, "28,300")
	assert.Contains(t, res.Output, "45")
	assert.Contains(t, res.Output, "22")
	assert.Contains(t, res.Output, "tool_result_clearing")
}

func TestCompactCommandNoReduction(t *testing.T) {
	cr := agentsdk.CompactResult{BeforeTokens: 1000, AfterTokens: 1000, BeforeMsgCount: 5, AfterMsgCount: 5}
	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return cr, nil
	})
	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "No compaction needed")
}

func TestCompactCommandNilCallback(t *testing.T) {
	cmd := commands.NewCompactCommand(nil)
	res, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "not available")
}

func TestCompactCommandError(t *testing.T) {
	cmd := commands.NewCompactCommand(func(ctx context.Context) (agentsdk.CompactResult, error) {
		return agentsdk.CompactResult{}, fmt.Errorf("compaction failed")
	})
	_, err := cmd.Execute(context.Background(), nil)
	assert.Error(t, err)
}
