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

// --- Middleware tests ---

func TestRuleEngineMiddlewareDenyShortCircuits(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryBash,
			Action:   toolexec.ActionDeny,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	mw := toolexec.RuleEngineMiddleware(engine)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "should not reach"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf /"}`),
	})

	assert.False(t, baseCalled, "base handler should not be called when deny short-circuits")
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, `tool "shell" blocked by deny rule`)
}

func TestRuleEngineMiddlewareAllowPassesThrough(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{
			Category: toolexec.CategoryFileRead,
			Action:   toolexec.ActionAllow,
		},
	}
	engine := toolexec.NewRuleEngine(rules)
	mw := toolexec.RuleEngineMiddleware(engine)

	var capturedAction toolexec.RuleAction
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		capturedAction = toolexec.RuleActionFromContext(ctx)
		return toolexec.Result{Content: "file contents"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryFileRead)
	result := handler(ctx, toolexec.ToolCall{
		ID:    "call-2",
		Name:  "file",
		Input: json.RawMessage(`{"path":"/tmp/test.go"}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "file contents", result.Content)
	assert.Equal(t, toolexec.ActionAllow, capturedAction)
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

// --- Glob pattern coverage tests ---

func TestRuleEngineGlobQuestionMark(t *testing.T) {
	// '?' matches a single character.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "ca?", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	// "cat" matches "ca?"
	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"cat"}`))
	assert.Equal(t, toolexec.ActionDeny, action, "'cat' should match 'ca?'")

	// "ca" does not match "ca?" (? requires exactly one char)
	action = engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"ca"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "'ca' should not match 'ca?'")
}

func TestRuleEngineGlobCharacterClass(t *testing.T) {
	// '[abc]' matches any single character in the set.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "[abc]at", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"bat"}`))
	assert.Equal(t, toolexec.ActionDeny, action, "'bat' should match '[abc]at'")

	action = engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"dat"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "'dat' should not match '[abc]at'")
}

func TestRuleEngineGlobCharacterClassNoMatch(t *testing.T) {
	// Character class with empty string input.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "[xy]", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":""}`))
	assert.Equal(t, toolexec.ActionAsk, action, "empty string should not match '[xy]'")
}

func TestRuleEngineGlobUnclosedBracket(t *testing.T) {
	// '[abc' with no closing bracket is treated as literal '['.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "[abc", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	// Should not match "a" because the bracket is escaped to literal.
	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"a"}`))
	assert.Equal(t, toolexec.ActionAsk, action)
}

func TestRuleEngineGlobEscapedSpecialChars(t *testing.T) {
	// Patterns containing regex special chars should be escaped.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "file.txt", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	// '.' in the pattern should be literal, not regex wildcard.
	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"file.txt"}`))
	assert.Equal(t, toolexec.ActionDeny, action, "'file.txt' should match literal dot pattern")

	action = engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"fileXtxt"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "'fileXtxt' should not match literal dot pattern")
}

func TestRuleEngineRegexSingleDotNoMatch(t *testing.T) {
	// Single '?' at end of pattern with empty remaining string.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "x?", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	// "x" should not match "x?" — '?' needs exactly one char.
	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"x"}`))
	assert.Equal(t, toolexec.ActionAsk, action)
}

func TestRuleEngineExtractStringValuesArray(t *testing.T) {
	// JSON with array values.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "secret*", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"args":["safe-cmd","secret-file"]}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action, "should match string inside JSON array")
}

func TestRuleEngineExtractStringValuesNestedObject(t *testing.T) {
	// JSON with nested objects.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "password*", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"config":{"key":"password123"}}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionDeny, action, "should match string inside nested JSON object")
}

func TestRuleEngineExtractStringValuesEmptyInput(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "*", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	// Empty JSON input has no string values, so the pattern with
	// empty patternMatchesInput returns true (empty pattern matches all).
	// But here the pattern is "*", not empty, so it checks extracted strings.
	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{}`))
	assert.Equal(t, toolexec.ActionAsk, action, "no strings in input means pattern doesn't match")
}

func TestRuleEngineExtractStringValuesNonStringJSON(t *testing.T) {
	// JSON with only non-string types (numbers and booleans).
	// Note: JSON null unmarshals to empty string in Go, so we only use
	// numbers and bools which produce Unmarshal errors.
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "hello*", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	input := json.RawMessage(`{"count":42,"active":true}`)
	action := engine.Evaluate(toolexec.CategoryBash, "shell", input)
	assert.Equal(t, toolexec.ActionAsk, action, "non-string JSON values should not produce matches")
}

func TestRuleEngineAskOverridesAllow(t *testing.T) {
	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Action: toolexec.ActionAllow},
		{Category: toolexec.CategoryBash, Action: toolexec.ActionAsk},
	}
	engine := toolexec.NewRuleEngine(rules)

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"ls"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "ask should take precedence over allow")
}

func TestRuleEngineUnknownCategoryDefaultsToAsk(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)

	// A category that does not exist in categoryDefaults.
	action := engine.Evaluate(toolexec.Category("unknown_category"), "tool", json.RawMessage(`{}`))
	assert.Equal(t, toolexec.ActionAsk, action, "unknown category should default to ask")
}

func TestRuleEngineEmptyCategoryAndToolSkipsRule(t *testing.T) {
	// Rule with both Category and Tool empty should not match anything.
	rules := []toolexec.PermissionRule{
		{Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "rule with empty category and tool should be skipped")
}
