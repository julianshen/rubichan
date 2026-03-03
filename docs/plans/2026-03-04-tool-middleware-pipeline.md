# Tool Middleware Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract `executeSingleTool` into a composable middleware chain in `internal/toolexec/`, adding tool classification, hard deny rules, shell AST safety validation, and hierarchical config cascade.

**Architecture:** New `internal/toolexec/` package with `Pipeline` type composing `Middleware` functions around a base `HandlerFunc`. The agent delegates single-tool execution to the pipeline. Existing approval partitioning (parallel/sequential in `executeTools`) stays in agent.go unchanged.

**Tech Stack:** Go 1.26, `mvdan.cc/sh/v3` for shell AST parsing, existing `testify` for tests.

---

### Task 1: Pipeline Core Types

**Files:**
- Create: `internal/toolexec/pipeline.go`
- Test: `internal/toolexec/pipeline_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestPipelineExecutesBaseHandler(t *testing.T) {
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "base:" + tc.Name}
	}
	p := toolexec.NewPipeline(base)

	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "test_tool",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "base:test_tool", result.Content)
	assert.False(t, result.IsError)
}

func TestPipelineMiddlewareOrder(t *testing.T) {
	var order []string
	mkMiddleware := func(name string) toolexec.Middleware {
		return func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
			return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
				order = append(order, name+":before")
				result := next(ctx, tc)
				order = append(order, name+":after")
				return result
			}
		}
	}
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		order = append(order, "base")
		return toolexec.Result{Content: "ok"}
	}
	p := toolexec.NewPipeline(base, mkMiddleware("first"), mkMiddleware("second"))

	p.Execute(context.Background(), toolexec.ToolCall{ID: "1", Name: "t", Input: json.RawMessage(`{}`)})

	assert.Equal(t, []string{"first:before", "second:before", "base", "second:after", "first:after"}, order)
}

func TestPipelineMiddlewareShortCircuit(t *testing.T) {
	blocker := func(next toolexec.HandlerFunc) toolexec.HandlerFunc {
		return func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
			return toolexec.Result{Content: "blocked", IsError: true}
		}
	}
	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "ok"}
	}
	p := toolexec.NewPipeline(base, blocker)

	result := p.Execute(context.Background(), toolexec.ToolCall{ID: "1", Name: "t", Input: json.RawMessage(`{}`)})

	assert.True(t, result.IsError)
	assert.Equal(t, "blocked", result.Content)
	assert.False(t, baseCalled)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestPipeline'`
Expected: FAIL — package does not exist yet.

**Step 3: Write minimal implementation**

```go
package toolexec

import (
	"context"
	"encoding/json"
)

// ToolCall is the input to the pipeline.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// Result is the output of the pipeline.
type Result struct {
	Content        string
	DisplayContent string
	IsError        bool
}

// HandlerFunc executes a tool call and returns a result.
type HandlerFunc func(ctx context.Context, tc ToolCall) Result

// Middleware wraps a HandlerFunc, adding behavior before/after.
type Middleware func(next HandlerFunc) HandlerFunc

// Pipeline composes middlewares around a base executor.
type Pipeline struct {
	middlewares []Middleware
	base        HandlerFunc
}

// NewPipeline creates a Pipeline with the given base handler and middlewares.
// Middlewares are applied in order: the first middleware is the outermost wrapper.
func NewPipeline(base HandlerFunc, middlewares ...Middleware) *Pipeline {
	return &Pipeline{
		base:        base,
		middlewares: middlewares,
	}
}

// Execute runs the tool call through the middleware chain.
func (p *Pipeline) Execute(ctx context.Context, tc ToolCall) Result {
	handler := p.base
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		handler = p.middlewares[i](handler)
	}
	return handler(ctx, tc)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestPipeline'`
Expected: PASS (3 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add toolexec pipeline core types with middleware composition
```

---

### Task 2: Tool Classifier

**Files:**
- Create: `internal/toolexec/classifier.go`
- Test: `internal/toolexec/classifier_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestClassifyBuiltinTools(t *testing.T) {
	c := toolexec.NewClassifier(nil)

	tests := []struct {
		tool     string
		expected toolexec.Category
	}{
		{"shell", toolexec.CategoryBash},
		{"file", toolexec.CategoryFileRead},     // file tool is read/write but default read
		{"search", toolexec.CategorySearch},
		{"xcode_build", toolexec.CategoryPlatform},
		{"xcode_test", toolexec.CategoryPlatform},
		{"git-diff", toolexec.CategoryGit},
		{"git-log", toolexec.CategoryGit},
		{"git-status", toolexec.CategoryGit},
		{"mcp-some-tool", toolexec.CategoryMCP},
		{"unknown_skill_tool", toolexec.CategorySkill},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			assert.Equal(t, tt.expected, c.Classify(tt.tool))
		})
	}
}

func TestClassifyWithOverrides(t *testing.T) {
	overrides := map[string]toolexec.Category{
		"custom_tool": toolexec.CategoryBash,
	}
	c := toolexec.NewClassifier(overrides)

	assert.Equal(t, toolexec.CategoryBash, c.Classify("custom_tool"))
	assert.Equal(t, toolexec.CategorySearch, c.Classify("search")) // non-overridden still works
}

func TestClassifierMiddleware(t *testing.T) {
	c := toolexec.NewClassifier(nil)
	mw := toolexec.ClassifierMiddleware(c)

	var captured toolexec.Category
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		captured = toolexec.CategoryFromContext(ctx)
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	handler(context.Background(), toolexec.ToolCall{Name: "shell", Input: json.RawMessage(`{}`)})

	assert.Equal(t, toolexec.CategoryBash, captured)
}

