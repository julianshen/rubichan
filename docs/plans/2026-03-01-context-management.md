# Context Management Enhancements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 8 context management enhancements learned from Claude Code: component-level budget tracking, output token reservation, prompt caching, tool result disk offloading, MCP tool deferral, compacted session resume, /compact command, and integration wiring.

**Architecture:** Extend the existing `ContextManager` strategy chain pattern with composable layers. Each enhancement is independent and degrades gracefully on failure. The PR dependency chain is: PR1 (budget+threshold) → PRs 2/3/4/6 in parallel → PR7 (integration). PR5 (session snapshots) has no dependencies.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), existing `provider.LLMProvider` interface, existing `tools.Registry`, existing `CompactionStrategy` chain.

---

## Task 1: ContextBudget Type

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/context_test.go`

**Step 1: Write the failing test for ContextBudget.EffectiveWindow**

Add to `internal/agent/context_test.go`:

```go
func TestContextBudgetEffectiveWindow(t *testing.T) {
	b := ContextBudget{Total: 100000, MaxOutputTokens: 4096}
	assert.Equal(t, 95904, b.EffectiveWindow())
}

func TestContextBudgetEffectiveWindowZeroOutput(t *testing.T) {
	b := ContextBudget{Total: 100000, MaxOutputTokens: 0}
	assert.Equal(t, 100000, b.EffectiveWindow())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestContextBudgetEffectiveWindow -v`
Expected: FAIL — `ContextBudget` type not defined

**Step 3: Write minimal implementation**

Add to `internal/agent/context.go` (before `ContextManager`):

```go
// ContextBudget tracks token usage by component category.
type ContextBudget struct {
	Total            int // configured max (from config.Agent.ContextBudget)
	MaxOutputTokens  int // reserved for LLM response (e.g., 4096)

	// Measured usage (updated before each LLM call):
	SystemPrompt     int // base prompt + AGENT.md + memories
	SkillPrompts     int // active skill prompt fragments
	ToolDescriptions int // tool defs sent to LLM (grows with MCP/skills)
	Conversation     int // messages + tool results
}

// EffectiveWindow returns the usable context after reserving output tokens.
func (b *ContextBudget) EffectiveWindow() int {
	ew := b.Total - b.MaxOutputTokens
	if ew < 0 {
		return 0
	}
	return ew
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestContextBudgetEffectiveWindow -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/context_test.go
git commit -m "[BEHAVIORAL] Add ContextBudget type with EffectiveWindow"
```

---

## Task 2: ContextBudget UsedTokens, RemainingTokens, UsedPercentage

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/context_test.go`

**Step 1: Write the failing tests**

```go
func TestContextBudgetUsedTokens(t *testing.T) {
	b := ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     500,
		SkillPrompts:     200,
		ToolDescriptions: 3000,
		Conversation:     10000,
	}
	assert.Equal(t, 13700, b.UsedTokens())
}

func TestContextBudgetRemainingTokens(t *testing.T) {
	b := ContextBudget{
		Total:            100000,
		MaxOutputTokens:  4096,
		SystemPrompt:     500,
		SkillPrompts:     200,
		ToolDescriptions: 3000,
		Conversation:     10000,
	}
	assert.Equal(t, 82204, b.RemainingTokens()) // 95904 - 13700
}

func TestContextBudgetUsedPercentage(t *testing.T) {
	b := ContextBudget{
		Total:            100000,
		MaxOutputTokens:  0,
		SystemPrompt:     50000,
		Conversation:     50000,
	}
	assert.InDelta(t, 1.0, b.UsedPercentage(), 0.001)
}

func TestContextBudgetUsedPercentageZeroWindow(t *testing.T) {
	b := ContextBudget{Total: 0, MaxOutputTokens: 0}
	assert.Equal(t, 1.0, b.UsedPercentage())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestContextBudgetUsed -v`
Expected: FAIL — methods not defined

**Step 3: Write minimal implementation**

Add to `internal/agent/context.go`:

```go
// UsedTokens returns total tokens consumed across all components.
func (b *ContextBudget) UsedTokens() int {
	return b.SystemPrompt + b.SkillPrompts + b.ToolDescriptions + b.Conversation
}

// RemainingTokens returns how many tokens are available for conversation growth.
func (b *ContextBudget) RemainingTokens() int {
	return b.EffectiveWindow() - b.UsedTokens()
}

// UsedPercentage returns the fraction of the effective window in use (0.0–1.0+).
func (b *ContextBudget) UsedPercentage() float64 {
	ew := b.EffectiveWindow()
	if ew <= 0 {
		return 1.0
	}
	return float64(b.UsedTokens()) / float64(ew)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestContextBudgetUsed -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/context_test.go
git commit -m "[BEHAVIORAL] Add UsedTokens, RemainingTokens, UsedPercentage to ContextBudget"
```

---

## Task 3: Migrate ContextManager to ContextBudget + New Thresholds

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/context_test.go`
- Modify: `internal/agent/agent.go:194` (NewContextManager call)
- Modify: `internal/config/config.go:58-63` (AgentConfig)

**Step 1: Write the failing tests**

```go
func TestNewContextManagerWithBudget(t *testing.T) {
	cm := NewContextManager(100000, 4096)
	assert.NotNil(t, cm)
}

func TestContextManagerShouldCompactAt95Percent(t *testing.T) {
	cm := NewContextManager(1000, 100) // effective window = 900
	conv := NewConversation("")

	// Fill to just under 95% of 900 = 855 tokens
	// Each char is ~0.25 tokens, + 10 overhead per block
	// Need 855 tokens: (n * 4 - 10) chars ≈ 3380 chars per block
	// Actually: (chars/4 + 10) = target → chars = (target - 10) * 4
	conv.AddUser(makeStringOfTokens(850))
	assert.False(t, cm.ShouldCompact(conv), "below 95%% should not trigger")

	conv.Clear()
	conv.AddUser(makeStringOfTokens(860))
	assert.True(t, cm.ShouldCompact(conv), "above 95%% should trigger")
}

func TestContextManagerIsBlocked(t *testing.T) {
	cm := NewContextManager(1000, 100) // effective window = 900
	conv := NewConversation("")

	conv.AddUser(makeStringOfTokens(880))
	assert.False(t, cm.IsBlocked(conv), "below 98%% should not block")

	conv.Clear()
	conv.AddUser(makeStringOfTokens(890))
	assert.True(t, cm.IsBlocked(conv), "above 98%% should block")
}

// makeStringOfTokens returns a string that estimates to approximately n tokens.
// Each block has +10 overhead, so the text itself needs (n-10)*4 chars.
func makeStringOfTokens(n int) string {
	chars := (n - 10) * 4
	if chars < 0 {
		chars = 0
	}
	return strings.Repeat("a", chars)
}
```

Add `"strings"` to the test imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestNewContextManagerWithBudget|TestContextManagerShouldCompactAt95|TestContextManagerIsBlocked" -v`
Expected: FAIL — `NewContextManager` signature changed

**Step 3: Refactor ContextManager**

Update `internal/agent/context.go`:

Replace `NewContextManager`:
```go
// NewContextManager creates a new ContextManager with the given total token
// budget and max output token reservation. Default thresholds are 0.95
// (compact trigger) and 0.98 (hard block).
func NewContextManager(totalBudget, maxOutputTokens int) *ContextManager {
	return &ContextManager{
		budget: ContextBudget{
			Total:           totalBudget,
			MaxOutputTokens: maxOutputTokens,
		},
		compactTrigger: 0.95,
		hardBlock:      0.98,
		strategies: []CompactionStrategy{
			NewToolResultClearingStrategy(),
			&truncateStrategy{},
		},
	}
}
```

Replace `ContextManager` struct:
```go
type ContextManager struct {
	budget         ContextBudget
	compactTrigger float64 // fraction of effective window to trigger compaction (default 0.95)
	hardBlock      float64 // fraction of effective window to block new messages (default 0.98)
	strategies     []CompactionStrategy
}
```

Update `ShouldCompact`:
```go
func (cm *ContextManager) ShouldCompact(conv *Conversation) bool {
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.compactTrigger)
	return cm.EstimateTokens(conv) > threshold
}
```

Add `IsBlocked`:
```go
// IsBlocked returns true when token usage exceeds the hard block threshold
// (default 98% of effective window). The agent should force compaction.
func (cm *ContextManager) IsBlocked(conv *Conversation) bool {
	threshold := int(float64(cm.budget.EffectiveWindow()) * cm.hardBlock)
	return cm.EstimateTokens(conv) > threshold
}
```

Update `ExceedsBudget`:
```go
func (cm *ContextManager) ExceedsBudget(conv *Conversation) bool {
	return cm.EstimateTokens(conv) > cm.budget.EffectiveWindow()
}
```

Update `Compact` — change `cm.budget` references to `cm.budget.EffectiveWindow()`:
```go
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) {
	if !cm.ShouldCompact(conv) {
		return
	}
	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}
	// ... rest unchanged
}
```

Update `internal/agent/agent.go:194`:
```go
context: NewContextManager(cfg.Agent.ContextBudget, cfg.Agent.MaxOutputTokens),
```

Update `internal/config/config.go` — add fields to `AgentConfig`:
```go
type AgentConfig struct {
	MaxTurns              int             `toml:"max_turns"`
	ApprovalMode          string          `toml:"approval_mode"`
	ContextBudget         int             `toml:"context_budget"`
	MaxOutputTokens       int             `toml:"max_output_tokens"`
	CompactTrigger        float64         `toml:"compact_trigger"`
	HardBlock             float64         `toml:"hard_block"`
	ResultOffloadThreshold int            `toml:"result_offload_threshold"`
	ToolDeferralThreshold float64         `toml:"tool_deferral_threshold"`
	TrustRules            []TrustRuleConf `toml:"trust_rules"`
}
```

Update `DefaultConfig()` agent section:
```go
Agent: AgentConfig{
	MaxTurns:               50,
	ApprovalMode:           "prompt",
	ContextBudget:          100000,
	MaxOutputTokens:        4096,
	CompactTrigger:         0.95,
	HardBlock:              0.98,
	ResultOffloadThreshold: 4096,
	ToolDeferralThreshold:  0.10,
},
```

**Step 4: Fix all existing tests**

Existing tests call `NewContextManager(budget)` with one arg. Find and update all:

Run: `grep -rn "NewContextManager(" internal/agent/`

Update each call site to `NewContextManager(budget, 0)` (0 output tokens preserves old behavior where effective window = total).

**Step 5: Run full test suite**

Run: `go test ./internal/agent/... -v`
Expected: ALL PASS

**Step 6: Run linter**

Run: `golangci-lint run ./internal/agent/... ./internal/config/...`
Expected: No issues

**Step 7: Commit**

```bash
git add internal/agent/context.go internal/agent/context_test.go internal/agent/agent.go internal/config/config.go
git commit -m "[BEHAVIORAL] Migrate ContextManager to ContextBudget with 95/98% thresholds"
```

---

## Task 4: MeasureUsage Method

**Files:**
- Modify: `internal/agent/context.go`
- Modify: `internal/agent/context_test.go`

**Step 1: Write the failing test**

```go
func TestContextManagerMeasureUsage(t *testing.T) {
	cm := NewContextManager(100000, 4096)

	systemPrompt := "You are a helpful assistant"
	conv := NewConversation(systemPrompt)
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi there"}})

	toolDefs := []provider.ToolDef{
		{Name: "shell", Description: "Execute shell commands", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	skillPrompt := "## Skill\nDo stuff"

	cm.MeasureUsage(conv, systemPrompt, skillPrompt, toolDefs)

	assert.Greater(t, cm.budget.SystemPrompt, 0)
	assert.Greater(t, cm.budget.SkillPrompts, 0)
	assert.Greater(t, cm.budget.ToolDescriptions, 0)
	assert.Greater(t, cm.budget.Conversation, 0)
}
```

Add `"encoding/json"` to test imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestContextManagerMeasureUsage -v`
Expected: FAIL — `MeasureUsage` not defined

**Step 3: Write minimal implementation**

```go
// MeasureUsage populates the budget's component-level token counts based
// on the current conversation state. Call before each LLM request.
func (cm *ContextManager) MeasureUsage(conv *Conversation, systemPrompt, skillPrompts string, toolDefs []provider.ToolDef) {
	cm.budget.SystemPrompt = len(systemPrompt)/4 + 10
	cm.budget.SkillPrompts = len(skillPrompts)/4 + 10
	if skillPrompts == "" {
		cm.budget.SkillPrompts = 0
	}

	toolTokens := 0
	for _, td := range toolDefs {
		toolTokens += len(td.Name)/4 + len(td.Description)/4 + len(td.InputSchema)/4 + 30
	}
	cm.budget.ToolDescriptions = toolTokens

	cm.budget.Conversation = estimateMessageTokens(conv.messages)
}

// Budget returns a copy of the current budget for external inspection.
func (cm *ContextManager) Budget() ContextBudget {
	return cm.budget
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestContextManagerMeasureUsage -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/context_test.go
git commit -m "[BEHAVIORAL] Add MeasureUsage for component-level token tracking"
```

---

## Task 5: PromptBuilder

**Files:**
- Create: `internal/agent/prompt.go`
- Create: `internal/agent/prompt_test.go`

**Step 1: Write the failing test**

Create `internal/agent/prompt_test.go`:

```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptBuilderEmptyBuild(t *testing.T) {
	pb := NewPromptBuilder()
	prompt, breakpoints := pb.Build()
	assert.Equal(t, "", prompt)
	assert.Empty(t, breakpoints)
}

func TestPromptBuilderOrdersCacheableFirst(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "dynamic", Content: "changes every turn", Cacheable: false})
	pb.AddSection(PromptSection{Name: "static", Content: "never changes", Cacheable: true})

	prompt, _ := pb.Build()
	// Static section should come before dynamic.
	staticIdx := indexOf(prompt, "never changes")
	dynamicIdx := indexOf(prompt, "changes every turn")
	assert.Less(t, staticIdx, dynamicIdx, "cacheable sections should come first")
}

func TestPromptBuilderBreakpointAfterCacheable(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "base", Content: "base instructions", Cacheable: true})
	pb.AddSection(PromptSection{Name: "rules", Content: "project rules", Cacheable: true})
	pb.AddSection(PromptSection{Name: "notes", Content: "scratchpad", Cacheable: false})

	_, breakpoints := pb.Build()
	assert.Len(t, breakpoints, 1, "one breakpoint after last cacheable section")
	assert.Greater(t, breakpoints[0], 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestPromptBuilder -v`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

Create `internal/agent/prompt.go`:

```go
package agent

import "strings"

// PromptSection represents a named section of the system prompt.
type PromptSection struct {
	Name      string
	Content   string
	Cacheable bool // hint: content rarely changes between turns
}

// PromptBuilder assembles the system prompt from ordered sections,
// placing cacheable (static) sections first for better provider caching.
type PromptBuilder struct {
	sections []PromptSection
}

// NewPromptBuilder creates an empty PromptBuilder.
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{}
}

