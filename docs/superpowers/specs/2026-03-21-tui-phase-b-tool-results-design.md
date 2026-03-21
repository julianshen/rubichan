# TUI Phase B: Tool Result Improvements — Design Spec

> **Date:** 2026-03-21 · **Status:** Proposed · **Depends on:** Phase A (merged)

## Goal

Improve how tool results are displayed — make truncated output expandable, differentiate tool types visually, and make the status bar responsive to terminal width.

## Changes

### B1: Expandable Truncated Tool Results

**Problem:** When tool output exceeds 20 lines, `[N more lines]` is shown but there's no way to view the full output without re-running the tool.

**Design:**
- Add `Expanded bool` field to `CollapsibleToolResult` (already exists as `Collapsed`)
- When collapsed: show header only (current behavior)
- When expanded but content > 20 lines: show truncated with `[N more lines — press e to expand]`
- When fully expanded: show all content (no truncation)
- `Ctrl+T` toggles collapse/expand (existing). Add `e` key when a tool result is "focused" (cursor near it) to toggle full expansion
- Alternative: `Ctrl+E` expands the most recent tool result to full

**Files:** `internal/tui/toolbox.go`, `internal/tui/update.go`

### B2: Tool-Type Visual Differentiation

**Problem:** All tool results look identical except for error borders. File reads, shell commands, search results all use the same box style.

**Design:**
- Add tool-type icon prefix to `CollapsibleToolResult` header:
  - Shell/exec: `$ ` prefix, command in monospace
  - File read/write: `📄 ` prefix + path
  - Search/grep: `🔍 ` prefix + match count badge
  - Process: `⚙ ` prefix
  - Task/subagent: `🔄 ` prefix
  - Default: current behavior
- Add exit code display for shell results: `[exit 0]` or `[exit 1]` in header
- Border color subtle variation: shell=default, file=dim, search=info, error=red (existing)

**Files:** `internal/tui/toolbox.go`, `internal/tui/styles.go`

### B3: Responsive Status Bar

**Problem:** Status bar packs 7-9 segments into one line. On narrow terminals (<80 cols), it overflows.

**Design:**
- Priority tiers for segments:
  - **Always show:** model name, turn count
  - **Show if width > 60:** token usage, cost
  - **Show if width > 80:** git branch, elapsed
  - **Show if width > 100:** wiki stage, skills, subagent
- Calculate total segment width, drop lowest-priority segments until it fits
- Status bar `View()` accepts width parameter (already has `s.width`)

**Files:** `internal/tui/statusbar.go`

## Testing Strategy

- Unit tests for tool-type icon selection (table-driven)
- Unit tests for status bar priority truncation at various widths
- Unit tests for expandable tool result state transitions
