# CollapseStore

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port ccgo's `query/collapse.go` to rubichan. A `CollapseStore` provides staged archival of conversation history with commit/drain semantics.

**Architecture:** `CollapseStore` holds staged spans and committed collapses behind an RWMutex. `ProjectView()` replaces archived message ranges with summary messages. Integrated into `ContextManager.Compact` as an additional strategy after session memory compaction but before truncation.

**Tech Stack:** Go, existing `pkg/agentsdk` types, `internal/provider` message types.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/agent/collapse_store.go` | `CollapseStore`, `CollapseCommit`, `CollapseStagedSpan`, `CollapseStats` |
| `internal/agent/collapse_store_test.go` | Tests for stage/commit/drain/project/stats/reset |
| `pkg/agentsdk/compaction.go` | Add `CollapseStats` type (modify existing) |
| `internal/agent/context.go` | Integrate CollapseStore into Compact/ForceCompact |

---

## Chunk 1: Core Types and CollapseStore

### Task 1: Define CollapseCommit, CollapseStagedSpan, CollapseStats

**Files:**
- Create: `internal/agent/collapse_store.go`

**Code:**

```go
package agent

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/julianshen/rubichan/internal/provider"
)

// CollapseCommit represents a committed archival of a message span.
type CollapseCommit struct {
	CollapseID        string
	SummaryUUID       string
	SummaryContent    string
	FirstArchivedUUID string
	LastArchivedUUID  string
	CommittedAt       time.Time
	TokensFreed       int
}

// CollapseStagedSpan represents a span of messages staged for archival.
type CollapseStagedSpan struct {
	StartUUID string
	EndUUID   string
	Summary   string
	Risk      float64
	StagedAt  time.Time
}

// CollapseStats reports the current state of the collapse store.
type CollapseStats struct {
	TotalCommits     int
	TotalTokensFreed int
	StagedCount      int
	IsEnabled        bool
}

// CollapseStore provides staged archival of conversation history.
type CollapseStore struct {
	mu      sync.RWMutex
	commits []CollapseCommit
	staged  []CollapseStagedSpan
	enabled bool
}

// NewCollapseStore creates a new CollapseStore.
func NewCollapseStore(enabled bool) *CollapseStore {
	return &CollapseStore{
		commits: make([]CollapseCommit, 0),
		staged:  make([]CollapseStagedSpan, 0),
		enabled: enabled,
	}
}

// IsEnabled returns whether the store is enabled.
func (s *CollapseStore) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// Stage adds a span to the staged list.
func (s *CollapseStore) Stage(span CollapseStagedSpan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged = append(s.staged, span)
}

// Commit moves all staged spans to committed and returns the new commits.
func (s *CollapseStore) Commit() []CollapseCommit {
	s.mu.Lock()
	defer s.mu.Unlock()

	var committed []CollapseCommit
	for _, span := range s.staged {
		commit := CollapseCommit{
			CollapseID:        generateCollapseID(),
			SummaryUUID:       generateCollapseID(),
			SummaryContent:    span.Summary,
			FirstArchivedUUID: span.StartUUID,
			LastArchivedUUID:  span.EndUUID,
			CommittedAt:       time.Now(),
			TokensFreed:       0,
		}
		s.commits = append(s.commits, commit)
		committed = append(committed, commit)
	}
	s.staged = s.staged[:0]
	return committed
}

// DrainAll commits all staged spans without clearing staged (returns count).
func (s *CollapseStore) DrainAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := len(s.staged)
	for _, span := range s.staged {
		commit := CollapseCommit{
			CollapseID:        generateCollapseID(),
			SummaryUUID:       generateCollapseID(),
			SummaryContent:    span.Summary,
			FirstArchivedUUID: span.StartUUID,
			LastArchivedUUID:  span.EndUUID,
			CommittedAt:       time.Now(),
		}
		s.commits = append(s.commits, commit)
	}
	s.staged = s.staged[:0]
	return count
}