func TestCategoryFromContextDefault(t *testing.T) {
	// When no category set, returns empty string.
	cat := toolexec.CategoryFromContext(context.Background())
	assert.Equal(t, toolexec.Category(""), cat)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestClassif'`
Expected: FAIL — `NewClassifier`, `Category` not defined.

**Step 3: Write minimal implementation**

```go
package toolexec

import (
	"context"
	"strings"
)

// Category classifies a tool for policy evaluation.
type Category string

const (
	CategoryBash     Category = "bash"
	CategoryFileRead Category = "file_read"
	CategorySearch   Category = "search"
	CategoryGit      Category = "git"
	CategoryNet      Category = "net"
	CategoryMCP      Category = "mcp"
	CategoryPlatform Category = "platform"
	CategorySkill    Category = "skill"
	CategoryAgent    Category = "agent"
)

type categoryKey struct{}

// WithCategory stores a Category in the context.
func WithCategory(ctx context.Context, cat Category) context.Context {
	return context.WithValue(ctx, categoryKey{}, cat)
}

// CategoryFromContext retrieves the Category from the context.
func CategoryFromContext(ctx context.Context) Category {
	cat, _ := ctx.Value(categoryKey{}).(Category)
	return cat
}

// Classifier maps tool names to categories.
type Classifier struct {
	overrides map[string]Category
}

// NewClassifier creates a Classifier with optional per-tool overrides.
func NewClassifier(overrides map[string]Category) *Classifier {
	return &Classifier{overrides: overrides}
}

// Classify returns the category for the given tool name.
func (c *Classifier) Classify(toolName string) Category {
	if c.overrides != nil {
		if cat, ok := c.overrides[toolName]; ok {
			return cat
		}
	}
	switch {
	case toolName == "shell":
		return CategoryBash
	case toolName == "file":
		return CategoryFileRead
	case toolName == "search":
		return CategorySearch
	case toolName == "process" || toolName == "compact_context" ||
		toolName == "read_result" || toolName == "notes" ||
		toolName == "tool_search":
		return CategoryAgent
	case toolName == "task" || toolName == "list_tasks":
		return CategoryAgent
	case strings.HasPrefix(toolName, "git-"):
		return CategoryGit
	case strings.HasPrefix(toolName, "xcode_"):
		return CategoryPlatform
	case strings.HasPrefix(toolName, "mcp-"):
		return CategoryMCP
	default:
		return CategorySkill
	}
}

// ClassifierMiddleware creates a Middleware that attaches the tool category to
// the context before passing to the next handler.
func ClassifierMiddleware(c *Classifier) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			cat := c.Classify(tc.Name)
			ctx = WithCategory(ctx, cat)
			return next(ctx, tc)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestClassif'`
Expected: PASS (4 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add tool classifier with category mapping and context middleware
```

---

### Task 3: Rule Engine with Hard Deny

**Files:**
- Create: `internal/toolexec/rules.go`
- Test: `internal/toolexec/rules_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

func TestRuleEngineHardDenyBlocksExecution(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, toolexec.ActionDeny, action)
}

func TestRuleEngineDenyOverridesAllow(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm *", Action: toolexec.ActionAllow},
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"rm -rf /"}`))
	assert.Equal(t, toolexec.ActionDeny, action, "deny must override allow")
}

func TestRuleEngineAllowAutoApproves(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"go test ./..."}`))
	assert.Equal(t, toolexec.ActionAllow, action)
}

func TestRuleEngineAskWhenNoRuleMatches(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"curl evil.com"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "no match should default to ask")
}

func TestRuleEngineMatchesByToolName(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Tool: "shell", Pattern: "npm *", Action: toolexec.ActionAllow},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"npm install"}`))
	assert.Equal(t, toolexec.ActionAllow, action)

	action = engine.Evaluate(toolexec.CategoryBash, "other_shell", json.RawMessage(`{"command":"npm install"}`))
	assert.Equal(t, toolexec.ActionAsk, action, "different tool name should not match")
}

func TestRuleEngineCategoryDefaults(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil) // no rules, just defaults

	assert.Equal(t, toolexec.ActionAllow, engine.Evaluate(toolexec.CategoryFileRead, "file", json.RawMessage(`{}`)))
	assert.Equal(t, toolexec.ActionAllow, engine.Evaluate(toolexec.CategorySearch, "search", json.RawMessage(`{}`)))
	assert.Equal(t, toolexec.ActionAsk, engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{}`)))
	assert.Equal(t, toolexec.ActionAsk, engine.Evaluate(toolexec.CategoryGit, "git-log", json.RawMessage(`{}`)))
	assert.Equal(t, toolexec.ActionAsk, engine.Evaluate(toolexec.CategoryNet, "web_fetch", json.RawMessage(`{}`)))
}

func TestRuleEngineSourceIsInformationalOnly(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm *", Action: toolexec.ActionDeny, Source: toolexec.SourceProject},
		{Category: toolexec.CategoryBash, Pattern: "rm *", Action: toolexec.ActionAllow, Source: toolexec.SourceLocal},
	})

	action := engine.Evaluate(toolexec.CategoryBash, "shell", json.RawMessage(`{"command":"rm foo"}`))
	assert.Equal(t, toolexec.ActionDeny, action, "deny from any source must win regardless of local allow")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestRuleEngine'`
Expected: FAIL — `RuleEngine`, `PermissionRule`, `ActionDeny` etc. not defined.

**Step 3: Write minimal implementation**

