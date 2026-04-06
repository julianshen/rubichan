# TUI Phase D Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor turn rendering into a reusable component and implement virtual scrolling for performance and memory efficiency.

**Architecture:** Phase D.1 extracts turn rendering into a pure TurnRenderer type; Phase D.2 builds on it with memory-efficient turn storage and archival. Both maintain 100% UI compatibility.

**Tech Stack:** Go, Bubble Tea, Lipgloss, TDD with unit + integration tests

---

## File Structure

### Phase D.1: TurnRenderer (Refactoring)

**New files:**
- `internal/tui/turnrenderer.go` — TurnRenderer, Turn, RenderOptions, RenderedToolCall types
- `internal/tui/turnrenderer_test.go` — Unit tests for TurnRenderer

**Modified files:**
- `internal/tui/model.go` — Add turnRenderer field; refactor View() to use it
- `internal/tui/update.go` — Add extractTurnForRendering() helper

### Phase D.2: Virtual Scrolling (Performance)

**New files:**
- `internal/tui/turnwindow.go` — TurnWindow, TurnCache, TurnRenderPool types
- `internal/tui/turncache.go` — Archive I/O, LRU eviction
- `internal/tui/turnwindow_test.go` — TurnWindow tests
- `internal/tui/turncache_test.go` — TurnCache tests
- `internal/tui/virtualscroll_test.go` — Integration tests

**Modified files:**
- `internal/tui/model.go` — Replace turn slice with TurnWindow; wire archival
- `internal/tui/update.go` — Call ArchiveOldTurns periodically
- `cmd/rubichan/main.go` — Add archive cleanup command

---

## Phase D.1: Shared TurnRenderer

### Task 1: Define TurnRenderer Data Types

**Files:**
- Create: `internal/tui/turnrenderer.go`

- [ ] **Step 1: Write test to verify Turn struct fields**

```go
// internal/tui/turnrenderer_test.go
package tui

import (
    "testing"
    "time"
)

func TestTurn_StructFields(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "Hello",
        ThinkingText:  "Thinking...",
        ToolCalls:     []RenderedToolCall{},
        Status:        "done",
        ErrorMsg:      "",
        StartTime:     time.Now(),
    }
    
    if turn.ID != "turn-1" {
        t.Errorf("ID not set correctly")
    }
    if turn.AssistantText != "Hello" {
        t.Errorf("AssistantText not set correctly")
    }
    if len(turn.ToolCalls) != 0 {
        t.Errorf("ToolCalls should be empty slice")
    }
}

func TestRenderedToolCall_StructFields(t *testing.T) {
    call := RenderedToolCall{
        ID:        "tool-1",
        Name:      "file",
        Args:      "path=main.go",
        Result:    "package main",
        IsError:   false,
        Collapsed: true,
        LineCount: 100,
    }
    
    if call.Name != "file" {
        t.Errorf("Name not set correctly")
    }
    if call.Collapsed != true {
        t.Errorf("Collapsed should be true")
    }
}

func TestRenderOptions_StructFields(t *testing.T) {
    opts := RenderOptions{
        Width:          80,
        IsStreaming:    true,
        CollapsedTools: false,
        HighlightError: false,
        MaxToolLines:   500,
    }
    
    if opts.Width != 80 {
        t.Errorf("Width not set correctly")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/julianshen/prj/rubichan
go test ./internal/tui -run TestTurn_StructFields -v
```

Expected output: `undefined: Turn`

- [ ] **Step 3: Create turnrenderer.go with data types**

