# Agent Enhancements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add subagent support (with task tool, agent definitions, async wake), glob-style permission patterns, and multi-provider prompt caching to Rubichan's agent system.

**Architecture:** Five enhancements layered bottom-up: glob trust rules (foundation), tool registry filtering, agent definitions, subagent spawner + task tool, async wake manager, and prompt caching across all three providers. Each layer is independently testable. The subagent system reuses the existing Agent, Provider, and ApprovalChecker interfaces.

**Tech Stack:** Go 1.22+, Bubble Tea TUI, custom HTTP+SSE providers, TOML config, `sourcegraph/conc` for structured concurrency, `testify` for assertions.

---

### Task 1: Glob Trust Rule Parsing

Add `ParseGlobRule()` to parse the user-friendly `ToolName(glob_pattern)` syntax into a tool name and compiled regex.

**Files:**
- Modify: `internal/agent/approval.go:127` (after `extractStringValues`, before `compiledRule`)
- Test: `internal/agent/approval_test.go`

**Step 1: Write the failing tests**

```go
// In internal/agent/approval_test.go

func TestParseGlobRule(t *testing.T) {
	tests := []struct {
		name    string
		glob    string
		tool    string
		match   []string // inputs that should match
		noMatch []string // inputs that should NOT match
		wantErr bool
	}{
		{
			name:    "simple wildcard",
			glob:    "shell(git *)",
			tool:    "shell",
			match:   []string{"git status", "git push origin main"},
			noMatch: []string{"npm test", "git"},
		},
		{
			name:    "question mark",
			glob:    "file(read:?.go)",
			tool:    "file",
			match:   []string{"read:a.go", "read:x.go"},
			noMatch: []string{"read:ab.go", "read:.go"},
		},
		{
			name:    "character class",
			glob:    "shell([gn]*)",
			tool:    "shell",
			match:   []string{"git status", "npm test"},
			noMatch: []string{"rm -rf /"},
		},
		{
			name:    "wildcard tool",
			glob:    "*(*.go)",
			tool:    "*",
			match:   []string{"main.go", "foo/bar.go"},
			noMatch: []string{"main.py"},
		},
		{
			name:    "missing parens",
			glob:    "shell",
			wantErr: true,
		},
		{
			name:    "empty pattern",
			glob:    "shell()",
			tool:    "shell",
			match:   []string{""},
			noMatch: []string{"anything"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, re, err := ParseGlobRule(tt.glob)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.tool, tool)

			for _, m := range tt.match {
				assert.True(t, re.MatchString(m), "expected %q to match", m)
			}
			for _, m := range tt.noMatch {
				assert.False(t, re.MatchString(m), "expected %q NOT to match", m)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestParseGlobRule -v`
Expected: FAIL — `ParseGlobRule` undefined

**Step 3: Write minimal implementation**

```go
// In internal/agent/approval.go, after extractStringValues function

// ParseGlobRule parses a glob trust rule in the format "ToolName(glob_pattern)"
// into a tool name and compiled regex. The glob pattern supports:
//   - * matches any sequence of characters
//   - ? matches any single character
//   - [abc] matches character classes (passed through to regex)
func ParseGlobRule(glob string) (string, *regexp.Regexp, error) {
	idx := strings.Index(glob, "(")
	if idx < 0 || !strings.HasSuffix(glob, ")") {
		return "", nil, fmt.Errorf("invalid glob rule %q: expected format ToolName(pattern)", glob)
	}

	tool := glob[:idx]
	pattern := glob[idx+1 : len(glob)-1]

	// Convert glob to regex.
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteByte('.')
		case '[':
			// Pass character class through to regex.
			j := strings.IndexByte(pattern[i:], ']')
			if j < 0 {
				return "", nil, fmt.Errorf("unclosed character class in glob %q", glob)
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

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return "", nil, fmt.Errorf("invalid glob pattern in %q: %w", glob, err)
	}

	return tool, re, nil
}
```

Add `"strings"` to imports if not already present.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestParseGlobRule -v`
Expected: PASS (all 6 sub-tests)

**Step 5: Commit**

```bash
git add internal/agent/approval.go internal/agent/approval_test.go
git commit -m "[BEHAVIORAL] Add ParseGlobRule for glob-style trust rule syntax"
```

---

### Task 2: Glob Trust Rules in Config and TrustRuleChecker

Add `Glob` field to `TrustRuleConf`, update `NewTrustRuleChecker` to compile glob rules alongside regex rules.

**Files:**
- Modify: `internal/config/config.go:70-77` (TrustRuleConf struct)
- Modify: `internal/agent/approval.go:86-96` (ValidateTrustRules), `approval.go:144-160` (NewTrustRuleChecker)
- Test: `internal/agent/approval_test.go`

**Step 1: Write the failing tests**

```go
func TestTrustRuleCheckerWithGlobRules(t *testing.T) {
	checker := NewTrustRuleChecker(nil, []GlobTrustRule{
		{Glob: "shell(git *)", Action: "allow"},
		{Glob: "shell(rm -rf *)", Action: "deny"},
		{Glob: "file(*.go)", Action: "allow"},
	})

	// git commands allowed
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"git status"}`)))
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"git push"}`)))

	// rm -rf denied (deny takes precedence)
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("shell", jsonInput(`{"command":"rm -rf /"}`)))

	// file globs
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("file", jsonInput(`{"path":"main.go"}`)))
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("file", jsonInput(`{"path":"main.py"}`)))

	// unmatched tool
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("search", jsonInput(`{"query":"foo"}`)))
}

func TestTrustRuleCheckerMixedRegexAndGlob(t *testing.T) {
	checker := NewTrustRuleChecker(
		[]TrustRule{
			{Tool: "shell", Pattern: "^npm test", Action: "allow"},
		},
		[]GlobTrustRule{
			{Glob: "shell(go test *)", Action: "allow"},
		},
	)

	// Regex rule matches
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"npm test"}`)))
	// Glob rule matches
	assert.Equal(t, TrustRuleApproved, checker.CheckApproval("shell", jsonInput(`{"command":"go test ./..."}`)))
	// Neither matches
	assert.Equal(t, ApprovalRequired, checker.CheckApproval("shell", jsonInput(`{"command":"curl evil.com"}`)))
}

// jsonInput helper — may already exist; if not, add it:
func jsonInput(s string) json.RawMessage {
	return json.RawMessage(s)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestTrustRuleChecker -v`
Expected: FAIL — `NewTrustRuleChecker` signature mismatch / `GlobTrustRule` undefined

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add `Glob` to `TrustRuleConf`:

```go
type TrustRuleConf struct {
	Tool    string `toml:"tool"`
	Pattern string `toml:"pattern"`
	Glob    string `toml:"glob"` // glob syntax: "tool(pattern)"
	Action  string `toml:"action"`
}
```

In `internal/agent/approval.go`, add the `GlobTrustRule` type and update `NewTrustRuleChecker`:

```go
// GlobTrustRule defines a trust rule using the user-friendly glob syntax.
type GlobTrustRule struct {
	Glob   string `toml:"glob"`
	Action string `toml:"action"`
}

// NewTrustRuleChecker creates a checker from both regex and glob trust rules.
// Rules with invalid patterns are silently skipped.
func NewTrustRuleChecker(rules []TrustRule, globs []GlobTrustRule) *TrustRuleChecker {
	var compiled []compiledRule
	for _, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledRule{
			tool:   r.Tool,
			re:     re,
			action: r.Action,
		})
	}
	for _, g := range globs {
		tool, re, err := ParseGlobRule(g.Glob)
		if err != nil {
			continue
		}
		compiled = append(compiled, compiledRule{
			tool:   tool,
			re:     re,
			action: g.Action,
		})
	}
	return &TrustRuleChecker{rules: compiled}
}
```

