# Shell Output Filtering — Design Document

> **Date:** 2026-04-16 · **Status:** Draft  
> **Research:** `docs/rtk-shell-tools-research.md`  
> **Feature:** RTK-inspired output filtering to reduce LLM token consumption

---

## 1. Overview

Rubichan's shell tool currently sends raw command output to the LLM, capped
at a naive 30 KB head-chop. RTK (rtk-ai/rtk) demonstrates that command-aware
output compression achieves 60–90% token reduction without losing actionable
information.

This document specifies a layered filtering system that:

1. Replaces naive head-chop with **head+tail windowing** and **ANSI stripping**
2. Adds **log deduplication** for build/test/server output
3. Adds **command-aware compression** for git, test runners, ls/tree/find/grep
4. Introduces a **declarative filter rule** layer for extensibility
5. Adds **failure output preservation** (tee) so full output is never truly lost

The existing security model (sandboxing, command substitution blocking,
recursive-rm interception) is untouched. The existing streaming architecture
(`EventBegin`/`EventDelta`/`EventEnd`) is untouched. Filtering is a pure
post-execution transformation applied to the captured byte buffer.

### Scope Boundary

This design covers output **post-processing** only. It does not cover:
- Shell mode UI (see `2026-03-30-shell-enhancements-design.md`)
- Agent loop or provider layer
- The dedicated `git_status`/`git_diff`/`git_log` tool family

---

## 2. Architecture

### 2.1 Filter Pipeline

Filtering is applied inside `ShellTool.ExecuteStream` immediately after
`wg.Wait()` captures the complete byte buffer, before truncation is applied.

```
raw output (bytes)
        │
        ▼
[StripANSI]          — remove terminal escape sequences
        │
        ▼
[LogDeduplicator]    — collapse repeated log lines with [×N] counts
        │
        ▼
[CommandFilter]      — command-aware compression (git, test, ls, …)
        │
        ▼
[HeadTailWindow]     — keep first N + last M lines, elide middle
        │
        ▼
 LLM content (≤ 30 KB)
```

Each stage is a pure function `func(input string, cmd string) string` (or
equivalent). Stages are composable and independently testable. A stage that
recognises nothing passes input through unchanged.

**Why LogDeduplicator precedes CommandFilter**: log dedup operates on raw line
structure (timestamps, repeated messages). Running it before command-specific
compression ensures it sees the original log lines, not structured output that
a command filter may have already transformed. Command filters (e.g. git, test
runner) are designed for their own output format and do not produce log-like
lines that dedup would accidentally collapse.

### 2.2 New Package: `internal/tools/filter`

All filtering logic lives in a new package to keep `shell.go` clean and make
the filter stages unit-testable in isolation.

```
internal/tools/filter/
    ansi.go          — StripANSI(s string) string
    ansi_test.go
    headtail.go      — HeadTail(s string, head, tail int) string
    headtail_test.go
    logdedup.go      — DeduplicateLog(s string) string
    logdedup_test.go
    git.go           — CompressGitOutput(subcmd, output string) string
    git_test.go
    testrunner.go    — SummarizeTestOutput(cmd, output string) string
    testrunner_test.go
    dirlist.go       — CompressDirList(cmd, output string) string
    dirlist_test.go
    grep.go          — CompressGrepOutput(output string) string
    grep_test.go
    rules.go         — Rule registry + TOML loader
    rules_test.go
    pipeline.go      — Apply(cmd, output string, rules []Rule) string
    pipeline_test.go
```

### 2.3 Integration Point in `shell.go`

Current code at `shell.go:428–452`:

```go
content := string(output)
var displayContent string
if len(output) > maxOutputBytes {
    content = string(output[:maxOutputBytes]) + "\n... output truncated"
    ...
}
```

Becomes:

```go
rawOutput := string(output)

// DisplayContent: raw output for the human viewer (ANSI preserved, uncompressed).
// Populated only when output exceeded the LLM cap so the user still sees full output.
var displayContent string
if len(output) > maxOutputBytes {
    displayContent = rawOutput
    if len(output) > maxDisplayBytes {
        displayContent = rawOutput[:maxDisplayBytes] + "\n... output truncated"
    }
}

// filter.Apply owns the LLM byte cap. All pipeline stages run inside Apply,
// which guarantees len(result) ≤ maxOutputBytes on return.
content := filter.Apply(in.Command, rawOutput)

// Defensive assertion: Apply guarantees the cap, but guard against filter bugs.
if len(content) > maxOutputBytes {
    content = filter.HeadTail(content, headLines, tailLines)
}
```

