# Shell Output Filtering — TDD Implementation Plan

> **Date:** 2026-04-16 · **Status:** Draft  
> **Design:** `2026-04-16-shell-output-filtering-design.md`  
> **Research:** `docs/rtk-shell-tools-research.md`

---

## Stage 0: Package Scaffold

- [ ] **0.1** `TestPackageCompiles` — Create `internal/tools/filter/` package
  with a no-op `Apply(cmd, output string) string` that returns `output`
  unchanged. Verify `go build ./internal/tools/filter/...` passes.

---

## Stage 1: ANSI Stripping

- [ ] **1.1** `TestStripANSIEmpty` — `StripANSI("")` returns `""`.
- [ ] **1.2** `TestStripANSINoEscapes` — Plain text passes through unchanged.
- [ ] **1.3** `TestStripANSIColorCode` — `"\x1b[32mgreen\x1b[0m"` → `"green"`.
- [ ] **1.4** `TestStripANSIBoldAndReset` — `"\x1b[1;31mred bold\x1b[0m"` →
  `"red bold"`.
- [ ] **1.5** `TestStripANSICursorControl` — Cursor movement sequences (`\x1b[A`,
  `\x1b[2J`) are removed.
- [ ] **1.6** `TestStripANSIOSCSequence` — OSC sequences (`\x1b]0;title\x07`)
  are removed.
- [ ] **1.7** `TestStripANSIMultiline` — Strip works correctly across multiple
  lines (newlines preserved, only escape sequences removed).
- [ ] **1.8** `TestStripANSIPreservesUnicode` — Multi-byte Unicode characters
  survive stripping unchanged.

---

## Stage 2: Head+Tail Windowing

- [ ] **2.1** `TestHeadTailBelowThreshold` — Input with fewer lines than
  `head + tail` passes through with no elision message inserted.
- [ ] **2.2** `TestHeadTailExactThreshold` — Input with exactly `head + tail`
  lines passes through unchanged.
- [ ] **2.3** `TestHeadTailElision` — Input with `head + tail + 50` lines
  produces `head` leading lines + `"... (50 lines omitted)"` + `tail` trailing
  lines.
- [ ] **2.4** `TestHeadTailElisionCount` — The omitted count in the message is
  exactly `total - head - tail`.
- [ ] **2.5** `TestHeadTailZeroTail` — `tail = 0` keeps only first `head` lines
  with elision at end; no trailing section.
- [ ] **2.6** `TestHeadTailZeroHead` — `head = 0` keeps only last `tail` lines
  with elision at start.
- [ ] **2.7** `TestHeadTailPreservesNewlines` — Output ends with a newline iff
  the original input ended with a newline.
- [ ] **2.8** `TestHeadTailByteCapNewlineBoundary` — When windowed output still
  exceeds `maxBytes`, final chop occurs at a newline boundary, not mid-line.
- [ ] **2.9** `TestHeadTailEmptyInput` — `HeadTail("", 200, 100)` returns `""`.

---

## Stage 3: Log Deduplication

- [ ] **3.1** `TestLogDedupNoLogLines` — Non-log output (e.g. plain Go source
  code, ~5% log-pattern lines) is returned unchanged.
- [ ] **3.2** `TestLogDedupDetectionThreshold` — Input with ~80% log-pattern
  lines triggers dedup; input with ~10% log-pattern lines does not. Tests use
  clearly supra- and sub-threshold inputs, not boundary values.
- [ ] **3.3** `TestLogDedupTimestampNormalization` — Lines differing only by
  ISO timestamp are collapsed into one `[×N]` entry.
- [ ] **3.4** `TestLogDedupUUIDNormalization` — Lines differing only by UUID
  are collapsed.
- [ ] **3.5** `TestLogDedupHexNormalization` — Lines differing only by hex
  values are collapsed.
- [ ] **3.6** `TestLogDedupLargeNumberNormalization` — Numbers ≥ 5 digits are
  normalized before comparison.
- [ ] **3.7** `TestLogDedupUniqueLines` — Lines that do not repeat are preserved
  verbatim (first occurrence, no `[×1]` tag).