```go
// internal/tui/turnrenderer.go
package tui

import (
    "context"
    "time"
)

// TurnRenderer encapsulates all turn rendering logic.
// It is a pure function: takes immutable turn data, returns rendered string.
// Zero state, zero side effects — easy to test and parallelize.
type TurnRenderer struct{}

// RenderOptions controls rendering behavior (streaming state, width, etc.)
type RenderOptions struct {
    Width           int   // viewport width in characters
    IsStreaming     bool  // whether turn is still streaming (affects UI state)
    CollapsedTools  bool  // whether tool results are collapsed
    HighlightError  bool  // highlight error messages in red
    MaxToolLines    int   // truncate tool output beyond this many lines (0 = no limit)
}

// Turn represents the complete state of a rendered turn (immutable).
// Extracted from Model's streaming state for rendering.
type Turn struct {
    ID            string              // unique turn identifier (for caching)
    AssistantText string              // raw assistant message text
    ThinkingText  string              // raw thinking block content (empty if not present)
    ToolCalls     []RenderedToolCall  // all tool calls in this turn
    Status        string              // "streaming", "done", or "error"
    ErrorMsg      string              // error message (if status == "error")
    StartTime     time.Time           // when turn started (for elapsed time display)
}

// RenderedToolCall represents a single tool invocation and its result.
type RenderedToolCall struct {
    ID        string // tool call ID
    Name      string // tool name (e.g., "file", "shell")
    Args      string // formatted tool arguments
    Result    string // raw tool output
    IsError   bool   // whether this tool call failed
    Collapsed bool   // true = show collapsed summary; false = show full output
    LineCount int    // total lines in Result (before truncation)
}

// Render produces the complete text representation of a turn.
// This is the main entry point used by Model.View().
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error) {
    // Stub: return empty for now, will implement in later tasks
    return "", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurn_StructFields -v
go test ./internal/tui -run TestRenderedToolCall_StructFields -v
go test ./internal/tui -run TestRenderOptions_StructFields -v
```

Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turnrenderer.go internal/tui/turnrenderer_test.go
git commit -m "[BEHAVIORAL] Define TurnRenderer data types (Turn, RenderedToolCall, RenderOptions)"
```

---

### Task 2: Implement TurnRenderer.Render() - Basic Rendering

**Files:**
- Modify: `internal/tui/turnrenderer.go`
- Modify: `internal/tui/turnrenderer_test.go`

- [ ] **Step 1: Write test for rendering assistant message**

```go
func TestTurnRenderer_RendersAssistantMessage(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "Hello world",
        Status:        "done",
        ToolCalls:     []RenderedToolCall{},
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Fatalf("Render failed: %v", err)
    }
    
    if !strings.Contains(output, "Hello world") {
        t.Errorf("Output should contain assistant text, got: %s", output)
    }
}

func TestTurnRenderer_RendersThinkingBlock(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "Response",
        ThinkingText:  "Let me think...",
        Status:        "done",
        ToolCalls:     []RenderedToolCall{},
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Fatalf("Render failed: %v", err)
    }
    
    if !strings.Contains(output, "Let me think...") {
        t.Errorf("Output should contain thinking text")
    }
    if !strings.Contains(output, "🧠") {
        t.Errorf("Output should contain thinking emoji")
    }
}