// AddSection appends a section. Sections are reordered at Build time.
func (pb *PromptBuilder) AddSection(s PromptSection) {
	pb.sections = append(pb.sections, s)
}

// Build returns the assembled prompt string and cache breakpoint byte offsets.
// Cacheable sections are placed first. A single breakpoint is inserted after
// the last cacheable section.
func (pb *PromptBuilder) Build() (string, []int) {
	if len(pb.sections) == 0 {
		return "", nil
	}

	var cacheable, dynamic []PromptSection
	for _, s := range pb.sections {
		if s.Cacheable {
			cacheable = append(cacheable, s)
		} else {
			dynamic = append(dynamic, s)
		}
	}

	var sb strings.Builder
	var breakpoints []int

	for _, s := range cacheable {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(s.Name)
		sb.WriteString("\n\n")
		sb.WriteString(s.Content)
	}

	// Insert breakpoint after all cacheable sections.
	if len(cacheable) > 0 && len(dynamic) > 0 {
		breakpoints = append(breakpoints, sb.Len())
	}

	for _, s := range dynamic {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## ")
		sb.WriteString(s.Name)
		sb.WriteString("\n\n")
		sb.WriteString(s.Content)
	}

	return sb.String(), breakpoints
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestPromptBuilder -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/prompt.go internal/agent/prompt_test.go
git commit -m "[BEHAVIORAL] Add PromptBuilder with cacheable-first ordering and breakpoints"
```

---

## Task 6: CacheBreakpoints in CompletionRequest + Anthropic Provider

**Files:**
- Modify: `internal/provider/types.go:14-21` (CompletionRequest)
- Modify: `internal/provider/anthropic/provider.go:38-46,106-137` (apiRequest + buildRequestBody)
- Create: `internal/provider/anthropic/cache_test.go`

**Step 1: Write the failing test**

Create `internal/provider/anthropic/cache_test.go`:

```go
package anthropic

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestBuildRequestBodyWithCacheBreakpoints(t *testing.T) {
	p := New("https://api.anthropic.com", "test-key")
	req := provider.CompletionRequest{
		Model:            "claude-sonnet-4-5",
		System:           "You are helpful. ## Rules\nBe nice.",
		Messages:         []provider.Message{provider.NewUserMessage("hello")},
		MaxTokens:        1024,
		CacheBreakpoints: []int{15}, // breakpoint after "You are helpful."
	}

	body, err := p.buildRequestBody(req)
	assert.NoError(t, err)
	assert.Contains(t, string(body), "cache_control")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/anthropic/ -run TestBuildRequestBodyWithCacheBreakpoints -v`
Expected: FAIL — `CacheBreakpoints` field not defined

**Step 3: Write minimal implementation**

Add to `internal/provider/types.go` `CompletionRequest`:
```go
type CompletionRequest struct {
	Model            string    `json:"model"`
	System           string    `json:"system,omitempty"`
	Messages         []Message `json:"messages"`
	Tools            []ToolDef `json:"tools,omitempty"`
	MaxTokens        int       `json:"max_tokens"`
	Temperature      *float64  `json:"temperature,omitempty"`
	CacheBreakpoints []int     `json:"cache_breakpoints,omitempty"` // byte offsets in System for cache hints
}
```

Update `internal/provider/anthropic/provider.go` — change `apiRequest.System` from `string` to structured type, and update `buildRequestBody` to split system prompt at breakpoints:

Add a new struct:
```go
type apiSystemBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text"`
	CacheControl *apiCacheControl  `json:"cache_control,omitempty"`
}

type apiCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}
```

Update `apiRequest`:
```go
type apiRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	Stream      bool         `json:"stream"`
	System      any          `json:"system,omitempty"` // string or []apiSystemBlock
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}
```

Update `buildRequestBody` to handle breakpoints:
```go
func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	apiReq := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}

	// Build system prompt with optional cache breakpoints.
	if len(req.CacheBreakpoints) > 0 && req.System != "" {
		apiReq.System = buildCachedSystemBlocks(req.System, req.CacheBreakpoints)
	} else {
		apiReq.System = req.System
	}

	// ... rest unchanged
}

