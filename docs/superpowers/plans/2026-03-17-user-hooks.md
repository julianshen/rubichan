# User Hooks Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add user-configured shell hooks that fire on agent events, integrating into the existing skill hook dispatch system.

**Architecture:** New `internal/hooks/` package with `UserHookRunner` that converts TOML/AGENT.md hook configs into `HookHandler` functions registered at `PriorityUserHook=5` in the existing `LifecycleManager`. Shell commands executed via `exec.CommandContext`. Project hooks gated by trust approval in SQLite.

**Tech Stack:** Go stdlib (`os/exec`, `crypto/sha256`), `gopkg.in/yaml.v3` (AGENT.md frontmatter), existing `internal/skills` hook infrastructure.

**Spec:** `docs/superpowers/specs/2026-03-17-user-hooks-design.md`

---

## File Structure

| File | Package | Responsibility |
|------|---------|---------------|
| `internal/skills/types.go` | `skills` | Add `PriorityUserHook = 5` constant |
| `internal/skills/runtime.go` | `skills` | Add `RegisterHook()` delegation method |
| `internal/hooks/runner.go` | `hooks` | UserHookRunner, RegisterInto, shell exec, template vars |
| `internal/hooks/runner_test.go` | `hooks` | Runner tests |
| `internal/hooks/trust.go` | `hooks` | Trust gate: hash, check, approve |
| `internal/hooks/trust_test.go` | `hooks` | Trust tests |
| `internal/config/config.go` | `config` | Add HooksConfig, HookRuleConfig |
| `internal/config/agentmd.go` | `config` | LoadAgentMDWithHooks (frontmatter parsing) |
| `internal/config/agentmd_test.go` | `config` | Frontmatter tests |
| `internal/store/store.go` | `store` | Add hook_approvals table |
| `internal/agent/agent.go` | `agent` | WithUserHooks option, wiring |
| `cmd/rubichan/main.go` | `main` | Load hooks, create runner, pass to agent |

---

## Chunk 1: Infrastructure

### Task 1: PriorityUserHook constant and Runtime.RegisterHook

**Files:**
- Modify: `internal/skills/types.go`
- Modify: `internal/skills/runtime.go`
- Test: `internal/skills/hooks_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/skills/hooks_test.go`:

```go
func TestUserHookPriorityBetweenBuiltinAndUser(t *testing.T) {
	lm := NewLifecycleManager()

	var order []string
	lm.Register(HookOnConversationStart, "skill-hook", PriorityUser, func(e HookEvent) (HookResult, error) {
		order = append(order, "skill")
		return HookResult{}, nil
	})
	lm.Register(HookOnConversationStart, "user-hook", PriorityUserHook, func(e HookEvent) (HookResult, error) {
		order = append(order, "user-hook")
		return HookResult{}, nil
	})
	lm.Register(HookOnConversationStart, "builtin-hook", PriorityBuiltin, func(e HookEvent) (HookResult, error) {
		order = append(order, "builtin")
		return HookResult{}, nil
	})

	_, err := lm.Dispatch(HookEvent{Phase: HookOnConversationStart})
	require.NoError(t, err)
	assert.Equal(t, []string{"builtin", "user-hook", "skill"}, order)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/skills/ -run TestUserHookPriority -v`
Expected: FAIL — PriorityUserHook not defined

- [ ] **Step 3: Write implementation**

In `internal/skills/types.go` (or `hooks.go` where priorities are defined), add between `PriorityBuiltin` and `PriorityUser`:

```go
// PriorityUserHook is the priority for user-configured shell hooks.
// Fires after built-in hooks but before skill-provided hooks.
PriorityUserHook = 5
```

In `internal/skills/runtime.go`, add a public delegation method:

```go
// RegisterHook adds a hook handler to the lifecycle manager at the given priority.
// This enables external packages (e.g., internal/hooks) to register handlers
// without accessing the unexported lifecycle field directly.
func (rt *Runtime) RegisterHook(phase HookPhase, name string, priority int, handler HookHandler) {
	rt.lifecycle.Register(phase, name, priority, handler)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/skills/ -run TestUserHookPriority -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add PriorityUserHook constant and Runtime.RegisterHook
```

---

### Task 2: HooksConfig in config

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestConfigHooksSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[hooks]
trust_project_hooks = true

[[hooks.rules]]
event = "post_edit"
pattern = "*.go"
command = "gofmt -w {file}"
timeout = "60s"

