# Markdown Console Rendering Design

## Problem

Two gaps in markdown rendering:

1. **Headless mode** outputs raw markdown to stdout — no ANSI styling even when the terminal supports it.
2. **TUI streaming** shows raw text during streaming; markdown is only rendered at turn completion ("done" event), causing a visual jump.

## Approach: Minimal renderer reuse with breakpoint detection (Approach A)

### Headless Styled Output

- New `StyledMarkdownFormatter` in `internal/output/` composing `MarkdownFormatter` + Glamour.
- Implements the existing `Formatter` interface.
- Uses `glamour.WithAutoStyle()` for terminal-aware light/dark theming.
- TTY detection in `cmd/rubichan/main.go`: `term.IsTerminal(os.Stdout.Fd())`.
  - TTY → `StyledMarkdownFormatter` (ANSI-styled)
  - Piped/redirected → `MarkdownFormatter` (raw markdown, current behavior)
- `StyledMarkdownFormatter` generates raw markdown via inner `MarkdownFormatter`, then passes through Glamour.

### TUI Incremental Rendering at Breakpoints

- `IsMarkdownBreakpoint(text string) bool` in `internal/tui/markdown.go` detects natural boundaries:
  - Double newline (`\n\n`) — paragraph boundary
  - Code fence closing (`` ```\n ``)
  - Heading line (`\n# `, `\n## `, etc.)
- In `handleTurnEvent` `text_delta` case: after appending to `rawAssistant`, check for breakpoint. If detected, re-render full `rawAssistant` through `mdRenderer.Render()` and replace content from `assistantStartIdx`.
- Extract shared "render and replace" logic into `renderAssistantMarkdown()` helper, used by both breakpoint renders and the final "done" render.
- Full re-render per breakpoint. Glamour handles <10KB in <5ms; optimize later if needed (YAGNI).

## File Map

### New files
- `internal/output/styled_markdown.go` — `StyledMarkdownFormatter`
- `internal/output/styled_markdown_test.go` — tests

### Modified files
- `internal/tui/markdown.go` — `IsMarkdownBreakpoint()`, `RenderOrFallback()`
- `internal/tui/markdown_test.go` — breakpoint detection tests
- `internal/tui/update.go` — wire breakpoint rendering, extract `renderAssistantMarkdown()`
- `cmd/rubichan/main.go` — TTY detection for formatter selection

### Dependencies
- `golang.org/x/term` — already indirect via Bubble Tea
- `glamour` — already direct dependency

## Test Strategy

- `IsMarkdownBreakpoint`: positive/negative for all three breakpoint types
- `StyledMarkdownFormatter.Format`: verify ANSI codes present, verify composition with inner formatter
- `renderAssistantMarkdown`: verify content replacement from `assistantStartIdx`
- Coverage target: >90%