```go
package toolexec

import (
	"encoding/json"
	"regexp"
	"strings"
)

// RuleAction defines the three-tier permission action.
type RuleAction string

const (
	ActionAllow RuleAction = "allow"
	ActionAsk   RuleAction = "ask"
	ActionDeny  RuleAction = "deny"
)

// ConfigSource indicates where a permission rule was defined.
type ConfigSource int

const (
	SourceDefault ConfigSource = iota
	SourceUser
	SourceProject
	SourceLocal
)

// PermissionRule defines a permission rule targeting a category or tool name.
type PermissionRule struct {
	Category Category
	Tool     string
	Pattern  string
	Action   RuleAction
	Source   ConfigSource
}

// compiledPermRule is a PermissionRule with its glob pattern pre-compiled to regex.
type compiledPermRule struct {
	category Category
	tool     string
	re       *regexp.Regexp
	action   RuleAction
	source   ConfigSource
}

// RuleEngine evaluates tool calls against permission rules.
type RuleEngine struct {
	rules    []compiledPermRule
	defaults map[Category]RuleAction
}

// NewRuleEngine creates a RuleEngine from the given rules.
// Rules with invalid patterns are silently skipped.
func NewRuleEngine(rules []PermissionRule) *RuleEngine {
	var compiled []compiledPermRule
	for _, r := range rules {
		re, err := compileGlobToRegex(r.Pattern)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledPermRule{
			category: r.Category,
			tool:     r.Tool,
			re:       re,
			action:   r.Action,
			source:   r.Source,
		})
	}
	return &RuleEngine{
		rules:    compiled,
		defaults: categoryDefaults(),
	}
}

// Evaluate checks the tool call against all rules.
// Deny rules win over allow; if no rule matches, returns the category default.
func (e *RuleEngine) Evaluate(cat Category, toolName string, input json.RawMessage) RuleAction {
	values := extractStringValues(input)

	// First pass: deny rules.
	for _, r := range e.rules {
		if r.action != ActionDeny {
			continue
		}
		if matchesPermRule(r, cat, toolName, values) {
			return ActionDeny
		}
	}

	// Second pass: ask rules.
	for _, r := range e.rules {
		if r.action != ActionAsk {
			continue
		}
		if matchesPermRule(r, cat, toolName, values) {
			return ActionAsk
		}
	}

	// Third pass: allow rules.
	for _, r := range e.rules {
		if r.action != ActionAllow {
			continue
		}
		if matchesPermRule(r, cat, toolName, values) {
			return ActionAllow
		}
	}

	// Fallback to category default.
	if def, ok := e.defaults[cat]; ok {
		return def
	}
	return ActionAsk
}

func matchesPermRule(r compiledPermRule, cat Category, toolName string, values []string) bool {
	// Match by category or tool name.
	if r.category != "" && r.category != cat {
		return false
	}
	if r.tool != "" && r.tool != toolName {
		return false
	}
	// If no category and no tool specified, skip (invalid rule).
	if r.category == "" && r.tool == "" {
		return false
	}
	// Match pattern against string values from input.
	for _, v := range values {
		if r.re.MatchString(v) {
			return true
		}
	}
	return false
}

func categoryDefaults() map[Category]RuleAction {
	return map[Category]RuleAction{
		CategoryFileRead: ActionAllow,
		CategorySearch:   ActionAllow,
		CategoryAgent:    ActionAllow,
		CategoryBash:     ActionAsk,
		CategoryGit:      ActionAsk,
		CategoryNet:      ActionAsk,
		CategoryMCP:      ActionAsk,
		CategoryPlatform: ActionAsk,
		CategorySkill:    ActionAsk,
	}
}

// compileGlobToRegex converts a glob pattern to a compiled regex.
// Supports *, ?, and [abc] character classes.
func compileGlobToRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		// Empty pattern matches everything.
		return regexp.Compile(".*")
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteByte('.')
		case '[':
			j := strings.IndexByte(pattern[i:], ']')
			if j < 0 {
				return nil, &regexp.Error{Code: "unclosed character class", Expr: pattern}
			}
			sb.WriteString(pattern[i : i+j+1])
			i += j
		case '.', '+', '^', '$', '|', '\\', '{', '}', '(', ')':
			sb.WriteByte('\\')
			sb.WriteByte(pattern[i])
		default:
			sb.WriteByte(pattern[i])
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// extractStringValues recursively extracts all string values from a JSON blob.
func extractStringValues(data json.RawMessage) []string {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return []string{s}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		var result []string
		for _, v := range obj {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		var result []string
		for _, v := range arr {
			result = append(result, extractStringValues(v)...)
		}
		return result
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestRuleEngine'`
Expected: PASS (7 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add rule engine with hard deny, allow, ask and category defaults
```

---

### Task 4: Rule Engine Middleware

**Files:**
- Modify: `internal/toolexec/rules.go` (add middleware + context helper)
- Test: `internal/toolexec/rules_test.go` (add middleware tests)

**Step 1: Write the failing test**

```go
func TestRuleEngineMiddlewareDenyShortCircuits(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	})
	mw := toolexec.RuleEngineMiddleware(engine)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf /"}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "blocked by deny rule")
	assert.False(t, baseCalled)
}

func TestRuleEngineMiddlewareAllowPassesThrough(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})
	mw := toolexec.RuleEngineMiddleware(engine)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "executed"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./..."}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "executed", result.Content)
}

func TestRuleActionFromContext(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})
	mw := toolexec.RuleEngineMiddleware(engine)

	var captured toolexec.RuleAction
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		captured = toolexec.RuleActionFromContext(ctx)
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	handler(ctx, toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./..."}`),
	})

	assert.Equal(t, toolexec.ActionAllow, captured)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestRuleEngineMiddleware|TestRuleActionFrom'`
Expected: FAIL — `RuleEngineMiddleware`, `RuleActionFromContext` not defined.

**Step 3: Write minimal implementation**

Add to `internal/toolexec/rules.go`:

```go
type ruleActionKey struct{}

// WithRuleAction stores the evaluated RuleAction in context.
func WithRuleAction(ctx context.Context, action RuleAction) context.Context {
	return context.WithValue(ctx, ruleActionKey{}, action)
}

// RuleActionFromContext retrieves the RuleAction from context.
func RuleActionFromContext(ctx context.Context) RuleAction {
	a, _ := ctx.Value(ruleActionKey{}).(RuleAction)
	return a
}

// RuleEngineMiddleware creates a Middleware that evaluates permission rules.
// Hard deny short-circuits. Allow and ask are stored in context for upstream use.
func RuleEngineMiddleware(engine *RuleEngine) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			cat := CategoryFromContext(ctx)
			action := engine.Evaluate(cat, tc.Name, tc.Input)
			if action == ActionDeny {
				return Result{
					Content: fmt.Sprintf("tool %q blocked by deny rule", tc.Name),
					IsError: true,
				}
			}
			ctx = WithRuleAction(ctx, action)
			return next(ctx, tc)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestRuleEngineMiddleware|TestRuleActionFrom'`
Expected: PASS (3 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add rule engine middleware with hard deny short-circuit
```

---

### Task 5: Shell Command Parser

**Files:**
- Create: `internal/toolexec/shell.go`
- Test: `internal/toolexec/shell_test.go`

**Step 1: Add mvdan.cc/sh dependency**

Run: `cd /Users/julianshen/prj/rubichan && go get mvdan.cc/sh/v3@latest`

**Step 2: Write the failing test**

```go
package toolexec_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCommandSimple(t *testing.T) {
	parts, err := toolexec.ParseCommand("git push origin main")
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "git", parts[0].Prefix)
	assert.Equal(t, "git push origin main", parts[0].Full)
}

func TestParseCommandCompound(t *testing.T) {
	parts, err := toolexec.ParseCommand("go test ./... && rm -rf /tmp/cache")
	require.NoError(t, err)
	require.Len(t, parts, 2)
	assert.Equal(t, "go", parts[0].Prefix)
	assert.Equal(t, "rm", parts[1].Prefix)
}

func TestParseCommandPipeline(t *testing.T) {
	parts, err := toolexec.ParseCommand("cat file.txt | grep error | wc -l")
	require.NoError(t, err)
	require.Len(t, parts, 3)
	assert.Equal(t, "cat", parts[0].Prefix)
	assert.Equal(t, "grep", parts[1].Prefix)
	assert.Equal(t, "wc", parts[2].Prefix)
}

func TestParseCommandSubshell(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo $(rm -rf /)")
	require.NoError(t, err)
	// Should find both echo and rm inside the command substitution.
	prefixes := make([]string, len(parts))
	for i, p := range parts {
		prefixes[i] = p.Prefix
	}
	assert.Contains(t, prefixes, "echo")
	assert.Contains(t, prefixes, "rm")
}

func TestParseCommandEnvPrefix(t *testing.T) {
	parts, err := toolexec.ParseCommand("RAILS_ENV=prod rails db:migrate")
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "rails", parts[0].Prefix, "should skip env var prefix")
}