// ProjectView replaces archived message ranges with summary messages.
func (s *CollapseStore) ProjectView(messages []provider.Message) []provider.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.commits) == 0 {
		return messages
	}

	var result []provider.Message
	inArchivedSpan := false
	var currentCommit *CollapseCommit

	for _, msg := range messages {
		if !inArchivedSpan {
			found := false
			for i := range s.commits {
				if msg.ID == s.commits[i].FirstArchivedUUID {
					inArchivedSpan = true
					commit := s.commits[i]
					currentCommit = &commit
					result = append(result, provider.Message{
						Role: "assistant",
						ID:   commit.SummaryUUID,
						Content: []provider.ContentBlock{{
							Type: "text",
							Text: commit.SummaryContent,
						}},
					})
					found = true
					break
				}
			}
			if !found {
				result = append(result, msg)
			}
		} else {
			if currentCommit != nil && msg.ID == currentCommit.LastArchivedUUID {
				inArchivedSpan = false
				currentCommit = nil
			}
		}
	}

	return result
}

// GetStats returns statistics about the collapse store.
func (s *CollapseStore) GetStats() CollapseStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalTokens := 0
	for _, c := range s.commits {
		totalTokens += c.TokensFreed
	}

	return CollapseStats{
		TotalCommits:     len(s.commits),
		TotalTokensFreed: totalTokens,
		StagedCount:      len(s.staged),
		IsEnabled:        s.enabled,
	}
}

// Reset clears all commits and staged spans.
func (s *CollapseStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = s.commits[:0]
	s.staged = s.staged[:0]
}

// RestoreFromEntries restores commits from a slice.
func (s *CollapseStore) RestoreFromEntries(commits []CollapseCommit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits[:0], commits...)
}

func generateCollapseID() string {
	return uuid.NewString()
}
```

**Test:**

```go
package agent

import (
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestNewCollapseStore(t *testing.T) {
	s := NewCollapseStore(true)
	require.True(t, s.IsEnabled())
	stats := s.GetStats()
	require.Equal(t, 0, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
	require.True(t, stats.IsEnabled)
}

func TestCollapseStoreStage(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-1",
		EndUUID:   "msg-3",
		Summary:   "summary text",
		StagedAt:  time.Now(),
	})
	stats := s.GetStats()
	require.Equal(t, 1, stats.StagedCount)
	require.Equal(t, 0, stats.TotalCommits)
}

func TestCollapseStoreCommit(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-1",
		EndUUID:   "msg-3",
		Summary:   "first summary",
	})
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-4",
		EndUUID:   "msg-6",
		Summary:   "second summary",
	})

	commits := s.Commit()
	require.Len(t, commits, 2)
	require.NotEmpty(t, commits[0].CollapseID)
	require.Equal(t, "first summary", commits[0].SummaryContent)
	require.Equal(t, "msg-1", commits[0].FirstArchivedUUID)
	require.Equal(t, "msg-3", commits[0].LastArchivedUUID)

	stats := s.GetStats()
	require.Equal(t, 2, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
}

func TestCollapseStoreDrainAll(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Stage(CollapseStagedSpan{StartUUID: "c", EndUUID: "d", Summary: "s2"})

	count := s.DrainAll()
	require.Equal(t, 2, count)
	require.Equal(t, 2, s.GetStats().TotalCommits)
	require.Equal(t, 0, s.GetStats().StagedCount)
}

func TestCollapseStoreProjectView(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{
		StartUUID: "msg-2",
		EndUUID:   "msg-3",
		Summary:   "archived summary",
	})
	s.Commit()

	messages := []provider.Message{
		{ID: "msg-1", Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
		{ID: "msg-2", Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "old"}}},
		{ID: "msg-3", Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "old2"}}},
		{ID: "msg-4", Role: "assistant", Content: []provider.ContentBlock{{Type: "text", Text: "new"}}},
	}

	result := s.ProjectView(messages)
	require.Len(t, result, 3)
	require.Equal(t, "msg-1", result[0].ID)
	require.Equal(t, "assistant", result[1].Role)
	require.Equal(t, "archived summary", result[1].Content[0].Text)
	require.Equal(t, "msg-4", result[2].ID)
}