Update all existing callers of `NewTrustRuleChecker(rules)` to `NewTrustRuleChecker(rules, nil)`. Search for call sites:

Run: `grep -rn "NewTrustRuleChecker" internal/ cmd/`

Update each call site to pass `nil` as the second argument.

Also update `ValidateTrustRules` to accept and validate glob rules:

```go
func ValidateTrustRules(rules []TrustRule, globs []GlobTrustRule) error {
	for i, r := range rules {
		if r.Action != "allow" && r.Action != "deny" {
			return fmt.Errorf("trust rule %d: invalid action %q (must be \"allow\" or \"deny\")", i, r.Action)
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("trust rule %d: invalid pattern %q: %w", i, r.Pattern, err)
		}
	}
	for i, g := range globs {
		if g.Action != "allow" && g.Action != "deny" {
			return fmt.Errorf("glob rule %d: invalid action %q (must be \"allow\" or \"deny\")", i, g.Action)
		}
		if _, _, err := ParseGlobRule(g.Glob); err != nil {
			return fmt.Errorf("glob rule %d: %w", i, err)
		}
	}
	return nil
}
```

Update callers of `ValidateTrustRules` similarly.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v && go test ./... 2>&1 | tail -5`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/agent/approval.go internal/agent/approval_test.go internal/config/config.go
git add -u  # catch any caller updates
git commit -m "[BEHAVIORAL] Add glob trust rules to config and TrustRuleChecker"
```

---

### Task 3: Tool Registry Filter Method

Add `Filter(names []string) *Registry` to create a subset registry for subagent tool sandboxing.

**Files:**
- Modify: `internal/tools/registry.go:12-15`
- Test: `internal/tools/registry_test.go`

**Step 1: Write the failing test**

```go
func TestRegistryFilter(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "file"})
	_ = r.Register(&stubTool{name: "shell"})
	_ = r.Register(&stubTool{name: "search"})

	// Filter to subset
	filtered := r.Filter([]string{"file", "search"})

	_, ok := filtered.Get("file")
	assert.True(t, ok, "file should be in filtered registry")

	_, ok = filtered.Get("search")
	assert.True(t, ok, "search should be in filtered registry")

	_, ok = filtered.Get("shell")
	assert.False(t, ok, "shell should NOT be in filtered registry")

	assert.Len(t, filtered.All(), 2)
}

func TestRegistryFilterNilReturnsAll(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "file"})
	_ = r.Register(&stubTool{name: "shell"})

	// nil whitelist = copy all tools
	filtered := r.Filter(nil)
	assert.Len(t, filtered.All(), 2)
}

func TestRegistryFilterUnknownNamesIgnored(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubTool{name: "file"})

	filtered := r.Filter([]string{"file", "nonexistent"})
	assert.Len(t, filtered.All(), 1)
}
```

If `stubTool` doesn't exist in the test file, add it:

```go
type stubTool struct {
	name string
}

func (s *stubTool) Name() string                   { return s.name }
func (s *stubTool) Description() string             { return s.name + " tool" }
func (s *stubTool) InputSchema() json.RawMessage     { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: "ok"}, nil
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestRegistryFilter -v`
Expected: FAIL — `Filter` method undefined

**Step 3: Write minimal implementation**

```go
// Filter creates a new Registry containing only tools whose names are in the
// whitelist. If names is nil, all tools are copied. Unknown names are ignored.
func (r *Registry) Filter(names []string) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := NewRegistry()
	if names == nil {
		for _, tool := range r.tools {
			_ = filtered.Register(tool)
		}
		return filtered
	}

	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		nameSet[n] = struct{}{}
	}
	for name, tool := range r.tools {
		if _, ok := nameSet[name]; ok {
			_ = filtered.Register(tool)
		}
	}
	return filtered
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestRegistryFilter -v`
Expected: PASS (all 3 sub-tests)

**Step 5: Commit**

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "[BEHAVIORAL] Add Registry.Filter for subagent tool subsetting"
```

---

### Task 4: AgentDef Type and AgentDefRegistry

Create `AgentDef` type and its registry with Register/Get/All/Unregister.

**Files:**
- Create: `internal/agent/agentdef.go`
- Create: `internal/agent/agentdef_test.go`

**Step 1: Write the failing test**

```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentDefRegistryRegisterAndGet(t *testing.T) {
	reg := NewAgentDefRegistry()

	def := &AgentDef{
		Name:         "explorer",
		Description:  "Explore codebase",
		SystemPrompt: "You are an explorer.",
		Tools:        []string{"file", "search"},
		MaxTurns:     5,
	}

	err := reg.Register(def)
	require.NoError(t, err)

	got, ok := reg.Get("explorer")
	assert.True(t, ok)
	assert.Equal(t, "explorer", got.Name)
	assert.Equal(t, []string{"file", "search"}, got.Tools)
}

func TestAgentDefRegistryDuplicateError(t *testing.T) {
	reg := NewAgentDefRegistry()
	def := &AgentDef{Name: "explorer"}
	require.NoError(t, reg.Register(def))
	assert.Error(t, reg.Register(def))
}

func TestAgentDefRegistryAll(t *testing.T) {
	reg := NewAgentDefRegistry()
	_ = reg.Register(&AgentDef{Name: "a"})
	_ = reg.Register(&AgentDef{Name: "b"})

	all := reg.All()
	assert.Len(t, all, 2)
}

func TestAgentDefRegistryUnregister(t *testing.T) {
	reg := NewAgentDefRegistry()
	_ = reg.Register(&AgentDef{Name: "explorer"})

	err := reg.Unregister("explorer")
	assert.NoError(t, err)

	_, ok := reg.Get("explorer")
	assert.False(t, ok)
}

func TestAgentDefRegistryUnregisterNotFound(t *testing.T) {
	reg := NewAgentDefRegistry()
	assert.Error(t, reg.Unregister("nonexistent"))
}