[[hooks.rules]]
event = "pre_shell"
command = "echo {command}"
`), 0644)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.True(t, cfg.Hooks.TrustProjectHooks)
	require.Len(t, cfg.Hooks.Rules, 2)
	assert.Equal(t, "post_edit", cfg.Hooks.Rules[0].Event)
	assert.Equal(t, "*.go", cfg.Hooks.Rules[0].Pattern)
	assert.Equal(t, "gofmt -w {file}", cfg.Hooks.Rules[0].Command)
	assert.Equal(t, "60s", cfg.Hooks.Rules[0].Timeout)
	assert.Equal(t, "pre_shell", cfg.Hooks.Rules[1].Event)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfigHooksSection -v`
Expected: FAIL — `cfg.Hooks` doesn't exist

- [ ] **Step 3: Write implementation**

Add to `internal/config/config.go`:

```go
// In Config struct:
Hooks HooksConfig `toml:"hooks"`

// New types:
type HooksConfig struct {
	TrustProjectHooks bool             `toml:"trust_project_hooks"`
	Rules             []HookRuleConfig `toml:"rules"`
}

type HookRuleConfig struct {
	Event       string `toml:"event"`
	Pattern     string `toml:"pattern"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
	Timeout     string `toml:"timeout"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfigHooksSection -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add HooksConfig to config for user hook rules
```

---

### Task 3: AGENT.md frontmatter parsing

**Files:**
- Modify: `internal/config/agentmd.go`
- Test: `internal/config/agentmd_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestLoadAgentMDWithHooks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(`---
hooks:
  - event: post_edit
    pattern: "*.go"
    command: "gofmt -w {file}"
  - event: pre_shell
    command: "echo {command}"
---

# Project Instructions
Use Go.
`), 0644)

	body, hooks, err := config.LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Project Instructions")
	assert.NotContains(t, body, "hooks:")
	require.Len(t, hooks, 2)
	assert.Equal(t, "post_edit", hooks[0].Event)
	assert.Equal(t, "*.go", hooks[0].Pattern)
	assert.Equal(t, "gofmt -w {file}", hooks[0].Command)
}

func TestLoadAgentMDWithHooksNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(`# Just markdown
No frontmatter here.
`), 0644)

	body, hooks, err := config.LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Just markdown")
	assert.Empty(t, hooks)
}

func TestLoadAgentMDWithHooksNoFile(t *testing.T) {
	dir := t.TempDir()
	body, hooks, err := config.LoadAgentMDWithHooks(dir)
	require.NoError(t, err)
	assert.Empty(t, body)
	assert.Empty(t, hooks)
}

func TestLoadAgentMDStripsHookFrontmatter(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(`---
hooks:
  - event: post_edit
    command: "test"
---

# Instructions
`), 0644)

	// Original LoadAgentMD should strip frontmatter too
	body, err := config.LoadAgentMD(dir)
	require.NoError(t, err)
	assert.Contains(t, body, "# Instructions")
	assert.NotContains(t, body, "hooks:")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run "TestLoadAgentMDWithHooks|TestLoadAgentMDStrips" -v`
Expected: FAIL — LoadAgentMDWithHooks not defined

- [ ] **Step 3: Write implementation**

Add to `internal/config/agentmd.go`:

```go
import "gopkg.in/yaml.v3"

// agentMDFrontmatter represents the YAML frontmatter in AGENT.md.
type agentMDFrontmatter struct {
	Hooks []HookRuleConfig `yaml:"hooks"`
}

// LoadAgentMDWithHooks loads AGENT.md, parses YAML frontmatter for hooks,
// and returns the markdown body (with frontmatter stripped) and hook configs.
func LoadAgentMDWithHooks(projectRoot string) (string, []HookRuleConfig, error) {
	body, err := loadAgentMDRaw(projectRoot)
	if err != nil || body == "" {
		return body, nil, err
	}

	strippedBody, fm := splitFrontmatter(body)
	if fm == "" {
		return body, nil, nil
	}

	var parsed agentMDFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &parsed); err != nil {
		return strippedBody, nil, nil // malformed frontmatter — return body, no hooks
	}

	return strippedBody, parsed.Hooks, nil
}

// splitFrontmatter splits "---\nyaml\n---\nbody" into (body, yaml).
// Returns (original, "") if no frontmatter found.
func splitFrontmatter(content string) (body, frontmatter string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return content, ""
	}
	// Find closing ---
	rest := content[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		idx = strings.Index(rest, "\n---\r\n")
	}
	if idx == -1 {
		return content, ""
	}
	return strings.TrimLeft(rest[idx+5:], "\r\n"), rest[:idx]
}
```