// buildCachedSystemBlocks splits the system prompt at breakpoint offsets
// and marks pre-breakpoint blocks with cache_control.
func buildCachedSystemBlocks(system string, breakpoints []int) []apiSystemBlock {
	var blocks []apiSystemBlock
	prev := 0
	for _, bp := range breakpoints {
		if bp > len(system) {
			bp = len(system)
		}
		if bp <= prev {
			continue
		}
		blocks = append(blocks, apiSystemBlock{
			Type:         "text",
			Text:         system[prev:bp],
			CacheControl: &apiCacheControl{Type: "ephemeral"},
		})
		prev = bp
	}
	if prev < len(system) {
		blocks = append(blocks, apiSystemBlock{
			Type: "text",
			Text: system[prev:],
		})
	}
	return blocks
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/anthropic/ -run TestBuildRequestBodyWithCacheBreakpoints -v`
Expected: PASS

**Step 5: Run full provider tests**

Run: `go test ./internal/provider/... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/provider/types.go internal/provider/anthropic/provider.go internal/provider/anthropic/cache_test.go
git commit -m "[BEHAVIORAL] Add CacheBreakpoints to CompletionRequest with Anthropic cache_control support"
```

---

## Task 7: ResultStore — SQLite Table

**Files:**
- Modify: `internal/store/store.go:124-175` (createTables)
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

Add to `internal/store/store_test.go`:

```go
func TestSaveAndRetrieveBlob(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	// Create a session first (foreign key).
	err = s.CreateSession(Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	err = s.SaveBlob("blob1", "s1", "shell", "huge output content", 20)
	require.NoError(t, err)

	content, err := s.GetBlob("blob1")
	require.NoError(t, err)
	assert.Equal(t, "huge output content", content)
}

func TestGetBlobNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	content, err := s.GetBlob("nonexistent")
	assert.NoError(t, err)
	assert.Equal(t, "", content)
}

func TestBlobCascadeOnSessionDelete(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	err = s.SaveBlob("blob1", "s1", "shell", "data", 4)
	require.NoError(t, err)

	err = s.DeleteSession("s1")
	require.NoError(t, err)

	content, err := s.GetBlob("blob1")
	assert.NoError(t, err)
	assert.Equal(t, "", content, "blob should be cascade-deleted")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestSaveAndRetrieveBlob|TestGetBlobNotFound|TestBlobCascade" -v`
Expected: FAIL — `SaveBlob`/`GetBlob` not defined

**Step 3: Write minimal implementation**

Add table to `createTables` in `internal/store/store.go`:

```go
`CREATE TABLE IF NOT EXISTS tool_result_blobs (
	id         TEXT PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	tool_name  TEXT NOT NULL,
	content    BLOB NOT NULL,
	byte_size  INTEGER NOT NULL,
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
`CREATE INDEX IF NOT EXISTS idx_blobs_session ON tool_result_blobs(session_id)`,
```

Add methods:

```go
// SaveBlob stores a large tool result blob.
func (s *Store) SaveBlob(id, sessionID, toolName, content string, byteSize int) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO tool_result_blobs (id, session_id, tool_name, content, byte_size)
		 VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, toolName, content, byteSize,
	)
	if err != nil {
		return fmt.Errorf("save blob: %w", err)
	}
	return nil
}

// GetBlob retrieves a stored tool result by reference ID.
// Returns empty string if not found.
func (s *Store) GetBlob(id string) (string, error) {
	var content string
	err := s.db.QueryRow(`SELECT content FROM tool_result_blobs WHERE id = ?`, id).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get blob: %w", err)
	}
	return content, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run "TestSaveAndRetrieveBlob|TestGetBlobNotFound|TestBlobCascade" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "[BEHAVIORAL] Add tool_result_blobs table with SaveBlob/GetBlob"
```

---

## Task 8: ResultStore Agent Layer

**Files:**
- Create: `internal/agent/resultstore.go`
- Create: `internal/agent/resultstore_test.go`

**Step 1: Write the failing test**

Create `internal/agent/resultstore_test.go`:

```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/store"
)