func TestAgentDefRegistryGetNotFound(t *testing.T) {
	reg := NewAgentDefRegistry()
	_, ok := reg.Get("nonexistent")
	assert.False(t, ok)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentDefRegistry -v`
Expected: FAIL — types undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/agentdef.go
package agent

import (
	"fmt"
	"sort"
	"sync"
)

// AgentDef is a named, pre-configured subagent template.
type AgentDef struct {
	Name         string   `toml:"name" yaml:"name"`
	Description  string   `toml:"description" yaml:"description"`
	SystemPrompt string   `toml:"system_prompt" yaml:"system_prompt"`
	Tools        []string `toml:"tools" yaml:"tools"`
	MaxTurns     int      `toml:"max_turns" yaml:"max_turns"`
	MaxDepth     int      `toml:"max_depth" yaml:"max_depth"`
	Model        string   `toml:"model" yaml:"model"`
}

// AgentDefRegistry stores named agent definitions.
type AgentDefRegistry struct {
	mu   sync.RWMutex
	defs map[string]*AgentDef
}

// NewAgentDefRegistry creates an empty registry.
func NewAgentDefRegistry() *AgentDefRegistry {
	return &AgentDefRegistry{defs: make(map[string]*AgentDef)}
}

// Register adds an agent definition. Returns error if name is already registered.
func (r *AgentDefRegistry) Register(def *AgentDef) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[def.Name]; ok {
		return fmt.Errorf("agent definition %q already registered", def.Name)
	}
	r.defs[def.Name] = def
	return nil
}

// Get retrieves an agent definition by name.
func (r *AgentDefRegistry) Get(name string) (*AgentDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[name]
	return def, ok
}

// All returns all registered agent definitions sorted by name.
func (r *AgentDefRegistry) All() []*AgentDef {
	r.mu.RLock()
	result := make([]*AgentDef, 0, len(r.defs))
	for _, def := range r.defs {
		result = append(result, def)
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Unregister removes an agent definition by name.
func (r *AgentDefRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[name]; !ok {
		return fmt.Errorf("agent definition %q not found", name)
	}
	delete(r.defs, name)
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentDefRegistry -v`
Expected: PASS (all 6 sub-tests)

**Step 5: Commit**

```bash
git add internal/agent/agentdef.go internal/agent/agentdef_test.go
git commit -m "[BEHAVIORAL] Add AgentDef type and AgentDefRegistry"
```

---

### Task 5: Config Integration for Agent Definitions

Add `[[agent.definitions]]` TOML config section and wire it into agent construction.

**Files:**
- Modify: `internal/config/config.go:57-68` (AgentConfig struct)
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestConfigAgentDefinitions(t *testing.T) {
	tomlData := `
[provider]
default = "anthropic"
model = "claude-3"

[agent]
max_turns = 10

[[agent.definitions]]
name = "explorer"
description = "Explore codebase"
system_prompt = "You are an explorer."
tools = ["file", "search"]
max_turns = 5
max_depth = 2
`
	cfg, err := ParseTOML([]byte(tomlData))
	require.NoError(t, err)
	require.Len(t, cfg.Agent.Definitions, 1)
	assert.Equal(t, "explorer", cfg.Agent.Definitions[0].Name)
	assert.Equal(t, []string{"file", "search"}, cfg.Agent.Definitions[0].Tools)
	assert.Equal(t, 5, cfg.Agent.Definitions[0].MaxTurns)
	assert.Equal(t, 2, cfg.Agent.Definitions[0].MaxDepth)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfigAgentDefinitions -v`
Expected: FAIL — `Definitions` field not found on AgentConfig

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add to `AgentConfig`:

```go
type AgentDefConf struct {
	Name         string   `toml:"name"`
	Description  string   `toml:"description"`
	SystemPrompt string   `toml:"system_prompt"`
	Tools        []string `toml:"tools"`
	MaxTurns     int      `toml:"max_turns"`
	MaxDepth     int      `toml:"max_depth"`
	Model        string   `toml:"model"`
}

// Add to AgentConfig struct:
Definitions []AgentDefConf `toml:"definitions"`
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfigAgentDefinitions -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "[BEHAVIORAL] Add agent definitions to TOML config"
```

---

### Task 6: SubagentConfig, SubagentResult, and SubagentSpawner Interface

Define the core subagent types and spawner interface.

**Files:**
- Create: `internal/agent/subagent.go`
- Create: `internal/agent/subagent_test.go`

**Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockSpawner is a test double for SubagentSpawner.
type mockSpawner struct {
	spawnCfg    SubagentConfig
	spawnPrompt string
	result      *SubagentResult
	err         error
}

func (m *mockSpawner) Spawn(_ context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error) {
	m.spawnCfg = cfg
	m.spawnPrompt = prompt
	return m.result, m.err
}

func TestSubagentConfigDefaults(t *testing.T) {
	cfg := SubagentConfig{Name: "test"}

	// Verify zero-value defaults
	assert.Equal(t, 0, cfg.MaxTurns, "default max turns should be 0 (caller sets default)")
	assert.Equal(t, 0, cfg.Depth, "default depth should be 0")
	assert.Equal(t, 0, cfg.MaxDepth, "default max depth should be 0 (caller sets default)")
	assert.Nil(t, cfg.Tools, "nil tools means all parent tools")
}

func TestSubagentResultFields(t *testing.T) {
	result := SubagentResult{
		Name:         "explorer",
		Output:       "Found 3 files matching pattern",
		ToolsUsed:    []string{"search", "file"},
		TurnCount:    2,
		InputTokens:  1500,
		OutputTokens: 300,
	}

	assert.Equal(t, "explorer", result.Name)
	assert.Equal(t, 2, result.TurnCount)
	assert.Nil(t, result.Error)
}

func TestMockSpawnerSatisfiesInterface(t *testing.T) {
	var _ SubagentSpawner = (*mockSpawner)(nil)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestSubagent -v`
Expected: FAIL — types undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/subagent.go
package agent

import "context"

// SubagentConfig defines how a child agent is created.
type SubagentConfig struct {
	Name         string   // Identifier (e.g., "explorer")
	SystemPrompt string   // Additional system prompt (appended to base)
	Tools        []string // Whitelist of tool names (nil = all parent tools)
	MaxTurns     int      // Turn limit (0 = default 10)
	MaxTokens    int      // Output token budget (0 = inherit)
	Model        string   // Override model (empty = inherit parent)
	Depth        int      // Current nesting level (0 = top-level)
	MaxDepth     int      // Maximum nesting (0 = default 3)
}

// SubagentResult is returned when a child agent completes.
type SubagentResult struct {
	Name         string   // Which agent definition was used
	Output       string   // Final text output from the child
	ToolsUsed    []string // Tools the child called
	TurnCount    int      // How many turns the child took
	InputTokens  int      // Total input tokens consumed
	OutputTokens int      // Total output tokens consumed
	Error        error    // Non-nil if the child failed
}

// SubagentSpawner creates and runs child agents. The interface decouples
// the TaskTool from Agent construction, enabling unit testing with mocks.
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestSubagent -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/subagent.go internal/agent/subagent_test.go
git commit -m "[BEHAVIORAL] Add SubagentConfig, SubagentResult, and SubagentSpawner interface"
```

---

### Task 7: DefaultSubagentSpawner Implementation

Implement the real spawner that creates child Agent instances, runs their turn loops, and collects results.

**Files:**
- Modify: `internal/agent/subagent.go`
- Modify: `internal/agent/subagent_test.go`

**Step 1: Write the failing test**

```go
func TestDefaultSubagentSpawnerMaxDepth(t *testing.T) {
	spawner := &DefaultSubagentSpawner{}
	cfg := SubagentConfig{
		Depth:    3,
		MaxDepth: 3,
	}
	_, err := spawner.Spawn(context.Background(), cfg, "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depth")
}

func TestDefaultSubagentSpawnerDefaults(t *testing.T) {
	spawner := &DefaultSubagentSpawner{}
	cfg := SubagentConfig{Name: "test"}

	// Spawn will fail because no provider is set, but we test that defaults
	// are applied before the failure.
	_, err := spawner.Spawn(context.Background(), cfg, "hello")
	// Should fail with a meaningful error (no provider), not panic
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestDefaultSubagentSpawner -v`
Expected: FAIL — `DefaultSubagentSpawner` undefined

**Step 3: Write minimal implementation**

```go
// In internal/agent/subagent.go

// DefaultSubagentSpawner creates real child Agent instances. It requires
// a parent provider, tool registry, config, and approval checker.
type DefaultSubagentSpawner struct {
	Provider        provider.LLMProvider
	ParentTools     *tools.Registry
	Config          *config.Config
	ApprovalChecker ApprovalChecker
	AgentDefs       *AgentDefRegistry
}

const (
	defaultSubagentMaxTurns = 10
	defaultSubagentMaxDepth = 3
)

// Spawn creates a child Agent with the given config and runs it to completion.
func (s *DefaultSubagentSpawner) Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error) {
	// Apply defaults.
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = defaultSubagentMaxTurns
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = defaultSubagentMaxDepth
	}

	// Depth check.
	if cfg.Depth >= cfg.MaxDepth {
		return nil, fmt.Errorf("subagent depth %d exceeds max depth %d", cfg.Depth, cfg.MaxDepth)
	}

	if s.Provider == nil {
		return nil, fmt.Errorf("subagent spawner has no provider configured")
	}

	// Filter tools.
	childTools := s.ParentTools.Filter(cfg.Tools)

	// Build child config.
	childCfg := *s.Config
	childCfg.Agent.MaxTurns = cfg.MaxTurns
	if cfg.Model != "" {
		childCfg.Provider.Model = cfg.Model
	}

	// Determine provider (may override model).
	childProvider := s.Provider

	// Build options.
	var opts []AgentOption
	if s.ApprovalChecker != nil {
		opts = append(opts, WithApprovalChecker(s.ApprovalChecker))
	}
	if cfg.SystemPrompt != "" {
		opts = append(opts, WithExtraSystemPrompt("Subagent Instructions", cfg.SystemPrompt))
	}

	// Create child agent (no store — subagent sessions are ephemeral).
	child := New(childProvider, childTools, nil, &childCfg, opts...)

	// Run turn loop and collect output.
	result := &SubagentResult{Name: cfg.Name}
	var output strings.Builder
	var toolsUsed []string
	toolSet := make(map[string]struct{})

	for turn := 0; turn < cfg.MaxTurns; turn++ {
		eventCh := child.Turn(ctx, prompt)
		// Only send the user prompt on the first turn.
		prompt = ""

		var hasTool bool
		for event := range eventCh {
			switch event.Type {
			case "text_delta":
				output.WriteString(event.Text)
			case "tool_call":
				if event.ToolCall != nil {
					if _, seen := toolSet[event.ToolCall.Name]; !seen {
						toolSet[event.ToolCall.Name] = struct{}{}
						toolsUsed = append(toolsUsed, event.ToolCall.Name)
					}
					hasTool = true
				}
			case "error":
				result.Error = event.Error
			case "done":
				result.InputTokens += event.InputTokens
				result.OutputTokens += event.OutputTokens
			}
		}

		result.TurnCount = turn + 1
		if !hasTool {
			break // No tool calls means the agent is done
		}
	}

	result.Output = output.String()
	result.ToolsUsed = toolsUsed
	return result, nil
}
```

Add imports: `"fmt"`, `"strings"`, provider/tools/config packages.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestDefaultSubagentSpawner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/subagent.go internal/agent/subagent_test.go
git commit -m "[BEHAVIORAL] Add DefaultSubagentSpawner implementation"
```

---

### Task 8: TaskTool (Synchronous Mode)

Create the `TaskTool` that delegates tasks to subagents via the spawner.

**Files:**
- Create: `internal/tools/task.go`
- Create: `internal/tools/task_test.go`

**Step 1: Write the failing test**

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
)