- [ ] **3.8** `TestLogDedupCountAccuracy` — Three identical normalized lines →
  `[×3]`, not `[×2]` or `[×4]`.
- [ ] **3.9** `TestLogDedupPreservesOrder` — Deduplicated output preserves the
  order of first occurrence.
- [ ] **3.10** `TestLogDedupMaxUniqueLines` — When unique line count exceeds
  `maxUniqueLines`, excess lines are truncated with a count message.
- [ ] **3.11** `TestLogDedupMultibyteCharacters` — Lines containing multi-byte
  Unicode are normalized and counted correctly without panicking.
- [ ] **3.12** `TestLogDedupShortInput` — Input with fewer than 10 lines is
  returned unchanged (deduplification not worth the overhead).

---

## Stage 4: Git Output Compression

### 4A. git status

- [ ] **4.1** `TestGitStatusEmpty` — Empty `git status --porcelain` output →
  `"nothing to commit, working tree clean"`.
- [ ] **4.2** `TestGitStatusStagedFiles` — Lines with first-column `M` are
  grouped under `Staged (N):`.
- [ ] **4.3** `TestGitStatusModifiedFiles` — Lines with second-column `M` or
  `D` are grouped under `Modified (N):`.
- [ ] **4.4** `TestGitStatusUntracked` — `??` lines grouped under
  `Untracked (N):`.
- [ ] **4.5** `TestGitStatusConflicts` — `UU`/`AA`/`DD` lines shown in full
  under `Conflicts (N):`.
- [ ] **4.6** `TestGitStatusStagedCap` — More than 15 staged files shows first
  15 + `... +N more`.
- [ ] **4.7** `TestGitStatusUntrackedCap` — More than 10 untracked files shows
  first 10 + `... +N more`.
- [ ] **4.8** `TestGitStatusNonPorcelainPassthrough` — Output not in porcelain
  format and not in human-readable format (no recognised headers, no two-char
  status codes) passes through unchanged.
- [ ] **4.9** `TestGitStatusMixedCategories` — Single porcelain output with
  staged + modified + untracked correctly groups all three sections.
- [ ] **4.10** `TestGitStatusHumanReadableStaged` — Human-readable `git status`
  output with `Changes to be committed:` section → staged files extracted,
  capped at 15, with `+N more` when exceeded.
- [ ] **4.11** `TestGitStatusHumanReadableUntracked` — Human-readable output
  with `Untracked files:` section capped at 10 with `+N more`.
- [ ] **4.12** `TestGitStatusFlagsBeforeSubcmd` — `git -C /some/path status`
  (flag before subcommand) does not match the filter regex; output passes
  through unchanged. Documents the known limitation.

### 4B. git log

- [ ] **4.13** `TestGitLogCommitCap` — More than 15 commits → first 15 + count
  message.
- [ ] **4.14** `TestGitLogMessageTruncation` — Commit message lines exceeding
  100 chars are truncated with `...`.
- [ ] **4.15** `TestGitLogTrailerStripping` — `Signed-off-by:`,
  `Co-authored-by:`, `Change-Id:` lines are removed.
- [ ] **4.16** `TestGitLogBodyLineLimit` — More than 3 body lines per commit →
  first 3 + `[+N lines omitted]`.
- [ ] **4.17** `TestGitLogNoBodyCommit` — Commit with subject only (no body)
  passes through without empty line injection.
- [ ] **4.18** `TestGitLogNonLogFormatPassthrough` — Input that does not begin
  with `commit [0-9a-f]{40}` passes through unchanged.

### 4C. git diff

- [ ] **4.19** `TestGitDiffFileHeaders` — `diff --git`, `index`, `---`, `+++`
  lines are preserved intact.
- [ ] **4.20** `TestGitDiffHunkHeader` — `@@` lines are preserved intact.
- [ ] **4.21** `TestGitDiffHunkTruncation` — Hunk with more than 60
  added/removed lines → first 60 + `... (N lines omitted in this hunk)`.
- [ ] **4.22** `TestGitDiffNoTruncationSmallHunk` — Hunk with ≤ 60 changed
  lines is preserved in full.
