# TUI Phase B: Tool Result Improvements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve tool result display with expandable truncated output, per-tool-type visual differentiation, and a responsive status bar that adapts to terminal width.

**Architecture:** Three independent features layered on the existing `CollapsibleToolResult` and `StatusBar` types. B1 adds a tri-state expand model (collapsed → truncated → full). B2 adds a `ToolType` enum with icon/color mapping. B3 adds priority-based segment elision to `StatusBar.View()`.

**Tech Stack:** Go, Bubble Tea v2 (`charmbracelet/bubbletea`), Lipgloss (`charmbracelet/lipgloss`), testify/assert

**Spec:** `docs/superpowers/specs/2026-03-21-tui-phase-b-tool-results-design.md`

---

## File Structure

### New Files
- `internal/tui/tooltype.go` — `ToolType` enum, `ClassifyTool()`, icon/color lookups
- `internal/tui/tooltype_test.go` — Tests for tool type classification and rendering

### Modified Files
- `internal/tui/toolbox.go` — Add `Expanded` field to `CollapsibleToolResult`, tri-state rendering, tool-type icon in header
- `internal/tui/toolbox_test.go` — Tests for tri-state expand, icon prefixes, exit code display
- `internal/tui/statusbar.go` — Priority-based segment elision based on terminal width
- `internal/tui/statusbar_test.go` — Tests for segment visibility at various widths
- `internal/tui/update.go` — Wire `Ctrl+E` key for per-result full expansion, pass tool type to `CollapsibleToolResult`
- `internal/tui/styles.go` — Subtle border color variants per tool type (if needed)

---

## Task 1: Tool Type Classification

Introduce a `ToolType` enum to classify tool names into categories (shell, file, search, process, subagent). This is a pure data module with no UI coupling — foundation for B2.

**Files:**
- Create: `internal/tui/tooltype.go`
- Create: `internal/tui/tooltype_test.go`

- [ ] **Step 1.1: Write failing test — ClassifyTool maps known tool names**

```go
// internal/tui/tooltype_test.go
package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     ToolType
	}{
		{"shell tool", "shell", ToolTypeShell},
		{"bash tool", "bash", ToolTypeShell},
		{"exec tool", "exec", ToolTypeShell},
		{"file_read", "file_read", ToolTypeFile},
		{"file_write", "file_write", ToolTypeFile},
		{"patch", "patch", ToolTypeFile},
		{"grep tool", "grep", ToolTypeSearch},
		{"code_search", "code_search", ToolTypeSearch},
		{"glob", "glob", ToolTypeSearch},
		{"process", "process", ToolTypeProcess},
		{"task", "task", ToolTypeSubagent},
		{"unknown", "custom_tool", ToolTypeDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifyTool(tt.toolName))
		})
	}
}
```

- [ ] **Step 1.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestClassifyTool -v`
Expected: FAIL — `ToolType` and `ClassifyTool` undefined

- [ ] **Step 1.3: Write minimal implementation**

```go
// internal/tui/tooltype.go
package tui

// ToolType classifies tool invocations for visual differentiation.
// Uses ASCII-safe icon characters instead of spec emojis for reliable
// terminal rendering across all emulators.
type ToolType int

const (
	ToolTypeDefault  ToolType = iota
	ToolTypeShell             // shell, bash, exec
	ToolTypeFile              // file_read, file_write, patch, edit
	ToolTypeSearch            // grep, code_search, glob, find
	ToolTypeProcess           // process, spawn
	ToolTypeSubagent          // task (subagent dispatch)
)

// ClassifyTool returns the ToolType for a given tool name.
func ClassifyTool(name string) ToolType {
	switch name {
	case "shell", "bash", "exec":
		return ToolTypeShell
	case "file_read", "file_write", "patch", "edit", "write":
		return ToolTypeFile
	case "grep", "code_search", "glob", "find":
		return ToolTypeSearch
	case "process", "spawn":
		return ToolTypeProcess
	case "task":
		return ToolTypeSubagent
	default:
		return ToolTypeDefault
	}
}
```

- [ ] **Step 1.4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestClassifyTool -v`
Expected: PASS

