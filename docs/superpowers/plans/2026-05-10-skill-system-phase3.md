# Skill System Improvements — Phase 3: Forked Skill Execution

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add forked skill execution — skills can run in isolated sub-agents with separate context, similar to claude-code's `context: fork`.

**Architecture:** 
- Add `ExecutionMode` field to `SkillManifest` (inline vs fork)
- Add `ForkedSkillExecutor` that wraps skill execution in a sub-agent
- Integrate with existing `DefaultSubagentSpawner` for isolation
- Skills marked with `execution_mode: fork` run in sub-agents, others run inline

**Tech Stack:** Go, existing `internal/skills` and `internal/agent` packages.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/skills/manifest.go` | Add `ExecutionMode` type and field to `SkillManifest` |
| `internal/skills/forked.go` | New: `ForkedSkillExecutor` for sub-agent skill execution |
| `internal/skills/forked_test.go` | Tests for forked execution |
| `internal/skills/runtime.go` | Modify: route forked skills through executor |
| `internal/skills/types.go` | Add `ExecutionModeInline` and `ExecutionModeFork` constants |

---

## Chunk 1: Execution Mode Types

### Task 1: Add ExecutionMode to manifest

**Files:**
- Modify: `internal/skills/manifest.go`
- Modify: `internal/skills/types.go`

**Context:** claude-code skills have `context: inline` (default) or `context: fork` (isolated sub-agent). We want similar semantics.

**Step 1: Write the failing test**

Add to `internal/skills/manifest_test.go`:

```go
func TestParseManifestWithExecutionMode(t *testing.T) {
	yaml := []byte(`
name: forked-skill
version: 1.0.0
description: "Runs in sub-agent"
types:
  - prompt
execution_mode: fork
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	assert.Equal(t, ExecutionModeFork, m.ExecutionMode)
}

func TestParseManifestDefaultExecutionMode(t *testing.T) {
	yaml := []byte(`
name: inline-skill
version: 1.0.0
description: "Runs inline"
types:
  - prompt
`)
	m, err := ParseManifest(yaml)
	require.NoError(t, err)
	assert.Equal(t, ExecutionModeInline, m.ExecutionMode)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestParseManifestWithExecutionMode -v
```

Expected: FAIL — `ExecutionMode` field doesn't exist.

**Step 3: Add ExecutionMode type and field**

Add to `internal/skills/types.go`:

```go
// ExecutionMode determines how a skill is executed.
type ExecutionMode string

const (
	// ExecutionModeInline runs the skill in the main agent context (default).
	ExecutionModeInline ExecutionMode = "inline"
	// ExecutionModeFork runs the skill in an isolated sub-agent.
	ExecutionModeFork ExecutionMode = "fork"
)
```

Add to `internal/skills/manifest.go` in `SkillManifest`:

```go
	ExecutionMode ExecutionMode `yaml:"execution_mode"`
```

Add default handling in `ParseManifest` or `validateManifest`:

```go
if m.ExecutionMode == "" {
	m.ExecutionMode = ExecutionModeInline
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestParseManifestWithExecutionMode -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/types.go internal/skills/manifest.go internal/skills/manifest_test.go
git commit -m "[BEHAVIORAL] Add ExecutionMode field to SkillManifest"
```

---

## Chunk 2: ForkedSkillExecutor

### Task 2: Create executor for forked skills

**Files:**
- Create: `internal/skills/forked.go`
- Create: `internal/skills/forked_test.go`

**Context:** Forked skills need a sub-agent spawner to execute. The executor takes a skill's prompt/instruction and runs it in a sub-agent, returning the result.

**Step 1: Write the failing test**

Create `internal/skills/forked_test.go`:

```go
package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkedSkillExecutorRequiresSpawner(t *testing.T) {
	_, err := NewForkedSkillExecutor(ForkedSkillExecutorConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "spawner required")
}

func TestForkedSkillExecutorInlineSkill(t *testing.T) {
	// Inline skills should not be handled by forked executor.
	cfg := ForkedSkillExecutorConfig{
		Spawner: &mockSubagentSpawner{},
	}
	exec, err := NewForkedSkillExecutor(cfg)
	require.NoError(t, err)

	// Inline skill should return nil, false (not handled).
	result, handled, err := exec.Execute(context.Background(), &Skill{
		Manifest: &SkillManifest{Name: "inline-skill", ExecutionMode: ExecutionModeInline},
	}, "test prompt")
	assert.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, result)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestForkedSkillExecutor -v
```

Expected: FAIL — types don't exist.

**Step 3: Implement ForkedSkillExecutor**

Create `internal/skills/forked.go`:

```go
package skills

import (
	"context"
	"fmt"
)

// SubagentSpawner is the interface for spawning sub-agents.
// This is satisfied by agent.DefaultSubagentSpawner.
type SubagentSpawner interface {
	Spawn(ctx context.Context, cfg SubagentConfig, prompt string) (*SubagentResult, error)
}

// SubagentConfig configures a sub-agent spawn.
type SubagentConfig struct {
	Name       string
	MaxTurns   int
	MaxDepth   int
	Depth      int
	Model      string
	SystemPrompt string
	Tools      []string
	Isolation  string
	ContextBudget int
	InheritSkills bool
	ExtraSkills   []string
	DisableSkills []string
}

// SubagentResult is the output of a sub-agent execution.
type SubagentResult struct {
	Name        string
	Output      string
	Error       error
	InputTokens  int
	OutputTokens int
	ToolsUsed   []string
}

// ForkedSkillExecutor runs forked skills in isolated sub-agents.
type ForkedSkillExecutor struct {
	spawner SubagentSpawner
	defaultMaxTurns int
	defaultMaxDepth int
}

// ForkedSkillExecutorConfig configures the executor.
type ForkedSkillExecutorConfig struct {
	Spawner         SubagentSpawner
	DefaultMaxTurns int
	DefaultMaxDepth int
}

// NewForkedSkillExecutor creates a new executor.
func NewForkedSkillExecutor(cfg ForkedSkillExecutorConfig) (*ForkedSkillExecutor, error) {
	if cfg.Spawner == nil {
		return nil, fmt.Errorf("forked skill executor: spawner required")
	}
	maxTurns := cfg.DefaultMaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}
	maxDepth := cfg.DefaultMaxDepth
	if maxDepth == 0 {
		maxDepth = 3
	}
	return &ForkedSkillExecutor{
		spawner:         cfg.Spawner,
		defaultMaxTurns: maxTurns,
		defaultMaxDepth: maxDepth,
	}, nil
}

// Execute runs a skill in a sub-agent if it's forked. Returns (result, handled, error).
// If the skill is not forked, returns (nil, false, nil) so the caller can handle it inline.
func (e *ForkedSkillExecutor) Execute(ctx context.Context, skill *Skill, prompt string) (*SubagentResult, bool, error) {
	if skill == nil || skill.Manifest == nil {
		return nil, false, nil
	}
	if skill.Manifest.ExecutionMode != ExecutionModeFork {
		return nil, false, nil
	}

	cfg := SubagentConfig{
		Name:       skill.Manifest.Name,
		MaxTurns:   e.defaultMaxTurns,
		MaxDepth:   e.defaultMaxDepth,
		Depth:      1,
		SystemPrompt: skill.InstructionBody,
	}

	result, err := e.spawner.Spawn(ctx, cfg, prompt)
	if err != nil {
		return nil, true, fmt.Errorf("forked skill %q: %w", skill.Manifest.Name, err)
	}

	return result, true, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestForkedSkillExecutor -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/skills/forked.go internal/skills/forked_test.go
git commit -m "[BEHAVIORAL] Add ForkedSkillExecutor for isolated skill execution"
```

---

## Chunk 3: Runtime Integration

### Task 3: Wire forked execution into Runtime

**Files:**
- Modify: `internal/skills/runtime.go`
- Test: `internal/skills/runtime_test.go`

**Context:** When a skill with `execution_mode: fork` is activated, instead of loading its backend inline, we should route its execution through the `ForkedSkillExecutor`.

**Step 1: Write the failing test**

Add to `internal/skills/runtime_test.go`:

```go
func TestRuntimeForkedSkillExecution(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a forked skill.
	skillDir := filepath.Join(userDir, "forked-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	writeSkillYAML(t, skillDir, "forked-skill", `name: forked-skill
version: 1.0.0
description: "A forked skill"
types:
  - prompt
execution_mode: fork
`)

	loader := NewLoader(userDir, projectDir)
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	registry := tools.NewRegistry()
	backendFactory := func(manifest SkillManifest, dir string) (SkillBackend, error) {
		return &mockBackend{}, nil
	}
	sandboxFactory := func(skillName string, declared []Permission) PermissionChecker {
		return &stubPermissionChecker{}
	}

	rt := NewRuntime(loader, s, registry, nil, backendFactory, sandboxFactory)
	require.NoError(t, rt.Discover(nil))

	// The skill should be discovered with fork execution mode.
	sk, ok := rt.skills["forked-skill"]
	require.True(t, ok)
	assert.Equal(t, ExecutionModeFork, sk.Manifest.ExecutionMode)
}
```

**Step 2: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestRuntimeForkedSkillExecution -v
```

Expected: PASS (skill is discovered with correct mode).

**Step 3: Add ForkedSkillExecutor to Runtime**

Add to `Runtime` struct in `runtime.go`:

```go
	forkedExecutor *ForkedSkillExecutor
```

Add method to set executor:

```go
// SetForkedSkillExecutor configures the executor for forked skills.
func (rt *Runtime) SetForkedSkillExecutor(executor *ForkedSkillExecutor) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.forkedExecutor = executor
}
```

**Step 4: Commit**

```bash
git add internal/skills/runtime.go internal/skills/runtime_test.go
git commit -m "[BEHAVIORAL] Integrate ForkedSkillExecutor into Runtime"
```

---

## Validation Commands

```bash
go test ./internal/skills/...
go test -cover ./internal/skills/...
golangci-lint run ./internal/skills/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] Skill system Phase 3: Forked skill execution`

**Body:**
- Add `ExecutionMode` field to `SkillManifest` (`inline` or `fork`)
- Add `ForkedSkillExecutor` that runs forked skills in isolated sub-agents
- Integrate executor into `Runtime` via `SetForkedSkillExecutor`
- Forked skills use sub-agent spawner for isolation
- Inline skills continue to work as before

**Commit prefix:** `[BEHAVIORAL]`
