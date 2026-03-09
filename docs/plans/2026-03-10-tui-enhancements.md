# TUI Enhancement Plan

Inspired by Claude Code's UI/UX patterns, adapted to Rubichan's Bubble Tea architecture.

## Overview

Seven enhancements organized into 4 PRs by dependency order. Each PR is independently mergeable.

---

## PR 1: Collapsible Tool Results + Diff Colorization (Features #1, #8)

These share the `ToolBoxRenderer` — ship together for a coherent tool output upgrade.

### Feature #1: Collapsible Tool Results

**Problem**: Tool results always show inline (up to 20 lines). Long sessions with many file reads/searches become cluttered and hard to navigate.

**Design**: Each tool result tracks its own collapsed/expanded state. Collapsed by default after turn completes; expanded during streaming (so you see live output). User toggles individual results with a keybind.

#### Architecture

**New type**: `CollapsibleToolResult` in `toolbox.go`

```go
type CollapsibleToolResult struct {
    ID        int      // sequential per-session, for keybind targeting
    Name      string   // tool name (e.g., "file", "shell")
    Args      string   // truncated arg summary
    Content   string   // full result text
    LineCount int      // total lines before truncation
    IsError   bool
    Collapsed bool     // true = show summary line only
}
```

**State tracking**: Add `toolResults []CollapsibleToolResult` to `Model`. Each `tool_result` event appends to this list. The `content` buffer stores a placeholder marker per result; `viewportContent()` replaces markers with rendered (collapsed or expanded) versions.

**Alternative approach (simpler)**: Instead of placeholder markers, track tool results separately and render them inline during `viewportContent()`. This avoids string splicing but requires careful position tracking.

**Chosen approach**: Store tool results in a slice on Model. During view rendering, each tool result renders itself based on its `Collapsed` state. When the turn is "done", mark all results as `Collapsed = true`. During streaming, results stay expanded.

#### Key Bindings

- **Ctrl+T**: Toggle *all* tool results in the current turn (collapse/expand)
- Individual toggle is complex in a text viewport — defer to v2

#### Collapsed View

```
▶ file(path="internal/tui/model.go") — 376 lines [read]
```

#### Expanded View (current behavior, bordered box)

```
▼ file(path="internal/tui/model.go") — 376 lines [read]
╭──────────────────────────────────────────╮
│ package tui                              │
│ ...                                      │
│ [356 more lines]                         │
╰──────────────────────────────────────────╯
```

#### Implementation Steps (TDD)

- [x] **Test**: `TestCollapsibleToolResult_CollapsedView` — collapsed state renders single summary line
- [x] **Test**: `TestCollapsibleToolResult_ExpandedView` — expanded state renders bordered box with content
- [x] **Test**: `TestCollapsibleToolResult_ErrorBorder` — error results use red border when expanded
- [x] **Test**: `TestCollapsibleToolResult_LineCounting` — line count shown accurately
- [x] **Impl**: Add `CollapsibleToolResult` type to `toolbox.go`, `Render()` method
- [x] **Test**: `TestModel_ToolResultsCollapsedOnDone` — all results collapse when turn completes
- [x] **Test**: `TestModel_ToolResultsExpandedDuringStreaming` — results stay expanded while streaming
- [x] **Test**: `TestModel_CtrlT_TogglesToolResults` — Ctrl+T toggles all results in current turn
- [x] **Impl**: Add `toolResults` slice to Model, wire into `handleTurnEvent`, add Ctrl+T handler
- [x] **Impl**: Update `viewportContent()` to render tool results using collapsed/expanded state

### Feature #8: Diff Colorization in Tool Output

**Problem**: File diffs in tool output (`shell` results from `git diff`, `file` patch results) display as plain text. Hard to scan +/- lines.

**Design**: Detect unified diff lines in tool result content and apply green/red coloring via Lipgloss.

#### Implementation Steps (TDD)

- [x] **Test**: `TestColorizeDiffLines_AddedLines` — lines starting with `+` (not `+++`) are green
- [x] **Test**: `TestColorizeDiffLines_RemovedLines` — lines starting with `-` (not `---`) are red
- [x] **Test**: `TestColorizeDiffLines_HeaderLines` — `@@` lines are cyan
- [x] **Test**: `TestColorizeDiffLines_PlainLines` — non-diff lines unchanged
- [x] **Test**: `TestColorizeDiffLines_EmptyInput` — empty string returns empty
- [x] **Impl**: Add `ColorizeDiffLines(content string) string` to `toolbox.go`
- [x] **Impl**: Call `ColorizeDiffLines` in `RenderToolResult` before display

#### Diff Detection Heuristic