func TestParseCommandBashDashC(t *testing.T) {
	parts, err := toolexec.ParseCommand(`bash -c "npm install"`)
	require.NoError(t, err)
	// Should extract npm from inside the -c argument.
	prefixes := make([]string, len(parts))
	for i, p := range parts {
		prefixes[i] = p.Prefix
	}
	assert.Contains(t, prefixes, "npm")
}

func TestParseCommandQuotedArgs(t *testing.T) {
	parts, err := toolexec.ParseCommand(`rm '-rf' /`)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "rm", parts[0].Prefix)
	assert.Contains(t, parts[0].Full, "-rf")
}

func TestParseCommandEmpty(t *testing.T) {
	parts, err := toolexec.ParseCommand("")
	require.NoError(t, err)
	assert.Empty(t, parts)
}

func TestParseCommandSemicolon(t *testing.T) {
	parts, err := toolexec.ParseCommand("echo hello; rm -rf /")
	require.NoError(t, err)
	require.Len(t, parts, 2)
	assert.Equal(t, "echo", parts[0].Prefix)
	assert.Equal(t, "rm", parts[1].Prefix)
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestParseCommand'`
Expected: FAIL — `ParseCommand`, `CommandPart` not defined.

**Step 4: Write minimal implementation**

```go
package toolexec

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandPart represents a single simple command extracted from a shell command string.
type CommandPart struct {
	Prefix string // the command name (first word)
	Full   string // the full command with arguments
}

// ParseCommand parses a shell command string into its AST and extracts every
// simple command as a CommandPart. Handles compound commands (&&, ||, ;, |),
// subshells, command substitutions, and env var prefixes.
func ParseCommand(command string) ([]CommandPart, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}

	parser := syntax.NewParser(syntax.KeepComments(false))
	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, err
	}

	var parts []CommandPart
	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		part := extractFromCallExpr(call)
		if part.Prefix != "" {
			parts = append(parts, part)
		}
		return true
	})
	return parts, nil
}

// extractFromCallExpr extracts a CommandPart from a syntax.CallExpr.
// It skips environment variable assignments to find the actual command prefix.
func extractFromCallExpr(call *syntax.CallExpr) CommandPart {
	// Find the first arg that isn't consumed by env assignments.
	startIdx := len(call.Assigns)
	if startIdx >= len(call.Args) {
		// All args are env vars, no actual command (e.g., "FOO=bar").
		// Check if there's a command after assigns.
		if len(call.Args) == 0 {
			return CommandPart{}
		}
		startIdx = 0
	}
	// Actually, Assigns are separate from Args in the AST.
	// Args[0] is always the command (after env assigns are parsed out).
	if len(call.Args) == 0 {
		return CommandPart{}
	}

	prefix := wordToString(call.Args[0])
	var fullParts []string
	for _, arg := range call.Args {
		fullParts = append(fullParts, wordToString(arg))
	}
	return CommandPart{
		Prefix: prefix,
		Full:   strings.Join(fullParts, " "),
	}
}

// wordToString converts a syntax.Word to its string representation,
// stripping quotes.
func wordToString(word *syntax.Word) string {
	var sb strings.Builder
	syntax.Walk(word, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Lit:
			sb.WriteString(n.Value)
		case *syntax.SglQuoted:
			sb.WriteString(n.Value)
		case *syntax.DblQuoted:
			// Let children (Lit nodes inside) handle it.
			return true
		}
		return true
	})
	return sb.String()
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestParseCommand'`
Expected: PASS (9 tests). Some tests like `TestParseCommandBashDashC` may need iteration — the `bash -c` case requires special handling where we re-parse the `-c` argument string. If so, add this helper:

```go
// isBashDashC checks if this is a `bash -c "..."` invocation and returns
// the inner command string if so.
func isBashDashC(call *syntax.CallExpr) (string, bool) {
	if len(call.Args) < 3 {
		return "", false
	}
	cmd := wordToString(call.Args[0])
	if cmd != "bash" && cmd != "sh" {
		return "", false
	}
	flag := wordToString(call.Args[1])
	if flag != "-c" {
		return "", false
	}
	return wordToString(call.Args[2]), true
}
```

And call it in `ParseCommand` to recursively parse the inner command. Run tests until all pass.

**Step 6: Commit**

```
[BEHAVIORAL] Add shell command parser using mvdan.cc/sh AST
```

---

### Task 6: Shell Safety Middleware

**Files:**
- Add to: `internal/toolexec/shell.go` (ShellValidator + middleware)
- Test: `internal/toolexec/shell_test.go` (add validator tests)

**Step 1: Write the failing test**

```go
func TestShellValidatorDeniesSubCommand(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})
	validator := toolexec.NewShellValidator(engine)

	// The compound command has an allowed prefix but a denied suffix.
	err := validator.Validate(context.Background(), "go test ./... && rm -rf /")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rm")
}

func TestShellValidatorAllowsCleanCommand(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	})
	validator := toolexec.NewShellValidator(engine)

	err := validator.Validate(context.Background(), "go test ./...")
	assert.NoError(t, err)
}

func TestShellSafetyMiddlewareBlocksDangerousCommand(t *testing.T) {
	engine := toolexec.NewRuleEngine([]toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	})
	validator := toolexec.NewShellValidator(engine)
	mw := toolexec.ShellSafetyMiddleware(validator)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryBash)
	result := handler(ctx, toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo hello && rm -rf /"}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "blocked")
	assert.False(t, baseCalled)
}

func TestShellSafetyMiddlewareSkipsNonBash(t *testing.T) {
	engine := toolexec.NewRuleEngine(nil)
	validator := toolexec.NewShellValidator(engine)
	mw := toolexec.ShellSafetyMiddleware(validator)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	ctx := toolexec.WithCategory(context.Background(), toolexec.CategoryFileRead)
	result := handler(ctx, toolexec.ToolCall{
		Name:  "file",
		Input: json.RawMessage(`{"path":"/etc/passwd"}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "ok", result.Content)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestShellValidator|TestShellSafety'`
Expected: FAIL — `ShellValidator`, `ShellSafetyMiddleware` not defined.

**Step 3: Write minimal implementation**

Add to `internal/toolexec/shell.go`:

```go
// ShellValidator validates shell commands against permission rules.
type ShellValidator struct {
	engine *RuleEngine
}

// NewShellValidator creates a ShellValidator backed by the given RuleEngine.
func NewShellValidator(engine *RuleEngine) *ShellValidator {
	return &ShellValidator{engine: engine}
}

// Validate parses the command string and checks each sub-command against rules.
// Returns an error if any sub-command is denied.
func (v *ShellValidator) Validate(ctx context.Context, command string) error {
	parts, err := ParseCommand(command)
	if err != nil {
		return fmt.Errorf("unparseable shell command: %w", err)
	}
	for _, part := range parts {
		input, _ := json.Marshal(map[string]string{"command": part.Full})
		action := v.engine.Evaluate(CategoryBash, "shell", input)
		if action == ActionDeny {
			return fmt.Errorf("sub-command %q blocked by deny rule", part.Prefix)
		}
	}
	return nil
}

// ShellSafetyMiddleware creates a Middleware that validates shell commands
// using full AST parsing. Only activates for CategoryBash tools.
func ShellSafetyMiddleware(validator *ShellValidator) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if CategoryFromContext(ctx) != CategoryBash {
				return next(ctx, tc)
			}
			command := extractCommandField(tc.Input)
			if command == "" {
				return next(ctx, tc)
			}
			if err := validator.Validate(ctx, command); err != nil {
				return Result{
					Content: fmt.Sprintf("shell command blocked: %s", err),
					IsError: true,
				}
			}
			return next(ctx, tc)
		}
	}
}

// extractCommandField extracts the "command" string value from JSON input.
func extractCommandField(input json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return ""
	}
	cmd, ok := obj["command"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(cmd, &s); err != nil {
		return ""
	}
	return s
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestShellValidator|TestShellSafety'`
Expected: PASS (4 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add shell safety validator with AST-based sub-command checking
```

---

### Task 7: Hook + Output Middlewares

**Files:**
- Create: `internal/toolexec/middleware.go`
- Test: `internal/toolexec/middleware_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
)

// mockHookDispatcher implements the HookDispatcher interface for testing.
type mockHookDispatcher struct {
	beforeCancel  bool
	afterModified map[string]any
}

func (m *mockHookDispatcher) DispatchBeforeToolCall(ctx context.Context, toolName string, input json.RawMessage) (bool, error) {
	return m.beforeCancel, nil
}

func (m *mockHookDispatcher) DispatchAfterToolResult(ctx context.Context, toolName, content string, isError bool) (map[string]any, error) {
	return m.afterModified, nil
}

func TestHookMiddlewareCancels(t *testing.T) {
	dispatcher := &mockHookDispatcher{beforeCancel: true}
	mw := toolexec.HookMiddleware(dispatcher)

	baseCalled := false
	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		baseCalled = true
		return toolexec.Result{Content: "ok"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "cancelled by skill")
	assert.False(t, baseCalled)
}

func TestHookMiddlewarePassesThrough(t *testing.T) {
	dispatcher := &mockHookDispatcher{}
	mw := toolexec.HookMiddleware(dispatcher)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "executed"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "executed", result.Content)
}

func TestPostHookMiddlewareModifiesContent(t *testing.T) {
	dispatcher := &mockHookDispatcher{
		afterModified: map[string]any{"content": "redacted"},
	}
	mw := toolexec.PostHookMiddleware(dispatcher)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "sensitive-data"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		Name:  "shell",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "redacted", result.Content)
}

// mockResultOffloader implements OutputOffloader for testing.
type mockResultOffloader struct {
	threshold int
}

func (m *mockResultOffloader) OffloadResult(toolName, toolUseID, content string) (string, error) {
	if len(content) > m.threshold {
		return "[offloaded]", nil
	}
	return content, nil
}

func TestOutputManagerMiddlewareOffloadsLargeResults(t *testing.T) {
	offloader := &mockResultOffloader{threshold: 10}
	mw := toolexec.OutputManagerMiddleware(offloader)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "this is a very long result that exceeds threshold"}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "1",
		Name: "shell",
		Input: json.RawMessage(`{}`),
	})

	assert.Equal(t, "[offloaded]", result.Content)
}

func TestOutputManagerMiddlewareSkipsErrors(t *testing.T) {
	offloader := &mockResultOffloader{threshold: 10}
	mw := toolexec.OutputManagerMiddleware(offloader)

	base := func(ctx context.Context, tc toolexec.ToolCall) toolexec.Result {
		return toolexec.Result{Content: "this is a very long error that exceeds threshold", IsError: true}
	}

	handler := mw(base)
	result := handler(context.Background(), toolexec.ToolCall{
		ID:   "1",
		Name: "shell",
		Input: json.RawMessage(`{}`),
	})

	// Errors should not be offloaded.
	assert.Contains(t, result.Content, "long error")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestHookMiddleware|TestPostHook|TestOutputManager'`
Expected: FAIL — `HookMiddleware`, `PostHookMiddleware`, `OutputManagerMiddleware`, interfaces not defined.

**Step 3: Write minimal implementation**

```go
package toolexec

import (
	"context"
	"encoding/json"
	"fmt"
)

// HookDispatcher abstracts the skill runtime's hook dispatch for the middleware.
// This avoids importing internal/skills in internal/toolexec.
type HookDispatcher interface {
	// DispatchBeforeToolCall returns true if the tool call should be cancelled.
	DispatchBeforeToolCall(ctx context.Context, toolName string, input json.RawMessage) (cancel bool, err error)
	// DispatchAfterToolResult returns modified data (e.g., {"content": "new"}).
	DispatchAfterToolResult(ctx context.Context, toolName, content string, isError bool) (modified map[string]any, err error)
}

// HookMiddleware creates a Middleware that dispatches HookOnBeforeToolCall.
// If any hook cancels, the tool call is short-circuited.
func HookMiddleware(dispatcher HookDispatcher) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			if dispatcher == nil {
				return next(ctx, tc)
			}
			cancel, err := dispatcher.DispatchBeforeToolCall(ctx, tc.Name, tc.Input)
			if err != nil {
				return Result{
					Content: fmt.Sprintf("hook error: %s", err),
					IsError: true,
				}
			}
			if cancel {
				return Result{
					Content: "tool call cancelled by skill",
					IsError: true,
				}
			}
			return next(ctx, tc)
		}
	}
}

// PostHookMiddleware creates a Middleware that dispatches HookOnAfterToolResult.
// Hooks can modify the result content (e.g., redact sensitive data).
func PostHookMiddleware(dispatcher HookDispatcher) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)
			if dispatcher == nil {
				return result
			}
			modified, err := dispatcher.DispatchAfterToolResult(ctx, tc.Name, result.Content, result.IsError)
			if err != nil {
				return result // graceful degradation
			}
			if modContent, ok := modified["content"].(string); ok {
				result.Content = modContent
				result.DisplayContent = "" // use modified content
			}
			return result
		}
	}
}

// OutputOffloader abstracts the ResultStore for the output manager middleware.
type OutputOffloader interface {
	OffloadResult(toolName, toolUseID, content string) (string, error)
}

// OutputManagerMiddleware creates a Middleware that offloads large results to disk.
// Errors are never offloaded.
func OutputManagerMiddleware(offloader OutputOffloader) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, tc ToolCall) Result {
			result := next(ctx, tc)
			if offloader == nil || result.IsError {
				return result
			}
			offloaded, err := offloader.OffloadResult(tc.Name, tc.ID, result.Content)
			if err == nil {
				result.Content = offloaded
			}
			return result
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestHookMiddleware|TestPostHook|TestOutputManager'`
Expected: PASS (5 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add hook dispatch and output manager middlewares
```

---

### Task 8: Base Executor

**Files:**
- Create: `internal/toolexec/executor.go`
- Test: `internal/toolexec/executor_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
)

func TestRegistryExecutorCallsTool(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "test_tool", result: "hello"})

	executor := toolexec.RegistryExecutor(reg)
	result := executor(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "test_tool",
		Input: json.RawMessage(`{}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "hello", result.Content)
}

func TestRegistryExecutorUnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	executor := toolexec.RegistryExecutor(reg)

	result := executor(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "nonexistent",
		Input: json.RawMessage(`{}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "unknown tool")
}