`filter.Apply` is the sole pipeline entry point. It is responsible for the byte
cap — callers must not impose an independent truncation before calling it.
`DisplayContent` is set from `rawOutput` before filtering so the human always
sees uncompressed, ANSI-coloured output regardless of what the LLM receives.

### 2.4 Failure Tee

When exit code is non-zero **and** output was filtered or truncated, write the
complete raw output to a persistent per-user directory and append a hint line
to the LLM content:

```
[Full output: ~/.cache/rubichan/tee/20260416-143201-a3f2.txt]
```

**Tee directory**: `os.UserCacheDir()/rubichan/tee/` (resolves to
`~/.cache/rubichan/tee/` on Linux, `~/Library/Caches/rubichan/tee/` on macOS).
This survives reboots unlike `os.TempDir()` (`/tmp`), so hint paths remain
valid across sessions. The agent can re-read the file via the file tool without
re-running the command.

The tee directory is capped at 20 files; oldest files are removed on each new
write. Enabled by default; disabled via `config.Shell.DisableTee = true`.

---

## 3. Feature Specifications

### 3.1 ANSI Stripping (`filter/ansi.go`)

Strip all ANSI SGR sequences (`ESC[...m`), cursor control sequences, and OSC
sequences before any other processing.

Regex: `\x1b\[[0-9;]*[mGKHFABCDJsu]` plus `\x1b\][^\x07]*\x07`.

Input (42 chars of escapes, 8 chars of text):
```
\x1b[32m✓ passed\x1b[0m
```
Output:
```
✓ passed
```

Always applied. Zero configuration.

### 3.2 Head+Tail Windowing (`filter/headtail.go`)

Replace the naive `output[:maxOutputBytes]` chop.

Parameters (configurable, with sensible defaults):
- `headLines = 200` — lines to keep from the start
- `tailLines = 100` — lines to keep from the end
- `maxBytes = 30 * 1024` — absolute byte cap (applied after line windowing)

When `total > headLines + tailLines`:
```
<first 200 lines>
... (N lines omitted)
<last 100 lines>
```

When total ≤ headLines + tailLines: pass through unchanged (no elision message
injected unnecessarily).

Byte cap is a second safety net applied to the already-windowed string. If
windowed output still exceeds `maxBytes`, apply a final `[:maxBytes]` chop
at a newline boundary.

### 3.3 Log Deduplication (`filter/logdedup.go`)

Triggered when output contains ≥ 10 lines that structurally resemble log
entries (i.e., begin with a timestamp, log level, or bracket pattern).

Detection heuristic: if ≥ 40% of lines match
`^\s*(\d{4}[-/]\d{2}[-/]\d{2}|[A-Z]{3,5}|\[\w+\])` → treat as log stream.

Normalization replacements (in order):
1. ISO timestamps `\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}` → `<TIME>`
2. UUIDs `[0-9a-f]{8}-[0-9a-f]{4}-…` → `<UUID>`
3. Hex strings `0x[0-9a-fA-F]{4,}` → `<HEX>`
4. Large numbers `\b\d{5,}\b` → `<NUM>`
5. Absolute paths `/[a-zA-Z0-9_/.-]{10,}` → `<PATH>`

Count normalized occurrences. Emit lines in **first-occurrence order** —
temporal sequence is preserved. For any line with count ≥ 2, replace it with
the first occurrence (original text, not normalized) and append `[×N]`:

```
[ERROR] Connection refused at 14:32:01 [×47]
[WARN]  Retry attempt 3 of 5 [×12]
[INFO]  Server started on :8080
```

Output order mirrors the original log order (by first occurrence), not
re-sorted by severity. This preserves the causal sequence of events, which
matters when diagnosing startup failures or cascading errors. When unique line
count exceeds `maxUniqueLines = 30`, the excess is truncated with a count
message.

### 3.4 Git Output Compression (`filter/git.go`)

Applied when the command matches `^git\s+(status|log|diff|show)\b`. Commands
with flags before the subcommand (e.g. `git -C /path status`,
`git --no-pager log`) are a known limitation: the regex will not match and
output passes through unchanged. This is acceptable for the initial
implementation; a shell-argument parser can be added later.

