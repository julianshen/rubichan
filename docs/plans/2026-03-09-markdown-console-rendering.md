# Markdown Console Rendering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render styled markdown in headless console output (TTY-aware) and incrementally during TUI streaming at natural breakpoints.

**Architecture:** Two independent features sharing the existing Glamour stack. (1) New `StyledMarkdownFormatter` in `internal/output/` composes raw `MarkdownFormatter` + Glamour with auto-style TTY detection. (2) Breakpoint detector in `internal/tui/markdown.go` triggers mid-stream re-renders through an extracted `renderAssistantMarkdown()` helper.

**Tech Stack:** Glamour (markdown→ANSI), `golang.org/x/term` (TTY detection), Bubble Tea (TUI event loop)

---

### Task 1: IsMarkdownBreakpoint — failing tests

**Files:**
- Modify: `internal/tui/markdown_test.go`

**Step 1: Write failing tests for IsMarkdownBreakpoint**

Add these tests to `internal/tui/markdown_test.go`:

```go
func TestIsMarkdownBreakpointDoubleNewline(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("some text\n\n"))
}

func TestIsMarkdownBreakpointCodeFenceClosing(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("fmt.Println()\n```\n"))
}

func TestIsMarkdownBreakpointHeading(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("some text\n## Section\n"))
}

func TestIsMarkdownBreakpointH1(t *testing.T) {
	assert.True(t, IsMarkdownBreakpoint("intro\n# Title\n"))
}

func TestIsMarkdownBreakpointSingleNewline(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint("some text\n"))
}

func TestIsMarkdownBreakpointMidWord(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint("some text"))
}

func TestIsMarkdownBreakpointEmpty(t *testing.T) {
	assert.False(t, IsMarkdownBreakpoint(""))
}

