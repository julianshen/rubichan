# Design: TUI Phase D — Shared TurnRenderer + Virtual Scrolling

**Date:** 2026-04-06  
**Status:** Design approved, ready for implementation  
**Author:** Julian Shen + Claude

---

## Executive Summary

TUI Phase D improves performance and code quality through two integrated features:

1. **Phase D.1: Shared TurnRenderer** — Refactor turn rendering into a reusable, testable component that eliminates code duplication across MarkdownRenderer, ToolBoxRenderer, and Model.View().
2. **Phase D.2: Virtual Scrolling** — Implement memory-efficient turn storage with on-demand archival and caching, enabling sessions with 500+ turns while maintaining constant rendering time and bounded memory usage.

Both phases share the same architectural foundation: turning is separated from state management, making rendering testable and optimizable.

---

## Goals & Success Criteria

**Goals:**
- Eliminate turn rendering code duplication
- Enable large sessions (500+ turns) with bounded memory (~10MB)
- Maintain constant rendering time regardless of transcript size
- Improve code testability by decoupling rendering from Model
- Maintain 100% UI compatibility (no visible behavior changes)

**Success Criteria:**
- ✅ Phase D.1: TurnRenderer produces identical output to current Model.View() for all turn states
- ✅ Phase D.1: 15+ unit tests passing, 100% coverage of TurnRenderer
- ✅ Phase D.2: Sessions with 500+ turns use <15MB memory
- ✅ Phase D.2: Rendering time stays O(k) where k = visible turns (~constant)
- ✅ Phase D.2: Scrolling to archived turns loads within 100ms
- ✅ Phase D.2: Archive persists across process restarts
- ✅ Both phases: Zero regressions in existing TUI functionality
- ✅ Both phases: All new code has >90% test coverage

---

## Phase D.1: Shared TurnRenderer

### Architecture

#### New Types

**File:** `internal/tui/turnrenderer.go`

```go
// TurnRenderer encapsulates all turn rendering logic.
// It is a pure function: takes immutable turn data, returns rendered string.
// Zero state, zero side effects — easy to test and parallelize.
type TurnRenderer struct{}

// RenderOptions controls rendering behavior (streaming state, width, etc.)
type RenderOptions struct {
    Width           int           // viewport width in characters
    IsStreaming     bool          // whether turn is still streaming (affects UI state)
    CollapsedTools  bool          // whether tool results are collapsed
    HighlightError  bool          // highlight error messages in red
    MaxToolLines    int           // truncate tool output beyond this many lines (0 = no limit)
}

// Turn represents the complete state of a rendered turn (immutable).
// Extracted from Model's streaming state for rendering.
type Turn struct {
    ID              string              // unique turn identifier (for caching)
    AssistantText   string              // raw assistant message text
    ThinkingText    string              // raw thinking block content (empty if not present)
    ToolCalls       []RenderedToolCall  // all tool calls in this turn
    Status          string              // "streaming", "done", or "error"
    ErrorMsg        string              // error message (if status == "error")
    StartTime       time.Time           // when turn started (for elapsed time display)
}

// RenderedToolCall represents a single tool invocation and its result.
type RenderedToolCall struct {
    ID              string    // tool call ID
    Name            string    // tool name (e.g., "file", "shell")
    Args            string    // formatted tool arguments
    Result          string    // raw tool output
    IsError         bool      // whether this tool call failed
    Collapsed       bool      // true = show collapsed summary; false = show full output
    LineCount       int       // total lines in Result (before truncation)
}

// Render produces the complete text representation of a turn.
// This is the main entry point used by Model.View().
func (r *TurnRenderer) Render(ctx context.Context, turn *Turn, opts RenderOptions) (string, error)
```

#### Data Flow

```
Model.Update(event agent.TurnEvent)
  → Update internal turn state (assistant text, tool results, etc.)
  → Mark viewport dirty

Model.View() string
  → Extract turn state into immutable Turn struct via extractTurnForRendering()
  → Call turnRenderer.Render(turn, opts)
  → TurnRenderer reads Turn, produces formatted string
  → Feed string to viewport
  → Return final UI
```

#### Rendering Pipeline

TurnRenderer.Render() follows this sequence:

1. **Thinking block** (if present and visible)
   - Format with header "🧠 Thinking..."
   - Wrap in Lipgloss box
   
2. **Assistant message**
   - Render with MarkdownRenderer (existing)
   - Apply syntax highlighting via Glamour (existing)
   
