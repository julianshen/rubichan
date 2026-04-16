# RTK Shell Tools Research

**Branch**: `claude/research-rtk-shell-tools-sTSYa`  
**Date**: 2026-04-16  
**Source**: https://github.com/rtk-ai/rtk/tree/master

---

## What RTK Is

RTK (rtk-ai/rtk) is a Rust CLI proxy that intercepts shell command output and
compresses it before it reaches an AI agent's context window. It is not an
agent — it is transparent middleware. The user or a PreToolUse hook rewrites
`git status` to `rtk git status`; the agent receives compressed output and
never sees the rewrite. RTK reports 60–90% token reduction on typical
development workflows.

### Repository Composition

| Layer | Path | Purpose |
|---|---|---|
| Core | `src/core/` | Filter pipeline, runner, config, tee, tracking, TOML registry |
| Command handlers | `src/cmds/` | Per-command Rust modules (git, system, go, js, python, …) |
| Declarative filters | `src/filters/*.toml` | 59 TOML definitions for tools without Rust handlers |
| Discovery | `src/discover/` | Rewrites live commands; mines session history for missed optimizations |
| Learning | `src/learn/` | Detects fail→correct CLI mistake patterns, auto-generates rules |
| Hooks | `hooks/` | PreToolUse scripts for Claude Code, Cursor, Copilot, Gemini, Windsurf, Cline |

Primary language: **Rust**. Single static binary, zero runtime dependencies.

---

## RTK's Core Techniques

### 1. Eight-Stage Filter Pipeline (`src/core/toml_filter.rs`)

Every filter, whether written in Rust or declared in TOML, runs through the
same ordered pipeline:

```
Stage 1: strip_ansi          — remove terminal escape sequences
Stage 2: replace             — line-by-line regex substitution (chained rules)
Stage 3: match_output        — if blob matches pattern → short-circuit with summary message
Stage 4: strip / keep lines  — regex-based line filter (mutually exclusive)
Stage 5: truncate_lines_at   — cap individual line length in characters
Stage 6: head + tail         — keep first N + last N lines, elide middle
Stage 7: max_lines           — absolute output cap
Stage 8: on_empty            — fallback message when nothing remains
```

Head+tail elision format:
```
line 1
line 2
... (847 lines omitted)
line 850
line 851
```

TOML filter schema (one file per command family):
```toml
[filters.pytest]
description         = "pytest test runner output"
match_command       = "^pytest"
strip_ansi          = true
strip_lines_matching = [
  "^platform ",
  "^rootdir:",
  "^plugins:",
  "^collecting …",
]
tail_lines          = 30
on_empty            = "No output"

[[tests.pytest]]
input    = "platform linux\nrootdir: /app\n1 passed in 0.5s"
expected = "1 passed in 0.5s"
```

Priority cascade: **project-local** `.rtk/filters.toml` (trust-gated) →
**user-global** `~/.config/rtk/filters.toml` → **built-in** (compiled in).

### 2. Git Command Compression (`src/cmds/git/git.rs`)

| Subcommand | Strategy |
|---|---|
| `git status` | Parses `--porcelain` output. Groups into Staged / Modified / Untracked / Conflict sections. Caps each section (15 / 15 / 10 files) with `+N more`. |
| `git log` | Truncates commit messages to 80 chars, caps at 10 commits, strips `Signed-off-by` / `Co-authored-by` trailers, keeps up to 3 body lines per commit. |
| `git diff` | Per-hunk truncation at 100 changed lines. `+X -Y` per-file counters. Recovery hint appended: `[full diff: rtk git diff --no-compact]`. |

### 3. Log Deduplication (`src/cmds/system/log_cmd.rs`)

Normalizes log lines with five patterns before comparison:

| Pattern | Replaced With |
|---|---|
| Timestamps | *(stripped)* |
| UUIDs | `<UUID>` |
| Hex values | `<HEX>` |
| Numbers ≥ 4 digits | `<NUM>` |
| File paths | `<PATH>` |