**git status** — two format paths:

*Porcelain format* (detected when first non-empty line matches
`^[MADRCU? ][MADRCU? ] `): parse two-char status codes.

| Symbol | Section |
|---|---|
| `M `, `A `, `D `, `R `, `C ` (first col) | Staged |
| ` M`, ` D` (second col) | Modified |
| `??` | Untracked |
| `UU`, `AA`, `DD` | Conflicts |

*Human-readable format* (default output of plain `git status`): detect section
headers and extract file lists.

| Header | Section |
|---|---|
| `Changes to be committed:` | Staged |
| `Changes not staged for commit:` | Modified |
| `Untracked files:` | Untracked |

Both paths produce the same structured output:
```
Staged (3): main.go, cmd/agent/main.go, internal/tools/shell.go
Modified (7): internal/agent/agent.go ... +5 more
Untracked (2): docs/notes.txt, scratch.sh
```

Cap: 15 staged, 15 modified, 10 untracked. Conflicts always shown in full.
If neither format is detected (unknown `git status` flags or custom format),
pass through unchanged.

**git log** — detect log output by leading `commit [0-9a-f]{40}` lines:

- Truncate commit message lines to 100 chars
- Strip `Signed-off-by:`, `Co-authored-by:`, `Change-Id:` trailers
- Keep up to 3 body lines per commit
- Cap at 15 commits total; append `... (N more commits)` if needed

**git diff** — detect by `diff --git` headers:

- Keep file headers (`diff --git`, `index`, `---`, `+++`) intact
- Per-hunk: keep `@@` context line + first 60 added/removed lines
- Excess hunk lines → `... (N lines omitted in this hunk)`
- Append `+X -Y` total counter at end
- If any truncation occurred: append hint
  `[Full diff available: re-run with git diff | head -n 500]`

### 3.5 Test Runner Summarization (`filter/testrunner.go`)

Detect test runner output by command prefix and/or output patterns.

| Trigger | Patterns to detect |
|---|---|
| `^go test` | `^--- FAIL`, `^--- PASS`, `^ok`, `^FAIL`, `^=== RUN` |
| `^pytest` | `PASSED`, `FAILED`, `ERROR`, `=====`, `short test summary` |
| `^cargo test` | `test .* \.\.\. ok`, `test .* \.\.\. FAILED`, `test result:` |
| `^npm test`, `^jest`, `^vitest` | `✓`, `✗`, `PASS`, `FAIL`, `Tests:` |

Strategy:
1. Extract summary line (pass/fail/skip counts) — always kept
2. Extract all failure blocks — always kept
3. Discard passing test lines (`=== RUN …`, `--- PASS …`, `✓ …`)
4. If no failures: emit `All N tests passed.`
5. If failures: emit counts + full failure output

Example (`go test -v ./...`, 2,500 lines → ~30 lines):
```
go test ./...
FAIL internal/tools: 2 failures

--- FAIL: TestShellToolTimeout (0.01s)
    shell_test.go:89: expected timeout error, got nil

--- FAIL: TestSandboxPolicy (0.00s)
    shell_sandbox_test.go:212: policy mismatch

ok  internal/agent (47 tests)
ok  internal/provider (23 tests)
FAIL
```

### 3.6 Directory Listing Compression (`filter/dirlist.go`)

**`ls` command** — detected by command matching `^ls(\s|$)`.

When output is from `ls -la` / `ls -l` (long format):
- Parse with date-field anchor (month-name anchor regex)
- Skip `.`, `..`, `total` lines
- Exclude noise directories unless `-a` flag present:
  `node_modules`, `.git`, `target`, `__pycache__`, `.next`, `dist`, `vendor`,
  `.cache`, `coverage`, `build`, `.idea`, `.vscode`
- Show directories first with `/` suffix
- Human-readable sizes: 500B, 1.2K, 3.4M
- Append extension frequency summary: `[38F 6D: .go×22 .md×8 .json×5 .yaml×3]`

When output is from plain `ls` (no `-l`): pass through (already compact).

**`tree` command** — detected by `^tree(\s|$)`:
- If no `-I` in original command, note that noise dirs would improve output
  (rubichan does not rewrite the command, but can suggest via warning)
- Strip trailing summary line (`N directories, M files`)
- Cap at 200 lines; head+tail elision if exceeded