3. **Tool calls** (in order)
   - For each tool call:
     - If collapsed: Show one-line summary "▶ tool_name(...) — N lines [status]"
     - If expanded: Show tool call + result in bordered box
   - Use ToolBoxRenderer for formatting (existing)
   
4. **Status indicator**
   - If streaming: Show spinner + elapsed time
   - If done: Show "Done" indicator
   - If error: Show error message in red

#### Integration with Model

**Model struct changes (minimal):**
```go
type Model struct {
    // ... existing fields ...
    turnRenderer *TurnRenderer  // NEW: add renderer instance
    // ... rest unchanged ...
}

// In main.go during initialization:
m.turnRenderer = &TurnRenderer{}
```

**View() method changes:**
```go
func (m *Model) View() string {
    // Extract current turn data into immutable struct
    turn := m.extractTurnForRendering()
    
    // Render turn
    opts := RenderOptions{
        Width: m.width,
        IsStreaming: m.state == StateStreaming,
        CollapsedTools: m.diffExpanded,  // reuse existing flag
        HighlightError: m.state == StateStreaming || m.state == StateAwaitingApproval,
    }
    rendered, err := m.turnRenderer.Render(context.Background(), turn, opts)
    if err != nil {
        // Fallback: render current state (should never happen in practice)
        rendered = "Render error: " + err.Error()
    }
    
    // Feed to viewport (existing logic)
    m.viewport.SetContent(rendered)
    
    // Return final UI with overlays, status bar, etc.
    return m.renderFinalUI()
}

// extractTurnForRendering extracts immutable Turn struct from Model's state
func (m *Model) extractTurnForRendering() *Turn {
    // Implementation: copy current turn state into Turn struct
    // This is a private helper, ~30-50 lines
}
```

### Rendering Output Example

```
╭─ Assistant Message ─────────────────────╮
│ The codebase uses Bubble Tea for TUI    │
│ and Lipgloss for styling. The main      │
│ Model type is defined in model.go.      │
│                                         │
│ [2 more lines]                          │
╰─────────────────────────────────────────╯

▶ file(path="internal/tui/model.go") — 376 lines [read]

▼ shell(command="wc -l internal/tui/model.go")
╭─────────────────────────────────────────╮
│ 376 internal/tui/model.go               │
╰─────────────────────────────────────────╯

⠋ Thinking... 2.3s · ~310 tokens
```

### Testing Strategy

**Unit tests in `internal/tui/turnrenderer_test.go`:**

```go
func TestTurnRenderer_RendersAssistantMessage(t *testing.T)
    // Turn with assistant text produces formatted output

func TestTurnRenderer_RendersThinkingBlock(t *testing.T)
    // Thinking section rendered with 🧠 header when present
    
func TestTurnRenderer_HidesThinkingWhenEmpty(t *testing.T)
    // Empty thinking block not rendered
    
func TestTurnRenderer_RendersToolCallCollapsed(t *testing.T)
    // Tool call with Collapsed=true shows one-line summary only
    
func TestTurnRenderer_RendersToolCallExpanded(t *testing.T)
    // Tool call with Collapsed=false shows full output in bordered box
    
func TestTurnRenderer_ErrorHighlighting(t *testing.T)
    // Error tool result shown in red border
    
func TestTurnRenderer_StatusStreaming(t *testing.T)
    // Streaming turn shows spinner; done turn shows checkmark
    
func TestTurnRenderer_TextWrapping(t *testing.T)
    // Output respects RenderOptions.Width
    
func TestTurnRenderer_MultipleToolCalls(t *testing.T)
    // Turn with multiple tools renders each independently
    
func TestTurnRenderer_LargeToolOutput(t *testing.T)
    // Tool output with 1000+ lines truncates at MaxToolLines
    
func TestTurnRenderer_EmptyTurn(t *testing.T)
    // Turn with no content renders without panic
    
func TestTurnRenderer_SpecialCharacters(t *testing.T)
    // Unicode, ANSI escapes, control chars handled safely
```

### Files Changed

| File | Change | Est. LOC |
|------|--------|----------|
| `internal/tui/turnrenderer.go` | New: TurnRenderer, Turn, RenderedToolCall, RenderOptions types | 300 |
| `internal/tui/turnrenderer_test.go` | New: unit tests for TurnRenderer | 400 |
| `internal/tui/model.go` | Add turnRenderer field; minimal changes to View() | 50 |
| `internal/tui/update.go` | Add extractTurnForRendering() helper | 60 |

**Total for D.1: ~810 LOC (code + tests)**