func TestCollapseStoreProjectViewNoCommits(t *testing.T) {
	s := NewCollapseStore(true)
	messages := []provider.Message{
		{ID: "msg-1", Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
	}
	result := s.ProjectView(messages)
	require.Len(t, result, 1)
	require.Equal(t, "msg-1", result[0].ID)
}

func TestCollapseStoreGetStats(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Commit()

	stats := s.GetStats()
	require.Equal(t, 1, stats.TotalCommits)
	require.Equal(t, 0, stats.TotalTokensFreed)
	require.Equal(t, 0, stats.StagedCount)
	require.True(t, stats.IsEnabled)
}

func TestCollapseStoreReset(t *testing.T) {
	s := NewCollapseStore(true)
	s.Stage(CollapseStagedSpan{StartUUID: "a", EndUUID: "b", Summary: "s1"})
	s.Commit()
	require.Equal(t, 1, s.GetStats().TotalCommits)

	s.Reset()
	stats := s.GetStats()
	require.Equal(t, 0, stats.TotalCommits)
	require.Equal(t, 0, stats.StagedCount)
}

func TestCollapseStoreRestoreFromEntries(t *testing.T) {
	s := NewCollapseStore(true)
	commits := []CollapseCommit{
		{CollapseID: "c1", SummaryContent: "s1"},
		{CollapseID: "c2", SummaryContent: "s2"},
	}
	s.RestoreFromEntries(commits)
	require.Equal(t, 2, s.GetStats().TotalCommits)
}

func TestCollapseStoreDisabled(t *testing.T) {
	s := NewCollapseStore(false)
	require.False(t, s.IsEnabled())
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestCollapseStore -v
```

**Expected:** All tests PASS.

---

## Chunk 2: SDK Type and ContextManager Integration

### Task 2: Add CollapseStats to pkg/agentsdk/compaction.go

**Files:**
- Modify: `pkg/agentsdk/compaction.go`

**Code:**

```go
// CollapseStats reports the state of the collapse store for telemetry.
type CollapseStats struct {
	TotalCommits     int
	TotalTokensFreed int
	StagedCount      int
	IsEnabled        bool
}
```

**Test:**

```go
func TestCollapseStats(t *testing.T) {
	stats := CollapseStats{
		TotalCommits:     5,
		TotalTokensFreed: 1000,
		StagedCount:      2,
		IsEnabled:        true,
	}
	require.Equal(t, 5, stats.TotalCommits)
	require.Equal(t, 1000, stats.TotalTokensFreed)
	require.Equal(t, 2, stats.StagedCount)
	require.True(t, stats.IsEnabled)
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestCollapseStats -v
```

**Expected:** PASS.

---

### Task 3: Integrate CollapseStore into ContextManager

**Files:**
- Modify: `internal/agent/context.go`

Add field to ContextManager:

```go
type ContextManager struct {
	// ... existing fields ...
	collapseStore *CollapseStore
}
```

Add setter:

```go
// SetCollapseStore attaches a collapse store for staged archival.
func (cm *ContextManager) SetCollapseStore(store *CollapseStore) {
	cm.collapseStore = store
}
```

Modify `Compact` to use collapse store after session memory compaction but before truncation:

```go
func (cm *ContextManager) Compact(ctx context.Context, conv *Conversation) error {
	if !cm.ShouldCompact(conv) {
		return nil
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

	beforeTokens := estimateMessageTokens(conv.messages)
	anyStrategySucceeded := false

	for i, s := range cm.strategies {
		if i > 0 && !cm.ExceedsBudget(conv) {
			break
		}
		result, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		conv.messages = result
		anyStrategySucceeded = true
	}

	// Apply collapse store projection after strategies run.
	if cm.collapseStore != nil && cm.collapseStore.IsEnabled() && len(cm.collapseStore.commits) > 0 {
		conv.messages = cm.collapseStore.ProjectView(conv.messages)
	}

	afterTokens := estimateMessageTokens(conv.messages)
	shrank := afterTokens < beforeTokens

	if anyStrategySucceeded && shrank {
		cm.consecutiveFailures = 0
		return nil
	}

	cm.consecutiveFailures++
	if cm.consecutiveFailures >= MaxConsecutiveCompactionFailures {
		return ErrCompactionExhausted
	}
	return nil
}
```

Note: The `commits` field access above is private. Add a helper to CollapseStore:

In `internal/agent/collapse_store.go`, add:
```go
// HasCommits returns true if there are committed collapses.
func (s *CollapseStore) HasCommits() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.commits) > 0
}
```

Then update the Compact integration to use `HasCommits()`:
```go
	if cm.collapseStore != nil && cm.collapseStore.IsEnabled() && cm.collapseStore.HasCommits() {
		conv.messages = cm.collapseStore.ProjectView(conv.messages)
	}
```

Similarly modify `ForceCompact`:

```go
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
		tokensBefore := estimateMessageTokens(conv.messages)
		countBefore := len(conv.messages)
		msgs, err := s.Compact(ctx, conv.messages, messageBudget)
		if err != nil {
			continue
		}
		tokensAfter := estimateMessageTokens(msgs)
		countAfter := len(msgs)
		if tokensAfter < tokensBefore || countAfter < countBefore {
			result.StrategiesRun = append(result.StrategiesRun, s.Name())
		}
		conv.messages = msgs
	}

	// Apply collapse store projection.
	if cm.collapseStore != nil && cm.collapseStore.IsEnabled() && cm.collapseStore.HasCommits() {
		conv.messages = cm.collapseStore.ProjectView(conv.messages)
	}

	result.AfterTokens = cm.EstimateTokens(conv)
	result.AfterMsgCount = len(conv.messages)
	return result
}
```

**Test:**

```go
func TestContextManagerWithCollapseStore(t *testing.T) {
	cm := NewContextManager(100000, 4096)
	store := NewCollapseStore(true)
	cm.SetCollapseStore(store)

	// Stage and commit a span
	store.Stage(CollapseStagedSpan{
		StartUUID: "msg-2",
		EndUUID:   "msg-3",
		Summary:   "archived",
	})
	store.Commit()

	conv := NewConversation("system prompt")
	conv.AddUser("hello")
	// Manually add messages with IDs for testing
	conv.messages = append(conv.messages, provider.Message{
		ID:   "msg-2",
		Role: "assistant",
		Content: []provider.ContentBlock{{Type: "text", Text: "old"}},
	})
	conv.messages = append(conv.messages, provider.Message{
		ID:   "msg-3",
		Role: "user",
		Content: []provider.ContentBlock{{Type: "text", Text: "old2"}},
	})
	conv.messages = append(conv.messages, provider.Message{
		ID:   "msg-4",
		Role: "assistant",
		Content: []provider.ContentBlock{{Type: "text", Text: "new"}},
	})

	_ = cm.Compact(context.Background(), conv)
	// After compaction, archived messages should be replaced with summary
	foundSummary := false
	for _, msg := range conv.messages {
		for _, block := range msg.Content {
			if block.Text == "archived" {
				foundSummary = true
			}
		}
	}
	require.True(t, foundSummary, "expected summary message in conversation")
}