type fakeSpawner struct {
	lastCfg    agent.SubagentConfig
	lastPrompt string
	result     *agent.SubagentResult
	err        error
}

func (f *fakeSpawner) Spawn(_ context.Context, cfg agent.SubagentConfig, prompt string) (*agent.SubagentResult, error) {
	f.lastCfg = cfg
	f.lastPrompt = prompt
	return f.result, f.err
}

func TestTaskToolName(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	assert.Equal(t, "task", tool.Name())
}

func TestTaskToolExecute(t *testing.T) {
	spawner := &fakeSpawner{
		result: &agent.SubagentResult{
			Name:   "general",
			Output: "Found 3 matching files.",
			TurnCount: 2,
		},
	}
	defReg := agent.NewAgentDefRegistry()

	tool := NewTaskTool(spawner, defReg, 0)

	input := json.RawMessage(`{"prompt":"Find all Go test files"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Found 3 matching files.")
	assert.Equal(t, "Find all Go test files", spawner.lastPrompt)
}

func TestTaskToolWithAgentType(t *testing.T) {
	spawner := &fakeSpawner{
		result: &agent.SubagentResult{Name: "explorer", Output: "done"},
	}
	defReg := agent.NewAgentDefRegistry()
	_ = defReg.Register(&agent.AgentDef{
		Name:         "explorer",
		SystemPrompt: "You explore code.",
		Tools:        []string{"file", "search"},
		MaxTurns:     5,
	})

	tool := NewTaskTool(spawner, defReg, 0)

	input := json.RawMessage(`{"prompt":"explore","agent_type":"explorer"}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "explorer", spawner.lastCfg.Name)
	assert.Equal(t, []string{"file", "search"}, spawner.lastCfg.Tools)
	assert.Equal(t, 5, spawner.lastCfg.MaxTurns)
}

func TestTaskToolMissingPrompt(t *testing.T) {
	tool := NewTaskTool(nil, nil, 0)
	input := json.RawMessage(`{}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "prompt")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestTaskTool -v`
Expected: FAIL — `NewTaskTool` undefined

**Step 3: Write minimal implementation**

```go
// internal/tools/task.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/agent"
)

// TaskTool spawns a subagent to handle a delegated task.
type TaskTool struct {
	spawner  agent.SubagentSpawner
	agentDefs *agent.AgentDefRegistry
	depth    int // current nesting depth
}

// NewTaskTool creates a TaskTool with the given spawner, agent definition
// registry, and current nesting depth.
func NewTaskTool(spawner agent.SubagentSpawner, defs *agent.AgentDefRegistry, depth int) *TaskTool {
	return &TaskTool{spawner: spawner, agentDefs: defs, depth: depth}
}

func (t *TaskTool) Name() string        { return "task" }
func (t *TaskTool) Description() string  { return "Delegate a task to a subagent for autonomous execution" }

func (t *TaskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "The task description for the subagent"
			},
			"agent_type": {
				"type": "string",
				"description": "Named agent definition to use (default: general)"
			},
			"max_turns": {
				"type": "integer",
				"description": "Override maximum turns for the subagent"
			},
			"background": {
				"type": "boolean",
				"description": "Run the subagent asynchronously in the background"
			}
		},
		"required": ["prompt"]
	}`)
}

type taskInput struct {
	Prompt    string `json:"prompt"`
	AgentType string `json:"agent_type"`
	MaxTurns  int    `json:"max_turns"`
	Background bool  `json:"background"`
}