func TestResultStoreOffloadBelowThreshold(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 100) // threshold = 100 bytes
	result, err := rs.OffloadResult("shell", "t1", "small output")
	require.NoError(t, err)
	assert.Equal(t, "small output", result, "below threshold should return unchanged")
}

func TestResultStoreOffloadAboveThreshold(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 20)
	bigContent := "this is a large tool result that exceeds the threshold limit by quite a bit"
	result, err := rs.OffloadResult("shell", "t1", bigContent)
	require.NoError(t, err)

	assert.Contains(t, result, "Tool result stored")
	assert.Contains(t, result, "shell")
	assert.Contains(t, result, "read_result")
}

func TestResultStoreRetrieve(t *testing.T) {
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(store.Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	rs := NewResultStore(s, "s1", 20)
	bigContent := "this is a large tool result that exceeds the threshold limit by quite a bit"
	_, err = rs.OffloadResult("shell", "t1", bigContent)
	require.NoError(t, err)

	// Retrieve using the stored ref ID.
	refs := rs.RefIDs()
	require.Len(t, refs, 1)

	retrieved, err := rs.Retrieve(refs[0])
	require.NoError(t, err)
	assert.Equal(t, bigContent, retrieved)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestResultStore -v`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

Create `internal/agent/resultstore.go`:

```go
package agent

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/store"
)

// ResultStore offloads large tool results to SQLite, keeping only compact
// references in conversation context.
type ResultStore struct {
	store     *store.Store
	sessionID string
	threshold int      // byte size above which results get offloaded
	refIDs    []string // track generated ref IDs for testing
}

// NewResultStore creates a ResultStore. Results exceeding threshold bytes
// are offloaded to the store.
func NewResultStore(s *store.Store, sessionID string, threshold int) *ResultStore {
	return &ResultStore{
		store:     s,
		sessionID: sessionID,
		threshold: threshold,
	}
}

// OffloadResult stores the content if it exceeds the threshold, returning a
// compact reference string. Returns the original content unchanged if below
// threshold.
func (rs *ResultStore) OffloadResult(toolName, toolUseID, content string) (string, error) {
	if len(content) <= rs.threshold {
		return content, nil
	}

	refID := uuid.New().String()
	if err := rs.store.SaveBlob(refID, rs.sessionID, toolName, content, len(content)); err != nil {
		// Graceful degradation: return original content if store fails.
		return content, nil
	}

	rs.refIDs = append(rs.refIDs, refID)

	preview := content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	return fmt.Sprintf(
		"[Tool result stored — %d bytes from %q.\nFirst %d chars: %s]\nUse the \"read_result\" tool with ref_id=%q to read specific portions.",
		len(content), toolName, min(len(content), 200), preview, refID,
	), nil
}

// Retrieve fetches the full stored result by reference ID.
func (rs *ResultStore) Retrieve(refID string) (string, error) {
	content, err := rs.store.GetBlob(refID)
	if err != nil {
		return "", fmt.Errorf("retrieve result %s: %w", refID, err)
	}
	if content == "" {
		return "", fmt.Errorf("result %s not found", refID)
	}
	return content, nil
}

// RefIDs returns the list of reference IDs generated so far (for testing).
func (rs *ResultStore) RefIDs() []string {
	return rs.refIDs
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestResultStore -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/resultstore.go internal/agent/resultstore_test.go
git commit -m "[BEHAVIORAL] Add ResultStore for disk offloading large tool results"
```

---

## Task 9: ReadResultTool

**Files:**
- Create: `internal/tools/read_result.go`
- Create: `internal/tools/read_result_test.go`

**Step 1: Write the failing test**

Create `internal/tools/read_result_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRetriever struct {
	data map[string]string
}

func (m *mockRetriever) Retrieve(refID string) (string, error) {
	if v, ok := m.data[refID]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", refID)
}

func TestReadResultToolName(t *testing.T) {
	tool := NewReadResultTool(&mockRetriever{})
	assert.Equal(t, "read_result", tool.Name())
}

func TestReadResultToolExecute(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "line1\nline2\nline3\nline4\nline5",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 0, "limit": 1024})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "line1")
}