- [ ] **Step 1.5: Write failing test — ToolTypeIcon returns correct icons**

```go
func TestToolTypeIcon(t *testing.T) {
	tests := []struct {
		name string
		tt   ToolType
		want string
	}{
		{"shell", ToolTypeShell, "$ "},
		{"file", ToolTypeFile, "~ "},
		{"search", ToolTypeSearch, "? "},
		{"process", ToolTypeProcess, "* "},
		{"subagent", ToolTypeSubagent, "> "},
		{"default", ToolTypeDefault, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.tt.Icon())
		})
	}
}
```

- [ ] **Step 1.6: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestToolTypeIcon -v`
Expected: FAIL — `Icon()` method undefined

- [ ] **Step 1.7: Write minimal implementation**

Add to `internal/tui/tooltype.go`:

```go
// Icon returns a short prefix icon for display in tool result headers.
// Uses ASCII-safe characters for terminal compatibility.
func (tt ToolType) Icon() string {
	switch tt {
	case ToolTypeShell:
		return "$ "
	case ToolTypeFile:
		return "~ "
	case ToolTypeSearch:
		return "? "
	case ToolTypeProcess:
		return "* "
	case ToolTypeSubagent:
		return "> "
	default:
		return ""
	}
}
```

- [ ] **Step 1.8: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestToolTypeIcon -v`
Expected: PASS

- [ ] **Step 1.9: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: All PASS

- [ ] **Step 1.10: Commit**

```bash
git add internal/tui/tooltype.go internal/tui/tooltype_test.go
git commit -m "[BEHAVIORAL] add ToolType classification for tool result visual differentiation"
```

---

## Task 2: Tool-Type Icons in CollapsibleToolResult Headers (B2)

Wire `ToolType` into `CollapsibleToolResult` so each result header shows its category icon.

**Files:**
- Modify: `internal/tui/toolbox.go` (add `ToolType` field, update `Render()`)
- Modify: `internal/tui/toolbox_test.go` (new tests)
- Modify: `internal/tui/update.go` (set `ToolType` when creating results)

- [ ] **Step 2.1: Write failing test — collapsed view shows icon prefix**

Add to `internal/tui/toolbox_test.go`:

```go
func TestCollapsibleToolResult_ShellIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="ls"`,
		Content:   "file1\nfile2",
		LineCount: 2,
		ToolType:  ToolTypeShell,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "$ ")
	assert.Contains(t, result, "▶")
}

func TestCollapsibleToolResult_FileIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `path="main.go"`,
		Content:   "package main",
		LineCount: 1,
		ToolType:  ToolTypeFile,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "~ ")
}

func TestCollapsibleToolResult_DefaultNoIcon(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "custom",
		Args:      "",
		Content:   "output",
		LineCount: 1,
		ToolType:  ToolTypeDefault,
		Collapsed: true,
	}
	result := cr.Render(r)
	// No icon prefix for default
	assert.NotContains(t, result, "$ ")
	assert.NotContains(t, result, "~ ")
	assert.NotContains(t, result, "? ")
}
```

- [ ] **Step 2.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run "TestCollapsibleToolResult_ShellIcon|TestCollapsibleToolResult_FileIcon|TestCollapsibleToolResult_DefaultNoIcon" -v`
Expected: FAIL — `ToolType` field doesn't exist on struct

- [ ] **Step 2.3: Write minimal implementation**

In `internal/tui/toolbox.go`, add `ToolType` field to `CollapsibleToolResult`:

```go
type CollapsibleToolResult struct {
	ID        int
	Name      string
	Args      string
	Content   string
	LineCount int
	IsError   bool
	Collapsed bool
	ToolType  ToolType  // tool category for visual differentiation
}
```

Update the `Render()` method to prepend the icon:

```go
func (c *CollapsibleToolResult) Render(r *ToolBoxRenderer) string {
	lineLabel := c.lineLabel()
	icon := c.ToolType.Icon()
	if c.Collapsed {
		return styleToolResultHeader.Render(fmt.Sprintf("▶ %s%s(%s)", icon, c.Name, c.Args)) +
			styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	}
	header := styleToolResultHeader.Render(fmt.Sprintf("▼ %s%s(%s)", icon, c.Name, c.Args)) +
		styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	return header + r.RenderToolResult(c.Name, c.Content, c.IsError)
}
```

- [ ] **Step 2.4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run "TestCollapsibleToolResult_ShellIcon|TestCollapsibleToolResult_FileIcon|TestCollapsibleToolResult_DefaultNoIcon" -v`
Expected: PASS

- [ ] **Step 2.5: Wire ToolType in update.go tool_result handler**

In `internal/tui/update.go`, in the `tool_result` case (around line 519), add `ToolType` to the `CollapsibleToolResult` initialization:

```go
cr := CollapsibleToolResult{
	ID:        m.nextToolResultID,
	Name:      resultName,
	Args:      args,
	Content:   resultContent,
	LineCount: lineCount,
	IsError:   isError,
	Collapsed: false,
	ToolType:  ClassifyTool(resultName),
}
```

- [ ] **Step 2.6: Run all TUI tests to ensure no regressions**

Run: `go test ./internal/tui/... -v`
Expected: All PASS

- [ ] **Step 2.7: Commit**

```bash
git add internal/tui/toolbox.go internal/tui/toolbox_test.go internal/tui/update.go
git commit -m "[BEHAVIORAL] add tool-type icon prefixes to CollapsibleToolResult headers"
```

---

## Task 3: Exit Code Display for Shell Results (B2)

Show `[exit 0]` or `[exit N]` in shell tool result headers.

**Files:**
- Modify: `internal/tui/toolbox.go` (add `ExitCode` field, update `lineLabel()`)
- Modify: `internal/tui/toolbox_test.go` (new tests)
- Modify: `internal/tui/update.go` (extract exit code from result)

- [ ] **Step 3.1: Write failing test — exit code shown in label**

Add to `internal/tui/toolbox_test.go`:

```go
func TestCollapsibleToolResult_ExitCodeDisplay(t *testing.T) {
	r := NewToolBoxRenderer(60)
	exitCode := 0
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="ls"`,
		Content:   "file1\nfile2",
		LineCount: 2,
		ToolType:  ToolTypeShell,
		ExitCode:  &exitCode,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "[exit 0]")
}

func TestCollapsibleToolResult_ExitCodeNonZero(t *testing.T) {
	r := NewToolBoxRenderer(60)
	exitCode := 1
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="make"`,
		Content:   "error: build failed",
		LineCount: 1,
		ToolType:  ToolTypeShell,
		ExitCode:  &exitCode,
		IsError:   true,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "[exit 1]")
}

