package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationTool is a minimal tools.Tool implementation for integration tests.
type integrationTool struct {
	name    string
	result  tools.ToolResult
	execErr error
}

func (t *integrationTool) Name() string                 { return t.name }
func (t *integrationTool) Description() string          { return "integration test tool" }
func (t *integrationTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *integrationTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return t.result, t.execErr
}

// buildFullPipeline creates a pipeline with classifier, rule engine, and
// shell safety middlewares — the same stack used in production.
func buildFullPipeline(registry *tools.Registry, rules []toolexec.PermissionRule) *toolexec.Pipeline {
	classifier := toolexec.NewClassifier(nil)
	engine := toolexec.NewRuleEngine(rules)
	validator := toolexec.NewShellValidator(engine, "")

	return toolexec.NewPipeline(
		toolexec.RegistryExecutor(registry),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(engine),
		toolexec.ShellSafetyMiddleware(validator),
	)
}

func TestFullPipelineDenyBlocksBeforeExecution(t *testing.T) {
	registry := tools.NewRegistry()
	shellTool := &integrationTool{
		name:   "shell",
		result: tools.ToolResult{Content: "should not execute"},
	}
	require.NoError(t, registry.Register(shellTool))

	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "*rm -rf*",
			Action:   toolexec.ActionDeny,
		},
	}

	pipeline := buildFullPipeline(registry, rules)

	result := pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID:    "call-deny-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf /"}`),
	})

	assert.True(t, result.IsError, "deny rule should block execution")
	assert.Contains(t, result.Content, "blocked")
}

func TestFullPipelineAllowExecutesNormally(t *testing.T) {
	registry := tools.NewRegistry()
	shellTool := &integrationTool{
		name:   "shell",
		result: tools.ToolResult{Content: "ok\nPASS"},
	}
	require.NoError(t, registry.Register(shellTool))

	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "go test*",
			Action:   toolexec.ActionAllow,
		},
	}

	pipeline := buildFullPipeline(registry, rules)

	result := pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID:    "call-allow-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./..."}`),
	})

	assert.False(t, result.IsError, "allow rule should let execution proceed")
	assert.Equal(t, "ok\nPASS", result.Content)
}

func TestFullPipelineCompoundCommandDeny(t *testing.T) {
	registry := tools.NewRegistry()
	shellTool := &integrationTool{
		name:   "shell",
		result: tools.ToolResult{Content: "should not execute"},
	}
	require.NoError(t, registry.Register(shellTool))

	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "go test*",
			Action:   toolexec.ActionAllow,
		},
		{
			Category: toolexec.CategoryBash,
			Pattern:  "*rm -rf*",
			Action:   toolexec.ActionDeny,
		},
	}

	pipeline := buildFullPipeline(registry, rules)

	// The compound command includes "go test" (allowed) AND "rm -rf /" (denied).
	// ShellSafetyMiddleware decomposes the command and checks each sub-command
	// independently, so the denied sub-command should block execution.
	result := pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID:    "call-compound-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./... && rm -rf /"}`),
	})

	assert.True(t, result.IsError, "compound command with denied sub-command should be blocked")
	assert.Contains(t, result.Content, "blocked")
}

func TestFullPipelineNonBashSkipsSafety(t *testing.T) {
	registry := tools.NewRegistry()
	fileTool := &integrationTool{
		name:   "file",
		result: tools.ToolResult{Content: "file contents here"},
	}
	require.NoError(t, registry.Register(fileTool))

	// Deny rule targeting bash — should not affect file tools.
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "*",
			Action:   toolexec.ActionDeny,
		},
	}

	pipeline := buildFullPipeline(registry, rules)

	// File tool is classified as file_read, not bash. The bash deny rule
	// does not match, and shell safety skips non-bash categories.
	result := pipeline.Execute(context.Background(), toolexec.ToolCall{
		ID:    "call-file-1",
		Name:  "file",
		Input: json.RawMessage(`{"path":"/tmp/test.go"}`),
	})

	assert.False(t, result.IsError, "file tool should not be blocked by bash deny rule")
	assert.Equal(t, "file contents here", result.Content)
}