func TestRegistryExecutorToolError(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "error_tool", err: fmt.Errorf("boom")})

	executor := toolexec.RegistryExecutor(reg)
	result := executor(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "error_tool",
		Input: json.RawMessage(`{}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "boom")
}

// stubTool implements tools.Tool for testing.
type stubTool struct {
	name   string
	result string
	err    error
}

func (s *stubTool) Name() string                { return s.name }
func (s *stubTool) Description() string         { return "stub" }
func (s *stubTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	if s.err != nil {
		return tools.ToolResult{}, s.err
	}
	return tools.ToolResult{Content: s.result}, nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestRegistryExecutor'`
Expected: FAIL — `RegistryExecutor` not defined.

**Step 3: Write minimal implementation**

```go
package toolexec

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/tools"
)

// ToolLookup abstracts the tool registry for the base executor.
type ToolLookup interface {
	Get(name string) (tools.Tool, bool)
}

// RegistryExecutor creates a base HandlerFunc that looks up and executes tools
// from a ToolLookup (typically *tools.Registry).
func RegistryExecutor(lookup ToolLookup) HandlerFunc {
	return func(ctx context.Context, tc ToolCall) Result {
		tool, found := lookup.Get(tc.Name)
		if !found {
			return Result{
				Content: fmt.Sprintf("unknown tool: %s", tc.Name),
				IsError: true,
			}
		}
		toolResult, err := tool.Execute(ctx, tc.Input)
		if err != nil {
			return Result{
				Content: fmt.Sprintf("tool execution error: %s", err),
				IsError: true,
			}
		}
		return Result{
			Content:        toolResult.Content,
			DisplayContent: toolResult.DisplayContent,
			IsError:        toolResult.IsError,
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestRegistryExecutor'`
Expected: PASS (3 tests)

**Step 5: Commit**

```
[BEHAVIORAL] Add registry-based base executor for tool pipeline
```

---

### Task 9: Config Loading for Tool Rules

**Files:**
- Modify: `internal/config/config.go` (add `ToolRules` field to `AgentConfig`)
- Modify: `internal/security/projectconfig.go` (add `ToolRules` to `ProjectSecurityConfig`)
- Create: `internal/toolexec/config.go` (rule loading from both sources)
- Test: `internal/toolexec/config_test.go`

**Step 1: Write the failing test**

```go
package toolexec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRulesFromSecurityYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
tool_rules:
  - category: bash
    pattern: "rm -rf *"
    action: deny
  - category: bash
    pattern: "go test *"
    action: allow
`
	err := os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(yamlContent), 0o644)
	require.NoError(t, err)

	rules, err := toolexec.LoadSecurityYAMLRules(filepath.Join(dir, ".security.yaml"))
	require.NoError(t, err)
	require.Len(t, rules, 2)

	assert.Equal(t, toolexec.CategoryBash, rules[0].Category)
	assert.Equal(t, "rm -rf *", rules[0].Pattern)
	assert.Equal(t, toolexec.ActionDeny, rules[0].Action)
	assert.Equal(t, toolexec.SourceProject, rules[0].Source)
}

func TestLoadRulesMissingSecurity YAML(t *testing.T) {
	rules, err := toolexec.LoadSecurityYAMLRules("/nonexistent/.security.yaml")
	require.NoError(t, err, "missing file should not error")
	assert.Empty(t, rules)
}

func TestLoadRulesFromTOMLConfig(t *testing.T) {
	rules := toolexec.TOMLRulesToPermissionRules([]toolexec.ToolRuleConf{
		{Category: "bash", Pattern: "npm *", Action: "allow"},
		{Tool: "shell", Pattern: "docker *", Action: "deny"},
	}, toolexec.SourceUser)

	require.Len(t, rules, 2)
	assert.Equal(t, toolexec.CategoryBash, rules[0].Category)
	assert.Equal(t, toolexec.ActionAllow, rules[0].Action)
	assert.Equal(t, "shell", rules[1].Tool)
	assert.Equal(t, toolexec.ActionDeny, rules[1].Action)
}