- [ ] **4.23** `TestGitDiffTotalCounter` — Output ends with `+X -Y` total
  counter.
- [ ] **4.24** `TestGitDiffHintOnTruncation` — When any truncation occurred,
  hint line is appended.
- [ ] **4.25** `TestGitDiffNoHintWhenNoTruncation` — No hint when output was
  not truncated.
- [ ] **4.26** `TestGitDiffNonDiffFormatPassthrough` — Input without
  `diff --git` header passes through unchanged.

---

## Stage 5: Test Runner Summarization

- [ ] **5.1** `TestGoTestAllPassed` — Output with all `--- PASS` lines + final
  `ok` line → `"All N tests passed."`.
- [ ] **5.2** `TestGoTestWithFailures` — Extract `--- FAIL` blocks + failure
  output; discard `--- PASS` and `=== RUN` lines.
- [ ] **5.3** `TestGoTestSummaryLine` — Final `ok`/`FAIL` summary line is
  always preserved.
- [ ] **5.4** `TestVerboseFlagRespected` — When command contains `-v` (go
  test), `--verbose` (pytest, cargo test), or `-v`/`--verbose` (jest/vitest),
  do NOT apply summarization. Tests each runner's verbosity flag.
- [ ] **5.5** `TestPytestAllPassed` — Output with only `PASSED` lines +
  summary → `"N passed in Xs"`.
- [ ] **5.6** `TestPytestWithFailures` — Extract failure section between
  `FAILURES` and `short test summary`; discard passing lines.
- [ ] **5.7** `TestCargoTestAllPassed` — `test ... ok` lines collapsed to
  count; `test result: ok` summary preserved.
- [ ] **5.8** `TestCargoTestWithFailures` — `FAILED` lines + `failures:` block
  preserved; passing lines discarded.
- [ ] **5.9** `TestTestRunnerUnrecognizedPassthrough` — Output that doesn't
  match any known test runner pattern passes through unchanged.
- [ ] **5.10** `TestTestRunnerEmptyOutput` — Empty output returns `""` (no
  fabricated summary).

---

## Stage 6: Directory Listing Compression

- [ ] **6.1** `TestLsLongFormatBasic` — Long-format `ls -la` output: `.` and
  `..` and `total` lines removed; remaining entries formatted with human-
  readable size.
- [ ] **6.2** `TestLsLongFormatNoiseExclusion` — `node_modules/`, `.git/`,
  `target/` excluded by default.
- [ ] **6.3** `TestLsLongFormatNoiseIncludedWithFlag` — Command `ls -la -a`
  (contains `-a`) → noise directories are NOT excluded.
- [ ] **6.4** `TestLsLongFormatDirectoriesFirst` — Directories appear before
  files in output.
- [ ] **6.5** `TestLsLongFormatHumanSizes` — `1024` bytes shown as `1.0K`;
  `1048576` as `1.0M`; `500` as `500B`.
- [ ] **6.6** `TestLsLongFormatExtensionSummary` — Summary line appended
  showing top-5 extensions by frequency.
- [ ] **6.7** `TestLsPlainFormatPassthrough` — Non-long-format `ls` output
  (no date anchor) passes through unchanged.
- [ ] **6.8** `TestTreeStripSummaryLine` — `5 directories, 23 files` trailing
  line is removed.
- [ ] **6.9** `TestTreePreservesStructure` — Tree symbols (`├──`, `│`, `└──`)
  are preserved exactly.
- [ ] **6.10** `TestFindGroupsByDirectory` — Results grouped by parent directory.
- [ ] **6.11** `TestFindResultCap` — More than 50 results → first 50 + `+N more`.
- [ ] **6.12** `TestFindPathAbbreviation` — Paths longer than 50 chars
  abbreviated to `...last47chars`.
- [ ] **6.13** `TestFindExtensionSummary` — Extension frequency summary appended.

---

## Stage 7: Grep/Ripgrep Compression

- [ ] **7.1** `TestGrepGroupByFile` — Matches grouped by filename.
- [ ] **7.2** `TestGrepPerFileCap` — More than 5 matches per file → first 5 +
  `... (+N more in this file)`.