Counts occurrences; collapses N identical normalized lines into one entry
with `[×N]`. Displays top-10 errors, top-5 warnings sorted by frequency.

Example:
```
# raw (47 lines)
[ERROR] Connection refused at 14:32:01
[ERROR] Connection refused at 14:32:02
…

# filtered (1 line)
[ERROR] Connection refused [×47]
```

### 4. File Read Filtering (`src/core/filter.rs`, `src/cmds/system/read.rs`)

Language detected from file extension. Three filter levels:

| Level | Effect |
|---|---|
| None | Raw content |
| Minimal | Remove comments, normalize multiple blank lines to max 2 |
| Aggressive | Keep only imports + function/class signatures; replace bodies with `// ... implementation` |

Safety fallback: if filtering empties a non-empty file, revert to raw content
with a warning.

### 5. System Command Compression

| Command | Strategy |
|---|---|
| `ls -la` | Parse with date-field anchor regex, skip `.`/`..`/`total`, exclude noise dirs (`node_modules`, `.git`, `target`, `__pycache__`), human-readable sizes, extension frequency summary |
| `tree` | Auto-inject `-I 'node_modules\|.git\|target\|…'` unless `-a` given; strip summary lines |
| `find` | Group results by directory; path abbreviation (`...last-47-chars`); top-5 extension summary; cap at N files |
| `grep`/`rg` | Group by filename with match count; per-file result cap; context window centered on match; long path compression to `first/.../last` |
| `wc` | Compact single-line summary |
| `ps` | Keep only significant process fields |

### 6. Failure Output Preservation (`src/core/tee.rs`)

When a filter truncates output or a command fails, RTK writes the complete raw
output to `~/.local/share/rtk/tee/<timestamp>-<command>.txt` and appends a
hint to the LLM result:

```
[Full output saved to: ~/.local/share/rtk/tee/20260416-143201-git-diff.txt]
```

Config: `TeeConfig { enabled, mode: Never|Failures|Always, max_files: 20,
max_file_size: 1MB }`. Old files are rotated automatically.

### 7. Command Rewrite Hook for Claude Code (`hooks/claude/rtk-rewrite.sh`)

A `PreToolUse` Bash hook reads the tool's `command` field from JSON via `jq`,
delegates to `rtk rewrite`, and interprets exit codes:

| Exit | Meaning | Hook action |
|---|---|---|
| 0 | Rewrite found, safe | Output rewritten command + `"permissionDecision": "allow"` |
| 1 | No RTK equivalent | Pass command through unchanged |
| 2 | Deny rule triggered | Let native denial handle it |
| 3 | Rewrite found, needs confirmation | Output rewritten command, omit permission decision |

The LLM never sees the rewrite. It issues `git status`; it receives compressed
output.

### 8. Token Analytics (`src/core/tracking.rs`)

SQLite database at `~/.local/share/rtk/history.db`. Records per-execution:
command, raw size, filtered size, estimated token savings (`chars / 4`),
execution time, project path. Retention: 90 days. Powers:

- `rtk gain` — cumulative savings report with per-command breakdown
- `rtk session` — savings for recent sessions
- `rtk discover` — mines Claude Code `.jsonl` session files to find commands
  that _could_ have been rewritten but weren't

### 9. Discover & Learn (`src/discover/`, `src/learn/`)

**discover**: 73-rule regex registry maps command patterns to `rtk <cmd>`
rewrites. For live interception: match → rewrite. For history analysis: parse
session JSONL, find unoptimized commands, report missed savings.

**learn**: Analyzes session history for fail→succeed command pairs. Classifies
error type (unknown flag, wrong path, missing argument). Assigns confidence
score. Auto-generates correction rules above threshold.

---

## Rubichan's Current Shell Tool State