func (t *TaskTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var ti taskInput
	if err := json.Unmarshal(input, &ti); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	if ti.Prompt == "" {
		return ToolResult{Content: "prompt is required", IsError: true}, nil
	}

	// Build subagent config from agent definition.
	cfg := agent.SubagentConfig{
		Name:  "general",
		Depth: t.depth,
	}

	if ti.AgentType != "" && t.agentDefs != nil {
		def, ok := t.agentDefs.Get(ti.AgentType)
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

	// TODO: background mode handled in Task 10 (async wake)

	result, err := t.spawner.Spawn(ctx, cfg, ti.Prompt)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("subagent failed: %v", err), IsError: true}, nil
	}

	// Format result.
	content := result.Output
	if result.Error != nil {
		content = fmt.Sprintf("subagent error: %v\n%s", result.Error, result.Output)
	}

	return ToolResult{
		Content:        content,
		DisplayContent: fmt.Sprintf("[subagent:%s] %d turns, %d input / %d output tokens\n%s", result.Name, result.TurnCount, result.InputTokens, result.OutputTokens, content),
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestTaskTool -v`
Expected: PASS (all 4 sub-tests)

**Step 5: Commit**

```bash
git add internal/tools/task.go internal/tools/task_test.go
git commit -m "[BEHAVIORAL] Add TaskTool for subagent delegation"
```

---

### Task 9: WakeManager for Background Subagents

Create the WakeManager that tracks background tasks and delivers completion events.

**Files:**
- Create: `internal/agent/wake.go`
- Create: `internal/agent/wake_test.go`

**Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWakeManagerSubmitAndComplete(t *testing.T) {
	wm := NewWakeManager()

	_, cancel := context.WithCancel(context.Background())
	taskID := wm.Submit("explorer", cancel)
	assert.NotEmpty(t, taskID)

	// Complete the task.
	wm.Complete(taskID, &SubagentResult{
		Name:   "explorer",
		Output: "found files",
	})

	// Read the wake event.
	select {
	case event := <-wm.Events():
		assert.Equal(t, "explorer", event.AgentName)
		assert.Equal(t, taskID, event.TaskID)
		assert.Equal(t, "found files", event.Result.Output)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for wake event")
	}
}

func TestWakeManagerStatus(t *testing.T) {
	wm := NewWakeManager()

	_, cancel1 := context.WithCancel(context.Background())
	id1 := wm.Submit("agent1", cancel1)
	_, cancel2 := context.WithCancel(context.Background())
	id2 := wm.Submit("agent2", cancel2)

	statuses := wm.Status()
	assert.Len(t, statuses, 2)

	// Complete one.
	wm.Complete(id1, &SubagentResult{Name: "agent1", Output: "done"})
	// Drain the event.
	<-wm.Events()

	statuses = wm.Status()
	assert.Len(t, statuses, 1)
	assert.Equal(t, id2, statuses[0].ID)
	assert.Equal(t, "running", statuses[0].Status)
}

func TestWakeManagerPendingCount(t *testing.T) {
	wm := NewWakeManager()
	assert.Equal(t, 0, wm.PendingCount())

	_, cancel := context.WithCancel(context.Background())
	wm.Submit("test", cancel)
	assert.Equal(t, 1, wm.PendingCount())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestWakeManager -v`
Expected: FAIL — types undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/wake.go
package agent

import (
	"sync"

	"github.com/google/uuid"
)

// WakeEvent signals that a background subagent has completed.
type WakeEvent struct {
	AgentName string
	TaskID    string
	Result    *SubagentResult
}

// TaskStatus represents the current state of a background task.
type TaskStatus struct {
	ID        string
	AgentName string
	Status    string // "running", "completed", "failed"
}

// backgroundTask tracks a running subagent goroutine.
type backgroundTask struct {
	ID        string
	AgentName string
	Cancel    context.CancelFunc
}

// WakeManager tracks background subagent tasks and delivers completion events.
type WakeManager struct {
	mu      sync.Mutex
	pending map[string]*backgroundTask
	wakeCh  chan WakeEvent
}

// NewWakeManager creates a WakeManager with a buffered event channel.
func NewWakeManager() *WakeManager {
	return &WakeManager{
		pending: make(map[string]*backgroundTask),
		wakeCh:  make(chan WakeEvent, 16),
	}
}

// Submit registers a new background task and returns its unique ID.
func (wm *WakeManager) Submit(name string, cancel context.CancelFunc) string {
	id := uuid.New().String()[:8]
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.pending[id] = &backgroundTask{
		ID:        id,
		AgentName: name,
		Cancel:    cancel,
	}
	return id
}

// Complete marks a background task as done and sends a wake event.
func (wm *WakeManager) Complete(taskID string, result *SubagentResult) {
	wm.mu.Lock()
	task, ok := wm.pending[taskID]
	if ok {
		delete(wm.pending, taskID)
	}
	wm.mu.Unlock()

	if ok {
		wm.wakeCh <- WakeEvent{
			AgentName: task.AgentName,
			TaskID:    taskID,
			Result:    result,
		}
	}
}

// Events returns the channel for receiving wake events.
func (wm *WakeManager) Events() <-chan WakeEvent {
	return wm.wakeCh
}

// Status returns the current status of all pending tasks.
func (wm *WakeManager) Status() []TaskStatus {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	result := make([]TaskStatus, 0, len(wm.pending))
	for _, task := range wm.pending {
		result = append(result, TaskStatus{
			ID:        task.ID,
			AgentName: task.AgentName,
			Status:    "running",
		})
	}
	return result
}

// PendingCount returns the number of background tasks still running.
func (wm *WakeManager) PendingCount() int {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	return len(wm.pending)
}
```

Add `"context"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestWakeManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/wake.go internal/agent/wake_test.go
git commit -m "[BEHAVIORAL] Add WakeManager for background subagent tracking"
```

---

### Task 10: TaskTool Background Mode and ListTasksTool

Wire background mode into TaskTool. Add a ListTasksTool for checking background task status.

**Files:**
- Modify: `internal/tools/task.go` (add background support)
- Create: `internal/tools/list_tasks.go`
- Test: `internal/tools/task_test.go`, `internal/tools/list_tasks_test.go`

**Step 1: Write the failing tests**

In `internal/tools/task_test.go`:

```go
func TestTaskToolBackgroundMode(t *testing.T) {
	spawner := &fakeSpawner{
		result: &agent.SubagentResult{Name: "explorer", Output: "done"},
	}

	wm := agent.NewWakeManager()
	tool := NewTaskTool(spawner, nil, 0)
	tool.SetWakeManager(wm)

	input := json.RawMessage(`{"prompt":"explore","background":true}`)
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Background task started")

	// Wait for the background task to complete.
	select {
	case event := <-wm.Events():
		assert.Equal(t, "general", event.AgentName)
		assert.Equal(t, "done", event.Result.Output)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background completion")
	}
}
```

In `internal/tools/list_tasks_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
)

func TestListTasksToolEmpty(t *testing.T) {
	wm := agent.NewWakeManager()
	tool := NewListTasksTool(wm)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No background tasks")
}

func TestListTasksToolWithPending(t *testing.T) {
	wm := agent.NewWakeManager()
	_, cancel := context.WithCancel(context.Background())
	wm.Submit("explorer", cancel)

	tool := NewListTasksTool(wm)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, result.Content, "explorer")
	assert.Contains(t, result.Content, "running")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tools/ -run "TestTaskToolBackground|TestListTasks" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Update `internal/tools/task.go` — add WakeManager field and background handling:

```go
// Add to TaskTool struct:
wakeManager *agent.WakeManager

// Add setter:
func (t *TaskTool) SetWakeManager(wm *agent.WakeManager) {
	t.wakeManager = wm
}

// In Execute(), before the synchronous spawn call, handle background mode:
if ti.Background {
	if t.wakeManager == nil {
		return ToolResult{Content: "background mode requires wake manager", IsError: true}, nil
	}
	bgCtx, cancel := context.WithCancel(context.Background())
	taskID := t.wakeManager.Submit(cfg.Name, cancel)

	go func() {
		defer cancel()
		result, _ := t.spawner.Spawn(bgCtx, cfg, ti.Prompt)
		if result == nil {
			result = &SubagentResult{Name: cfg.Name, Error: fmt.Errorf("spawn returned nil")}
		}
		t.wakeManager.Complete(taskID, result)
	}()

	return ToolResult{
		Content: fmt.Sprintf("Background task started: %s (agent: %s)", taskID, cfg.Name),
	}, nil
}
```

Create `internal/tools/list_tasks.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/agent"
)

// ListTasksTool reports the status of background subagent tasks.
type ListTasksTool struct {
	wakeManager *agent.WakeManager
}

// NewListTasksTool creates a ListTasksTool backed by the given WakeManager.
func NewListTasksTool(wm *agent.WakeManager) *ListTasksTool {
	return &ListTasksTool{wakeManager: wm}
}

func (t *ListTasksTool) Name() string        { return "list_tasks" }
func (t *ListTasksTool) Description() string  { return "List background subagent task status" }

func (t *ListTasksTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ListTasksTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	statuses := t.wakeManager.Status()
	if len(statuses) == 0 {
		return ToolResult{Content: "No background tasks running."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d background task(s):\n", len(statuses)))
	for _, s := range statuses {
		sb.WriteString(fmt.Sprintf("  - %s [%s] %s\n", s.ID, s.AgentName, s.Status))
	}
	return ToolResult{Content: sb.String()}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/ -run "TestTaskToolBackground|TestListTasks" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/task.go internal/tools/task_test.go internal/tools/list_tasks.go internal/tools/list_tasks_test.go
git commit -m "[BEHAVIORAL] Add TaskTool background mode and ListTasksTool"
```

---

### Task 11: Agent Loop Wake Integration

Wire the WakeManager into the Agent struct and check for wake events between turns.

**Files:**
- Modify: `internal/agent/agent.go:165-187` (Agent struct, add wakeManager field)
- Modify: `internal/agent/agent.go:518-659` (runLoop, add wake event check)
- Add: `internal/agent/agent.go` (new AgentOption: `WithWakeManager`)
- Test: `internal/agent/agent_test.go`

**Step 1: Write the failing test**

```go
func TestAgentWithWakeManager(t *testing.T) {
	wm := NewWakeManager()

	// Verify the option works.
	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text", Text: "hello"},
		{Type: "done", InputTokens: 10, OutputTokens: 5},
	}}
	cfg := &config.Config{Agent: config.AgentConfig{MaxTurns: 3}}
	a := New(p, tools.NewRegistry(), nil, cfg, WithWakeManager(wm))

	assert.NotNil(t, a.wakeManager)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentWithWakeManager -v`
Expected: FAIL — `WithWakeManager` undefined

**Step 3: Write minimal implementation**

In `internal/agent/agent.go`:

Add `wakeManager *WakeManager` to the `Agent` struct (around line 186).

Add the option function:

```go
// WithWakeManager attaches a wake manager for background subagent tracking.
func WithWakeManager(wm *WakeManager) AgentOption {
	return func(a *Agent) {
		a.wakeManager = wm
	}
}
```

In `runLoop()` (around line 650, after tool execution and before looping), add wake event injection:

```go
// Check for completed background subagent tasks.
if a.wakeManager != nil {
	for {
		select {
		case wake := <-a.wakeManager.Events():
			wakeMsg := fmt.Sprintf("[Background task %q completed (agent: %s)]\n%s",
				wake.TaskID, wake.AgentName, wake.Result.Output)
			a.conversation.AddUser(wakeMsg)
			ch <- TurnEvent{Type: "subagent_done", Text: wakeMsg}
		default:
			goto doneWake
		}
	}
doneWake:
}
```

Add `SubagentResult *SubagentResult` field to `TurnEvent` struct (around line 46).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentWithWakeManager -v && go test ./internal/agent/ -v 2>&1 | tail -5`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Wire WakeManager into Agent loop for background subagent events"
```

---

### Task 12: Anthropic Tool Definition Caching

Mark the last tool definition with `cache_control` for prompt caching.

**Files:**
- Modify: `internal/provider/anthropic/provider.go:118-155` (buildRequestBody)
- Test: `internal/provider/anthropic/cache_test.go`

**Step 1: Write the failing test**

```go
func TestToolDefinitionCaching(t *testing.T) {
	p := &Provider{apiKey: "test-key", baseURL: "http://test"}

	req := provider.CompletionRequest{
		Model:  "claude-3",
		System: "You are helpful.",
		Tools: []provider.ToolDef{
			{Name: "file", Description: "Read files", InputSchema: json.RawMessage(`{}`)},
			{Name: "shell", Description: "Run commands", InputSchema: json.RawMessage(`{}`)},
		},
	}

	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	// Parse the body to check for cache_control on last tool.
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &parsed))

	var tools []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(parsed["tools"], &tools))
	require.Len(t, tools, 2)

	// Last tool should have cache_control.
	assert.Contains(t, string(tools[1]["cache_control"]), "ephemeral")

	// First tool should NOT have cache_control.
	_, hasCacheControl := tools[0]["cache_control"]
	assert.False(t, hasCacheControl)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/anthropic/ -run TestToolDefinitionCaching -v`
Expected: FAIL — no cache_control on tools

**Step 3: Write minimal implementation**

In `internal/provider/anthropic/provider.go`, add a `cache_control` field to the API tool struct and set it on the last tool:

```go
// apiTool represents a tool definition for the Anthropic API.
type apiTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *apiCacheControl `json:"cache_control,omitempty"`
}
```

In `buildRequestBody()`, after converting tools to API format, mark the last one:

```go
if len(apiReq.Tools) > 0 {
	apiReq.Tools[len(apiReq.Tools)-1].CacheControl = &apiCacheControl{Type: "ephemeral"}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/anthropic/ -run TestToolDefinitionCaching -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provider/anthropic/provider.go internal/provider/anthropic/cache_test.go
git commit -m "[BEHAVIORAL] Add cache_control to last tool definition for Anthropic prompt caching"
```

---

### Task 13: OpenAI Deterministic Tool Ordering

Sort tool definitions alphabetically before serialization to maximize auto-cache hits.

**Files:**
- Modify: `internal/provider/openai/provider.go` (buildRequestBody, sort tools)
- Test: `internal/provider/openai/provider_test.go`

**Step 1: Write the failing test**

```go
func TestToolDefinitionsSortedAlphabetically(t *testing.T) {
	p := &Provider{apiKey: "test-key", baseURL: "http://test"}

	req := provider.CompletionRequest{
		Model:  "gpt-4",
		System: "You are helpful.",
		Tools: []provider.ToolDef{
			{Name: "shell", Description: "Run commands", InputSchema: json.RawMessage(`{}`)},
			{Name: "file", Description: "Read files", InputSchema: json.RawMessage(`{}`)},
			{Name: "search", Description: "Search code", InputSchema: json.RawMessage(`{}`)},
		},
	}

	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	var parsed struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Len(t, parsed.Tools, 3)

	// Tools should be sorted: file, search, shell
	assert.Equal(t, "file", parsed.Tools[0].Function.Name)
	assert.Equal(t, "search", parsed.Tools[1].Function.Name)
	assert.Equal(t, "shell", parsed.Tools[2].Function.Name)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/openai/ -run TestToolDefinitionsSorted -v`
