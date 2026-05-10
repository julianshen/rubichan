# Skill System Improvements — Phase 2: Core Features

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement three core improvements: async skill prefetch, skill token budgeting, and ToolsAllow/ToolsDeny enforcement.

**Architecture:** 
- **Prefetch**: Add `PrefetchHandle` pattern (async, cancellable) that preloads skills before explicit request based on trigger scores
- **Token budgeting**: Cap skill descriptions in system prompt at 1% of context window (~8K chars), truncate non-bundled skills
- **ToolsAllow/ToolsDeny**: Enforce tool name allowlists/denylists in `CapabilityBroker.CheckExecution()`

**Tech Stack:** Go, existing `internal/skills` package.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/skills/prefetch.go` | New: async prefetch handles with states (Pending, Settled, Consumed, Error) |
| `internal/skills/prefetch_test.go` | Tests for prefetch lifecycle and cancellation |
| `internal/skills/broker.go` | Modify: add ToolsAllow/ToolsDeny enforcement to CheckExecution |
| `internal/skills/broker_test.go` | Tests for tool allowlist/denylist enforcement |
| `internal/skills/runtime.go` | Modify: integrate prefetch into EvaluateAndActivate |
| `internal/skills/types.go` | Modify: add SkillPrefetchState enum |
| `internal/agent/prompt.go` | Modify: add skill token budgeting (1% cap) |

---

## Chunk 1: ToolsAllow/ToolsDeny Enforcement

### Task 1: Add tool name filtering to CapabilityBroker

**Files:**
- Modify: `internal/skills/broker.go`
- Test: `internal/skills/broker_test.go`

**Context:** `SkillManifest` already has `ToolsAllow` and `ToolsDeny` string slices, but `CapabilityBroker.CheckExecution()` only checks `Permissions`, not tool names. We need to add tool name filtering.

**Rules:**
- If `ToolsAllow` is non-empty, only allow tools in the list (deny everything else)
- If `ToolsDeny` is non-empty, deny tools in the list (allow everything else)
- `ToolsDeny` takes precedence over `ToolsAllow` (deny wins)
- Both are checked case-insensitively
- Empty lists mean "no restriction" for that dimension

**Step 1: Write the failing test**

Add to `internal/skills/broker_test.go`:

```go
func TestCapabilityBrokerToolsAllow(t *testing.T) {
	checker := &mockPermissionChecker{granted: true}
	
	// Only allow "read" and "write" tools.
	broker := NewCapabilityBroker("test-skill", checker, nil)
	broker.toolsAllow = []string{"read", "write"}
	
	// Allowed tools pass.
	err := broker.CheckExecution(context.Background(), "read", nil)
	assert.NoError(t, err)
	
	err = broker.CheckExecution(context.Background(), "write", nil)
	assert.NoError(t, err)
	
	// Denied tools fail.
	err = broker.CheckExecution(context.Background(), "delete", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool \"delete\" not in allowlist")
}

func TestCapabilityBrokerToolsDeny(t *testing.T) {
	checker := &mockPermissionChecker{granted: true}
	
	// Deny "delete" tool.
	broker := NewCapabilityBroker("test-skill", checker, nil)
	broker.toolsDeny = []string{"delete"}
	
	// Allowed tools pass.
	err := broker.CheckExecution(context.Background(), "read", nil)
	assert.NoError(t, err)
	
	// Denied tool fails.
	err = broker.CheckExecution(context.Background(), "delete", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool \"delete\" in denylist")
}

func TestCapabilityBrokerToolsDenyPrecedence(t *testing.T) {
	checker := &mockPermissionChecker{granted: true}
	
	// Both allow and deny lists present; deny wins.
	broker := NewCapabilityBroker("test-skill", checker, nil)
	broker.toolsAllow = []string{"read", "write", "delete"}
	broker.toolsDeny = []string{"delete"}
	
	// "delete" is in allowlist but also in denylist → denied.
	err := broker.CheckExecution(context.Background(), "delete", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tool \"delete\" in denylist")
}