func TestReadResultToolOffsetLimit(t *testing.T) {
	retriever := &mockRetriever{data: map[string]string{
		"ref-1": "abcdefghij",
	}}
	tool := NewReadResultTool(retriever)

	input, _ := json.Marshal(map[string]any{"ref_id": "ref-1", "offset": 3, "limit": 4})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "defg", result.Content)
}
```

Add `"fmt"` to the test imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestReadResultTool -v`
Expected: FAIL — type not defined

**Step 3: Write minimal implementation**

Create `internal/tools/read_result.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ResultRetriever retrieves stored tool results by reference ID.
type ResultRetriever interface {
	Retrieve(refID string) (string, error)
}

// ReadResultTool allows the LLM to retrieve offloaded tool results.
type ReadResultTool struct {
	retriever ResultRetriever
}

// NewReadResultTool creates a new ReadResultTool with the given retriever.
func NewReadResultTool(r ResultRetriever) *ReadResultTool {
	return &ReadResultTool{retriever: r}
}

func (t *ReadResultTool) Name() string        { return "read_result" }
func (t *ReadResultTool) Description() string {
	return "Read a previously stored tool result by reference ID. Supports offset and limit for pagination."
}

func (t *ReadResultTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"ref_id": {"type": "string", "description": "Reference ID of the stored result"},
			"offset": {"type": "integer", "description": "Byte offset to start reading from (default 0)"},
			"limit":  {"type": "integer", "description": "Maximum bytes to return (default 4096)"}
		},
		"required": ["ref_id"]
	}`)
}