**`find` command** — detected by `^find(\s|$)`:
- Group results by parent directory
- Cap at 50 results total; append `+N more` if exceeded
- Abbreviate long paths: keep last 47 chars → `...last/47/chars`
- Append extension frequency summary

### 3.7 Grep/Ripgrep Compression (`filter/grep.go`)

Detected by command matching `^(grep|rg|ripgrep)(\s|$)`.

- Group matches by filename
- Per-file cap: 5 matches (configurable)
- Excess per file: `... (+N more in this file)`
- Total file cap: 20 files; `... (+N more files)`
- Long match lines: center a 60-char window around the match + `...`
- Long paths: compress to `first/.../last` format

### 3.8 Declarative Filter Rules (`filter/rules.go`)

A TOML-based rule system for commands not covered by built-in Rust handlers.
Loaded at startup from:
1. `.rubichan/filters.toml` (project-local, requires explicit trust grant)
2. `~/.config/rubichan/filters.toml` (user-global)

Schema:

```toml
schema_version = 1

[filters.my-tool]
description         = "My internal build tool"
match_command       = "^my-build"
strip_ansi          = true
strip_lines_matching = [
  "^Downloading",
  "^Resolving",
]
tail_lines          = 40
on_empty            = "Build produced no output"

# match_output: short-circuit entire output with a canned message if the
# full output blob matches this pattern (useful for "build succeeded" cases).
[[filters.my-tool.match_output]]
pattern = "BUILD SUCCESS"
message = "Build succeeded."
unless  = "ERROR"          # optional: suppress match when this also matches
```

Supported fields: `match_command`, `strip_ansi`, `strip_lines_matching`,
`keep_lines_matching`, `replace` (pattern+replacement pairs), `head_lines`,
`tail_lines`, `max_lines`, `on_empty`, `match_output` (array of
`{pattern, message, unless?}` short-circuit rules).

`strip_lines_matching` and `keep_lines_matching` are mutually exclusive — a
validation error is returned at load time if both are present.

Project-local filters require `rubichan trust .rubichan/filters.toml` to
activate. Untrusted project filters are silently skipped with a warning
printed to stderr only (not sent to LLM).

---

## 4. Configuration

New section in `~/.config/rubichan/config.toml`:

```toml
[shell.output_filter]
enabled              = true          # master switch; false → bypass all filtering
ansi_strip           = true
log_dedup            = true
git_compress         = true
test_summarize       = true
dir_compress         = true
grep_compress        = true

head_lines           = 200
tail_lines           = 100

[shell.tee]
enabled              = true
mode                 = "failures"    # "never" | "failures" | "always"
max_files            = 20
```

---

## 5. Correctness Invariants

1. **Pass-through on error**: If any filter stage panics or returns empty for
   non-empty input, the previous stage's output is used (never lose content).
2. **Failure full output**: Non-zero exit code always triggers tee write
   regardless of filter settings, unless `tee.enabled = false`.
3. **No command modification**: Filtering is output-only. The command string
   sent to `sh -c` is never altered.
4. **Streaming unaffected**: `EventDelta` events during execution emit raw
   bytes. Filtering applies only to the final `ToolResult.Content`, not to
   streaming events. Users see full output in real time; LLM sees compressed.
5. **Idempotent**: Applying a filter twice produces the same result as
   applying it once.
6. **DisplayContent preserved**: `ToolResult.DisplayContent` is populated from
   the raw (pre-filter) byte buffer when output exceeded `maxOutputBytes`.
   It retains ANSI codes and is not passed through any compression stage.
   The human viewer always sees the unmodified output; only the LLM receives
   the compressed version.

---

## 6. Non-Goals

- Command rewriting (RTK-style hook) — rubichan owns execution, no hook needed
- LLM-powered summarization of arbitrary output — out of scope, different cost
- Token counting via LLM tokenizer — approximate char/4 is sufficient
- Windows support for tee paths — follow existing OS path conventions

---

## 7. Open Questions

1. Should `head_lines`/`tail_lines` be per-command configurable, or global
   only? Start global; per-command in declarative rules handles edge cases.
2. Should git compression apply when the user explicitly pipes git output
   (`git log | grep foo`)? No — only apply when `git` is the root command.
3. Should test runner summarization be skipped when the user passes `-v` or
   `--verbose`? Yes — respect explicit verbosity flags.