A tool result is treated as a diff if it contains at least one line matching `^@@ .* @@`. This avoids false positives on regular output that happens to start with `+` or `-`.

---

## PR 2: Enhanced Status Bar + Turn Timer (Features #6, #3-partial)

### Feature #6: Turn Elapsed Time + Token Rate

**Problem**: During long streaming turns, users have no sense of how long the agent has been thinking or how fast tokens are arriving.

**Design**: Track turn start time. Display elapsed time and token rate in the spinner line during streaming, and in the status bar after turn completion.

#### Architecture Changes

**Model additions**:
```go
turnStartTime time.Time    // set when streaming begins
turnTokens    int          // running count of text_delta characters (approx tokens)
```

**Spinner line** (during streaming):
```
⠋ Thinking... 3.2s · ~420 tokens
```

**Status bar** (after turn):
```
Ruby ♡  claude-3.5  1.2k/100k  Turn 3/10  ~$0.45  ⏱ 4.1s
```

#### Implementation Steps (TDD)

- [ ] **Test**: `TestStatusBar_ElapsedTime` — elapsed time displayed when set
- [ ] **Test**: `TestStatusBar_NoElapsedWhenZero` — no elapsed section when not set
- [ ] **Impl**: Add `SetElapsed(d time.Duration)` to StatusBar, display in View()
- [ ] **Test**: `TestModel_TurnTimerStartsOnStreaming` — turnStartTime set when streaming begins
- [ ] **Test**: `TestModel_TurnTimerShownInSpinner` — spinner view includes elapsed time
- [ ] **Impl**: Set `turnStartTime` in `startTurn`, compute elapsed in `View()` spinner section
- [ ] **Impl**: On "done" event, compute final elapsed, set on status bar

#### Git Branch in Status Bar (partial #3)

- [ ] **Test**: `TestStatusBar_GitBranch` — branch name displayed when set
- [ ] **Impl**: Add `SetGitBranch(branch string)` to StatusBar
- [ ] **Impl**: In `main.go`, detect current git branch at startup via `git rev-parse --abbrev-ref HEAD`, pass to status bar

---

## PR 3: @ File Mentions with Autocomplete (Feature #2)

### Feature #2: @ File Completion

**Problem**: Users can't reference files in their prompts with autocomplete. Must type full paths or hope the agent finds the right file.

**Design**: When user types `@`, trigger a file completion overlay (similar to the existing `/` command completion). Source candidates from `git ls-files` (or filesystem walk for non-git projects), cached at startup.

#### Architecture

**New type**: `FileCompletionSource` in new file `internal/tui/filecompletion.go`

```go
type FileCompletionSource struct {
    mu       sync.RWMutex
    files    []string         // sorted file paths
    workDir  string
    indexed  bool
}
```

**Index strategy**:
1. On startup, run `git ls-files` in a background goroutine
2. Cache result in `FileCompletionSource`
3. On `@` trigger, filter cached files by typed prefix (fuzzy or prefix match)
4. Limit display to 8 candidates (same as command completion)

**CompletionOverlay extension**: The existing `CompletionOverlay` handles `/` commands. Rather than complicating it, create a parallel `FileCompletionOverlay` that activates on `@` prefix. Both share the same visual rendering pattern.

**Input handling**: When `@` is typed and not inside a word:
- Show file completion overlay
- Tab accepts selected file, inserts full path
- Escape dismisses
- Continue typing narrows the filter

**Resolution**: Before sending the prompt to the agent, replace `@path/to/file` references with the file's content (or leave as-is for the agent to interpret — start with the simpler approach of just inserting the path, letting the agent read it).

#### Implementation Steps (TDD)

- [ ] **Test**: `TestFileCompletionSource_IndexFromGitLsFiles` — parses git ls-files output into sorted file list
- [ ] **Test**: `TestFileCompletionSource_MatchPrefix` — filters files by prefix after `@`
- [ ] **Test**: `TestFileCompletionSource_MatchFuzzy` — fuzzy matching (path segments)
- [ ] **Test**: `TestFileCompletionSource_EmptyRepo` — gracefully handles empty/non-git repos
- [ ] **Impl**: Create `FileCompletionSource` in `filecompletion.go`
- [ ] **Test**: `TestFileCompletionOverlay_ActivatesOnAt` — overlay shows when `@` typed
- [ ] **Test**: `TestFileCompletionOverlay_HidesOnSpace` — overlay hides after space
- [ ] **Test**: `TestFileCompletionOverlay_TabAccepts` — Tab inserts selected path
- [ ] **Test**: `TestFileCompletionOverlay_EscapeDismisses` — Escape hides overlay
- [ ] **Impl**: Create `FileCompletionOverlay` in `filecompletion.go`
- [ ] **Test**: `TestModel_AtMentionTriggersFileCompletion` — @ in input activates file overlay
- [ ] **Impl**: Wire `FileCompletionOverlay` into Model, add to `syncCompletion()`, add to `View()`
- [ ] **Impl**: In `main.go`, initialize `FileCompletionSource` with cwd, pass to Model