func TestCapabilityBrokerToolsAllowCaseInsensitive(t *testing.T) {
	checker := &mockPermissionChecker{granted: true}
	
	broker := NewCapabilityBroker("test-skill", checker, nil)
	broker.toolsAllow = []string{"READ", "Write"}
	
	err := broker.CheckExecution(context.Background(), "read", nil)
	assert.NoError(t, err)
	
	err = broker.CheckExecution(context.Background(), "WRITE", nil)
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestCapabilityBrokerTools -v
```

Expected: FAIL — `toolsAllow`/`toolsDeny` fields don't exist on `DefaultCapabilityBroker`.

**Step 3: Modify DefaultCapabilityBroker**

Modify `internal/skills/broker.go`:

```go
// DefaultCapabilityBroker checks all declared permissions via the
// skill's PermissionChecker before each tool execution.
type DefaultCapabilityBroker struct {
	skillName string
	checker   PermissionChecker
	perms     []Permission
	toolsAllow []string
	toolsDeny  []string
}

// NewCapabilityBroker creates a broker that enforces the given
// permissions on every tool call for the named skill.
func NewCapabilityBroker(skillName string, checker PermissionChecker, perms []Permission) *DefaultCapabilityBroker {
	permsCopy := make([]Permission, len(perms))
	copy(permsCopy, perms)
	return &DefaultCapabilityBroker{
		skillName: skillName,
		checker:   checker,
		perms:     permsCopy,
	}
}

// SetToolsAllow sets the list of allowed tool names. If non-empty,
// only tools in this list are permitted.
func (b *DefaultCapabilityBroker) SetToolsAllow(allow []string) {
	b.toolsAllow = make([]string, len(allow))
	copy(b.toolsAllow, allow)
}

// SetToolsDeny sets the list of denied tool names. If non-empty,
// tools in this list are rejected.
func (b *DefaultCapabilityBroker) SetToolsDeny(deny []string) {
	b.toolsDeny = make([]string, len(deny))
	copy(b.toolsDeny, deny)
}

// CheckExecution validates that all declared permissions are still
// granted and that the tool name passes allowlist/denylist checks.
func (b *DefaultCapabilityBroker) CheckExecution(_ context.Context, toolName string, _ json.RawMessage) error {
	// Check tool name against denylist first (deny wins).
	for _, denied := range b.toolsDeny {
		if strings.EqualFold(denied, toolName) {
			return fmt.Errorf("skill %q tool %q: tool in denylist", b.skillName, toolName)
		}
	}
	
	// Check tool name against allowlist.
	if len(b.toolsAllow) > 0 {
		allowed := false
		for _, allowedTool := range b.toolsAllow {
			if strings.EqualFold(allowedTool, toolName) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("skill %q tool %q: tool not in allowlist", b.skillName, toolName)
		}
	}
	
	// Check declared permissions.
	for _, perm := range b.perms {
		if err := b.checker.CheckPermission(perm); err != nil {
			return fmt.Errorf("skill %q tool %q: capability %s denied: %w", b.skillName, toolName, perm, err)
		}
	}
	return nil
}
```

Add `strings` import to `broker.go`.

**Step 4: Wire ToolsAllow/ToolsDeny into Runtime.Activate**

In `internal/skills/runtime.go`, after creating the broker:

```go
broker := NewCapabilityBroker(name, sb, permissions)
if sk.Manifest != nil {
	broker.SetToolsAllow(sk.Manifest.ToolsAllow)
	broker.SetToolsDeny(sk.Manifest.ToolsDeny)
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestCapabilityBrokerTools -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/skills/broker.go internal/skills/broker_test.go internal/skills/runtime.go
git commit -m "[BEHAVIORAL] Enforce ToolsAllow/ToolsDeny in CapabilityBroker"
```

---

## Chunk 2: Async Skill Prefetch

### Task 2: Add PrefetchHandle for async skill loading

**Files:**
- Create: `internal/skills/prefetch.go`
- Create: `internal/skills/prefetch_test.go`
- Modify: `internal/skills/runtime.go`

**Context:** Based on ccgo's `SkillPrefetchHandle` pattern. We want to preload skills that are likely to activate based on trigger scores, before the user explicitly requests them.

**Step 1: Write the failing test**

Create `internal/skills/prefetch_test.go`:

```go
package skills

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefetchHandleLifecycle(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")
	assert.Equal(t, PrefetchStatePending, ph.State())
	
	// Simulate async load.
	go func() {
		time.Sleep(10 * time.Millisecond)
		ph.Settle(&Skill{Manifest: &SkillManifest{Name: "test-skill"}}, nil)
	}()
	
	// Wait for settlement.
	require.Eventually(t, func() bool {
		return ph.State() == PrefetchStateSettled
	}, time.Second, 10*time.Millisecond)
	
	// Consume the settled skill.
	sk, err := ph.Consume()
	require.NoError(t, err)
	assert.Equal(t, "test-skill", sk.Manifest.Name)
	assert.Equal(t, PrefetchStateConsumed, ph.State())
	
	// Second consume returns error.
	_, err = ph.Consume()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already consumed")
}

func TestPrefetchHandleError(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")
	
	ph.Settle(nil, assert.AnError)
	
	assert.Equal(t, PrefetchStateError, ph.State())
	
	_, err := ph.Consume()
	assert.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestPrefetchHandleCancel(t *testing.T) {
	ph := NewPrefetchHandle("test-skill")
	
	ph.Cancel()
	
	assert.Equal(t, PrefetchStateError, ph.State())
	
	_, err := ph.Consume()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/skills/... -run TestPrefetchHandle -v
```

Expected: FAIL — `PrefetchHandle` doesn't exist.

**Step 3: Implement PrefetchHandle**

Create `internal/skills/prefetch.go`:

```go
package skills

import (
	"context"
	"fmt"
	"sync"
)

// PrefetchState represents the lifecycle state of a skill prefetch.
type PrefetchState int

const (
	// PrefetchStatePending means the prefetch has been initiated but not yet completed.
	PrefetchStatePending PrefetchState = iota
	// PrefetchStateSettled means the prefetch completed successfully and the skill is ready.
	PrefetchStateSettled
	// PrefetchStateConsumed means the settled skill has been retrieved by a caller.
	PrefetchStateConsumed
	// PrefetchStateError means the prefetch failed or was cancelled.
	PrefetchStateError
)

// PrefetchHandle tracks the async loading of a single skill.
// It is thread-safe and can be consumed exactly once.
type PrefetchHandle struct {
	skillName string
	state     PrefetchState
	skill     *Skill
	err       error
	mu        sync.Mutex
	done      chan struct{}
}

// NewPrefetchHandle creates a new prefetch handle for the named skill.
func NewPrefetchHandle(skillName string) *PrefetchHandle {
	return &PrefetchHandle{
		skillName: skillName,
		state:     PrefetchStatePending,
		done:      make(chan struct{}),
	}
}

// State returns the current prefetch state.
func (ph *PrefetchHandle) State() PrefetchState {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	return ph.state
}

// Settle marks the prefetch as complete with either a skill or an error.
// This must be called exactly once by the loader goroutine.
func (ph *PrefetchHandle) Settle(skill *Skill, err error) {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	
	if ph.state != PrefetchStatePending {
		return // already settled or cancelled
	}
	
	ph.skill = skill
	ph.err = err
	if err != nil {
		ph.state = PrefetchStateError
	} else {
		ph.state = PrefetchStateSettled
	}
	close(ph.done)
}

// Cancel marks the prefetch as cancelled (error state).
func (ph *PrefetchHandle) Cancel() {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	
	if ph.state != PrefetchStatePending {
		return
	}
	
	ph.state = PrefetchStateError
	ph.err = fmt.Errorf("prefetch for skill %q cancelled", ph.skillName)
	close(ph.done)
}

// Consume retrieves the prefetched skill. Returns an error if the prefetch
// failed, was cancelled, or has already been consumed.
// This can be called multiple times but only the first call succeeds.
func (ph *PrefetchHandle) Consume() (*Skill, error) {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	
	switch ph.state {
	case PrefetchStatePending:
		return nil, fmt.Errorf("prefetch for skill %q still pending", ph.skillName)
	case PrefetchStateConsumed:
		return nil, fmt.Errorf("prefetch for skill %q already consumed", ph.skillName)
	case PrefetchStateError:
		return nil, ph.err
	case PrefetchStateSettled:
		ph.state = PrefetchStateConsumed
		return ph.skill, nil
	default:
		return nil, fmt.Errorf("prefetch for skill %q in unknown state", ph.skillName)
	}
}

// Wait blocks until the prefetch settles (or is cancelled).
func (ph *PrefetchHandle) Wait(ctx context.Context) error {
	select {
	case <-ph.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

**Step 4: Integrate prefetch into Runtime**

Add to `internal/skills/runtime.go`:

```go
// prefetchHandles tracks active prefetches keyed by skill name.
prefetches map[string]*PrefetchHandle
```

Add field initialization in `NewRuntime`:

```go
prefetches: make(map[string]*PrefetchHandle),
```

Add method to start prefetch:

```go
// StartPrefetch initiates async loading of skills that are likely to activate
// based on the given trigger context. Skills with trigger scores above the
// threshold are prefetched.
func (rt *Runtime) StartPrefetch(ctx TriggerContext) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	
	// Build candidates from current skill map.
	var candidates []DiscoveredSkill
	for _, sk := range rt.skills {
		candidates = append(candidates, DiscoveredSkill{
			Manifest: sk.Manifest,
			Dir:      sk.Dir,
			Source:   sk.Source,
			RootDir:  sk.Dir,
		})
	}
	
	reports := EvaluateTriggerReports(candidates, ctx, rt.activationThreshold)
	
	for _, report := range reports {
		if !report.Activated {
			continue
		}
		name := report.Skill.Manifest.Name
		
		// Skip if already active or already prefetching.
		if _, active := rt.active[name]; active {
			continue
		}
		if _, prefetching := rt.prefetches[name]; prefetching {
			continue
		}
		
		// Start prefetch.
		ph := NewPrefetchHandle(name)
		rt.prefetches[name] = ph
		
		go func(skillName string, handle *PrefetchHandle) {
			// Attempt activation; if successful, store the skill.
			if err := rt.Activate(skillName); err != nil {
				handle.Settle(nil, err)
				return
			}
			
			rt.mu.RLock()
			sk, ok := rt.active[skillName]
			rt.mu.RUnlock()
			
			if !ok {
				handle.Settle(nil, fmt.Errorf("skill %q not found after activation", skillName))
				return
			}
			
			handle.Settle(sk, nil)
		}(name, ph)
	}
}

// ConsumePrefetch retrieves a prefetched skill by name. Returns nil if no
// prefetch exists or the prefetch failed.
func (rt *Runtime) ConsumePrefetch(name string) (*Skill, error) {
	rt.mu.Lock()
	ph, ok := rt.prefetches[name]
	delete(rt.prefetches, name)
	rt.mu.Unlock()
	
	if !ok {
		return nil, nil
	}
	
	return ph.Consume()
}

// CancelPrefetches cancels all active prefetches.
func (rt *Runtime) CancelPrefetches() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	
	for _, ph := range rt.prefetches {
		ph.Cancel()
	}
	rt.prefetches = make(map[string]*PrefetchHandle)
}
```

**Step 5: Run test to verify it passes**

```bash
go test ./internal/skills/... -run TestPrefetchHandle -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/skills/prefetch.go internal/skills/prefetch_test.go internal/skills/runtime.go internal/skills/types.go
git commit -m "[BEHAVIORAL] Add async skill prefetch with PrefetchHandle"
```

---

## Chunk 3: Skill Token Budgeting

### Task 3: Cap skill descriptions in system prompt

**Files:**
- Modify: `internal/agent/prompt.go` (or wherever system prompt is built)
- Test: relevant test file

**Context:** ccgo caps skill token budget at 1% of context window (~8K characters). We need to truncate skill descriptions when building the system prompt.

**Step 1: Find where skill indexes are added to system prompt**

Search for `GetSkillIndexes` usage in agent package.

**Step 2: Add token budgeting function**

Add to `internal/skills/types.go` (or a new file):

```go
// SkillTokenBudget caps skill descriptions at a percentage of the context window.
type SkillTokenBudget struct {
	// MaxChars is the maximum total characters for all skill descriptions.
	MaxChars int
}

// DefaultSkillTokenBudget returns a budget of ~1% of a 128K context window.
func DefaultSkillTokenBudget() SkillTokenBudget {
	return SkillTokenBudget{MaxChars: 8192} // 1% of 128K context
}

// BudgetSkillIndexes truncates skill descriptions to fit within the budget.
// Bundled/built-in skills are not truncated. Non-bundled skills are truncated
// proportionally if the total exceeds the budget.
func BudgetSkillIndexes(indexes []SkillIndex, budget SkillTokenBudget) []SkillIndex {
	if budget.MaxChars <= 0 {
		return indexes
	}
	
	// Separate bundled and non-bundled skills.
	var bundled, nonBundled []SkillIndex
	for _, idx := range indexes {
		if idx.Source == SourceBuiltin {
			bundled = append(bundled, idx)
		} else {
			nonBundled = append(nonBundled, idx)
		}
	}
	
	// Calculate total description length for non-bundled skills.
	totalLen := 0
	for _, idx := range nonBundled {
		totalLen += len(idx.Description)
	}
	
	// If within budget, return as-is.
	if totalLen <= budget.MaxChars {
		return indexes
	}
	
	// Truncate non-bundled skills proportionally.
	result := make([]SkillIndex, 0, len(indexes))
	result = append(result, bundled...)
	
	// Allocate budget proportionally.
	budgetPerSkill := budget.MaxChars / len(nonBundled)
	for _, idx := range nonBundled {
		truncated := idx
		if len(truncated.Description) > budgetPerSkill {
			// Leave room for "...".
			maxLen := budgetPerSkill - 3
			if maxLen < 10 {
				maxLen = 10 // minimum meaningful description
			}
			truncated.Description = truncated.Description[:maxLen] + "..."
		}
		result = append(result, truncated)
	}
	
	return result
}
```

**Step 3: Add tests**

```go
func TestBudgetSkillIndexes(t *testing.T) {
	indexes := []SkillIndex{
		{Name: "builtin", Description: "This is a built-in skill with a long description", Source: SourceBuiltin},
		{Name: "user1", Description: "User skill one with a very long description that exceeds budget", Source: SourceUser},
		{Name: "user2", Description: "User skill two with another very long description", Source: SourceUser},
	}
	
	budget := SkillTokenBudget{MaxChars: 50}
	result := BudgetSkillIndexes(indexes, budget)
	
	// Built-in skill should not be truncated.
	assert.Equal(t, indexes[0].Description, result[0].Description)
	
	// Non-bundled skills should be truncated.
	totalLen := 0
	for _, idx := range result[1:] {
		totalLen += len(idx.Description)
	}
	assert.LessOrEqual(t, totalLen, budget.MaxChars)
}

func TestBudgetSkillIndexesWithinBudget(t *testing.T) {
	indexes := []SkillIndex{
		{Name: "skill1", Description: "Short", Source: SourceUser},
		{Name: "skill2", Description: "Also short", Source: SourceUser},
	}
	
	budget := SkillTokenBudget{MaxChars: 100}
	result := BudgetSkillIndexes(indexes, budget)
	
	// Should return unchanged.
	assert.Equal(t, indexes[0].Description, result[0].Description)
	assert.Equal(t, indexes[1].Description, result[1].Description)
}
```

**Step 4: Integrate into system prompt building**

Find where `GetSkillIndexes` is used and add budgeting:

```go
indexes := rt.GetSkillIndexes()
budget := skills.DefaultSkillTokenBudget()
budgeted := skills.BudgetSkillIndexes(indexes, budget)
// Use budgeted instead of indexes for prompt building
```

**Step 5: Run tests**

```bash
go test ./internal/skills/... -run TestBudgetSkillIndexes -v
go test ./internal/agent/... -run TestPrompt -v
```

**Step 6: Commit**

```bash
git add internal/skills/types.go internal/skills/types_test.go internal/agent/prompt.go
git commit -m "[BEHAVIORAL] Add skill token budgeting for system prompt"
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

**Title:** `[BEHAVIORAL] Skill system Phase 2: Prefetch, token budgeting, and tool enforcement`

**Body:**
- Add async skill prefetch with `PrefetchHandle` (Pending → Settled → Consumed → Error states)
- Enforce `ToolsAllow`/`ToolsDeny` in `CapabilityBroker` (deny wins, case-insensitive)
- Add skill token budgeting: cap non-bundled skill descriptions at 1% of context window (~8K chars)
- All changes additive, backward compatible

**Commit prefix:** `[BEHAVIORAL]`