func TestIsMarkdownBreakpointCodeFenceOpening(t *testing.T) {
	// Opening fence is NOT a breakpoint — only closing fences are.
	assert.False(t, IsMarkdownBreakpoint("text\n```go\n"))
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestIsMarkdownBreakpoint -v`
Expected: FAIL — `IsMarkdownBreakpoint` undefined

---

### Task 2: IsMarkdownBreakpoint — implementation

**Files:**
- Modify: `internal/tui/markdown.go`

**Step 1: Implement IsMarkdownBreakpoint**

Add to `internal/tui/markdown.go`:

```go
import "strings"

// IsMarkdownBreakpoint returns true when text ends at a natural markdown
// boundary suitable for incremental rendering: double newline (paragraph),
// closing code fence, or heading line.
func IsMarkdownBreakpoint(text string) bool {
	if len(text) == 0 {
		return false
	}

	// Double newline — paragraph boundary.
	if strings.HasSuffix(text, "\n\n") {
		return true
	}

	// Find the last complete line (text ending with \n).
	if text[len(text)-1] != '\n' {
		return false
	}
	// Trim trailing newline, find previous newline to get last line.
	trimmed := text[:len(text)-1]
	lastNL := strings.LastIndex(trimmed, "\n")
	var lastLine string
	if lastNL == -1 {
		lastLine = trimmed
	} else {
		lastLine = trimmed[lastNL+1:]
	}

	// Closing code fence: line is exactly ``` (possibly with whitespace).
	stripped := strings.TrimSpace(lastLine)
	if stripped == "```" {
		return true
	}

	// Heading: line starts with # (one or more).
	if len(stripped) > 0 && stripped[0] == '#' {
		// Ensure it's a valid heading (# followed by space or end of line).
		hashes := strings.TrimLeft(stripped, "#")
		if len(hashes) == 0 || hashes[0] == ' ' {
			return true
		}
	}

	return false
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestIsMarkdownBreakpoint -v`
Expected: PASS (all 8 tests)

**Step 3: Run full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/tui/markdown.go internal/tui/markdown_test.go
git commit -m "[BEHAVIORAL] Add IsMarkdownBreakpoint for detecting natural rendering points"
```

---

### Task 3: Extract renderAssistantMarkdown helper — failing test

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/model_test.go` (or appropriate test file)

**Step 1: Write failing test for renderAssistantMarkdown**

The helper should render `rawAssistant` through `mdRenderer` and replace content from `assistantStartIdx`. Add to an appropriate test file (e.g., `internal/tui/update_test.go` if it exists, else `model_test.go`):

```go
func TestRenderAssistantMarkdown(t *testing.T) {
	m := &Model{}
	r, err := NewMarkdownRenderer(80)
	require.NoError(t, err)
	m.mdRenderer = r

	// Simulate: user prompt is prefix, raw markdown is assistant text.
	m.content.WriteString("> hello\n")
	m.assistantStartIdx = m.content.Len()
	m.rawAssistant.WriteString("Hello **world**")
	m.content.WriteString("Hello **world**") // raw during streaming

	m.renderAssistantMarkdown()

	result := m.content.String()
	assert.True(t, strings.HasPrefix(result, "> hello\n"))
	// After rendering, raw ** markers should be stripped.
	assert.NotContains(t, result[m.assistantStartIdx:], "**world**")
	assert.Contains(t, result, "world")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderAssistantMarkdown -v`
Expected: FAIL — `renderAssistantMarkdown` undefined

---

### Task 4: Extract renderAssistantMarkdown helper — implementation

**Files:**
- Modify: `internal/tui/update.go`

**Step 1: Extract helper from "done" handler**

Add this method to `update.go`:

```go
// renderAssistantMarkdown re-renders the accumulated rawAssistant markdown
// through the Glamour renderer and replaces content from assistantStartIdx.
// If rendering fails or produces empty output, the existing raw content is
// kept unchanged.
func (m *Model) renderAssistantMarkdown() {
	raw := m.rawAssistant.String()
	if raw == "" {
		return
	}
	rendered, err := m.mdRenderer.Render(raw)
	if err != nil || rendered == "" {
		return
	}
	contentStr := m.content.String()
	m.content.Reset()
	m.content.WriteString(contentStr[:m.assistantStartIdx])
	m.content.WriteString(rendered)
}
```

**Step 2: Replace duplicated logic in "done" handler**

In `handleTurnEvent`, replace the "done" case rendering block:

```go
case "done":
	m.renderAssistantMarkdown()
	m.rawAssistant.Reset()
	m.content.WriteString(persona.SuccessMessage())
	// ... rest unchanged
```

**Step 3: Run tests**

Run: `go test ./internal/tui/ -v`
Expected: PASS (all existing + new test)

**Step 4: Commit**

```bash
git add internal/tui/update.go internal/tui/model_test.go
git commit -m "[STRUCTURAL] Extract renderAssistantMarkdown helper from done handler"
```

---

### Task 5: Wire breakpoint rendering into text_delta

**Files:**
- Modify: `internal/tui/update.go`

**Step 1: Add breakpoint check to text_delta handler**

In `handleTurnEvent`, update the `text_delta` case:

```go
case "text_delta":
	m.rawAssistant.WriteString(msg.Text)
	m.content.WriteString(msg.Text)
	if IsMarkdownBreakpoint(m.rawAssistant.String()) {
		m.renderAssistantMarkdown()
	}
	m.setContentAndAutoScroll(m.content.String())
	return m, m.waitForEvent()
```

**Step 2: Run full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/tui/update.go
git commit -m "[BEHAVIORAL] Render markdown incrementally at natural breakpoints during streaming"
```

---

### Task 6: StyledMarkdownFormatter — failing tests

**Files:**
- Create: `internal/output/styled_markdown_test.go`

**Step 1: Write failing tests**

```go
package output

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStyledMarkdownFormatterContainsANSI(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "say hello",
		Response:   "Hello **world**",
		TurnCount:  1,
		DurationMs: 2000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	// ANSI escape codes start with ESC[
	assert.Contains(t, s, "\x1b[", "expected ANSI escape codes in styled output")
	assert.Contains(t, s, "world")
}

func TestStyledMarkdownFormatterPreservesContent(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:   "review code",
		Response: "Code looks good.",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{}`), Result: "ok"},
		},
		TurnCount:  2,
		DurationMs: 3000,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Code looks good")
	assert.Contains(t, s, "file")
	assert.Contains(t, s, "2")
}