- [ ] **7.3** `TestGrepTotalFileCap` — More than 20 files → first 20 +
  `... (+N more files)`.
- [ ] **7.4** `TestGrepLineTruncation` — Match lines longer than 120 chars
  truncated to 60-char window centered on match.
- [ ] **7.5** `TestGrepPathCompression` — Long file paths compressed to
  `first/.../last` format.
- [ ] **7.6** `TestGrepSingleMatch` — Single match file with one result needs
  no truncation message.
- [ ] **7.7** `TestGrepNoMatchPassthrough` — Output with no file:line: format
  passes through unchanged.

---

## Stage 8: Declarative Filter Rules

- [ ] **8.1** `TestRuleParseValidTOML` — Valid TOML with `match_command`,
  `strip_lines_matching`, `tail_lines` parses without error.
- [ ] **8.2** `TestRuleParseInvalidTOML` — Malformed TOML returns parse error;
  no rules loaded.
- [ ] **8.3** `TestRuleMutualExclusion` — Filter with both `strip_lines_matching`
  and `keep_lines_matching` returns validation error.
- [ ] **8.4** `TestRuleMatchCommand` — Rule with `match_command = "^my-tool"`
  matches `"my-tool --flag"` and does not match `"other-tool"`.
- [ ] **8.5** `TestRuleStripLines` — Lines matching `strip_lines_matching`
  patterns are removed.
- [ ] **8.6** `TestRuleKeepLines` — Only lines matching `keep_lines_matching`
  patterns are kept.
- [ ] **8.7** `TestRuleTailLines` — Only last N lines retained with elision
  prefix.
- [ ] **8.8** `TestRuleHeadLines` — Only first N lines retained with elision
  suffix.
- [ ] **8.9** `TestRuleOnEmpty` — When filtering empties output, `on_empty`
  message returned.
- [ ] **8.10** `TestRuleReplace` — Regex replace rule applied line-by-line.
- [ ] **8.11** `TestRuleStripANSI` — `strip_ansi = true` strips escape codes
  as the first stage.
- [ ] **8.12** `TestRuleMaxLines` — Output capped at `max_lines` with truncation
  message.
- [ ] **8.13** `TestRulePriorityCascade` — Project-local rule takes precedence
  over user-global rule with the same `match_command`.
- [ ] **8.14** `TestRuleUntrustedProjectSkipped` — Project-local rule without
  trust grant is silently skipped; warning only to stderr.
- [ ] **8.15** `TestRuleSchemaVersionEnforced` — TOML with
  `schema_version = 2` rejected with error.
- [ ] **8.16** `TestRuleNoMatchPassthrough` — Command that matches no rule
  passes output through unchanged.

---

## Stage 9: Pipeline Integration