func TestMergeRulesFromAllSources(t *testing.T) {
	user := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "npm *", Action: toolexec.ActionAllow, Source: toolexec.SourceUser},
	}
	project := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm *", Action: toolexec.ActionDeny, Source: toolexec.SourceProject},
	}

	merged := toolexec.MergeRules(user, project)
	assert.Len(t, merged, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/toolexec/... -v -run 'TestLoadRules|TestMergeRules'`
Expected: FAIL — functions not defined.

**Step 3: Modify config structs**

Add to `internal/config/config.go` inside `AgentConfig`:
```go
ToolRules []ToolRuleConf `toml:"tool_rules"`
```

Add new type:
```go
// ToolRuleConf defines a tool permission rule in config.
type ToolRuleConf struct {
	Category string `toml:"category"`
	Tool     string `toml:"tool"`
	Pattern  string `toml:"pattern"`
	Action   string `toml:"action"`
}
```

Add to `internal/security/projectconfig.go` inside `ProjectSecurityConfig`:
```go
ToolRules []ToolRuleYAML `yaml:"tool_rules"`
```

Add new type:
```go
// ToolRuleYAML defines a tool permission rule in .security.yaml.
type ToolRuleYAML struct {
	Category string `yaml:"category"`
	Tool     string `yaml:"tool"`
	Pattern  string `yaml:"pattern"`
	Action   string `yaml:"action"`
}
```

**Step 4: Implement config loading**

Create `internal/toolexec/config.go`:
```go
package toolexec

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ToolRuleConf mirrors config.ToolRuleConf to avoid importing config.
type ToolRuleConf struct {
	Category string
	Tool     string
	Pattern  string
	Action   string
}

// securityYAMLToolRules is the partial YAML structure for tool_rules.
type securityYAMLToolRules struct {
	ToolRules []struct {
		Category string `yaml:"category"`
		Tool     string `yaml:"tool"`
		Pattern  string `yaml:"pattern"`
		Action   string `yaml:"action"`
	} `yaml:"tool_rules"`
}

// LoadSecurityYAMLRules loads tool rules from a .security.yaml file.
func LoadSecurityYAMLRules(path string) ([]PermissionRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var parsed securityYAMLToolRules
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	var rules []PermissionRule
	for _, r := range parsed.ToolRules {
		rules = append(rules, PermissionRule{
			Category: Category(r.Category),
			Tool:     r.Tool,
			Pattern:  r.Pattern,
			Action:   RuleAction(r.Action),
			Source:   SourceProject,
		})
	}
	return rules, nil
}

// TOMLRulesToPermissionRules converts TOML config rules to PermissionRules.
func TOMLRulesToPermissionRules(confs []ToolRuleConf, source ConfigSource) []PermissionRule {
	var rules []PermissionRule
	for _, c := range confs {
		rules = append(rules, PermissionRule{
			Category: Category(c.Category),
			Tool:     c.Tool,
			Pattern:  c.Pattern,
			Action:   RuleAction(c.Action),
			Source:   source,
		})
	}
	return rules
}

// MergeRules concatenates rule slices from multiple sources.
func MergeRules(sources ...[]PermissionRule) []PermissionRule {
	var all []PermissionRule
	for _, s := range sources {
		all = append(all, s...)
	}
	return all
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/toolexec/... -v -run 'TestLoadRules|TestMergeRules'`
Expected: PASS (4 tests)

**Step 6: Commit (two commits)**

```
[STRUCTURAL] Add ToolRules fields to config and security YAML structs
```
```
[BEHAVIORAL] Add tool rule config loading and merging
```

---

### Task 10: Wire Pipeline into Agent

**Files:**
- Modify: `internal/agent/agent.go` (add `pipeline` field, `WithPipeline` option, slim down `executeSingleTool`)
- Test: `internal/agent/agent_test.go` (verify pipeline integration)

**Step 1: Write the failing test**

In `internal/agent/agent_test.go`, add a test that creates an Agent with a pipeline and verifies that `executeSingleTool` delegates to it. This test should use the existing test infrastructure (look at how other agent tests work in this file — if there are existing integration tests, follow that pattern).

If agent_test.go uses a test helper to create agents, use it. Otherwise, the simplest approach:

```go
func TestAgentExecutesSingleToolViaPipeline(t *testing.T) {
	// Verify pipeline is called by checking that executeSingleTool
	// returns the pipeline's result. This is tested via Turn() since
	// executeSingleTool is unexported.
	// Use a mock provider that returns a single tool_use block,
	// and verify the pipeline's base executor receives the call.
}
```

Because `executeSingleTool` is unexported, integration-test this through the existing `Turn()` mechanism or add a package-level test. The exact test depends on the existing test infrastructure — check `internal/agent/agent_test.go` for patterns.

**Step 2: Add pipeline field and option**

In `internal/agent/agent.go`, add to the `Agent` struct:

```go
pipeline *toolexec.Pipeline
```

Add option:

```go
// WithPipeline attaches a tool execution pipeline to the agent.
func WithPipeline(p *toolexec.Pipeline) AgentOption {
	return func(a *Agent) {
		a.pipeline = p
	}
}
```

**Step 3: Refactor executeSingleTool**

Replace the current `executeSingleTool` (lines 874-958) with:

```go
func (a *Agent) executeSingleTool(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
	if a.pipeline != nil {
		result := a.pipeline.Execute(ctx, toolexec.ToolCall{
			ID: tc.ID, Name: tc.Name, Input: tc.Input,
		})
		return toolExecResult{
			toolUseID: tc.ID,
			content:   result.Content,
			isError:   result.IsError,
			event:     makeToolResultEvent(tc.ID, tc.Name, result.Content, result.DisplayContent, result.IsError),
		}
	}
	// Fallback: legacy inline execution (keep for backward compat during rollout).
	return a.executeSingleToolLegacy(ctx, tc)
}
```

Rename the current `executeSingleTool` to `executeSingleToolLegacy` to preserve backward compatibility during rollout.

**Step 4: Run all tests**

Run: `go test ./internal/agent/... -v -count=1`
Expected: PASS — all existing tests still work via legacy path.

Run: `go test ./internal/toolexec/... -v -count=1`
Expected: PASS — all toolexec tests pass.

Run: `go test ./... -count=1`
Expected: PASS — full suite green.

**Step 5: Commit**

```
[BEHAVIORAL] Wire tool execution pipeline into agent with legacy fallback
```

---

### Task 11: Wire Pipeline in cmd/rubichan/main.go

**Files:**
- Modify: `cmd/rubichan/main.go` (construct pipeline and pass to agent)

**Step 1: Add pipeline construction**

After registry creation and skill runtime setup, add:

```go
// Build tool execution pipeline.
classifier := toolexec.NewClassifier(nil)

// Load rules from config and project.
var allRules []toolexec.PermissionRule
if cfg.Agent.ToolRules != nil {
    userRules := toolexec.TOMLRulesToPermissionRules(
        toToolRuleConfs(cfg.Agent.ToolRules), toolexec.SourceUser,
    )
    allRules = append(allRules, userRules...)
}
projRules, _ := toolexec.LoadSecurityYAMLRules(filepath.Join(projectRoot, ".security.yaml"))
allRules = append(allRules, projRules...)
localRules, _ := toolexec.LoadSecurityYAMLRules(filepath.Join(projectRoot, ".security.local.yaml"))
for i := range localRules {
    localRules[i].Source = toolexec.SourceLocal
}
allRules = append(allRules, localRules...)

ruleEngine := toolexec.NewRuleEngine(allRules)
shellValidator := toolexec.NewShellValidator(ruleEngine)

base := toolexec.RegistryExecutor(registry)
pipeline := toolexec.NewPipeline(
    base,
    toolexec.ClassifierMiddleware(classifier),
    toolexec.RuleEngineMiddleware(ruleEngine),
    toolexec.HookMiddleware(hookAdapter),  // adapter wrapping skills.Runtime
    toolexec.ShellSafetyMiddleware(shellValidator),
    // post-execution middlewares (applied in reverse, so listed after base):
    toolexec.PostHookMiddleware(hookAdapter),
    toolexec.OutputManagerMiddleware(resultStore),
)

// Pass pipeline to agent.
a := agent.New(provider, registry, approvalFunc, cfg,
    agent.WithPipeline(pipeline),
    // ... existing options
)
```

**Step 2: Create HookDispatcher adapter**

The `skills.Runtime` needs to satisfy `toolexec.HookDispatcher`. Create an adapter (either in `cmd/rubichan/main.go` or a small file):

```go
type hookDispatcherAdapter struct {
    rt *skills.Runtime
}

func (h *hookDispatcherAdapter) DispatchBeforeToolCall(ctx context.Context, toolName string, input json.RawMessage) (bool, error) {
    result, err := h.rt.DispatchHook(skills.HookEvent{
        Phase: skills.HookOnBeforeToolCall,
        Data:  map[string]any{"tool_name": toolName, "input": string(input)},
        Ctx:   ctx,
    })
    if err != nil {
        return false, err
    }
    if result != nil && result.Cancel {
        return true, nil
    }
    return false, nil
}

func (h *hookDispatcherAdapter) DispatchAfterToolResult(ctx context.Context, toolName, content string, isError bool) (map[string]any, error) {
    result, err := h.rt.DispatchHook(skills.HookEvent{
        Phase: skills.HookOnAfterToolResult,
        Data:  map[string]any{"tool_name": toolName, "content": content, "is_error": isError},
        Ctx:   ctx,
    })
    if err != nil {
        return nil, err
    }
    if result != nil {
        return result.Modified, nil
    }
    return nil, nil
}
```

**Step 3: Run full test suite**

Run: `go build ./cmd/rubichan/...`
Expected: Compiles successfully.

Run: `go test ./... -count=1`
Expected: All tests pass.

**Step 4: Commit**

```
[BEHAVIORAL] Wire tool middleware pipeline in CLI entrypoint
```

---

### Task 12: Remove Legacy Fallback

**Files:**
- Modify: `internal/agent/agent.go` (remove `executeSingleToolLegacy`, make pipeline required)

**Step 1: Verify all code paths use pipeline**

Check that all callers in `cmd/rubichan/main.go` (interactive, headless, code-review modes) create and pass a pipeline.

**Step 2: Remove legacy path**

Remove `executeSingleToolLegacy` and the `if a.pipeline != nil` conditional. Make `executeSingleTool` unconditionally delegate to pipeline:

```go
func (a *Agent) executeSingleTool(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
    result := a.pipeline.Execute(ctx, toolexec.ToolCall{
        ID: tc.ID, Name: tc.Name, Input: tc.Input,
    })
    return toolExecResult{
        toolUseID: tc.ID,
        content:   result.Content,
        isError:   result.IsError,
        event:     makeToolResultEvent(tc.ID, tc.Name, result.Content, result.DisplayContent, result.IsError),
    }
}
```

**Step 3: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

Run: `golangci-lint run ./...`
Expected: No warnings

Run: `gofmt -l .`
Expected: No unformatted files

**Step 4: Commit**

```
[STRUCTURAL] Remove legacy executeSingleTool fallback, pipeline is now required
```

---

### Task 13: Integration Test — Full Pipeline

**Files:**
- Create: `internal/toolexec/integration_test.go`

**Step 1: Write integration test**

```go
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

func TestFullPipelineDenyBlocksBeforeExecution(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", result: "should not run"})

	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)
	classifier := toolexec.NewClassifier(nil)
	validator := toolexec.NewShellValidator(engine)

	p := toolexec.NewPipeline(
		toolexec.RegistryExecutor(reg),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(engine),
		toolexec.ShellSafetyMiddleware(validator),
	)

	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf /"}`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "blocked")
}

func TestFullPipelineAllowExecutesNormally(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", result: "test output"})

	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
	}
	engine := toolexec.NewRuleEngine(rules)
	classifier := toolexec.NewClassifier(nil)
	validator := toolexec.NewShellValidator(engine)

	p := toolexec.NewPipeline(
		toolexec.RegistryExecutor(reg),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(engine),
		toolexec.ShellSafetyMiddleware(validator),
	)

	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./..."}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "test output", result.Content)
}

