# TUI Phase C: Architectural Rewrites — Design Spec

> **Date:** 2026-03-21 · **Status:** Proposed · **Depends on:** Phase B (merged)

## Goal

Rewrite the TUI's content rendering architecture for performance, introduce an undo UI surface, create a generic overlay system, and share more logic between TUI and plain interactive modes.

## Changes

### C1: Segment-Based Content Model

**Problem:** The viewport holds the entire conversation as one string. `viewportContent()` runs N `strings.Replace` calls for tool result placeholders on every render. For long sessions (50+ tool calls), this causes O(N*M) render scaling.

**Design:**
```go
type SegmentType int
const (
    SegmentText SegmentType = iota
    SegmentToolResult
    SegmentDiffSummary
)

type ContentSegment struct {
    Type    SegmentType
    Text    string                 // pre-rendered for SegmentText
    Result  *CollapsibleToolResult // for SegmentToolResult
    Summary *DiffSummaryData       // for SegmentDiffSummary
}

type ContentBuffer struct {
    segments []ContentSegment
    dirty    map[int]bool // segments needing re-render
}

func (b *ContentBuffer) Render(width int) string  // only re-renders dirty segments
func (b *ContentBuffer) AppendText(text string)
func (b *ContentBuffer) AppendToolResult(r CollapsibleToolResult)
func (b *ContentBuffer) ToggleToolResult(id int)   // marks only that segment dirty
```

- Replaces `strings.Builder` content buffer + placeholder system
- Tool result toggle is O(1) — only re-renders the toggled segment
- `Render()` caches segment strings, concatenates them

**Migration:** The `content strings.Builder` in Model gets replaced by `ContentBuffer`. All writes go through `ContentBuffer.Append*()`. `viewportContent()` calls `ContentBuffer.Render()`.

**Files:** New `internal/tui/contentbuffer.go`, modify `internal/tui/model.go`, `update.go`, `view.go`

### C2: Virtual Scrolling (Bounded Memory)

**Problem:** Content grows unbounded in memory. Hour-long sessions accumulate megabytes.

**Design:**
- `ContentBuffer` maintains a window: keep last N segments in memory (e.g., 500)
- Older segments evicted but their byte counts tracked for scroll position math
- On scroll-up past the window, reconstruct from session event log (already persisted)
- Viewport height + lookahead buffer determines visible window
- Only reconstruct on-demand (user scrolls up past window boundary)

**Depends on:** C1 (segment model)

**Files:** `internal/tui/contentbuffer.go`

### C3: Generic Overlay System

**Problem:** Four overlay types (approval, config, wiki, completion) each have separate routing in `Update()`. Adding new overlays requires modifying the Update switch in multiple places.

**Design:**
```go
type Overlay interface {
    Update(msg tea.Msg) (tea.Model, tea.Cmd)
    View() string
    Done() bool
    Result() any
}
```

- Model holds `activeOverlay Overlay` (nil when no overlay)
- `Update()` checks `activeOverlay != nil` first, delegates entirely
- On `Done()`, process `Result()` and set `activeOverlay = nil`
- Approval, Config, Wiki, ModelPicker all implement `Overlay`
- Completion overlays remain separate (they coexist with normal input)

**Files:** New `internal/tui/overlay.go`, modify `internal/tui/model.go`, `update.go`, `view.go`, `approval.go`, `configform.go`, `wiki_command.go`

### C4: Undo/Rewind UI

**Problem:** The checkpoint system (`internal/checkpoint/`) exists but has no TUI surface.

**Design:**
- `/undo` slash command shows the last 5 file modifications as a numbered list
- User selects a checkpoint to restore (1-5 or `all`)
- Confirmation prompt before restoring
- Uses existing `checkpoint.Manager.Restore()` API
- Shows diff preview of what will be reverted

**Files:** New `internal/tui/undo.go`, modify `internal/tui/model.go`, new command in `internal/commands/`

### C5: Shared TurnRenderer for TUI/Plain Modes

**Problem:** Plain interactive mode reimplements approval prompting, tool output formatting, session event emission.

**Design:**
```go
type TurnRenderer interface {
    WriteText(text string)
    WriteToolCall(name, args string)
    WriteToolResult(name, content string, isError bool)
    WriteError(err string)
    WriteDone(stats TurnStats)
}
```

- TUI Model implements `TurnRenderer` (writes to ContentBuffer)
- Plain host implements `TurnRenderer` (writes to stdout)
- `handleTurnEvent()` logic extracted into shared function that calls TurnRenderer methods
- Approval flow remains mode-specific (overlay vs stdin prompt)

**Files:** New `internal/tui/turnrenderer.go`, modify `internal/tui/update.go`, `cmd/rubichan/plain_interactive.go`

## Ordering

1. **C1** first (segment model) — foundation for everything else
2. **C3** second (overlay system) — simplifies C4
3. **C5** third (shared renderer) — reduces code before C2
4. **C2** fourth (virtual scrolling) — requires stable C1
5. **C4** last (undo UI) — independent feature using C3 overlay

## Risk

- C1 is a large refactor touching the core rendering path. Requires comprehensive test coverage of the new ContentBuffer before migrating Model.
- C2's on-demand reconstruction from event log adds complexity. May be deferred if C1 alone solves the performance issue for realistic session lengths.
- C3 must handle overlay lifecycle carefully — the approval overlay blocks an agent goroutine via channel, which is different from form overlays.

## Testing Strategy

- ContentBuffer: unit tests for append, render, toggle, dirty tracking, segment eviction
- Overlay interface: mock overlay tests for lifecycle (Update→Done→Result)
- TurnRenderer: shared test cases run against both TUI and plain implementations
- Undo: integration test with mock checkpoint manager
