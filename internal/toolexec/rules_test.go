package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

// --- Rule Engine tests ---

func TestRuleEngineHardDenyBlocksExecution(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm -rf *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"command":"rm -rf *"}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action)
}

func TestRuleEngineDenyOverridesAllow(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Action:   toolexec.ActionAllow,
		},
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm -rf *",
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"command":"rm -rf *"}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action, "deny should win over allow regardless of rule order")
}

func TestRuleEngineAllowAutoApproves(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "ls *",
			Action:   toolexec.ActionAllow,
		},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"command":"ls -la"}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionAllow, action)
}

func TestRuleEngineAskWhenNoRuleMatches(t *testing.T) {
	// Empty rule set, bash category defaults to ask.
	engine := toolexec.NewRuleEngine(nil)

	input := json.RawMessage(`{"command":"echo hello"}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionAsk, action, "bash with no matching rules should fall back to ask")
}

func TestRuleEngineMatchesByToolName(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Tool:   "shell",
			Action: toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)

	// Tool name matches "shell", even though category is different.
	input := json.RawMessage(`{}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action)

	// A different tool name should not match.
	action = engine.Evaluate(toolexec.CategoryBash, "exec", input)
	assert.Equal(t, toolexec.ActionAsk, action, "non-matching tool name falls through to category default")
}

func TestRuleEngineCategoryDefaults(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	input := json.RawMessage(`{}`)

	// Categories that default to allow.
	allowCategories := []toolexec.Category{
		toolexec.CategoryFileRead,
		toolexec.CategorySearch,
		toolexec.CategoryAgent,
	}
	for _, cat := range allowCategories {
		t.Run(string(cat)+"_defaults_to_allow", func(t *testing.T) {
			action := engine.Evaluate(cat, "some-tool", input)
			assert.Equal(t, toolexec.ActionAllow, action)
		})
	}

	// Categories that default to ask.
	askCategories := []toolexec.Category{
		toolexec.CategoryBash,
		toolexec.CategoryGit,
		toolexec.CategoryNet,
		toolexec.CategoryMCP,
		toolexec.CategoryPlatform,
		toolexec.CategorySkill,
	}
	for _, cat := range askCategories {
		t.Run(string(cat)+"_defaults_to_ask", func(t *testing.T) {
			action := engine.Evaluate(cat, "some-tool", input)
			assert.Equal(t, toolexec.ActionAsk, action)
		})
	}
}

func TestRuleEngineSourceIsInformationalOnly(t *testing.T) {
	// Even when deny comes from SourceDefault and allow comes from SourceLocal,
	// deny still wins — Source does not affect precedence.
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionDeny,
			Source:   toolexec.SourceDefault,
		},
		{
			Category: toolexec.CategoryBash,
			Pattern:  "rm *",
			Action:   toolexec.ActionAllow,
			Source:   toolexec.SourceLocal,
		},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"command":"rm -rf /"}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action, "deny from any source wins over allow from local source")
}

// --- Context helpers tests ---

func TestRuleActionFromContext(t *testing.T) {
	// No action set returns zero value.
	action := toolexec.RuleActionFromContext(context.Background())
	assert.Equal(t, toolexec.RuleAction(""), action)

	// Set and retrieve.
	ctx := toolexec.WithRuleAction(context.Background(), toolexec.ActionAsk)
	action = toolexec.RuleActionFromContext(ctx)
	assert.Equal(t, toolexec.ActionAsk, action)

	// Overwrite with a new value.
	ctx = toolexec.WithRuleAction(ctx, toolexec.ActionAllow)
	action = toolexec.RuleActionFromContext(ctx)
	assert.Equal(t, toolexec.ActionAllow, action)
}