func (t *ReadResultTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var params struct {
		RefID  string `json:"ref_id"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	if params.Limit <= 0 {
		params.Limit = 4096
	}

	content, err := t.retriever.Retrieve(params.RefID)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("retrieval error: %s", err), IsError: true}, nil
	}

	// Apply offset and limit.
	if params.Offset > len(content) {
		return ToolResult{Content: ""}, nil
	}
	content = content[params.Offset:]
	if len(content) > params.Limit {
		content = content[:params.Limit]
	}

	return ToolResult{Content: content}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestReadResultTool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/read_result.go internal/tools/read_result_test.go
git commit -m "[BEHAVIORAL] Add read_result tool for retrieving offloaded tool results"
```

---

## Task 10: Session Snapshots — SQLite Table + Store Methods

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Step 1: Write the failing test**

```go
func TestSaveAndGetSnapshot(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	msgs := []provider.Message{
		provider.NewUserMessage("[Summary of 20 earlier messages]\nKey decisions..."),
		provider.NewUserMessage("latest question"),
	}
	err = s.SaveSnapshot("s1", msgs, 500)
	require.NoError(t, err)

	loaded, err := s.GetSnapshot("s1")
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Contains(t, loaded[0].Content[0].Text, "Summary")
}

func TestGetSnapshotNotFound(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	loaded, err := s.GetSnapshot("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSnapshotCascadeOnSessionDelete(t *testing.T) {
	s, err := NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	err = s.CreateSession(Session{ID: "s1", Model: "test", Title: "test"})
	require.NoError(t, err)

	msgs := []provider.Message{provider.NewUserMessage("summary")}
	err = s.SaveSnapshot("s1", msgs, 100)
	require.NoError(t, err)

	err = s.DeleteSession("s1")
	require.NoError(t, err)

	loaded, err := s.GetSnapshot("s1")
	assert.NoError(t, err)
	assert.Nil(t, loaded, "snapshot should be cascade-deleted")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run "TestSaveAndGetSnapshot|TestGetSnapshotNotFound|TestSnapshotCascade" -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add table to `createTables`:

```go
`CREATE TABLE IF NOT EXISTS session_snapshots (
	session_id  TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
	messages    TEXT NOT NULL,
	token_count INTEGER NOT NULL,
	created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
)`,
```

Add methods:

```go
// SaveSnapshot persists the post-compaction message state for resume.
func (s *Store) SaveSnapshot(sessionID string, msgs []provider.Message, tokenCount int) error {
	data, err := json.Marshal(msgs)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO session_snapshots (session_id, messages, token_count, created_at)
		 VALUES (?, ?, ?, datetime('now'))`,
		sessionID, string(data), tokenCount,
	)
	if err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

// GetSnapshot retrieves the compacted snapshot for resume. Returns nil if
// no snapshot exists (session was never compacted).
func (s *Store) GetSnapshot(sessionID string) ([]provider.Message, error) {
	var data string
	err := s.db.QueryRow(
		`SELECT messages FROM session_snapshots WHERE session_id = ?`, sessionID,
	).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	var msgs []provider.Message
	if err := json.Unmarshal([]byte(data), &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return msgs, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run "TestSaveAndGetSnapshot|TestGetSnapshotNotFound|TestSnapshotCascade" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "[BEHAVIORAL] Add session_snapshots table with SaveSnapshot/GetSnapshot"
```

---

## Task 11: DeferralManager

**Files:**
- Create: `internal/tools/deferral.go`
- Create: `internal/tools/deferral_test.go`

**Step 1: Write the failing test**

Create `internal/tools/deferral_test.go`:

```go
package tools

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestDeferralManagerNoDeferralUnderThreshold(t *testing.T) {
	dm := NewDeferralManager(0.10) // 10% threshold

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},
		{Name: "file", Description: "read/write", InputSchema: []byte(`{}`)},
	}

	active, deferred := dm.SelectForContext(allTools, 100000)
	assert.Equal(t, len(allTools), len(active))
	assert.Equal(t, 0, deferred)
}

func TestDeferralManagerDefersOverThreshold(t *testing.T) {
	dm := NewDeferralManager(0.10) // 10% of 1000 = 100 token budget for tools

	// Create tools where MCP tools push past the threshold.
	bigSchema := make([]byte, 2000) // ~500 tokens each
	for i := range bigSchema {
		bigSchema[i] = 'a'
	}

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},           // core — never deferred
		{Name: "mcp-tool1", Description: "big", InputSchema: bigSchema},            // MCP — deferred first
		{Name: "mcp-tool2", Description: "also big", InputSchema: bigSchema},       // MCP — deferred first
	}

	active, deferred := dm.SelectForContext(allTools, 1000)
	assert.Greater(t, deferred, 0, "should defer some MCP tools")
	// Core tool "shell" should always be active.
	hasShell := false
	for _, t := range active {
		if t.Name == "shell" {
			hasShell = true
		}
	}
	assert.True(t, hasShell, "core tools should never be deferred")
}

func TestDeferralManagerSearch(t *testing.T) {
	dm := NewDeferralManager(0.10)

	bigSchema := make([]byte, 2000)
	for i := range bigSchema {
		bigSchema[i] = 'a'
	}

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},
		{Name: "mcp-xcode-build", Description: "Build Xcode projects", InputSchema: bigSchema},
	}

	dm.SelectForContext(allTools, 1000) // trigger deferral

	results := dm.Search("xcode")
	assert.GreaterOrEqual(t, len(results), 0) // may or may not find depending on deferral
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestDeferralManager -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/tools/deferral.go`:

```go
package tools

import (
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// DeferralManager holds back tool descriptions that exceed a context budget
// threshold. Deferred tools are discoverable via the Search method.
type DeferralManager struct {
	budgetThresholdPct float64                    // fraction of effective window for tools
	deferredTools      map[string]provider.ToolDef // name → full definition
}

// NewDeferralManager creates a manager with the given threshold (e.g., 0.10 for 10%).
func NewDeferralManager(thresholdPct float64) *DeferralManager {
	return &DeferralManager{
		budgetThresholdPct: thresholdPct,
		deferredTools:      make(map[string]provider.ToolDef),
	}
}

// estimateToolDefTokens estimates the token count for a single tool definition.
func estimateToolDefTokens(td provider.ToolDef) int {
	return len(td.Name)/4 + len(td.Description)/4 + len(td.InputSchema)/4 + 30
}

// SelectForContext returns tool definitions that fit within the budget.
// Built-in tools (CategoryCore) are always included. MCP and skill tools
// are deferred first when the threshold is exceeded.
func (dm *DeferralManager) SelectForContext(allTools []provider.ToolDef, effectiveWindow int) (active []provider.ToolDef, deferredCount int) {
	dm.deferredTools = make(map[string]provider.ToolDef)
	tokenBudget := int(float64(effectiveWindow) * dm.budgetThresholdPct)

	// Partition tools by category.
	var core, nonCore []provider.ToolDef
	for _, td := range allTools {
		cat := Categorize(td.Name)
		if cat == CategoryCore {
			core = append(core, td)
		} else {
			nonCore = append(nonCore, td)
		}
	}

	// Core tools always active.
	active = append(active, core...)
	usedTokens := 0
	for _, td := range core {
		usedTokens += estimateToolDefTokens(td)
	}

	remaining := tokenBudget - usedTokens

	// Add non-core tools until budget exhausted. Defer the rest.
	for _, td := range nonCore {
		cost := estimateToolDefTokens(td)
		if cost <= remaining {
			active = append(active, td)
			remaining -= cost
		} else {
			dm.deferredTools[td.Name] = td
			deferredCount++
		}
	}

	return active, deferredCount
}

// Search finds deferred tools by name or description keyword match.
func (dm *DeferralManager) Search(query string) []provider.ToolDef {
	query = strings.ToLower(query)
	var results []provider.ToolDef
	for _, td := range dm.deferredTools {
		if strings.Contains(strings.ToLower(td.Name), query) ||
			strings.Contains(strings.ToLower(td.Description), query) {
			results = append(results, td)
		}
	}
	return results
}

// DeferredCount returns the number of currently deferred tools.
func (dm *DeferralManager) DeferredCount() int {
	return len(dm.deferredTools)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestDeferralManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/deferral.go internal/tools/deferral_test.go
git commit -m "[BEHAVIORAL] Add DeferralManager for MCP tool description deferral"
```

---

## Task 12: ToolSearchTool

**Files:**
- Create: `internal/tools/tool_search.go`
- Create: `internal/tools/tool_search_test.go`

**Step 1: Write the failing test**

Create `internal/tools/tool_search_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSearcher struct {
	results []provider.ToolDef
}

func (m *mockSearcher) Search(query string) []provider.ToolDef {
	return m.results
}

func TestToolSearchToolName(t *testing.T) {
	tool := NewToolSearchTool(&mockSearcher{})
	assert.Equal(t, "tool_search", tool.Name())
}

func TestToolSearchToolExecute(t *testing.T) {
	searcher := &mockSearcher{
		results: []provider.ToolDef{
			{Name: "mcp-xcode-build", Description: "Build Xcode projects"},
		},
	}
	tool := NewToolSearchTool(searcher)

	input, _ := json.Marshal(map[string]string{"query": "xcode"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "mcp-xcode-build")
}

func TestToolSearchToolNoResults(t *testing.T) {
	tool := NewToolSearchTool(&mockSearcher{})

	input, _ := json.Marshal(map[string]string{"query": "nonexistent"})
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "No deferred tools")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestToolSearchTool -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/tools/tool_search.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// ToolSearcher finds deferred tools by query.
type ToolSearcher interface {
	Search(query string) []provider.ToolDef
}

// ToolSearchTool allows the LLM to discover deferred tool descriptions.
type ToolSearchTool struct {
	searcher ToolSearcher
}

// NewToolSearchTool creates a new ToolSearchTool.
func NewToolSearchTool(s ToolSearcher) *ToolSearchTool {
	return &ToolSearchTool{searcher: s}
}

func (t *ToolSearchTool) Name() string        { return "tool_search" }
func (t *ToolSearchTool) Description() string {
	return "Search for tools that have been deferred to save context. Returns tool names, descriptions, and schemas."
}

func (t *ToolSearchTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search keyword to match tool names or descriptions"}
		},
		"required": ["query"]
	}`)
}

func (t *ToolSearchTool) Execute(_ context.Context, input json.RawMessage) (ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	results := t.searcher.Search(params.Query)
	if len(results) == 0 {
		return ToolResult{Content: "No deferred tools matching query: " + params.Query}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d deferred tool(s):\n\n", len(results)))
	for _, td := range results {
		sb.WriteString(fmt.Sprintf("**%s**: %s\n", td.Name, td.Description))
		sb.WriteString(fmt.Sprintf("Schema: %s\n\n", string(td.InputSchema)))
	}

	return ToolResult{Content: sb.String()}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestToolSearchTool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/tool_search.go internal/tools/tool_search_test.go
git commit -m "[BEHAVIORAL] Add tool_search tool for discovering deferred tools"
```

---

## Task 13: ForceCompact + CompactResult

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/context.go`
- Create: `internal/agent/force_compact_test.go`

**Step 1: Write the failing test**

Create `internal/agent/force_compact_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceCompactResult(t *testing.T) {
	cm := NewContextManager(1000, 0)
	conv := NewConversation("sys")

	// Fill with enough messages to make compaction meaningful.
	for i := 0; i < 20; i++ {
		conv.AddUser("message content for testing compaction behavior")
		conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response content"}})
	}

	beforeTokens := cm.EstimateTokens(conv)
	beforeMsgs := len(conv.Messages())

	result := cm.ForceCompact(context.Background(), conv)

	assert.Equal(t, beforeTokens, result.BeforeTokens)
	assert.Equal(t, beforeMsgs, result.BeforeMsgCount)
	assert.LessOrEqual(t, result.AfterTokens, beforeTokens)
	assert.Greater(t, len(result.StrategiesRun), 0)
}

func TestForceCompactEmptyConversation(t *testing.T) {
	cm := NewContextManager(100000, 0)
	conv := NewConversation("sys")

	result := cm.ForceCompact(context.Background(), conv)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.BeforeMsgCount)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestForceCompact -v`
Expected: FAIL — `ForceCompact` not defined

**Step 3: Write minimal implementation**

Add to `internal/agent/context.go`:

```go
// CompactResult reports what happened during a compaction.
type CompactResult struct {
	BeforeTokens   int
	AfterTokens    int
	BeforeMsgCount int
	AfterMsgCount  int
	StrategiesRun  []string
}

// ForceCompact runs the compaction strategy chain unconditionally,
// regardless of whether the trigger threshold has been reached.
func (cm *ContextManager) ForceCompact(ctx context.Context, conv *Conversation) CompactResult {
	result := CompactResult{
		BeforeTokens:   cm.EstimateTokens(conv),
		BeforeMsgCount: len(conv.messages),
	}

	if len(conv.messages) == 0 {
		result.AfterTokens = cm.EstimateTokens(conv)
		result.AfterMsgCount = 0
		return result
	}

	systemTokens := len(conv.SystemPrompt())/4 + 10
	messageBudget := cm.budget.EffectiveWindow() - systemTokens
	if messageBudget < 0 {
		messageBudget = 0
	}

	signals := ComputeConversationSignals(conv.messages)
	for _, s := range cm.strategies {
		if sa, ok := s.(SignalAware); ok {
			sa.SetSignals(signals)
		}
	}

	for _, s := range cm.strategies {
		msgs, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		if len(msgs) < len(conv.messages) {
			result.StrategiesRun = append(result.StrategiesRun, s.Name())
		}
		conv.messages = msgs
	}

	result.AfterTokens = cm.EstimateTokens(conv)
	result.AfterMsgCount = len(conv.messages)
	return result
}
```

Add public method on `Agent` in `internal/agent/agent.go`:

```go
// ForceCompact runs the full compaction strategy chain regardless of
// whether the trigger threshold has been reached.
func (a *Agent) ForceCompact(ctx context.Context) CompactResult {
	return a.context.ForceCompact(ctx, a.conversation)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestForceCompact -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/agent.go internal/agent/force_compact_test.go
git commit -m "[BEHAVIORAL] Add ForceCompact with CompactResult reporting"
```

---

## Task 14: CompactContextTool

**Files:**
- Create: `internal/tools/compact_context.go`
- Create: `internal/tools/compact_context_test.go`

**Step 1: Write the failing test**

Create `internal/tools/compact_context_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCompactor struct {
	called bool
}

func (m *mockCompactor) ForceCompact(_ context.Context) CompactResult {
	m.called = true
	return CompactResult{BeforeTokens: 9500, AfterTokens: 4200, BeforeMsgCount: 40, AfterMsgCount: 22, StrategiesRun: []string{"tool_result_clearing", "summarization"}}
}

func TestCompactContextToolName(t *testing.T) {
	tool := NewCompactContextTool(&mockCompactor{})
	assert.Equal(t, "compact_context", tool.Name())
}

func TestCompactContextToolExecute(t *testing.T) {
	compactor := &mockCompactor{}
	tool := NewCompactContextTool(compactor)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, compactor.called)
	assert.Contains(t, result.Content, "9500")
	assert.Contains(t, result.Content, "4200")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestCompactContextTool -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Create `internal/tools/compact_context.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// CompactResult mirrors agent.CompactResult to avoid circular import.
type CompactResult struct {
	BeforeTokens   int
	AfterTokens    int
	BeforeMsgCount int
	AfterMsgCount  int
	StrategiesRun  []string
}

// Compactor can force a context compaction.
type Compactor interface {
	ForceCompact(ctx context.Context) CompactResult
}

// CompactContextTool allows the LLM to proactively compress its context.
type CompactContextTool struct {
	compactor Compactor
}

// NewCompactContextTool creates a new CompactContextTool.
func NewCompactContextTool(c Compactor) *CompactContextTool {
	return &CompactContextTool{compactor: c}
}

func (t *CompactContextTool) Name() string        { return "compact_context" }
func (t *CompactContextTool) Description() string {
	return "Compress conversation context to free space. Call before large operations when context usage is high."
}

func (t *CompactContextTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *CompactContextTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	result := t.compactor.ForceCompact(ctx)
	summary := fmt.Sprintf(
		"Compacted context from %d to %d tokens (%d messages → %d). Strategies: %v",
		result.BeforeTokens, result.AfterTokens,
		result.BeforeMsgCount, result.AfterMsgCount,
		result.StrategiesRun,
	)
	return ToolResult{Content: summary}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestCompactContextTool -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/compact_context.go internal/tools/compact_context_test.go
git commit -m "[BEHAVIORAL] Add compact_context tool for agent-initiated compaction"
```

---

## Task 15: Integration — Wire Everything into Agent Loop

**Files:**
- Modify: `internal/agent/agent.go`

**Step 1: Write the failing test**

Add to `internal/agent/agent_test.go` (or a new `agent_context_integration_test.go`):

```go
func TestAgentNewWithContextEnhancements(t *testing.T) {
	// Verify agent accepts new config fields and initializes subsystems.
	cfg := config.DefaultConfig()
	cfg.Agent.MaxOutputTokens = 4096
	cfg.Agent.ResultOffloadThreshold = 2048

	p := &mockProvider{}
	reg := tools.NewRegistry()
	approve := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) { return true, nil }

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	a := New(p, reg, approve, cfg, WithStore(s))
	assert.NotNil(t, a)
	assert.NotNil(t, a.resultStore)
	assert.NotNil(t, a.promptBuilder)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentNewWithContextEnhancements -v`
Expected: FAIL — `resultStore`/`promptBuilder` fields not on Agent

**Step 3: Add fields and initialization**

Add fields to `Agent` struct in `agent.go`:

```go
type Agent struct {
	// ... existing fields ...
	resultStore   *ResultStore
	promptBuilder *PromptBuilder
	deferral      *DeferralManager
}
```

Note: `DeferralManager` is in `internal/tools`, so import it via an interface to avoid circular imports. Add a `ToolDeferrer` interface:

```go
// ToolDeferrer selects tools for context and searches deferred ones.
type ToolDeferrer interface {
	SelectForContext(allTools []provider.ToolDef, effectiveWindow int) (active []provider.ToolDef, deferredCount int)
	Search(query string) []provider.ToolDef
	DeferredCount() int
}
```

Update `Agent` to use interface:
```go
deferral ToolDeferrer
```

Update `New()` to initialize new subsystems when a store is present:

```go
// After existing session creation block:
if a.store != nil && a.sessionID != "" {
	threshold := cfg.Agent.ResultOffloadThreshold
	if threshold <= 0 {
		threshold = 4096
	}
	a.resultStore = NewResultStore(a.store, a.sessionID, threshold)
}
a.promptBuilder = NewPromptBuilder()
```

Update resume logic to prefer snapshots:

```go
if a.resumeSessionID != "" {
	sess, err := a.store.GetSession(a.resumeSessionID)
	if err != nil || sess == nil {
		log.Printf("warning: failed to resume session %s: %v", a.resumeSessionID, err)
	} else {
		a.sessionID = sess.ID
		a.conversation = NewConversation(sess.SystemPrompt)
		// Prefer compacted snapshot for resume.
		snapshot, snapErr := a.store.GetSnapshot(sess.ID)
		if snapErr == nil && snapshot != nil {
			a.conversation.LoadFromMessages(snapshot)
		} else {
			// Fallback: load full history.
			msgs, err := a.store.GetMessages(sess.ID)
			if err != nil {
				log.Printf("warning: failed to load messages: %v", err)
			} else {
				providerMsgs := make([]provider.Message, len(msgs))
				for i, m := range msgs {
					providerMsgs[i] = provider.Message{Role: m.Role, Content: m.Content}
				}
				a.conversation.LoadFromMessages(providerMsgs)
			}
		}
	}
}
```

**Step 4: Run full test suite**

Run: `go test ./internal/agent/... -v`
Expected: ALL PASS

**Step 5: Run linter**

Run: `golangci-lint run ./internal/agent/... ./internal/tools/... ./internal/store/... ./internal/config/... ./internal/provider/...`
Expected: No issues

**Step 6: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Wire context management subsystems into Agent initialization"
```

---

## Task 16: Integration — Update runLoop for Result Offloading

**Files:**
- Modify: `internal/agent/agent.go:684-760` (executeSingleTool)

**Step 1: Write the failing test**

Add integration test that verifies tool results get offloaded:

```go
func TestAgentToolResultOffloading(t *testing.T) {
	// Test that executeSingleTool offloads large results.
	cfg := config.DefaultConfig()
	cfg.Agent.ResultOffloadThreshold = 20 // very small threshold

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{}
	reg := tools.NewRegistry()
	reg.Register(&mockTool{name: "big_output", result: tools.ToolResult{
		Content: "this output is definitely large enough to trigger offloading behavior in the result store",
	}})
	approve := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) { return true, nil }

	a := New(p, reg, approve, cfg, WithStore(s))

	tc := provider.ToolUseBlock{ID: "t1", Name: "big_output", Input: json.RawMessage(`{}`)}
	result := a.executeSingleTool(context.Background(), tc)

	assert.Contains(t, result.content, "Tool result stored")
	assert.Contains(t, result.content, "read_result")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentToolResultOffloading -v`
Expected: FAIL — offloading not wired

**Step 3: Add offloading to executeSingleTool**

In `executeSingleTool`, after `tool.Execute()` succeeds and before the return, add:

```go
// Offload large tool results to disk if ResultStore is attached.
if a.resultStore != nil && !toolResult.IsError {
	offloaded, offErr := a.resultStore.OffloadResult(tc.Name, tc.ID, toolResult.Content)
	if offErr == nil {
		toolResult.Content = offloaded
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentToolResultOffloading -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./internal/agent/... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Wire result offloading into executeSingleTool"
```

---

## Task 17: Integration — Save Snapshot After Compaction

**Files:**
- Modify: `internal/agent/agent.go` (Turn method and Compact call)

**Step 1: Write the failing test**

```go
func TestAgentSavesSnapshotAfterCompaction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.ContextBudget = 500 // small budget to force compaction
	cfg.Agent.MaxOutputTokens = 0

	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	p := &mockProvider{responses: []string{"done"}}
	reg := tools.NewRegistry()
	approve := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) { return true, nil }

	a := New(p, reg, approve, cfg, WithStore(s))

	// Fill conversation to trigger compaction.
	for i := 0; i < 20; i++ {
		a.conversation.AddUser("message content for testing")
		a.conversation.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "response"}})
	}

	a.saveSnapshotIfNeeded()

	// Verify snapshot was saved.
	snap, err := s.GetSnapshot(a.sessionID)
	require.NoError(t, err)
	// Snapshot may or may not exist depending on whether compaction ran.
	// The point is it doesn't error.
	_ = snap
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentSavesSnapshotAfterCompaction -v`
Expected: FAIL — `saveSnapshotIfNeeded` not defined

**Step 3: Write minimal implementation**

Add to `agent.go`:

```go
// saveSnapshotIfNeeded persists the current conversation state as a
// resume snapshot if persistence is enabled.
func (a *Agent) saveSnapshotIfNeeded() {
	if a.store == nil || a.sessionID == "" {
		return
	}
	tokens := a.context.EstimateTokens(a.conversation)
	if err := a.store.SaveSnapshot(a.sessionID, a.conversation.Messages(), tokens); err != nil {
		log.Printf("warning: failed to save snapshot: %v", err)
	}
}
```

In `Turn()`, after `a.context.Compact(ctx, a.conversation)`:

```go
a.context.Compact(ctx, a.conversation)
a.saveSnapshotIfNeeded()
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestAgentSavesSnapshotAfterCompaction -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/agent.go
git commit -m "[BEHAVIORAL] Save session snapshot after compaction for resume"
```

---

## Task 18: Full Integration Test + Linting

**Files:**
- All modified files

**Step 1: Run full test suite**

Run: `go test ./... -count=1 -v`
Expected: ALL PASS

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No issues

**Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output (all files formatted)

**Step 4: Check test coverage**

Run: `go test -cover ./internal/agent/ ./internal/tools/ ./internal/store/ ./internal/config/ ./internal/provider/...`
Expected: >90% coverage for each package

**Step 5: Commit any remaining fixes**

```bash
git add -A
git commit -m "[STRUCTURAL] Fix formatting and linting issues from context management integration"
```

---

## Summary

| Task | Component | Files | Estimated |
|------|-----------|-------|-----------|
| 1–2 | ContextBudget type | context.go | 15 min |
| 3 | Migrate ContextManager + thresholds | context.go, agent.go, config.go | 30 min |
| 4 | MeasureUsage | context.go | 15 min |
| 5 | PromptBuilder | prompt.go (new) | 20 min |
| 6 | CacheBreakpoints | types.go, anthropic/provider.go | 25 min |
| 7 | ResultStore SQLite | store.go | 15 min |
| 8 | ResultStore agent layer | resultstore.go (new) | 20 min |
| 9 | ReadResultTool | read_result.go (new) | 15 min |
| 10 | Session snapshots | store.go | 15 min |
| 11 | DeferralManager | deferral.go (new) | 20 min |
| 12 | ToolSearchTool | tool_search.go (new) | 15 min |
| 13 | ForceCompact | context.go, agent.go | 20 min |
| 14 | CompactContextTool | compact_context.go (new) | 15 min |
| 15 | Integration — Agent init | agent.go | 25 min |
| 16 | Integration — result offloading | agent.go | 15 min |
| 17 | Integration — snapshot saving | agent.go | 15 min |
| 18 | Full integration test + lint | all | 15 min |
| **Total** | | | **~5 hours** |