func TestCollapsibleToolResult_ExitCodeWithTruncation(t *testing.T) {
	r := NewToolBoxRenderer(60)
	exitCode := 1
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="find /"`,
		Content:   strings.Repeat("line\n", 50),
		LineCount: 50,
		ToolType:  ToolTypeShell,
		ExitCode:  &exitCode,
		IsError:   true,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "50 lines (20 shown)")
	assert.Contains(t, result, "[exit 1]")
}

func TestCollapsibleToolResult_NoExitCodeForNonShell(t *testing.T) {
	r := NewToolBoxRenderer(60)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "file_read",
		Args:      `path="a.go"`,
		Content:   "content",
		LineCount: 1,
		ToolType:  ToolTypeFile,
		Collapsed: true,
	}
	result := cr.Render(r)
	assert.NotContains(t, result, "[exit")
}
```

- [ ] **Step 3.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run "TestCollapsibleToolResult_ExitCode" -v`
Expected: FAIL — `ExitCode` field doesn't exist

- [ ] **Step 3.3: Write minimal implementation**

Add `ExitCode *int` field to `CollapsibleToolResult`:

```go
type CollapsibleToolResult struct {
	ID        int
	Name      string
	Args      string
	Content   string
	LineCount int
	IsError   bool
	Collapsed bool
	ToolType  ToolType
	ExitCode  *int     // shell exit code (nil for non-shell tools)
}
```

Update `lineLabel()` to append exit code for shell tools:

```go
func (c *CollapsibleToolResult) lineLabel() string {
	label := ""
	if c.LineCount == 0 {
		label = "empty"
	} else if c.LineCount > maxToolResultLines {
		label = fmt.Sprintf("%d lines (%d shown)", c.LineCount, maxToolResultLines)
	} else if c.LineCount == 1 {
		label = "1 line"
	} else {
		label = fmt.Sprintf("%d lines", c.LineCount)
	}
	if c.ExitCode != nil {
		label += fmt.Sprintf(" [exit %d]", *c.ExitCode)
	}
	return label
}
```

- [ ] **Step 3.4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run "TestCollapsibleToolResult_ExitCode|TestCollapsibleToolResult_NoExit" -v`
Expected: PASS

- [ ] **Step 3.5: Wire exit code in update.go**

The `ToolResultEvent` type (`pkg/agentsdk/events.go`) does **not** carry an exit code field. The shell tool sets `IsError: true` for non-zero exits but doesn't propagate the numeric code. For now, infer exit code from the `IsError` field for shell tools:

```go
// In the tool_result case, after creating cr:
if cr.ToolType == ToolTypeShell {
	if cr.IsError {
		code := 1
		cr.ExitCode = &code
	} else {
		code := 0
		cr.ExitCode = &code
	}
}
```

This is a simplification (always 0 or 1). A future enhancement could add `ExitCode` to `ToolResultEvent` for exact exit codes.

- [ ] **Step 3.6: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: All PASS

- [ ] **Step 3.7: Commit**

```bash
git add internal/tui/toolbox.go internal/tui/toolbox_test.go internal/tui/update.go
git commit -m "[BEHAVIORAL] add exit code display for shell tool results"
```

---

## Task 4: Expandable Truncated Tool Results — Tri-State Model (B1)

Transform tool results from two states (collapsed/expanded) to three: **collapsed** (header only) → **expanded-truncated** (20 lines + `[N more lines]`) → **expanded-full** (all lines). The `Ctrl+E` key expands the most recent tool result to full.

**Files:**
- Modify: `internal/tui/toolbox.go` (add `FullyExpanded` field, update `Render()`)
- Modify: `internal/tui/toolbox_test.go` (new tests)
- Modify: `internal/tui/update.go` (add `Ctrl+E` handler)

- [ ] **Step 4.1: Write failing test — fully expanded shows all lines**

Add to `internal/tui/toolbox_test.go`:

```go
func TestCollapsibleToolResult_FullyExpanded(t *testing.T) {
	r := NewToolBoxRenderer(80)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	cr := &CollapsibleToolResult{
		ID:            1,
		Name:          "file_read",
		Args:          `path="big.go"`,
		Content:       strings.Join(lines, "\n"),
		LineCount:     50,
		Collapsed:     false,
		FullyExpanded: true,
	}
	result := cr.Render(r)
	assert.Contains(t, result, "line 50")
	assert.NotContains(t, result, "more lines")
}
```

- [ ] **Step 4.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestCollapsibleToolResult_FullyExpanded -v`
Expected: FAIL — `FullyExpanded` field doesn't exist

- [ ] **Step 4.3: Write failing test — truncated shows hint to expand**

```go
func TestCollapsibleToolResult_TruncatedShowsExpandHint(t *testing.T) {
	r := NewToolBoxRenderer(80)
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	cr := &CollapsibleToolResult{
		ID:            1,
		Name:          "file_read",
		Args:          `path="big.go"`,
		Content:       strings.Join(lines, "\n"),
		LineCount:     50,
		Collapsed:     false,
		FullyExpanded: false,
	}
	result := cr.Render(r)
	// Should show truncated content with hint
	assert.Contains(t, result, "30 more lines")
	assert.Contains(t, result, "Ctrl+E")
	// Should NOT show line 50
	assert.NotContains(t, result, "line 50")
}
```

- [ ] **Step 4.4: Write failing test — short results have no expand hint**

```go
func TestCollapsibleToolResult_ShortNoExpandHint(t *testing.T) {
	r := NewToolBoxRenderer(80)
	cr := &CollapsibleToolResult{
		ID:        1,
		Name:      "shell",
		Args:      `command="echo hi"`,
		Content:   "hi",
		LineCount: 1,
		Collapsed: false,
	}
	result := cr.Render(r)
	assert.NotContains(t, result, "Ctrl+E")
	assert.NotContains(t, result, "more lines")
}
```

- [ ] **Step 4.5: Write minimal implementation**

Add `FullyExpanded bool` to `CollapsibleToolResult`. Update `Render()`:

```go
type CollapsibleToolResult struct {
	ID            int
	Name          string
	Args          string
	Content       string
	LineCount     int
	IsError       bool
	Collapsed     bool
	FullyExpanded bool     // show all content (no truncation)
	ToolType      ToolType
	ExitCode      *int
}

func (c *CollapsibleToolResult) Render(r *ToolBoxRenderer) string {
	lineLabel := c.lineLabel()
	icon := c.ToolType.Icon()
	if c.Collapsed {
		return styleToolResultHeader.Render(fmt.Sprintf("▶ %s%s(%s)", icon, c.Name, c.Args)) +
			styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	}
	header := styleToolResultHeader.Render(fmt.Sprintf("▼ %s%s(%s)", icon, c.Name, c.Args)) +
		styleSectionLabel.Render(fmt.Sprintf(" — %s", lineLabel)) + "\n"
	if c.FullyExpanded {
		return header + r.RenderToolResultFull(c.Name, c.Content, c.IsError)
	}
	return header + r.RenderToolResult(c.Name, c.Content, c.IsError)
}
```

Add `RenderToolResultFull()` to `ToolBoxRenderer` — same as `RenderToolResult` but without truncation:

```go
// RenderToolResultFull renders a tool result without truncation.
func (r *ToolBoxRenderer) RenderToolResultFull(name, content string, isError bool) string {
	display := ColorizeDiffLines(content)
	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(display) + "\n"
}
```

Update `RenderToolResult` to include the expand hint instead of bare `[N more lines]`:

```go
func (r *ToolBoxRenderer) RenderToolResult(name, content string, isError bool) string {
	lines := strings.Split(content, "\n")
	truncated := 0
	if len(lines) > maxToolResultLines {
		truncated = len(lines) - maxToolResultLines
		lines = lines[:maxToolResultLines]
	}

	display := ColorizeDiffLines(strings.Join(lines, "\n"))
	if truncated > 0 {
		display += fmt.Sprintf("\n[%d more lines — Ctrl+E to expand]", truncated)
	}

	box := r.normalBox
	if isError {
		box = r.errorBox
	}
	return box.Render(display) + "\n"
}
```

- [ ] **Step 4.6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run "TestCollapsibleToolResult_FullyExpanded|TestCollapsibleToolResult_TruncatedShowsExpandHint|TestCollapsibleToolResult_ShortNoExpandHint" -v`
Expected: PASS

- [ ] **Step 4.7: Run all TUI tests to check for regressions**

Run: `go test ./internal/tui/... -v`
Expected: All PASS. Note: existing test `TestRenderToolResultTruncation` should still pass since it checks for "more lines" which is still present.

- [ ] **Step 4.8: Commit**

```bash
git add internal/tui/toolbox.go internal/tui/toolbox_test.go
git commit -m "[BEHAVIORAL] add tri-state expand model for tool results (collapsed/truncated/full)"
```

---

## Task 5: Ctrl+E Key Handler for Full Expansion (B1)

Wire `Ctrl+E` to toggle full expansion on the most recent truncated tool result.

> **Scoping note:** The spec offers two alternatives: `Ctrl+E` for most recent, or per-result focus-based `e` key. This plan implements `Ctrl+E` for most recent only — individual focus would require a cursor/selection model for tool results, which is deferred to Phase C's overlay system.

**Files:**
- Modify: `internal/tui/update.go` (add key handler)
- Modify: `internal/tui/toolbox_test.go` (integration-style test)

- [ ] **Step 5.1: Write failing test — toggleFullExpand helper**

Add a helper function test:

```go
func TestToggleFullExpandMostRecent(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "shell", LineCount: 5, Collapsed: true},
		{ID: 1, Name: "file_read", LineCount: 50, Collapsed: false, FullyExpanded: false},
	}
	toggleFullExpandMostRecent(results)
	// Only the most recent non-collapsed, truncatable result should toggle
	assert.False(t, results[0].FullyExpanded)
	assert.True(t, results[1].FullyExpanded)
}

func TestToggleFullExpandMostRecentToggleOff(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "file_read", LineCount: 50, Collapsed: false, FullyExpanded: true},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}

func TestToggleFullExpandSkipsCollapsed(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "file_read", LineCount: 50, Collapsed: true},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}

func TestToggleFullExpandSkipsShortResults(t *testing.T) {
	results := []CollapsibleToolResult{
		{ID: 0, Name: "shell", LineCount: 5, Collapsed: false},
	}
	toggleFullExpandMostRecent(results)
	assert.False(t, results[0].FullyExpanded)
}
```

- [ ] **Step 5.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestToggleFullExpand -v`
Expected: FAIL — `toggleFullExpandMostRecent` undefined

- [ ] **Step 5.3: Write minimal implementation**

Add to `internal/tui/toolbox.go`:

```go
// toggleFullExpandMostRecent toggles FullyExpanded on the most recent
// non-collapsed tool result that has truncatable content (LineCount > maxToolResultLines).
// Iterates from end to find the right target.
func toggleFullExpandMostRecent(results []CollapsibleToolResult) {
	for i := len(results) - 1; i >= 0; i-- {
		if !results[i].Collapsed && results[i].LineCount > maxToolResultLines {
			results[i].FullyExpanded = !results[i].FullyExpanded
			return
		}
	}
}
```

- [ ] **Step 5.4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestToggleFullExpand -v`
Expected: PASS

- [ ] **Step 5.5: Wire Ctrl+E in update.go**

In `internal/tui/update.go`, add a handler near the existing `Ctrl+T` handler (around line 264):

```go
// Ctrl+E toggles full expansion on the most recent truncated tool result.
if msg.Type == tea.KeyCtrlE && m.state == StateInput && len(m.toolResults) > 0 {
	toggleFullExpandMostRecent(m.toolResults)
	m.viewport.SetContent(m.viewportContent())
	return m, nil
}
```

- [ ] **Step 5.6: Run all TUI tests**

Run: `go test ./internal/tui/... -v`
Expected: All PASS

- [ ] **Step 5.7: Commit**

```bash
git add internal/tui/toolbox.go internal/tui/toolbox_test.go internal/tui/update.go
git commit -m "[BEHAVIORAL] wire Ctrl+E to toggle full expansion on most recent tool result"
```

---

## Task 6: Responsive Status Bar — Priority Segments (B3)

Add priority-based segment elision so the status bar adapts to terminal width.

**Files:**
- Modify: `internal/tui/statusbar.go` (refactor `View()` to use priority tiers)
- Modify: `internal/tui/statusbar_test.go` (new tests)

- [ ] **Step 6.1: Write failing test — narrow terminal hides low-priority segments**

Add to `internal/tui/statusbar_test.go`:

```go
func TestStatusBarNarrowHidesGitBranch(t *testing.T) {
	sb := NewStatusBar(70) // narrower than 80
	sb.SetModel("claude-sonnet")
	sb.SetTokens(1200, 100000)
	sb.SetTurn(3, 50)
	sb.SetCost(0.02)
	sb.SetGitBranch("feature/test")
	sb.SetElapsed(30 * time.Second)
	result := sb.View()
	// At width 70, git branch and elapsed should be hidden
	assert.NotContains(t, result, "feature/test")
	assert.NotContains(t, result, "⏱")
	// Core segments should remain
	assert.Contains(t, result, "claude-sonnet")
	assert.Contains(t, result, "Turn 3/50")
}
```

- [ ] **Step 6.2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestStatusBarNarrowHidesGitBranch -v`
Expected: FAIL — current `View()` always includes git branch

- [ ] **Step 6.3: Write failing test — fitSegments drops low-priority first**

Test the `fitSegments` method directly with known string widths to avoid dependency on persona prefix length:

```go
func TestFitSegmentsDropsLowestPriority(t *testing.T) {
	sb := NewStatusBar(40) // very narrow
	sep := " | "
	segments := []statusSegment{
		{"Model", priorityAlways},    // 5 chars
		{"Turn", priorityAlways},     // 4 chars
		{"Tokens", priorityHigh},     // 6 chars
		{"$0.02", priorityHigh},      // 5 chars
		{"branch", priorityMedium},   // 6 chars
		{"elapsed", priorityMedium},  // 7 chars
		{"skills", priorityLow},      // 6 chars
	}
	result := sb.fitSegments(segments, sep)
	// Should keep Always segments, drop others as needed
	names := make([]string, len(result))
	for i, s := range result {
		names[i] = s.content
	}
	assert.Contains(t, names, "Model")
	assert.Contains(t, names, "Turn")
	// Low priority should be gone first
	assert.NotContains(t, names, "skills")
}

func TestFitSegmentsKeepsAllWhenFit(t *testing.T) {
	sb := NewStatusBar(200)
	sep := " | "
	segments := []statusSegment{
		{"Model", priorityAlways},
		{"Turn", priorityAlways},
		{"Tokens", priorityHigh},
	}
	result := sb.fitSegments(segments, sep)
	assert.Len(t, result, 3)
}
```

- [ ] **Step 6.4: Write failing test — wide terminal shows everything**

```go
func TestStatusBarWideShowsAll(t *testing.T) {
	sb := NewStatusBar(120)
	sb.SetModel("claude-sonnet")
	sb.SetTokens(1200, 100000)
	sb.SetTurn(3, 50)
	sb.SetCost(0.02)
	sb.SetGitBranch("main")
	sb.SetElapsed(65 * time.Second)
	sb.SetSubagent("worker")
	sb.SetSkillSummary("2 active")
	result := sb.View()
	assert.Contains(t, result, "claude-sonnet")
	assert.Contains(t, result, "1.2k/100k")
	assert.Contains(t, result, "Turn 3/50")
	assert.Contains(t, result, "$0.02")
	assert.Contains(t, result, "main")
	assert.Contains(t, result, "⏱")
	assert.Contains(t, result, "worker")
	assert.Contains(t, result, "2 active")
}
```

- [ ] **Step 6.5: Write minimal implementation**

Refactor `StatusBar.View()` to use a priority-based segment model:

```go
// segment priority tiers (lower = higher priority = shown first)
const (
	priorityAlways = iota // model name, turn count
	priorityHigh          // tokens, cost
	priorityMedium        // git branch, elapsed
	priorityLow           // wiki, subagent, skills
)

type statusSegment struct {
	content  string
	priority int
}

func (s *StatusBar) View() string {
	sep := styleTextDim.Render(" │ ")

	// Build all segments with priorities
	segments := []statusSegment{
		{styleStatusLabel.Render(persona.StatusPrefix()), priorityAlways},
		{styleStatusValue.Render(s.model), priorityAlways},
		{styleTextDim.Render(fmt.Sprintf("%s/%s", formatTokens(s.inputTokens), formatTokens(s.maxTokens))), priorityHigh},
		{styleStatusValue.Render(fmt.Sprintf("Turn %d/%d", s.turn, s.maxTurns)), priorityAlways},
		{styleTextDim.Render(fmt.Sprintf("~$%.2f", s.cost)), priorityHigh},
	}
	if s.gitBranch != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("⎇ ") + styleStatusValue.Render(s.gitBranch), priorityMedium,
		})
	}
	if s.elapsed > 0 {
		segments = append(segments, statusSegment{
			styleTextDim.Render("⏱ " + formatElapsed(s.elapsed)), priorityMedium,
		})
	}
	if s.wikiStage != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("Wiki: ") + styleStatusValue.Render(s.wikiStage), priorityLow,
		})
	}
	if s.subagentName != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("🔄 ") + styleStatusValue.Render(s.subagentName), priorityLow,
		})
	}
	if s.skillSummary != "" {
		segments = append(segments, statusSegment{
			styleStatusLabel.Render("Skills: ") + styleStatusValue.Render(s.skillSummary), priorityLow,
		})
	}

	// Filter by width: drop lowest-priority segments until they fit.
	// Use lipgloss.Width() for accurate ANSI-aware width calculation.
	visible := s.fitSegments(segments, sep)

	var b strings.Builder
	b.WriteString(" ")
	for i, seg := range visible {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(seg.content)
	}
	return b.String()
}