Expected: FAIL — tools in original order (shell, file, search)

**Step 3: Write minimal implementation**

In `internal/provider/openai/provider.go`, in `buildRequestBody()`, sort tools before serialization:

```go
import "sort"

// Before serializing tools, sort alphabetically for cache-friendly ordering.
sort.Slice(apiReq.Tools, func(i, j int) bool {
	return apiReq.Tools[i].Function.Name < apiReq.Tools[j].Function.Name
})
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/openai/ -run TestToolDefinitionsSorted -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provider/openai/provider.go internal/provider/openai/provider_test.go
git commit -m "[BEHAVIORAL] Sort OpenAI tool definitions alphabetically for cache optimization"
```

---

### Task 14: Ollama Keep-Alive Configuration

Add `keep_alive` parameter to Ollama chat requests and config support.

**Files:**
- Modify: `internal/provider/ollama/provider.go:39-45` (apiRequest struct)
- Modify: `internal/config/config.go` (add CacheConfig)
- Test: `internal/provider/ollama/provider_test.go`, `internal/config/config_test.go`

**Step 1: Write the failing tests**

In `internal/provider/ollama/provider_test.go`:

```go
func TestKeepAliveInRequest(t *testing.T) {
	p := &Provider{baseURL: "http://localhost:11434", keepAlive: "10m"}

	req := provider.CompletionRequest{
		Model:  "llama3",
		System: "You are helpful.",
	}

	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.Equal(t, "10m", parsed["keep_alive"])
}

func TestKeepAliveDefault(t *testing.T) {
	p := &Provider{baseURL: "http://localhost:11434"}

	req := provider.CompletionRequest{Model: "llama3"}
	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	// Default: "5m"
	assert.Equal(t, "5m", parsed["keep_alive"])
}
```

In `internal/config/config_test.go`:

```go
func TestConfigCacheSection(t *testing.T) {
	tomlData := `
[provider]
default = "ollama"
model = "llama3"

[agent.cache]
ollama_keep_alive = "15m"
`
	cfg, err := ParseTOML([]byte(tomlData))
	require.NoError(t, err)
	assert.Equal(t, "15m", cfg.Agent.Cache.OllamaKeepAlive)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ollama/ -run TestKeepAlive -v && go test ./internal/config/ -run TestConfigCacheSection -v`
Expected: FAIL

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add:

```go
type CacheConfig struct {
	OllamaKeepAlive string `toml:"ollama_keep_alive"`
}

// Add to AgentConfig struct:
Cache CacheConfig `toml:"cache"`
```

In `internal/provider/ollama/provider.go`:

Add `keepAlive string` field to `Provider` struct.

Add `KeepAlive string` to `apiRequest` struct:

```go
KeepAlive string `json:"keep_alive,omitempty"`
```

In `buildRequestBody()`, set the keep_alive value:

```go
keepAlive := p.keepAlive
if keepAlive == "" {
	keepAlive = "5m"
}
apiReq.KeepAlive = keepAlive
```

Wire the config value when constructing the Ollama provider (in provider factory or constructor).

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/provider/ollama/ -run TestKeepAlive -v && go test ./internal/config/ -run TestConfigCacheSection -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provider/ollama/provider.go internal/provider/ollama/provider_test.go internal/config/config.go internal/config/config_test.go
git commit -m "[BEHAVIORAL] Add Ollama keep_alive config for model caching"
```

---

### Task 15: SkillBackend Agents() Extension and Runtime Wiring

Add `Agents()` to SkillBackend interface. Wire agent def registration/unregistration in Runtime.Activate/Deactivate.

**Files:**
- Modify: `internal/skills/types.go:146-164` (SkillBackend interface, add Agents())
- Modify: `internal/skills/runtime.go:44-60` (Runtime struct, add agentDefRegistry)
- Modify: `internal/skills/runtime.go:167-305` (Activate, register agent defs)
- Modify: `internal/skills/runtime.go:309-364` (Deactivate, unregister agent defs)
- Modify: All SkillBackend implementations (add `Agents()` returning nil)
- Test: `internal/skills/runtime_test.go`

**Step 1: Write the failing test**

```go
func TestRuntimeActivateRegistersAgentDefs(t *testing.T) {
	// Setup runtime with mock backend that provides agent defs.
	// ... (follow existing TestRuntimeActivateRegistersCommands pattern)

	// Assert that agent defs are registered in the AgentDefRegistry.
}
```

Refer to `internal/skills/runtime_test.go` for the existing test patterns (look at `TestRuntimeActivateRegistersCommands`).

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestRuntimeActivateRegistersAgentDefs -v`
Expected: FAIL

**Step 3: Write minimal implementation**

In `internal/skills/types.go`, add to `SkillBackend` interface:

```go
// Agents returns agent definitions contributed by this skill.
Agents() []*agent.AgentDef
```

In `internal/skills/runtime.go`:

Add `agentDefRegistry *agent.AgentDefRegistry` field to `Runtime` struct.

Add `SetAgentDefRegistry(reg *agent.AgentDefRegistry)` method.

In `Activate()`, after command registration (around line 275):

```go
// Register agent definitions from backend.
var registeredAgentDefs []string
if rt.agentDefRegistry != nil {
	for _, def := range backend.Agents() {
		if err := rt.agentDefRegistry.Register(def); err != nil {
			// Roll back agent defs.
			for _, name := range registeredAgentDefs {
				_ = rt.agentDefRegistry.Unregister(name)
			}
			// Roll back commands and tools too.
			for _, c := range registeredCmds {
				_ = rt.cmdRegistry.Unregister(c.Name())
			}
			for _, t := range registeredTools {
				_ = rt.registry.Unregister(t.Name())
			}
			_ = sk.TransitionTo(SkillStateError)
			_ = sk.TransitionTo(SkillStateInactive)
			return fmt.Errorf("register agent def for skill %q: %w", name, err)
		}
		registeredAgentDefs = append(registeredAgentDefs, def.Name)
	}
}
```

In `Deactivate()`, after command unregistration (around line 330):

```go
// Unregister agent definitions.
if rt.agentDefRegistry != nil && sk.Backend != nil {
	for _, def := range sk.Backend.Agents() {
		_ = rt.agentDefRegistry.Unregister(def.Name)
	}
}
```

Update all SkillBackend implementations to add:

```go
func (b *SomeBackend) Agents() []*agent.AgentDef { return nil }
```

Files to update (same list as Commands() — refer to Task 7 from the previous PR):
- `internal/skills/starlark/engine.go`
- `internal/skills/goplugin/goplugin.go`
- `internal/skills/process/manager.go`
- `internal/skills/mcpbackend/backend.go`
- `internal/skills/builtin/wiki_skill.go`
- `internal/skills/builtin/core_tools.go`
- `internal/skills/builtin/git.go`
- `internal/skills/builtin/appledev/backend.go`
- `cmd/rubichan/main.go` (noopPromptBackend)
- Test mocks in `internal/skills/runtime_test.go` and others

**Step 4: Run tests to verify they pass**

Run: `go test ./... 2>&1 | tail -10`
Expected: All PASS