func TestFullPipelineCompoundCommandDeny(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", result: "should not run"})

	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "go test *", Action: toolexec.ActionAllow},
		{Category: toolexec.CategoryBash, Pattern: "rm -rf *", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)
	classifier := toolexec.NewClassifier(nil)
	validator := toolexec.NewShellValidator(engine)

	p := toolexec.NewPipeline(
		toolexec.RegistryExecutor(reg),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(engine),
		toolexec.ShellSafetyMiddleware(validator),
	)

	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"go test ./... && rm -rf /"}`),
	})

	assert.True(t, result.IsError, "compound command with denied sub-command should be blocked")
}

func TestFullPipelineNonBashSkipsSafety(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "file", result: "file content"})

	rules := []toolexec.PermissionRule{
		{Category: toolexec.CategoryBash, Pattern: "rm *", Action: toolexec.ActionDeny},
	}
	engine := toolexec.NewRuleEngine(rules)
	classifier := toolexec.NewClassifier(nil)
	validator := toolexec.NewShellValidator(engine)

	p := toolexec.NewPipeline(
		toolexec.RegistryExecutor(reg),
		toolexec.ClassifierMiddleware(classifier),
		toolexec.RuleEngineMiddleware(engine),
		toolexec.ShellSafetyMiddleware(validator),
	)

	result := p.Execute(context.Background(), toolexec.ToolCall{
		ID:    "1",
		Name:  "file",
		Input: json.RawMessage(`{"action":"read","path":"/tmp/rm-note.txt"}`),
	})

	assert.False(t, result.IsError)
	assert.Equal(t, "file content", result.Content)
}
```

**Step 2: Run integration test**

Run: `go test ./internal/toolexec/... -v -run 'TestFullPipeline'`
Expected: PASS (4 tests)

**Step 3: Run full suite with coverage**

Run: `go test ./internal/toolexec/... -cover`
Expected: >90% coverage

Run: `go test ./... -count=1`
Expected: All tests pass

**Step 4: Commit**

```
[BEHAVIORAL] Add integration tests for full tool middleware pipeline
```

---

### Task 14: Final Cleanup and Lint

**Step 1: Run all quality checks**

Run: `go test ./... -count=1`
Run: `golangci-lint run ./...`
Run: `gofmt -l .`

Fix any issues found.

**Step 2: Verify test coverage**

Run: `go test ./internal/toolexec/... -cover`
Expected: >90% coverage

**Step 3: Commit any fixes**

```
[STRUCTURAL] Fix lint warnings and formatting in toolexec package
```