func TestStyledMarkdownFormatterError(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "fail",
		Error:      "timeout exceeded",
		TurnCount:  0,
		DurationMs: 0,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "timeout exceeded")
}

func TestStyledMarkdownFormatterEmpty(t *testing.T) {
	f := NewStyledMarkdownFormatter(80)
	result := &RunResult{
		Prompt:     "hello",
		Response:   "",
		TurnCount:  1,
		DurationMs: 100,
		Mode:       "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/output/ -run TestStyledMarkdown -v`
Expected: FAIL — `NewStyledMarkdownFormatter` undefined

---

### Task 7: StyledMarkdownFormatter — implementation

**Files:**
- Create: `internal/output/styled_markdown.go`

**Step 1: Implement StyledMarkdownFormatter**

```go
package output

import (
	"fmt"

	"github.com/charmbracelet/glamour"
)

// StyledMarkdownFormatter composes MarkdownFormatter with Glamour to produce
// ANSI-styled terminal output. It uses glamour.WithAutoStyle() to detect
// the terminal's light/dark background preference.
type StyledMarkdownFormatter struct {
	inner    *MarkdownFormatter
	renderer *glamour.TermRenderer
}

// NewStyledMarkdownFormatter creates a StyledMarkdownFormatter with auto-detected
// terminal style and the given word wrap width.
func NewStyledMarkdownFormatter(width int) *StyledMarkdownFormatter {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	return &StyledMarkdownFormatter{
		inner:    NewMarkdownFormatter(),
		renderer: r,
	}
}

// Format generates raw markdown via the inner MarkdownFormatter, then renders
// it through Glamour for ANSI styling. Falls back to raw markdown if Glamour
// rendering fails.
func (f *StyledMarkdownFormatter) Format(result *RunResult) ([]byte, error) {
	raw, err := f.inner.Format(result)
	if err != nil {
		return nil, fmt.Errorf("styled markdown: %w", err)
	}

	if f.renderer == nil {
		return raw, nil
	}

	styled, err := f.renderer.Render(string(raw))
	if err != nil {
		return raw, nil
	}

	return []byte(styled), nil
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./internal/output/ -run TestStyledMarkdown -v`
Expected: PASS (all 4 tests)

**Step 3: Run full output test suite**

Run: `go test ./internal/output/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/output/styled_markdown.go internal/output/styled_markdown_test.go
git commit -m "[BEHAVIORAL] Add StyledMarkdownFormatter with Glamour auto-style rendering"
```

---

### Task 8: Wire TTY detection into headless runner

**Files:**
- Modify: `cmd/rubichan/main.go`

**Step 1: Add TTY detection to formatter selection**

In `runHeadless()`, around line 1271-1277, change the formatter selection:

```go
import "golang.org/x/term"

// In the formatter selection block:
var formatter output.Formatter
switch outputFlag {
case "json":
	formatter = output.NewJSONFormatter()
default:
	if term.IsTerminal(int(os.Stdout.Fd())) {
		formatter = output.NewStyledMarkdownFormatter(80)
	} else {
		formatter = output.NewMarkdownFormatter()
	}
}
```

**Step 2: Verify build**

Run: `go build ./cmd/rubichan/`
Expected: SUCCESS

**Step 3: Run full test suite**

Run: `go test ./cmd/rubichan/... ./internal/output/... ./internal/tui/...`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/rubichan/main.go
git commit -m "[BEHAVIORAL] Auto-detect TTY for styled markdown output in headless mode"
```

---

### Task 9: Coverage check and final verification

**Step 1: Check coverage for new code**

Run: `go test -cover ./internal/tui/ ./internal/output/`
Expected: Both packages >90%

**Step 2: Run lint and format checks**

Run: `golangci-lint run ./... && gofmt -l .`
Expected: Clean

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS

**Step 4: If coverage gaps exist, add targeted tests and commit**

```bash
git commit -m "[BEHAVIORAL] Boost test coverage for markdown rendering"
```