---

## Phase D.2: Virtual Scrolling

### Problem Statement

Current implementation (after D.1):
- All turns kept in memory
- All turns rendered every frame
- Sessions with 100+ turns: O(n) rendering time, O(n) memory

**Performance impact:**
- 100 turns: ~500ms per frame (unacceptable)
- 500 turns: laggy/unusable
- Large sessions consume 50+MB memory

### Architecture

#### New Types

**File:** `internal/tui/turnwindow.go`

```go
// TurnWindow manages memory-efficient access to turns.
// It maintains a sliding window of in-memory turns and archives old ones to disk.
type TurnWindow struct {
    cache          *TurnCache        // in-memory + archived turn storage
    renderPool     *TurnRenderPool   // caches rendered turn strings
    visibleStart   int               // index of first visible turn
    visibleEnd     int               // index of last visible turn
    mu             sync.RWMutex
}

// TurnCache manages turn storage with automatic archival.
// Recent turns stay in memory; old turns move to disk.
type TurnCache struct {
    mu              sync.RWMutex
    turns           map[int]*Turn     // in-memory turns (index → turn)
    archivedPaths   map[int]string    // archived turn paths (index → file path)
    maxMemoryTurns  int               // keep last N turns in memory (default: 50)
    archiveDir      string            // directory for archived turns (~/.rubichan/archive/)
    sessionID       string            // session identifier for archive paths
    lru             *LRUEviction      // track memory usage; evict old loaded turns
}

// TurnRenderPool caches rendered turn strings with invalidation.
type TurnRenderPool struct {
    mu              sync.RWMutex
    rendered        map[int]string    // index → cached rendered string
    dirty           set[int]          // indices needing re-render
}

// LRUEviction tracks which archived turns are loaded, evicts on memory pressure.
type LRUEviction struct {
    mu              sync.Mutex
    loadOrder       []int             // order turns were loaded
    maxLoadedTurns  int               // max archived turns to keep loaded (default: 10)
}

// Archive file format (JSON)
type ArchivedTurn struct {
    ID              string              `json:"id"`
    AssistantText   string              `json:"assistant_text"`
    ThinkingText    string              `json:"thinking_text"`
    ToolCalls       []RenderedToolCall  `json:"tool_calls"`
    Status          string              `json:"status"`
    ErrorMsg        string              `json:"error_msg"`
    StartTime       time.Time           `json:"start_time"`
    ArchivedAt      time.Time           `json:"archived_at"`
}

// Main API
func (w *TurnWindow) AddTurn(index int, turn *Turn)
    // Add new turn to cache (in-memory by default)

func (w *TurnWindow) GetTurn(index int) (*Turn, error)
    // Get turn by index; load from archive if needed

func (w *TurnWindow) UpdateVisibleRange(scrollPos, viewportHeight int)
    // Update which turns are visible; trigger archival if needed

func (w *TurnWindow) RenderVisible(renderer *TurnRenderer) (string, error)
    // Render only visible turns; load from archive on-demand

func (c *TurnCache) ArchiveOldTurns(beforeIndex int) error
    // Move turns before index to disk

func (c *TurnCache) LoadFromArchive(index int) (*Turn, error)
    // Load archived turn from disk

func (c *TurnCache) CurrentMemoryUsage() int
    // Estimate memory used by loaded turns
```

#### Data Flow

```
Model.Update(event agent.TurnEvent)
  → Append turn to turnWindow
  → If turnCount % 10 == 0:
      → turnWindow.ArchiveOldTurns(turnCount - 50)  // keep 50 in memory
  → Mark viewport dirty

Model.View() string
  → Compute viewport scroll position
  → turnWindow.UpdateVisibleRange(scrollPos, height)
      → Identifies which turns are visible (typically 5-10)
      → Loads turns from memory/archive as needed
      → Invalidates render cache for changed turns
  → rendered := turnWindow.RenderVisible(turnRenderer)
      → For each visible turn:
          → If cached: return cached string
          → Else: render + cache
      → Concatenate rendered turns
  → Feed to viewport
  → Return final UI

Background/On-Idle
  → Periodically invoke turnCache.ArchiveOldTurns()
  → Move turns to disk; free memory
```

#### Memory Strategy

**Thresholds:**
- **In-memory limit:** Keep last 50 turns in RAM
- **Loaded archive limit:** Keep last 10 archived turns loaded (for fast re-scroll)
- **Archive location:** `~/.rubichan/archive/{sessionID}/turn-{index}.json`