Update existing `LoadAgentMD` to strip frontmatter:

```go
func LoadAgentMD(projectRoot string) (string, error) {
	body, err := loadAgentMDRaw(projectRoot)
	if err != nil {
		return "", err
	}
	stripped, _ := splitFrontmatter(body)
	return stripped, nil
}
```

Extract the raw file reading into `loadAgentMDRaw` (refactored from current `LoadAgentMD`).

Note: Add `gopkg.in/yaml.v3` to go.mod: `go get gopkg.in/yaml.v3`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestLoadAgentMDWithHooks|TestLoadAgentMDStrips" -v`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```
[BEHAVIORAL] add AGENT.md frontmatter parsing for hook configs
```

---

## Chunk 2: Hook Runner + Trust

### Task 4: UserHookRunner core — shell execution and template substitution

**Files:**
- Create: `internal/hooks/runner.go`
- Create: `internal/hooks/runner_test.go`

- [ ] **Step 1: Write failing tests**

```go
package hooks_test

import (
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerRegistersHandlers(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "session_start", Command: "echo hello", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterInto(lm)

	// Dispatch should invoke the handler
	result, err := lm.Dispatch(skills.HookEvent{Phase: skills.HookOnConversationStart})
	require.NoError(t, err)
	_ = result // handler ran successfully
}

func TestRunnerPreToolBlocksOnFailure(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterInto(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"test.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "pre_tool hook with exit 1 should cancel")
}

func TestRunnerPostToolDoesNotBlock(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_tool", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterInto(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Data:  map[string]any{"tool_name": "file", "content": "ok"},
	})
	require.NoError(t, err)
	// post_tool should not cancel even on exit 1
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestRunnerTemplateSubstitution(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", Command: "test '{tool}' = 'shell'", Timeout: 5 * time.Second},
	}, dir)

	runner.RegisterInto(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"ls"}`},
	})
	require.NoError(t, err)
	// Command should succeed (test 'shell' = 'shell' exits 0)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestRunnerPreEditFiltersByPattern(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Pattern: "*.py", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterInto(lm)

	// .go file should NOT be blocked (pattern is *.py)
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"main.go"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "*.py pattern should not match main.go")
	}
}

func TestRunnerInvalidEventSkipped(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "invalid_event", Command: "echo bad"},
	}, t.TempDir())

	// Should not panic, just skip
	runner.RegisterInto(lm)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hooks/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write implementation**

Create `internal/hooks/runner.go`:

```go
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/skills"
)

const defaultTimeout = 30 * time.Second

// UserHookConfig defines a single user-configured hook.
type UserHookConfig struct {
	Event       string
	Pattern     string        // file glob (optional)
	Command     string        // shell command with {var} templates
	Description string
	Timeout     time.Duration
	Source      string        // "config" or "agent.md"
}

// UserHookRunner converts user hook configs into skill hook handlers.
type UserHookRunner struct {
	hooks   []UserHookConfig
	workDir string
}

// NewUserHookRunner creates a runner for the given hooks.
func NewUserHookRunner(hooks []UserHookConfig, workDir string) *UserHookRunner {
	return &UserHookRunner{hooks: hooks, workDir: workDir}
}

// RegisterInto registers all user hooks as handlers in the skill runtime.
func (r *UserHookRunner) RegisterInto(rt *skills.Runtime) {
	for i, h := range r.hooks {
		phase, isPreEvent, filter := mapEventToPhase(h.Event)
		if phase == 0 {
			log.Printf("user hook: unknown event %q, skipping", h.Event)
			continue
		}

		hookCfg := h // capture for closure
		name := fmt.Sprintf("user-hook-%d-%s", i, h.Event)
		timeout := h.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}

		rt.RegisterHook(phase, name, skills.PriorityUserHook, func(event skills.HookEvent) (skills.HookResult, error) {
			// Apply filter (e.g., pre_edit only for file write/patch)
			if !filter(event, hookCfg.Pattern) {
				return skills.HookResult{}, nil
			}

			// Substitute template variables
			cmd := expandTemplateVars(hookCfg.Command, event)

			// Execute
			ctx, cancel := context.WithTimeout(event.Ctx, timeout)
			defer cancel()

			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Dir = r.workDir
			output, err := c.CombinedOutput()

			if err != nil && isPreEvent {
				log.Printf("user hook %q blocked: %s (output: %s)", hookCfg.Description, err, strings.TrimSpace(string(output)))
				return skills.HookResult{Cancel: true}, nil
			}
			if err != nil {
				log.Printf("user hook %q failed (non-blocking): %s", hookCfg.Description, err)
			}

			return skills.HookResult{}, nil
		})
	}
}