- [ ] **9.1** `TestPipelineANSIFirst` — ANSI stripping occurs before all other
  stages (escape codes don't confuse downstream regex).
- [ ] **9.2** `TestPipelineCommandRouting` — `git status` output routed to git
  compressor; `go test` to test summarizer; `ls -la` to dir compressor.
- [ ] **9.3** `TestPipelineLogDedupBeforeCommandFilter` — Log dedup stage runs
  on post-ANSI-stripped output, before the command filter. Verifies that a
  log-heavy output with repeated lines is deduplicated before git/test
  compression runs on the result.
- [ ] **9.4** `TestPipelineHeadTailLast` — Head+tail windowing is applied last,
  after all command-specific compression.
- [ ] **9.5** `TestPipelineDeclarativeRuleOverride` — A matching TOML rule
  overrides built-in compression for that command.
- [ ] **9.6** `TestPipelinePassthroughNoMatch` — Command with no matching
  filter passes raw output through the pipeline unchanged.
- [ ] **9.7** `TestPipelineDisabledMasterSwitch` — `enabled = false` in config
  bypasses all filtering; raw output returned.
- [ ] **9.8** `TestPipelineFilterPanicRecovery` — If a filter stage panics, the
  panic is recovered and the previous stage's output is used (no crash).

---

## Stage 10: Failure Tee

- [ ] **10.1** `TestTeeWritesOnNonZeroExit` — Non-zero exit code triggers write
  to tee dir; file contains full raw output.
- [ ] **10.2** `TestTeeSkipsOnZeroExitInFailuresMode` — Exit code 0 in
  `mode = "failures"` does not write tee file.
- [ ] **10.3** `TestTeeWritesOnZeroExitInAlwaysMode` — `mode = "always"` writes
  tee file even on exit code 0.
- [ ] **10.4** `TestTeeHintAppendedToContent` — After tee write, LLM content
  contains `[Full output: /path/to/file]` hint line.
- [ ] **10.5** `TestTeeFilenameFormat` — Filename matches
  `<yyyyMMdd-HHmmss>-<hash>.txt` format.
- [ ] **10.6** `TestTeeDirectoryCreated` — Tee directory is created if it does
  not exist.
- [ ] **10.7** `TestTeeRotation` — When file count exceeds `max_files`, oldest
  file is deleted.
- [ ] **10.8** `TestTeeDisabled` — `enabled = false` never writes files.
- [ ] **10.9** `TestTeeUTF8Boundary` — When output is truncated to
  `max_file_size`, truncation occurs at a valid UTF-8 boundary.
- [ ] **10.10** `TestTeeFileSizeLimit` — Output exceeding `max_file_size` is
  truncated in the tee file (raw hint file is size-bounded).

---

## Stage 11: Shell Tool Integration

- [ ] **11.1** `TestShellToolFilterApplied` — `ShellTool.Execute` with a
  `git status` command: LLM content uses compressed porcelain grouping.
- [ ] **11.2** `TestShellToolStreamingUnaffected` — `EventDelta` events emit
  raw bytes during execution; filtering only affects final `ToolResult.Content`.
- [ ] **11.3** `TestShellToolANSIStrippedInResult` — Command that emits ANSI
  codes: `ToolResult.Content` contains no escape sequences.
- [ ] **11.4** `TestShellToolDisplayContentUnfiltered` — `DisplayContent` (shown
  to human) is set from raw bytes when output exceeded `maxOutputBytes`; it is
  not passed through any compression stage.
- [ ] **11.5** `TestShellToolDisplayContentRetainsANSI` — Command that emits
  ANSI codes with output exceeding `maxOutputBytes`: `ToolResult.Content` has
  no escape sequences; `ToolResult.DisplayContent` retains the original ANSI
  codes verbatim.
- [ ] **11.6** `TestShellToolTeeOnFailure` — Failed command (exit code 1) with
  `tee.enabled = true` produces tee file under `os.UserCacheDir()/rubichan/tee/`
  and appends hint to content.
- [ ] **11.7** `TestShellToolFilterDisabled` — Config `enabled = false`:
  `ToolResult.Content` is raw output (same as current behavior).
- [ ] **11.8** `TestShellToolMaxBytesRespected` — After all filtering, content
  never exceeds `maxOutputBytes`.

---

## Commit Sequence

Each stage maps to one `[BEHAVIORAL]` commit after all tests in that stage pass.
Structural commits (e.g., creating the package, moving constants) use
`[STRUCTURAL]` prefix and are committed separately before any behavioral change.

| Commit | Scope |
|---|---|
| `[STRUCTURAL]` | Create `internal/tools/filter/` package scaffold |
| `[BEHAVIORAL]` | Stage 1: ANSI stripping |
| `[BEHAVIORAL]` | Stage 2: Head+tail windowing |
| `[BEHAVIORAL]` | Stage 3: Log deduplication |
| `[BEHAVIORAL]` | Stage 4: Git output compression |
| `[BEHAVIORAL]` | Stage 5: Test runner summarization |
| `[BEHAVIORAL]` | Stage 6: Directory listing compression |
| `[BEHAVIORAL]` | Stage 7: Grep compression |
| `[BEHAVIORAL]` | Stage 8: Declarative filter rules |
| `[BEHAVIORAL]` | Stage 9: Pipeline integration |
| `[BEHAVIORAL]` | Stage 10: Failure tee |
| `[BEHAVIORAL]` | Stage 11: Shell tool integration |

Total: ~117 test cases across 12 commits.