**Archive trigger:**
- Every 10 new turns, or
- When total memory exceeds threshold, or
- On idle (low priority background task)

**Example flow (session with 150 turns):**
```
Turn 1-50:    In memory (recent)
Turn 51-100:  Archived to disk
Turn 101-110: Archived to disk + loaded (LRU cache)
Turn 111-150: Archived to disk
```

If user scrolls to turn 75:
```
Turn 1-50:    In memory
Turn 51-75:   Loaded from archive (added to LRU)
Turn 101-110: Still loaded (newer in LRU order)
Turn 76-100:  Remain archived (not loaded)
```

#### Rendering Optimization

**Render pool caching:**

```go
// When rendering visible turns:
for _, idx := range visibleTurns {
    if cached := pool.Get(idx); cached != "" {
        output += cached
        continue
    }
    
    turn := cache.GetTurn(idx)
    rendered := renderer.Render(turn, opts)
    pool.Set(idx, rendered)
    output += rendered
}
```

**Cache invalidation:**
- Tool result collapse state changes → invalidate that turn
- Turn finishes streaming → invalidate (status changes to "done")
- Scroll outside visible range → can discard (LRU cleanup)

**Performance impact:**
- Before: O(n) rendering per frame (all turns)
- After: O(k) rendering per frame (k = visible turns ≈ 5-10)
- For 500 turns: 50x speedup (500/10)

#### Integration with Model

**Model struct changes:**
```go
type Model struct {
    // ... remove: turns []Turn
    turnWindow     *TurnWindow        // NEW: replace turn slice
    
    // ... keep existing fields ...
}

// During initialization:
m.turnWindow = NewTurnWindow(sessionID, archiveDir)
m.turnWindow.SetRenderer(m.turnRenderer)

// On shutdown:
defer m.turnWindow.Close()  // cleanup archive
```

**Update handler changes:**
```go
func (m *Model) handleTurnEvent(event agent.TurnEvent) {
    switch event.Type {
    case agent.TurnStarted:
        turn := &Turn{ /* ... */ }
        m.turnWindow.AddTurn(m.turnCount, turn)
        m.turnCount++
        
        // Archive every 10 turns
        if m.turnCount%10 == 0 {
            m.turnWindow.ArchiveOldTurns(m.turnCount - 50)
        }
        
    case agent.TextDelta:
        // Update turn in cache (same turn, streaming)
        turn := m.turnWindow.MustGetTurn(m.turnCount - 1)
        turn.AssistantText += event.Content
        
    // ... etc ...
    }
}
```

**View changes:**
```go
func (m *Model) View() string {
    // Update viewport based on scroll position
    m.turnWindow.UpdateVisibleRange(
        m.viewport.YOffset,
        m.viewport.Height,
    )
    
    // Render only visible turns
    rendered, err := m.turnWindow.RenderVisible(m.turnRenderer)
    if err != nil {
        rendered = "Render error: " + err.Error()
    }
    
    m.viewport.SetContent(rendered)
    
    // ... rest of View() unchanged ...
}
```

### Archive Persistence

**Directory structure:**
```
~/.rubichan/archive/
  {sessionID}/
    turn-0.json
    turn-1.json
    turn-50.json
    metadata.json  (session info for recovery)
```

**Metadata file** (`metadata.json`):
```json
{
  "sessionID": "session-abc123",
  "startTime": "2026-04-06T12:00:00Z",
  "totalTurns": 250,
  "lastArchivedAt": "2026-04-06T12:45:30Z",
  "nextTurnIndex": 250
}
```

**Recovery on restart:**
- Load metadata to know how many turns exist
- Archive remains on disk; loaded on-demand when user scrolls
- No loss of state

### Cleanup

**On app exit:**
- Keep archive (user may restart and want history)
- Mark session as closed in metadata

**Manual cleanup:**
- `rubichan clean-archive --keep-sessions 5` → remove archives older than 5 most recent sessions
- `rubichan clean-archive --age 30d` → remove archives older than 30 days

### Testing Strategy

**TurnWindow tests in `internal/tui/turnwindow_test.go`:**