**Step 5: Commit**

```bash
git add -u
git commit -m "[BEHAVIORAL] Add Agents() to SkillBackend and wire in Runtime"
```

---

### Task 16: Startup Wiring in main.go

Wire everything together in `cmd/rubichan/main.go`: create AgentDefRegistry, register config-defined agent defs, create WakeManager, create TaskTool and ListTasksTool, pass them to the agent.

**Files:**
- Modify: `cmd/rubichan/main.go` (agent construction area)

**Step 1: Identify wiring points**

Read `cmd/rubichan/main.go` and find:
1. Where the tool registry is created and tools are registered
2. Where the skill runtime is created
3. Where the agent is constructed
4. Where the approval checker is built

**Step 2: Add wiring code**

After the tool registry is created and before the agent is constructed:

```go
// Create agent definition registry and register config definitions.
agentDefReg := agent.NewAgentDefRegistry()
// Register the built-in "general" agent def.
_ = agentDefReg.Register(&agent.AgentDef{
	Name:        "general",
	Description: "General-purpose agent with all available tools",
})
// Register config-defined agent definitions.
for _, defConf := range cfg.Agent.Definitions {
	_ = agentDefReg.Register(&agent.AgentDef{
		Name:         defConf.Name,
		Description:  defConf.Description,
		SystemPrompt: defConf.SystemPrompt,
		Tools:        defConf.Tools,
		MaxTurns:     defConf.MaxTurns,
		MaxDepth:     defConf.MaxDepth,
		Model:        defConf.Model,
	})
}

// Create wake manager for background subagents.
wakeManager := agent.NewWakeManager()

// Create spawner (provider set after agent creation via SetProvider).
spawner := &agent.DefaultSubagentSpawner{
	Config:      cfg,
	AgentDefs:   agentDefReg,
}

// Register task and list_tasks tools.
taskTool := tools.NewTaskTool(spawner, agentDefReg, 0)
taskTool.SetWakeManager(wakeManager)
registry.Register(taskTool)
registry.Register(tools.NewListTasksTool(wakeManager))

// Pass agent def registry to skill runtime.
rt.SetAgentDefRegistry(agentDefReg)
```

After the agent is created, wire the spawner's remaining dependencies:

```go
spawner.Provider = p
spawner.ParentTools = registry
spawner.ApprovalChecker = approvalChecker
```

Pass wake manager to agent:

```go
a := agent.New(p, registry, approveFunc, cfg,
	// ... existing options ...
	agent.WithWakeManager(wakeManager),
)
```

**Step 3: Update glob trust rule construction**

Where trust rules are built (look for `NewTrustRuleChecker`), update to pass glob rules:

```go
// Extract glob rules from config.
var globRules []agent.GlobTrustRule
var regexRules []agent.TrustRule
for _, r := range cfg.Agent.TrustRules {
	if r.Glob != "" {
		globRules = append(globRules, agent.GlobTrustRule{Glob: r.Glob, Action: r.Action})
	} else {
		regexRules = append(regexRules, agent.TrustRule{Tool: r.Tool, Pattern: r.Pattern, Action: r.Action})
	}
}
trustChecker := agent.NewTrustRuleChecker(regexRules, globRules)
```

**Step 4: Run all tests and build**

Run: `go build ./cmd/rubichan && go test ./... 2>&1 | tail -10`
Expected: Build succeeds, all tests PASS

**Step 5: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Wire subagent system, wake manager, and glob rules in main.go"
```

---

### Task 17: Update Manifest for Skill-Contributed Agent Defs

Add `AgentDef` fields to `SkillManifest` for skills to declare agent definitions in YAML.

**Files:**
- Modify: `internal/skills/manifest.go:94-113` (SkillManifest struct)
- Test: `internal/skills/manifest_test.go`

**Step 1: Write the failing test**

```go
func TestManifestWithAgentDefs(t *testing.T) {
	yamlData := `
name: k8s-tools
version: "1.0"
description: "K8s debugging tools"
types: [tool]
agents:
  - name: k8s-debugger
    description: "Debug Kubernetes issues"
    system_prompt: "You specialize in K8s troubleshooting."
    tools: ["shell", "file"]
    max_turns: 8
`
	var manifest SkillManifest
	err := yaml.Unmarshal([]byte(yamlData), &manifest)
	require.NoError(t, err)
	require.Len(t, manifest.Agents, 1)
	assert.Equal(t, "k8s-debugger", manifest.Agents[0].Name)
	assert.Equal(t, []string{"shell", "file"}, manifest.Agents[0].Tools)
	assert.Equal(t, 8, manifest.Agents[0].MaxTurns)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestManifestWithAgentDefs -v`
Expected: FAIL — `Agents` field not found on SkillManifest

**Step 3: Write minimal implementation**

In `internal/skills/manifest.go`, add to `SkillManifest`:

```go
type AgentDefManifest struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`
	MaxTurns     int      `yaml:"max_turns"`
	MaxDepth     int      `yaml:"max_depth"`
	Model        string   `yaml:"model"`
}

// Add to SkillManifest struct:
Agents []AgentDefManifest `yaml:"agents"`
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run TestManifestWithAgentDefs -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/skills/manifest.go internal/skills/manifest_test.go
git commit -m "[BEHAVIORAL] Add agent definitions to skill manifest YAML"
```

---

### Task 18: Integration Test — End-to-End Subagent Spawn

Write an integration test that creates a parent agent, spawns a subagent via TaskTool with a mock provider, and verifies the result flows back.

**Files:**
- Create: `internal/agent/subagent_integration_test.go`

**Step 1: Write the integration test**

```go
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
	ch <- provider.StreamEvent{Type: "text", Text: "Echo: " + text}
	ch <- provider.StreamEvent{Type: "done", InputTokens: 10, OutputTokens: 5}
	close(ch)
	return ch, nil
}

func TestSubagentIntegration(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{MaxTurns: 3},
		Provider: config.ProviderConfig{Model: "echo"},
	}

	reg := tools.NewRegistry()
	ep := &echoProvider{}

	defReg := agent.NewAgentDefRegistry()
	_ = defReg.Register(&agent.AgentDef{
		Name:        "general",
		Description: "General agent",
	})

	spawner := &agent.DefaultSubagentSpawner{
		Provider:    ep,
		ParentTools: reg,
		Config:      cfg,
		AgentDefs:   defReg,
	}

	taskTool := tools.NewTaskTool(spawner, defReg, 0)
	_ = reg.Register(taskTool)

	// Execute the task tool directly.
	input := json.RawMessage(`{"prompt":"find all test files"}`)
	result, err := taskTool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "Echo: find all test files")
}
```

**Step 2: Run test**

Run: `go test ./internal/agent/ -run TestSubagentIntegration -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/agent/subagent_integration_test.go
git commit -m "[BEHAVIORAL] Add integration test for subagent spawning"
```

---

### Task 19: Final Verification

Run full test suite, verify coverage, check formatting.

**Step 1: Run all tests**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: All packages PASS

**Step 2: Check coverage on new code**

Run: `go test -cover ./internal/agent/ ./internal/tools/ ./internal/config/ ./internal/provider/...`
Expected: >90% on all packages with new code

**Step 3: Check formatting**

Run: `gofmt -l . && go vet ./...`
Expected: No output (clean)

**Step 4: Build**

Run: `go build ./cmd/rubichan`
Expected: Success