func TestTurnRenderer_HidesThinkingWhenEmpty(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "Response",
        ThinkingText:  "",  // Empty
        Status:        "done",
        ToolCalls:     []RenderedToolCall{},
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Fatalf("Render failed: %v", err)
    }
    
    if strings.Contains(output, "🧠") {
        t.Errorf("Output should not contain thinking emoji when thinking is empty")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestTurnRenderer_Renders -v
```

Expected: Tests fail because Render() returns empty string

- [ ] **Step 3: Implement Render() with assistant + thinking**

```go
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error) {
    var output strings.Builder
    
    // Render thinking block if present
    if turn.ThinkingText != "" {
        output.WriteString("🧠 Thinking...\n")
        output.WriteString(turn.ThinkingText)
        output.WriteString("\n\n")
    }
    
    // Render assistant message
    if turn.AssistantText != "" {
        output.WriteString(turn.AssistantText)
        output.WriteString("\n")
    }
    
    return output.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurnRenderer_Renders -v
```

Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turnrenderer.go internal/tui/turnrenderer_test.go
git commit -m "[BEHAVIORAL] Implement TurnRenderer.Render() for assistant + thinking"
```

---

### Task 3: Implement Tool Call Rendering (Collapsed)

**Files:**
- Modify: `internal/tui/turnrenderer.go`
- Modify: `internal/tui/turnrenderer_test.go`

- [ ] **Step 1: Write test for collapsed tool call**

```go
func TestTurnRenderer_RendersToolCallCollapsed(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "I read the file",
        Status:        "done",
        ToolCalls: []RenderedToolCall{
            {
                ID:        "tool-1",
                Name:      "file",
                Args:      `path="main.go"`,
                Result:    "package main\n\nfunc main() {}",
                IsError:   false,
                Collapsed: true,
                LineCount: 3,
            },
        },
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Fatalf("Render failed: %v", err)
    }
    
    // Collapsed should show summary line only
    if !strings.Contains(output, "▶") {
        t.Errorf("Collapsed tool should have ▶ indicator")
    }
    if !strings.Contains(output, "file") {
        t.Errorf("Output should contain tool name")
    }
    if !strings.Contains(output, "3 lines") || !strings.Contains(output, "lines") {
        t.Errorf("Output should contain line count")
    }
    // Should NOT expand the result
    if strings.Count(output, "package main") > 1 {
        t.Errorf("Collapsed tool should not show full result")
    }
}

func TestTurnRenderer_RendersToolCallExpanded(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "I read the file",
        Status:        "done",
        ToolCalls: []RenderedToolCall{
            {
                ID:        "tool-1",
                Name:      "file",
                Args:      `path="main.go"`,
                Result:    "package main\n\nfunc main() {}",
                IsError:   false,
                Collapsed: false,  // Expanded
                LineCount: 3,
            },
        },
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Fatalf("Render failed: %v", err)
    }
    
    // Expanded should show full result
    if !strings.Contains(output, "▼") {
        t.Errorf("Expanded tool should have ▼ indicator")
    }
    if !strings.Contains(output, "package main") {
        t.Errorf("Expanded tool should show result content")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestTurnRenderer_RendersToolCall -v
```

- [ ] **Step 3: Implement tool call rendering**

```go
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error) {
    var output strings.Builder
    
    // Render thinking block if present
    if turn.ThinkingText != "" {
        output.WriteString("🧠 Thinking...\n")
        output.WriteString(turn.ThinkingText)
        output.WriteString("\n\n")
    }
    
    // Render assistant message
    if turn.AssistantText != "" {
        output.WriteString(turn.AssistantText)
        output.WriteString("\n")
    }
    
    // Render tool calls
    for _, call := range turn.ToolCalls {
        if call.Collapsed {
            // Collapsed summary line
            output.WriteString(fmt.Sprintf("▶ %s(%s) — %d lines\n", 
                call.Name, call.Args, call.LineCount))
        } else {
            // Expanded with full result
            output.WriteString(fmt.Sprintf("▼ %s(%s)\n", call.Name, call.Args))
            output.WriteString("╭──────────────────────────╮\n")
            output.WriteString(call.Result)
            output.WriteString("\n╰──────────────────────────╯\n")
        }
    }
    
    return output.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurnRenderer_RendersToolCall -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turnrenderer.go internal/tui/turnrenderer_test.go
git commit -m "[BEHAVIORAL] Implement tool call rendering (collapsed + expanded)"
```

---

### Task 4: Add extractTurnForRendering() Helper to Model

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Write test for extractTurnForRendering**

```go
// In model_test.go or update_test.go
func TestModel_ExtractTurnForRendering(t *testing.T) {
    m := &Model{
        turnCount: 5,
        // ... initialize other fields ...
    }
    
    // Add some mock turn data to model
    m.rawAssistant.WriteString("Assistant response")
    
    turn := m.extractTurnForRendering()
    
    if turn == nil {
        t.Fatalf("extractTurnForRendering returned nil")
    }
    if turn.ID == "" {
        t.Errorf("Turn ID should not be empty")
    }
    if turn.AssistantText != "Assistant response" {
        t.Errorf("Assistant text not extracted correctly")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./cmd/rubichan -run TestModel_ExtractTurnForRendering -v
```

- [ ] **Step 3: Implement extractTurnForRendering in model.go**

```go
// In internal/tui/model.go, add method to Model

func (m *Model) extractTurnForRendering() *Turn {
    turn := &Turn{
        ID:            fmt.Sprintf("turn-%d", m.turnCount),
        AssistantText: m.rawAssistant.String(),
        ThinkingText:  m.rawThinking.String(),
        Status:        "done",
        ErrorMsg:      "",
        StartTime:     m.turnStartTime,
        ToolCalls:     []RenderedToolCall{},
    }
    
    // TODO: Extract tool calls from model state (done in later task)
    
    return turn
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./cmd/rubichan -run TestModel_ExtractTurnForRendering -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go
git commit -m "[BEHAVIORAL] Add extractTurnForRendering() helper to Model"
```

---

### Task 5: Wire TurnRenderer into Model.View()

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Write test that TurnRenderer is initialized**

```go
func TestModel_TurnRendererInitialized(t *testing.T) {
    m := &Model{}
    
    if m.turnRenderer == nil {
        t.Errorf("turnRenderer should be initialized")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestModel_TurnRendererInitialized -v
```

- [ ] **Step 3: Add turnRenderer field to Model and initialize it**

```go
// In internal/tui/model.go Model struct
type Model struct {
    // ... existing fields ...
    turnRenderer *TurnRenderer  // NEW: add renderer instance
}

// In NewModel() or initialization function
m.turnRenderer = &TurnRenderer{}
```

- [ ] **Step 4: Update Model.View() to use TurnRenderer**

```go
func (m *Model) View() string {
    // Extract current turn data
    turn := m.extractTurnForRendering()
    
    // Render turn
    opts := RenderOptions{
        Width:          m.width,
        IsStreaming:    m.state == StateStreaming,
        CollapsedTools: m.diffExpanded,
        HighlightError: m.state == StateStreaming || m.state == StateAwaitingApproval,
        MaxToolLines:   500,
    }
    
    rendered, err := m.turnRenderer.Render(context.Background(), turn, opts)
    if err != nil {
        rendered = "Render error: " + err.Error()
    }
    
    m.viewport.SetContent(rendered)
    
    // Return final UI with overlays, status bar, etc.
    return m.renderFinalUI()
}
```

- [ ] **Step 5: Run tests to verify nothing broke**

```bash
go test ./internal/tui -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "[BEHAVIORAL] Wire TurnRenderer into Model.View()"
```

---

### Task 6: Add Error Handling and Edge Cases

**Files:**
- Modify: `internal/tui/turnrenderer.go`
- Modify: `internal/tui/turnrenderer_test.go`

- [ ] **Step 1: Write tests for edge cases**

```go
func TestTurnRenderer_EmptyTurn(t *testing.T) {
    turn := &Turn{}
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Errorf("Should not error on empty turn")
    }
    // Should return empty or minimal output, not panic
    if len(output) < 0 {
        t.Errorf("Output should not be negative length")
    }
}

func TestTurnRenderer_ContextCancellation(t *testing.T) {
    turn := &Turn{AssistantText: "test"}
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    
    // Should handle cancelled context gracefully
    output, err := renderer.Render(ctx, turn, opts)
    // Should still render (context is informational for now)
    if output == "" && err == nil {
        // OK - empty output is fine
    }
}

func TestTurnRenderer_SpecialCharacters(t *testing.T) {
    turn := &Turn{
        ID:            "turn-1",
        AssistantText: "Test with emoji 🎉 and unicode ñ",
        ToolCalls: []RenderedToolCall{
            {
                Name:      "shell",
                Result:    "error: file not found\n✗ failed",
                Collapsed: false,
            },
        },
    }
    
    renderer := &TurnRenderer{}
    opts := RenderOptions{Width: 80}
    
    output, err := renderer.Render(context.Background(), turn, opts)
    if err != nil {
        t.Errorf("Should handle special characters: %v", err)
    }
    if !strings.Contains(output, "🎉") {
        t.Errorf("Should preserve emoji in output")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui -run TestTurnRenderer_Empty -v
go test ./internal/tui -run TestTurnRenderer_Context -v
go test ./internal/tui -run TestTurnRenderer_Special -v
```

- [ ] **Step 3: Update Render() to handle edge cases**

```go
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error) {
    if turn == nil {
        return "", nil
    }
    
    var output strings.Builder
    
    // Render thinking block if present
    if turn.ThinkingText != "" {
        output.WriteString("🧠 Thinking...\n")
        output.WriteString(turn.ThinkingText)
        output.WriteString("\n\n")
    }
    
    // Render assistant message
    if turn.AssistantText != "" {
        output.WriteString(turn.AssistantText)
        output.WriteString("\n")
    }
    
    // Render tool calls
    for _, call := range turn.ToolCalls {
        if call.Collapsed {
            output.WriteString(fmt.Sprintf("▶ %s(%s) — %d lines\n", 
                call.Name, call.Args, call.LineCount))
        } else {
            output.WriteString(fmt.Sprintf("▼ %s(%s)\n", call.Name, call.Args))
            output.WriteString("╭──────────────────────────╮\n")
            output.WriteString(call.Result)
            output.WriteString("\n╰──────────────────────────╯\n")
        }
    }
    
    return output.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui -run TestTurnRenderer -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turnrenderer.go internal/tui/turnrenderer_test.go
git commit -m "[BEHAVIORAL] Add error handling and edge case tests to TurnRenderer"
```

---

### Task 7: Final Phase D.1 Testing and Validation

**Files:**
- None (testing only)

- [ ] **Step 1: Run full TUI test suite**

```bash
go test ./internal/tui -v
go test ./cmd/rubichan -v
```

Expected: All tests pass, no regressions

- [ ] **Step 2: Build the application**

```bash
go build ./cmd/rubichan
```

Expected: Clean build with no errors or warnings

- [ ] **Step 3: Manual smoke test (if possible)**

```bash
./rubichan --help
```

- [ ] **Step 4: Verify test coverage**

```bash
go test ./internal/tui -cover | grep turnrenderer
```

Expected: >90% coverage of turnrenderer.go

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "[STRUCTURAL] Phase D.1 complete: TurnRenderer refactoring"
```

---

## Phase D.2: Virtual Scrolling

> **Note:** Phase D.2 depends on Phase D.1. Ensure all Phase D.1 tasks are complete before starting Phase D.2.

### Task 8: Define TurnWindow and TurnCache Types

**Files:**
- Create: `internal/tui/turnwindow.go`
- Create: `internal/tui/turncache.go`

- [ ] **Step 1: Write test for TurnCache initialization**

```go
// internal/tui/turncache_test.go
package tui

import (
    "testing"
    "os"
    "path/filepath"
)

func TestTurnCache_Initialize(t *testing.T) {
    tmpDir := t.TempDir()
    sessionID := "test-session-1"
    
    cache := NewTurnCache(tmpDir, sessionID, 50)
    
    if cache == nil {
        t.Fatalf("NewTurnCache returned nil")
    }
    if cache.maxMemoryTurns != 50 {
        t.Errorf("maxMemoryTurns not set correctly")
    }
}

func TestTurnWindow_Initialize(t *testing.T) {
    cache := &TurnCache{}
    window := NewTurnWindow(cache)
    
    if window == nil {
        t.Fatalf("NewTurnWindow returned nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestTurnCache_Initialize -v
```

- [ ] **Step 3: Define TurnCache type**

```go
// internal/tui/turncache.go
package tui

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "time"
)

// TurnCache manages turn storage with automatic archival.
type TurnCache struct {
    mu              sync.RWMutex
    turns           map[int]*Turn     // in-memory turns
    archivedPaths   map[int]string    // archived turn paths
    maxMemoryTurns  int               // keep last N turns in memory
    archiveDir      string            // directory for archived turns
    sessionID       string            // session identifier
}

// NewTurnCache creates a new turn cache.
func NewTurnCache(archiveDir, sessionID string, maxMemoryTurns int) *TurnCache {
    return &TurnCache{
        turns:          make(map[int]*Turn),
        archivedPaths:  make(map[int]string),
        maxMemoryTurns: maxMemoryTurns,
        archiveDir:     archiveDir,
        sessionID:      sessionID,
    }
}

// AddTurn adds a turn to cache (in-memory by default)
func (c *TurnCache) AddTurn(index int, turn *Turn) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.turns[index] = turn
}

// GetTurn gets a turn by index
func (c *TurnCache) GetTurn(index int) (*Turn, error) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if turn, ok := c.turns[index]; ok {
        return turn, nil
    }
    
    // TODO: load from archive if not in memory
    return nil, fmt.Errorf("turn %d not found", index)
}

// ArchiveOldTurns moves turns before index to disk
func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error {
    // TODO: implement archival
    return nil
}
```

- [ ] **Step 4: Define TurnWindow type**

```go
// internal/tui/turnwindow.go
package tui

import (
    "sync"
)

// TurnWindow manages memory-efficient access to turns.
type TurnWindow struct {
    cache        *TurnCache
    visibleStart int
    visibleEnd   int
    mu           sync.RWMutex
}

// NewTurnWindow creates a new turn window.
func NewTurnWindow(cache *TurnCache) *TurnWindow {
    return &TurnWindow{
        cache: cache,
    }
}

// AddTurn adds a turn to the window.
func (w *TurnWindow) AddTurn(index int, turn *Turn) {
    w.cache.AddTurn(index, turn)
}

// UpdateVisibleRange updates which turns are visible.
func (w *TurnWindow) UpdateVisibleRange(scrollPos, viewportHeight int) {
    // TODO: compute visible range based on scroll position
}

// RenderVisible renders only visible turns.
func (w *TurnWindow) RenderVisible(renderer *TurnRenderer) (string, error) {
    // TODO: render visible turns
    return "", nil
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurnCache_Initialize -v
go test ./internal/tui -run TestTurnWindow_Initialize -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/turncache.go internal/tui/turnwindow.go internal/tui/turncache_test.go
git commit -m "[BEHAVIORAL] Define TurnWindow and TurnCache types"
```

---

### Task 9: Implement Turn Archival to Disk

**Files:**
- Modify: `internal/tui/turncache.go`
- Modify: `internal/tui/turncache_test.go`

- [ ] **Step 1: Write test for archiving turns**

```go
func TestTurnCache_ArchiveOldTurns(t *testing.T) {
    tmpDir := t.TempDir()
    cache := NewTurnCache(tmpDir, "test-1", 2)  // keep 2 in memory
    
    // Add 5 turns
    for i := 0; i < 5; i++ {
        turn := &Turn{
            ID:            fmt.Sprintf("turn-%d", i),
            AssistantText: fmt.Sprintf("Turn %d", i),
            Status:        "done",
        }
        cache.AddTurn(i, turn)
    }
    
    // Archive turns before index 3 (should archive 0, 1, 2)
    err := cache.ArchiveOldTurns(3)
    if err != nil {
        t.Fatalf("ArchiveOldTurns failed: %v", err)
    }
    
    // Verify archive files exist
    for i := 0; i < 3; i++ {
        archivePath := filepath.Join(tmpDir, "test-1", fmt.Sprintf("turn-%d.json", i))
        if _, err := os.Stat(archivePath); os.IsNotExist(err) {
            t.Errorf("Archive file for turn %d not found at %s", i, archivePath)
        }
    }
}

func TestTurnCache_LoadFromArchive(t *testing.T) {
    tmpDir := t.TempDir()
    cache := NewTurnCache(tmpDir, "test-1", 2)
    
    // Create and archive a turn
    originalTurn := &Turn{
        ID:            "turn-0",
        AssistantText: "Original text",
        Status:        "done",
    }
    cache.AddTurn(0, originalTurn)
    err := cache.ArchiveOldTurns(1)
    if err != nil {
        t.Fatalf("ArchiveOldTurns failed: %v", err)
    }
    
    // Remove from memory to simulate eviction
    cache.mu.Lock()
    delete(cache.turns, 0)
    cache.mu.Unlock()
    
    // Load from archive
    loaded, err := cache.LoadFromArchive(0)
    if err != nil {
        t.Fatalf("LoadFromArchive failed: %v", err)
    }
    
    if loaded.ID != "turn-0" {
        t.Errorf("Loaded turn ID mismatch")
    }
    if loaded.AssistantText != "Original text" {
        t.Errorf("Loaded turn text mismatch")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestTurnCache_Archive -v
```

- [ ] **Step 3: Implement archival**

```go
// In turncache.go, update ArchiveOldTurns and add LoadFromArchive

// ArchivedTurn is the serialized format for archived turns
type ArchivedTurn struct {
    ID            string              `json:"id"`
    AssistantText string              `json:"assistant_text"`
    ThinkingText  string              `json:"thinking_text"`
    ToolCalls     []RenderedToolCall  `json:"tool_calls"`
    Status        string              `json:"status"`
    ErrorMsg      string              `json:"error_msg"`
    StartTime     time.Time           `json:"start_time"`
    ArchivedAt    time.Time           `json:"archived_at"`
}

func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Create archive directory
    archiveSessionDir := filepath.Join(c.archiveDir, c.sessionID)
    if err := os.MkdirAll(archiveSessionDir, 0755); err != nil {
        return fmt.Errorf("create archive dir: %w", err)
    }
    
    // Archive turns before threshold
    for i := 0; i < beforeIndex; i++ {
        if turn, ok := c.turns[i]; ok {
            // Convert to ArchivedTurn
            archived := ArchivedTurn{
                ID:            turn.ID,
                AssistantText: turn.AssistantText,
                ThinkingText:  turn.ThinkingText,
                ToolCalls:     turn.ToolCalls,
                Status:        turn.Status,
                ErrorMsg:      turn.ErrorMsg,
                StartTime:     turn.StartTime,
                ArchivedAt:    time.Now(),
            }
            
            // Write to disk
            data, _ := json.MarshalIndent(archived, "", "  ")
            archivePath := filepath.Join(archiveSessionDir, fmt.Sprintf("turn-%d.json", i))
            if err := os.WriteFile(archivePath, data, 0644); err != nil {
                return fmt.Errorf("write archive: %w", err)
            }
            
            // Track archived path
            c.archivedPaths[i] = archivePath
            
            // Remove from memory
            delete(c.turns, i)
        }
    }
    
    return nil
}

func (c *TurnCache) LoadFromArchive(index int) (*Turn, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    archivePath, ok := c.archivedPaths[index]
    if !ok {
        return nil, fmt.Errorf("turn %d not archived", index)
    }
    
    // Read from disk
    data, err := os.ReadFile(archivePath)
    if err != nil {
        return nil, fmt.Errorf("read archive: %w", err)
    }
    
    // Unmarshal
    var archived ArchivedTurn
    if err := json.Unmarshal(data, &archived); err != nil {
        return nil, fmt.Errorf("unmarshal archive: %w", err)
    }
    
    // Convert back to Turn
    turn := &Turn{
        ID:            archived.ID,
        AssistantText: archived.AssistantText,
        ThinkingText:  archived.ThinkingText,
        ToolCalls:     archived.ToolCalls,
        Status:        archived.Status,
        ErrorMsg:      archived.ErrorMsg,
        StartTime:     archived.StartTime,
    }
    
    return turn, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurnCache_Archive -v
go test ./internal/tui -run TestTurnCache_Load -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turncache.go internal/tui/turncache_test.go
git commit -m "[BEHAVIORAL] Implement turn archival and loading from disk"
```

---

### Task 10: Implement TurnWindow Virtual Rendering

**Files:**
- Modify: `internal/tui/turnwindow.go`
- Modify: `internal/tui/turnwindow_test.go`

- [ ] **Step 1: Write test for visible range computation**

```go
// internal/tui/turnwindow_test.go
func TestTurnWindow_ComputesVisibleRange(t *testing.T) {
    cache := NewTurnCache(t.TempDir(), "test", 50)
    window := NewTurnWindow(cache)
    
    // Add 20 turns
    for i := 0; i < 20; i++ {
        turn := &Turn{ID: fmt.Sprintf("turn-%d", i)}
        window.AddTurn(i, turn)
    }
    
    // Simulate viewport at height 10 (showing ~5-10 turns)
    window.UpdateVisibleRange(0, 10)  // scroll at top
    
    if window.visibleStart < 0 {
        t.Errorf("visibleStart should be >= 0")
    }
    if window.visibleEnd < window.visibleStart {
        t.Errorf("visibleEnd should be >= visibleStart")
    }
}

func TestTurnWindow_RenderVisibleOnly(t *testing.T) {
    tmpDir := t.TempDir()
    cache := NewTurnCache(tmpDir, "test", 50)
    window := NewTurnWindow(cache)
    renderer := &TurnRenderer{}
    
    // Add 10 turns
    for i := 0; i < 10; i++ {
        turn := &Turn{
            ID:            fmt.Sprintf("turn-%d", i),
            AssistantText: fmt.Sprintf("Turn %d", i),
        }
        window.AddTurn(i, turn)
    }
    
    window.UpdateVisibleRange(0, 10)
    output, err := window.RenderVisible(renderer)
    if err != nil {
        t.Fatalf("RenderVisible failed: %v", err)
    }
    
    if output == "" {
        t.Errorf("Output should not be empty")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui -run TestTurnWindow_Computes -v
go test ./internal/tui -run TestTurnWindow_RenderVisible -v
```

- [ ] **Step 3: Implement virtual rendering**

```go
// In turnwindow.go

func (w *TurnWindow) UpdateVisibleRange(scrollPos, viewportHeight int) {
    w.mu.Lock()
    defer w.mu.Unlock()
    
    // Estimate turns per viewport (roughly 30 chars per line, accounting for wrapping)
    linesPerTurn := 5  // rough estimate
    turnsPerViewport := viewportHeight / linesPerTurn
    if turnsPerViewport < 1 {
        turnsPerViewport = 1
    }
    
    // Compute visible range based on scroll position
    // (This is simplified; real implementation would track pixel offsets)
    w.visibleStart = (scrollPos / linesPerTurn) - turnsPerViewport
    if w.visibleStart < 0 {
        w.visibleStart = 0
    }
    w.visibleEnd = w.visibleStart + (turnsPerViewport * 2)  // slightly ahead for smoothness
}

func (w *TurnWindow) RenderVisible(renderer *TurnRenderer) (string, error) {
    w.mu.RLock()
    visibleStart := w.visibleStart
    visibleEnd := w.visibleEnd
    w.mu.RUnlock()
    
    var output strings.Builder
    
    // Render visible turns only
    for i := visibleStart; i <= visibleEnd; i++ {
        turn, err := w.cache.GetTurn(i)
        if err != nil {
            // Turn not in range, stop
            continue
        }
        
        rendered, err := renderer.Render(context.Background(), turn, RenderOptions{Width: 80})
        if err != nil {
            continue
        }
        
        output.WriteString(rendered)
        output.WriteString("\n")
    }
    
    return output.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/tui -run TestTurnWindow -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tui/turnwindow.go internal/tui/turnwindow_test.go
git commit -m "[BEHAVIORAL] Implement TurnWindow virtual rendering and visible range"
```

---

### Task 11: Integrate TurnWindow into Model

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`

- [ ] **Step 1: Write test for Model with TurnWindow**

```go
func TestModel_UsesTurnWindow(t *testing.T) {
    m := &Model{}
    
    if m.turnWindow == nil {
        t.Errorf("Model should have turnWindow initialized")
    }
}
```

- [ ] **Step 2: Replace turn slice with TurnWindow**

```go
// In model.go, replace:
//   turns []Turn
// With:
//   turnWindow *TurnWindow
//   turnCache  *TurnCache

type Model struct {
    // ... existing fields ...
    turnWindow *TurnWindow  // NEW
    turnCache  *TurnCache   // NEW
    // ... rest ...
}
```

- [ ] **Step 3: Initialize TurnWindow in Model**

```go
// In initialization code (main.go or NewModel):
archiveDir := filepath.Join(os.Getenv("HOME"), ".rubichan", "archive")
cache := tui.NewTurnCache(archiveDir, sessionID, 50)
m.turnWindow = tui.NewTurnWindow(cache)
m.turnCache = cache
```

- [ ] **Step 4: Update View() to use TurnWindow**

```go
// In Model.View(), replace turn rendering with:
rendered, err := m.turnWindow.RenderVisible(m.turnRenderer)
if err != nil {
    rendered = "Render error: " + err.Error()
}
m.viewport.SetContent(rendered)
```

- [ ] **Step 5: Add archival hook in Update()**

```go
// In Model.Update() when turn completes:
func (m *Model) archiveOldTurns() {
    if m.turnCount%10 == 0 {  // archive every 10 turns
        m.turnCache.ArchiveOldTurns(m.turnCount - 50)
    }
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/tui -v
go test ./cmd/rubichan -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/update.go
git commit -m "[BEHAVIORAL] Integrate TurnWindow into Model"
```

---

### Task 12: Final Phase D.2 Testing

**Files:**
- None (testing only)

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -v
go test -cover ./internal/tui | grep -E "(turnwindow|turncache|virtualscroll)"
```

- [ ] **Step 2: Verify memory efficiency**

```bash
# Create a session with many turns and check memory usage
# (Manual testing or integration test)
```

- [ ] **Step 3: Verify archive persistence**

```bash
# Check that archive directory structure is created correctly
ls -la ~/.rubichan/archive/
```

- [ ] **Step 4: Build clean**

```bash
go build ./cmd/rubichan
```

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "[STRUCTURAL] Phase D.2 complete: Virtual scrolling with archival"
```

---

## Summary

**Phase D.1 (Tasks 1-7):** TurnRenderer refactoring
- ~800 LOC total (code + tests)
- Decouples rendering from Model state
- 100% test coverage
- Zero UI changes

**Phase D.2 (Tasks 8-12):** Virtual scrolling
- ~1600 LOC total (code + tests)  
- O(k) rendering instead of O(n)
- Bounded memory with archival
- Persistent archive across restarts

**Total:** 12 focused tasks, frequent commits, comprehensive test coverage