```go
func TestTurnWindow_AddTurnInMemory(t *testing.T)
    // Recent turns kept in memory

func TestTurnWindow_ComputesVisibleRange(t *testing.T)
    // Correct indices for given scroll position

func TestTurnWindow_LoadsTurnsInVisibleRange(t *testing.T)
    // Only loads turns that will be rendered

func TestTurnWindow_RenderVisibleReturnsOnlyVisible(t *testing.T)
    // Output contains only visible turns, in order

func TestTurnWindow_ArchivesOldTurns(t *testing.T)
    // Moves turns before threshold to disk

func TestTurnWindow_LoadsFromArchiveOnDemand(t *testing.T)
    // Gets archived turn; loads from disk

func TestTurnWindow_LRUEvictionOnMemoryPressure(t *testing.T)
    // Evicts old archived turns when memory limit exceeded

func TestTurnWindow_RenderPoolCaching(t *testing.T)
    // Cached render returned without re-rendering

func TestTurnWindow_RenderPoolInvalidation(t *testing.T)
    // Cache invalidated when turn changes
```

**TurnCache tests in `internal/tui/turncache_test.go`:**

```go
func TestTurnCache_KeepsRecentInMemory(t *testing.T)
    // Last maxMemoryTurns stay in RAM

func TestTurnCache_WritesToArchiveDirectory(t *testing.T)
    // Archived turns appear in ~/.rubichan/archive/

func TestTurnCache_ReadsFromArchive(t *testing.T)
    // LoadFromArchive returns correct turn

func TestTurnCache_ArchiveFormatIsValid(t *testing.T)
    // Archive JSON unmarshals to Turn struct

func TestTurnCache_HandlesCorruptedArchive(t *testing.T)
    // Returns error gracefully; doesn't crash
```

**Integration tests in `internal/tui/virtualscroll_test.go`:**

```go
func TestLargeSession_MemoryBounded(t *testing.T)
    // Session with 500 turns uses <15MB memory

func TestLargeSession_RenderingConstantTime(t *testing.T)
    // Rendering time doesn't increase with total turns

func TestLargeSession_ScrollingSmooth(t *testing.T)
    // Scrolling through archived turns < 100ms

func TestArchivePersistence_SurvivesRestart(t *testing.T)
    // Save session; restart; verify turns still accessible

func TestArchiveCleanup_RemoveOldArchives(t *testing.T)
    // Cleanup command removes old session archives
```

### Files Changed

| File | Change | Est. LOC |
|------|--------|----------|
| `internal/tui/turnwindow.go` | New: TurnWindow, TurnCache, TurnRenderPool | 400 |
| `internal/tui/turncache.go` | New: archive I/O, LRU eviction | 250 |
| `internal/tui/turnwindow_test.go` | New: TurnWindow tests | 300 |
| `internal/tui/turncache_test.go` | New: TurnCache tests | 250 |
| `internal/tui/virtualscroll_test.go` | New: integration tests | 200 |
| `internal/tui/model.go` | Replace turn slice with TurnWindow; wire archival | 100 |
| `internal/tui/update.go` | Call ArchiveOldTurns periodically | 30 |
| `cmd/rubichan/main.go` | Add archive cleanup command | 50 |

**Total for D.2: ~1580 LOC (code + tests)**

---

## Phase D Summary

| Phase | Component | Files | Tests | LOC | Purpose |
|-------|-----------|-------|-------|-----|---------|
| **D.1** | TurnRenderer | +1 new, modify 2 | 15+ | ~810 | Refactor: decouples rendering from Model state |
| **D.2** | Virtual Scrolling | +3 new, modify 2 | 25+ | ~1580 | Optimize: O(k) rendering, bounded memory, archival |
| **Total** | | +4 new, modify 4 | 40+ | ~2390 | Rendering quality + performance |

**Dependencies:** D.2 depends on D.1 (uses TurnRenderer API)  
**Merge strategy:** Ship D.1 first (standalone), then D.2 (builds on D.1)  
**Timeline:** ~2-3 weeks per phase (depends on testing rigor)

---

## Quality Commitments

**Test Coverage:**
- Phase D.1: >90% coverage of TurnRenderer
- Phase D.2: >90% coverage of TurnWindow, TurnCache, TurnRenderPool

**Performance Validation:**
- Benchmark rendering: before vs after (should be 10-50x on large sessions)
- Memory profiling: before vs after (should be O(1) instead of O(n))
- Real session testing: 500+ turn session on actual machine

**Backward Compatibility:**
- Zero UI changes (output identical to Phase D.0)
- Archive format versioned for future migration
- Graceful degradation if archive corrupted (load from memory only)

---

## Future Enhancements (Out of Scope)

- Parallel rendering of multiple turns (could speed up even further)
- Compression for archived turns (reduce disk space)
- Archive migration/cleanup policies (user-configurable retention)
- Turn search across archived sessions
- Export session as PDF/HTML with archive included