| Aspect | Current Implementation | File |
|---|---|---|
| LLM output cap | Naive head chop at 30 KB | `internal/tools/limits.go:5` |
| Display cap | Naive head chop at 100 KB | `internal/tools/limits.go:7` |
| Truncation strategy | `output[:maxOutputBytes] + "\n... output truncated"` — head only, tail lost | `internal/tools/shell.go:431` |
| ANSI stripping | None — raw escape codes reach the LLM | — |
| Command-aware filtering | None — `ls`, `git log`, `pytest` all treated identically | — |
| Log deduplication | None | — |
| Failure output preservation | None — failed commands lose output same as success | — |
| Token analytics | None | — |

Rubichan does have strong points RTK lacks: OS-level sandboxing (seatbelt /
bubblewrap), command substitution blocking, recursive-rm interception,
diff tracking, background process management, and a streaming event
architecture. These are orthogonal to output filtering and should be
preserved.

---

## Token Reduction Benchmarks (from RTK README)

Measured over a representative 30-minute Claude Code session:

| Command family | Raw tokens | Filtered tokens | Reduction |
|---|---|---|---|
| `ls` / `tree` | 2,000 | 400 | −80% |
| File reads (`cat`) | 40,000 | 12,000 | −70% |
| Test runners | 25,000 | 2,500 | −90% |
| **Session total** | **~118,000** | **~23,900** | **−80%** |

Per-category savings from RTK's rule registry:

| Category | Example commands | Claimed savings |
|---|---|---|
| Git | git, yadm | 70% |
| GitHub CLI | gh pr/issue/run | 82% |
| Test runners | vitest, jest, pytest | 90–99% |
| Linters | eslint, biome, tsc | 83–87% |
| Build tools | cargo, next build | 80–87% |
| Infra | docker, kubectl, aws | 80–85% |
| Go toolchain | go test/build, golangci-lint | 85% |

---

## Improvement Opportunities for Rubichan

### Quick Wins (Low Complexity, High Impact)

**QW-1: Head+tail truncation**  
Replace `output[:maxOutputBytes]` with a head+tail window. Keep the first
`N` lines and last `M` lines, elide the middle. The tail is where errors,
test failure summaries, and build results appear — losing it on truncation
is a significant quality regression.

**QW-2: ANSI escape code stripping**  
Strip terminal color/formatting sequences before storing output in the
`ToolResult`. ANSI codes consume tokens without conveying meaning to the LLM.
A single `go test` run with color enabled can waste hundreds of tokens on
escape sequences alone.

**QW-3: Failure output preservation (tee)**  
On non-zero exit code, write full raw output to a temp file and append the
path as a hint. The LLM can re-read it via the file tool without re-running
the command. Prevents information loss when filtered/truncated output omits
the root cause.

**QW-4: Noise directory exclusion for ls / tree**  
When `ls -la` or `tree` is invoked through the shell tool, automatically
inject ignore patterns for `node_modules`, `.git`, `target`, `__pycache__`,
`.next`, `dist`, `vendor`. Skip when `-a` is present (respecting user intent).

### Medium Complexity, High Impact

**MI-1: Log deduplication**  
Detect when shell output is a log stream (consecutive timestamped lines).
Normalize and collapse repeated lines with `[×N]` counts. Particularly
valuable for build systems, test runners, and server logs.

**MI-2: Git output compression in shell tool**  
When `git status`, `git log`, or `git diff` is invoked through the shell
tool (not the dedicated git tools), apply structured compression: porcelain
parsing for status, message truncation and trailer stripping for log, per-hunk
limiting for diff. Rubichan's dedicated git tools handle the structured path
but raw `git` through shell is unfiltered.

**MI-3: Test runner summarization**  
Detect `go test`, `pytest`, `cargo test`, `npm test`, `jest`, `vitest` output
patterns. Extract pass/fail/skip counts + failure details only. Discard
passing test output. A full `go test -v ./...` on a large codebase can produce
megabytes of output; only failures and the summary line matter to the LLM.