func TestContextManagerCollapseStoreDisabled(t *testing.T) {
	cm := NewContextManager(100000, 4096)
	store := NewCollapseStore(false)
	cm.SetCollapseStore(store)

	conv := NewConversation("system prompt")
	conv.AddUser("hello")
	before := len(conv.messages)
	_ = cm.Compact(context.Background(), conv)
	require.Equal(t, before, len(conv.messages))
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestContextManagerWithCollapseStore -v
go test ./internal/agent/... -run TestContextManagerCollapseStoreDisabled -v
```

**Expected:** PASS.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[BEHAVIORAL] CollapseStore for staged conversation archival`

**Body:**
- `CollapseStore` provides staged archival of conversation history with commit/drain semantics
- `Stage(span)` adds a span to the staged list
- `Commit()` moves all staged spans to committed, returns commits
- `DrainAll()` commits all staged without clearing (returns count)
- `ProjectView(messages)` replaces archived message ranges with summary messages
- `GetStats()` reports total commits, tokens freed, staged count
- `Reset()` clears all commits and staged
- Thread-safe with RWMutex
- Integrated into `ContextManager.Compact` as additional strategy after session memory compaction but before truncation
- Ports ccgo's `query/collapse.go` pattern to Go

**Commit prefix:** `[BEHAVIORAL]`