#### Trade-off: Fuzzy vs Prefix Matching

Prefix matching is simpler and faster. Fuzzy matching (matching path segments like `m/t/m` → `internal/tui/model.go`) is more powerful but requires a scoring algorithm. **Start with prefix matching**, add fuzzy in a follow-up if needed.

---

## PR 4: Input History + Enhanced Approval Prompt + OSC 8 Links (Features #4, #5, #7)

Three small, independent features bundled into one PR since they're each <100 LOC.

### Feature #4: Input History

**Problem**: Users can't recall previous prompts. Must retype or copy-paste from scrollback.

**Design**: Store submitted prompts in a ring buffer. Cycle through them with `Ctrl+P` (previous) / `Ctrl+N` (next). Up/Down are reserved for viewport scroll.

#### Architecture

**New type**: `InputHistory` in new file `internal/tui/inputhistory.go`

```go
type InputHistory struct {
    entries []string   // most recent last
    cursor  int        // -1 = composing new input
    draft   string     // saves in-progress input when cycling
    maxSize int        // ring buffer limit (default: 100)
}
```

**Behavior**:
- `Ctrl+P`: Save current input as draft (if cursor == -1), move cursor up, set input to entry
- `Ctrl+N`: Move cursor down; if past end, restore draft
- On submit: append to entries, reset cursor to -1

#### Implementation Steps (TDD)

- [ ] **Test**: `TestInputHistory_AddAndRecall` — add entries, Ctrl+P recalls most recent
- [ ] **Test**: `TestInputHistory_CycleThrough` — multiple Ctrl+P moves through history
- [ ] **Test**: `TestInputHistory_RestoreDraft` — Ctrl+N past end restores in-progress text
- [ ] **Test**: `TestInputHistory_MaxSize` — oldest entries evicted beyond maxSize
- [ ] **Test**: `TestInputHistory_EmptyHistory` — Ctrl+P on empty history does nothing
- [ ] **Impl**: Create `InputHistory` in `inputhistory.go`
- [ ] **Test**: `TestModel_CtrlP_RecallsHistory` — Ctrl+P in input state recalls previous prompt
- [ ] **Test**: `TestModel_CtrlN_AdvancesHistory` — Ctrl+N moves forward
- [ ] **Impl**: Add `history *InputHistory` to Model, wire Ctrl+P/N in `handleKeyMsg`

### Feature #5: Rich Permission Prompts

**Problem**: Current approval prompt shows only tool name, raw args, and `(y)es (n)o (a)lways`. No risk indication, no deny-always option, no argument preview.

**Design**: Enhance `ApprovalPrompt` with:
1. **Risk indicator**: Color-coded label based on tool name (shell = high, file write = medium, file read = low)
2. **Formatted args**: Parse JSON args and show key fields (e.g., `path: "src/main.go"`, `command: "rm -rf /"`)
3. **Deny always option**: `(d)eny always` — adds tool to session denylist
4. **Destructive command highlighting**: If shell tool args contain destructive patterns (`rm -rf`, `git reset --hard`, `drop table`), show warning in red

#### Architecture Changes

**ApprovalPrompt additions**:
```go
type ApprovalPrompt struct {
    // ... existing fields ...
    riskLevel  RiskLevel  // Low, Medium, High
    parsedArgs []ArgField // key-value pairs extracted from JSON
}

type RiskLevel int
const (
    RiskLow RiskLevel = iota    // file read, search
    RiskMedium                   // file write, patch
    RiskHigh                     // shell, process
)

type ArgField struct {
    Key   string
    Value string
}
```

**Model additions**: Add `alwaysDenied sync.Map` alongside existing `alwaysApproved`. Check deny list in `MakeApprovalFunc`.

#### Implementation Steps (TDD)