**MI-4: Language-aware `cat` filtering**  
When `cat <file>` is invoked through shell, detect the file's language from
its extension and apply minimal filtering: strip comment blocks, collapse
repeated blank lines to max 2. (Aggressive mode — signature-only — is better
left to an explicit agent request.)

### Architectural / Strategic

**AR-1: Declarative output filter rules (TOML/YAML)**  
Introduce a per-command filter registry analogous to RTK's TOML system. Store
filter definitions at `~/.config/rubichan/filters.toml` and
`.rubichan/filters.toml` (project-local, trust-gated). Each definition
specifies `match_command` regex plus pipeline stages. Enables users to add
filters for internal tools without modifying rubichan source.

**AR-2: Token usage analytics**  
Track raw vs. filtered output sizes per command. Store in the SQLite database
(already planned in Milestone 4). Surface via `rubichan stats` or in the TUI
status bar. Helps users and developers understand the real impact of filtering.

**AR-3: Session history mining**  
Analyse completed session logs to identify commands with large raw output that
had no filter applied. Report them as optimization opportunities. Analogous to
`rtk discover`.

---

## Prioritized Improvement Matrix

| ID | Opportunity | Token Impact | Complexity | Milestone Fit |
|---|---|---|---|---|
| QW-1 | Head+tail truncation | High | Low | M1 (agent loop) |
| QW-2 | ANSI stripping | High | Low | M1 (agent loop) |
| QW-3 | Failure tee | Medium | Low | M1 (agent loop) |
| QW-4 | Noise dir exclusion | Medium | Low | M1 (shell tool) |
| MI-1 | Log deduplication | High | Medium | M2 (tool layer) |
| MI-2 | Git shell compression | High | Medium | M2 (tool layer) |
| MI-3 | Test runner summary | High | Medium | M2 (tool layer) |
| MI-4 | Language-aware cat | Medium | Medium | M2 (tool layer) |
| AR-1 | Declarative filters | High | High | M3 (skill system) |
| AR-2 | Token analytics | Low | High | M4 (SQLite) |
| AR-3 | Session mining | Low | High | M4 (SQLite) |

Milestone alignment uses rubichan's existing roadmap from `CLAUDE.md`.

---

## What NOT to Copy

RTK's hook-based command rewriting (rewiring `git` → `rtk git` in the shell)
is elegant for a proxy tool but wrong for rubichan. Rubichan owns the tool
execution loop directly — it can filter output at the point of capture without
any shell hook indirection. The equivalent in rubichan is a filter applied
inside `ExecuteStream` after `wg.Wait()` at `shell.go:408`.

RTK's separate binary model also does not apply. Rubichan is a single agent
binary; filtering belongs inside `internal/tools/` as a pure library concern.

---

## Key Source Locations in RTK

| File | What to study |
|---|---|
| `src/core/toml_filter.rs` | Full 8-stage pipeline + TOML schema |
| `src/core/filter.rs` | Language-aware comment/whitespace filter + smart_truncate |
| `src/core/runner.rs` | run_filtered() — how filters hook into command execution |
| `src/core/tee.rs` | Failure output preservation |
| `src/core/tracking.rs` | Token savings analytics with SQLite |
| `src/cmds/git/git.rs` | Git status/log/diff compression |
| `src/cmds/system/ls.rs` | ls compact format |
| `src/cmds/system/log_cmd.rs` | Log deduplication with normalization |
| `src/cmds/system/summary.rs` | Auto-detect output type + summarize |
| `src/cmds/system/grep_cmd.rs` | Grep grouping + path compression |
| `src/cmds/system/find_cmd.rs` | Find directory grouping |
| `src/discover/rules.rs` | Full 73-rule rewrite registry |
| `hooks/claude/rtk-rewrite.sh` | Claude Code PreToolUse hook |