// mapEventToPhase returns the HookPhase, whether it's a pre-event (can block),
// and a filter function. Returns phase=0 for unknown events.
func mapEventToPhase(event string) (skills.HookPhase, bool, func(skills.HookEvent, string) bool) {
	noFilter := func(_ skills.HookEvent, _ string) bool { return true }

	switch event {
	case "pre_tool":
		return skills.HookOnBeforeToolCall, true, noFilter
	case "post_tool":
		return skills.HookOnAfterToolResult, false, noFilter
	case "pre_edit":
		return skills.HookOnBeforeToolCall, true, filterFileWritePatch
	case "post_edit":
		return skills.HookOnAfterToolResult, false, filterFileWritePatch
	case "pre_shell":
		return skills.HookOnBeforeToolCall, true, filterShellTool
	case "session_start":
		return skills.HookOnConversationStart, false, noFilter
	default:
		return 0, false, nil
	}
}

// filterFileWritePatch returns true if the event is a file write/patch matching the glob.
func filterFileWritePatch(event skills.HookEvent, pattern string) bool {
	toolName, _ := event.Data["tool_name"].(string)
	if toolName != "file" {
		return false
	}
	inputStr, _ := event.Data["input"].(string)
	var input struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(inputStr), &input); err != nil {
		return false
	}
	if input.Operation != "write" && input.Operation != "patch" {
		return false
	}
	if pattern == "" {
		return true
	}
	matched, _ := path.Match(pattern, path.Base(input.Path))
	return matched
}

// filterShellTool returns true if the event is a shell tool call.
func filterShellTool(event skills.HookEvent, _ string) bool {
	toolName, _ := event.Data["tool_name"].(string)
	return toolName == "shell"
}

// expandTemplateVars replaces {var} placeholders in the command string.
func expandTemplateVars(cmd string, event skills.HookEvent) string {
	toolName, _ := event.Data["tool_name"].(string)
	inputStr, _ := event.Data["input"].(string)

	// Parse file path and command from input JSON
	var filePath, shellCmd string
	var parsed map[string]any
	if err := json.Unmarshal([]byte(inputStr), &parsed); err == nil {
		if p, ok := parsed["path"].(string); ok {
			filePath = p
		}
		if c, ok := parsed["command"].(string); ok {
			shellCmd = c
		}
	}

	replacer := strings.NewReplacer(
		"{tool}", toolName,
		"{file}", filePath,
		"{command}", shellCmd,
	)
	return replacer.Replace(cmd)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add UserHookRunner with shell execution and template vars
```

---

### Task 5: Trust gate

**Files:**
- Create: `internal/hooks/trust.go`
- Create: `internal/hooks/trust_test.go`
- Modify: `internal/store/store.go` (add hook_approvals table)

- [ ] **Step 1: Write failing tests**

```go
package hooks_test

import (
	"testing"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckTrustNotApproved(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}
	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.False(t, trusted)
}

func TestApproveTrustAndCheck(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}

	err = hooks.ApproveTrust(s, "/project", hks)
	require.NoError(t, err)

	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.True(t, trusted)
}

func TestTrustInvalidatedOnChange(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	hks := []hooks.UserHookConfig{{Event: "post_edit", Command: "gofmt -w {file}"}}
	hooks.ApproveTrust(s, "/project", hks)

	// Change the hook command
	hks[0].Command = "golangci-lint run"
	trusted, err := hooks.CheckTrust(s, "/project", hks)
	require.NoError(t, err)
	assert.False(t, trusted, "trust should be invalidated when hooks change")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/hooks/ -run "TestCheckTrust|TestApproveTrust|TestTrustInvalidated" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write implementation**

Add to `internal/store/store.go` in `createTables()`:

```go
`CREATE TABLE IF NOT EXISTS hook_approvals (
    project_path TEXT NOT NULL,
    hook_hash    TEXT NOT NULL,
    approved_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (project_path, hook_hash)
)`,
```

Add store methods:

```go
func (s *Store) CheckHookApproval(projectPath, hookHash string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM hook_approvals WHERE project_path = ? AND hook_hash = ?`,
		projectPath, hookHash,
	).Scan(&count)
	return count > 0, err
}