// fitSegments returns segments that fit within the status bar width,
// dropping lowest-priority segments first.
func (s *StatusBar) fitSegments(segments []statusSegment, sep string) []statusSegment {
	if s.width <= 0 {
		return segments
	}

	// Try with all segments first
	visible := make([]statusSegment, len(segments))
	copy(visible, segments)

	for {
		if s.segmentsWidth(visible, sep) <= s.width {
			return visible
		}
		// Find and remove the lowest-priority (highest priority number) segment
		worstIdx := -1
		worstPri := -1
		for i := len(visible) - 1; i >= 0; i-- {
			if visible[i].priority > worstPri {
				worstPri = visible[i].priority
				worstIdx = i
			}
		}
		if worstIdx < 0 || worstPri == priorityAlways {
			break // can't remove anything more
		}
		visible = append(visible[:worstIdx], visible[worstIdx+1:]...)
	}
	return visible
}

// segmentsWidth calculates the total rendered width of segments with separators.
func (s *StatusBar) segmentsWidth(segments []statusSegment, sep string) int {
	total := 1 // leading space
	sepW := lipgloss.Width(sep)
	for i, seg := range segments {
		if i > 0 {
			total += sepW
		}
		total += lipgloss.Width(seg.content)
	}
	return total
}
```

Add `"github.com/charmbracelet/lipgloss"` to the imports in `statusbar.go`.

- [ ] **Step 6.6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run "TestStatusBarNarrow|TestFitSegments|TestStatusBarWide" -v`
Expected: PASS

- [ ] **Step 6.7: Run all TUI tests to check for regressions**

Run: `go test ./internal/tui/... -v`
Expected: All PASS. Existing status bar tests should still pass because they use width 80, which shows all core segments.

- [ ] **Step 6.8: Commit**

```bash
git add internal/tui/statusbar.go internal/tui/statusbar_test.go
git commit -m "[BEHAVIORAL] add priority-based responsive status bar for narrow terminals"
```

---

## Task 7: Final Integration & Coverage Verification

Run full test suite, check coverage, lint.

**Files:** No new files

- [ ] **Step 7.1: Run full test suite**

Run: `go test ./internal/tui/... -v -cover`
Expected: All PASS, coverage ≥ 87% (current baseline)

- [ ] **Step 7.2: Run linter**

Run: `golangci-lint run ./internal/tui/...`
Expected: No warnings

- [ ] **Step 7.3: Run formatter**

Run: `gofmt -l internal/tui/`
Expected: No output (all files formatted)

- [ ] **Step 7.4: Verify no regressions in full codebase**

Run: `go test ./... -count=1`
Expected: All PASS

- [ ] **Step 7.5: Commit any final fixes if needed**

Only if steps 7.1–7.4 revealed issues.