- [ ] **Test**: `TestRiskLevel_ShellIsHigh` — shell tool returns RiskHigh
- [ ] **Test**: `TestRiskLevel_FileReadIsLow` — file read returns RiskLow
- [ ] **Test**: `TestRiskLevel_FileWriteIsMedium` — file write returns RiskMedium
- [ ] **Impl**: Add `classifyRisk(toolName string, args json.RawMessage) RiskLevel`
- [ ] **Test**: `TestParseApprovalArgs_ExtractsKeyFields` — JSON args parsed into display fields
- [ ] **Test**: `TestParseApprovalArgs_TruncatesLongValues` — values over 80 chars truncated
- [ ] **Impl**: Add `parseApprovalArgs(args string) []ArgField`
- [ ] **Test**: `TestApprovalPrompt_RiskColorCoding` — high risk shows red label, low shows green
- [ ] **Test**: `TestApprovalPrompt_DenyKey` — pressing 'd' sets ApprovalDenyAlways
- [ ] **Test**: `TestApprovalPrompt_DestructiveWarning` — shell with "rm -rf" shows warning
- [ ] **Impl**: Update `ApprovalPrompt` with risk level, parsed args, deny-always, destructive detection
- [ ] **Test**: `TestModel_DenyAlways_BlocksFutureCalls` — deny-always prevents re-prompting
- [ ] **Impl**: Add `alwaysDenied` to Model, check in `MakeApprovalFunc`

#### Enhanced Prompt View

```
╭──────────────────────────────────────────────────╮
│ ⚠ HIGH RISK  shell                               │
│                                                   │
│   command: rm -rf /tmp/build                      │
│   timeout: 120s                                   │
│                                                   │
│   ⚠ Destructive command detected                  │
│                                                   │
│   (y)es  (n)o  (a)lways  (d)eny always           │
╰──────────────────────────────────────────────────╯
```

### Feature #7: OSC 8 Hyperlinks

**Problem**: File paths in tool output and assistant messages aren't clickable. Users must manually open files.

**Design**: Detect file paths in tool output and wrap them in OSC 8 escape sequences for terminals that support it (iTerm2, Kitty, WezTerm, Ghostty, etc.).

#### Architecture

**New file**: `internal/tui/hyperlink.go`

```go
// LinkifyFilePaths wraps recognized file paths in OSC 8 hyperlinks.
// Only activates when terminal supports it (detected via TERM_PROGRAM).
func LinkifyFilePaths(text string, workDir string) string

// SupportsHyperlinks checks if the current terminal supports OSC 8.
func SupportsHyperlinks() bool
```

**OSC 8 format**: `\033]8;;file:///absolute/path\033\\display text\033]8;;\033\\`

**Detection**: Match paths that:
- Start with `./`, `../`, `/`, or look like `dir/file.ext`
- End with a recognized extension or contain `/`
- Don't contain spaces (avoid false positives in prose)

**Integration**: Apply `LinkifyFilePaths` in:
1. `ToolBoxRenderer.RenderToolResult` — file paths in tool output
2. `ToolBoxRenderer.RenderToolCall` — file path in tool call args

#### Implementation Steps (TDD)

- [ ] **Test**: `TestSupportsHyperlinks_iTerm` — returns true when TERM_PROGRAM=iTerm.app
- [ ] **Test**: `TestSupportsHyperlinks_Unknown` — returns false for unknown terminals
- [ ] **Impl**: `SupportsHyperlinks()` checking `TERM_PROGRAM` env var
- [ ] **Test**: `TestLinkifyFilePaths_AbsolutePath` — `/foo/bar.go` wrapped in OSC 8
- [ ] **Test**: `TestLinkifyFilePaths_RelativePath` — `./src/main.go` resolved and wrapped
- [ ] **Test**: `TestLinkifyFilePaths_NoMatch` — prose text unchanged
- [ ] **Test**: `TestLinkifyFilePaths_DisabledTerminal` — returns text unchanged when unsupported
- [ ] **Impl**: `LinkifyFilePaths` with path regex and OSC 8 wrapping
- [ ] **Impl**: Integrate into `ToolBoxRenderer`

---

## Dependency Graph

```
PR 1 (Collapsible Tools + Diff Color)  ─┐
PR 2 (Status Bar + Timer)               ─┤─→ All independent, merge in any order
PR 3 (@ File Mentions)                  ─┤
PR 4 (History + Approval + Links)       ─┘
```

No dependencies between PRs. Recommended merge order by value/risk:
1. **PR 2** (smallest, safest — status bar is pure additive)
2. **PR 1** (highest UX impact — collapsible tools)
3. **PR 4** (three small features, moderate risk)
4. **PR 3** (largest, most complex — file indexing + new overlay)

## Estimated Scope

| PR | New Files | Modified Files | Est. LOC | Tests |
|----|-----------|---------------|----------|-------|
| 1  | 0         | toolbox.go, model.go, update.go, view.go | ~200 | ~12 |
| 2  | 0         | statusbar.go, model.go, update.go, view.go, main.go | ~80 | ~6 |
| 3  | 1 (filecompletion.go) | model.go, update.go, view.go, main.go | ~300 | ~12 |
| 4  | 2 (inputhistory.go, hyperlink.go) | approval.go, model.go, update.go | ~250 | ~18 |
| **Total** | **3** | **~8** | **~830** | **~48** |