func (s *Store) ApproveHook(projectPath, hookHash string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO hook_approvals (project_path, hook_hash) VALUES (?, ?)`,
		projectPath, hookHash,
	)
	return err
}
```

Create `internal/hooks/trust.go`:

```go
package hooks

import (
	"crypto/sha256"
	"fmt"

	"github.com/julianshen/rubichan/internal/store"
)

// HookApprovalStore abstracts the store methods needed for trust checks.
type HookApprovalStore interface {
	CheckHookApproval(projectPath, hookHash string) (bool, error)
	ApproveHook(projectPath, hookHash string) error
}

// CheckTrust returns true if the given hooks have been approved for this project.
func CheckTrust(s HookApprovalStore, projectPath string, hooks []UserHookConfig) (bool, error) {
	hash := computeHookHash(hooks)
	return s.CheckHookApproval(projectPath, hash)
}

// ApproveTrust records approval for the given hooks.
func ApproveTrust(s HookApprovalStore, projectPath string, hooks []UserHookConfig) error {
	hash := computeHookHash(hooks)
	return s.ApproveHook(projectPath, hash)
}

func computeHookHash(hooks []UserHookConfig) string {
	h := sha256.New()
	for _, hk := range hooks {
		fmt.Fprintf(h, "%s:%s\n", hk.Event, hk.Command)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -run "TestCheckTrust|TestApproveTrust|TestTrustInvalidated" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] add hook trust gate with SHA-256 content hashing
```

---

## Chunk 3: Agent + Main Wiring

### Task 6: Agent.WithUserHooks + wiring

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `cmd/rubichan/main.go`

- [ ] **Step 1: Add WithUserHooks option to agent**

```go
import "github.com/julianshen/rubichan/internal/hooks"

// Add to Agent struct:
userHookRunner *hooks.UserHookRunner

// New option:
func WithUserHooks(runner *hooks.UserHookRunner) AgentOption {
	return func(a *Agent) {
		a.userHookRunner = runner
	}
}
```

In `New()`, after skill runtime is created (around line ~410), add:

```go
if a.userHookRunner != nil && a.skillRuntime != nil {
    a.userHookRunner.RegisterInto(a.skillRuntime)
}
```

- [ ] **Step 2: Wire in main.go**

In both interactive and headless setup, after config loading and before agent creation:

```go
import "github.com/julianshen/rubichan/internal/hooks"

// Load user hooks from config
var userHooks []hooks.UserHookConfig
for _, rule := range cfg.Hooks.Rules {
    timeout := 30 * time.Second
    if rule.Timeout != "" {
        if parsed, err := time.ParseDuration(rule.Timeout); err == nil {
            timeout = parsed
        }
    }
    userHooks = append(userHooks, hooks.UserHookConfig{
        Event:       rule.Event,
        Pattern:     rule.Pattern,
        Command:     rule.Command,
        Description: rule.Description,
        Timeout:     timeout,
        Source:      "config",
    })
}

// Load project hooks from AGENT.md frontmatter
_, agentMDHooks, _ := config.LoadAgentMDWithHooks(cwd)
if len(agentMDHooks) > 0 {
    projectHooks := convertHookRules(agentMDHooks, "agent.md")
    if cfg.Hooks.TrustProjectHooks {
        userHooks = append(userHooks, projectHooks...)
    } else {
        trusted, _ := hooks.CheckTrust(s, cwd, projectHooks)
        if trusted {
            userHooks = append(userHooks, projectHooks...)
        } else {
            log.Printf("Project hooks in AGENT.md not trusted — skipping. Use [hooks] trust_project_hooks = true or approve interactively.")
        }
    }
}

if len(userHooks) > 0 {
    opts = append(opts, agent.WithUserHooks(hooks.NewUserHookRunner(userHooks, cwd)))
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: BUILD OK

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```
[BEHAVIORAL] wire user hooks into agent and main.go
```

---

### Task 7: Final integration — tests + lint + coverage

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: ALL PASS

- [ ] **Step 2: Check formatting**

Run: `gofmt -l .`
Expected: No files

- [ ] **Step 3: Check coverage**

Run: `go test -cover ./internal/hooks/`
Expected: >90%

- [ ] **Step 4: Commit any fixes**

```
[STRUCTURAL] fix lint and formatting
```
