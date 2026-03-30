# Shell Enhancements — TDD Implementation Plan

> **Date:** 2026-03-30 · **Status:** Draft
> **Design:** `2026-03-30-shell-enhancements-design.md`

---

## Feature 1: Auto-completion & Argument Hints

### 1A. Completer (local, synchronous)

- [ ] **1.1** `TestCompleterExecutableCompletion` — Given executables `{ls, lsof, lsblk}` and input `"ls"` at pos 2, returns all three as candidates. Input `"lsb"` returns only `lsblk`.
- [ ] **1.2** `TestCompleterFilePathCompletion` — Given workDir with files `{main.go, main_test.go, README.md}`, input `"cat ma"` returns `main.go` and `main_test.go`. Directories include trailing `/`.
- [ ] **1.3** `TestCompleterDirectoryCompletion` — Input `"cd sr"` with subdirectory `src/` completes to `src/`. Only directories are returned for `cd`.
- [ ] **1.4** `TestCompleterSlashCommandCompletion` — Input `"/mo"` returns `/model` from registered slash commands `{model, quit, help}`.
- [ ] **1.5** `TestCompleterBuiltinCompletion` — Input `"ex"` returns `exit` from builtins. Input `"cd"` does not complete (it's already a full command).
- [ ] **1.6** `TestCompleterEmptyInput` — Empty input or whitespace-only returns no candidates.
- [ ] **1.7** `TestCompleterSecondArgFilePath` — Input `"cat "` (with trailing space) completes file paths for the second argument. Input `"ls src/"` completes paths within `src/`.
- [ ] **1.8** `TestCompleterGitBranchCompletion` — Input `"git checkout ma"` returns branches matching `ma*` (e.g., `main`, `major-fix`). Only triggers for git checkout/switch/branch.
- [ ] **1.9** `TestCompleterNoLLMCall` — Verify that `Complete()` never invokes `AgentTurnFunc`. Completion is purely local.

### 1B. HintProvider (async, LLM-powered)

- [ ] **1.10** `TestHintProviderCacheMiss` — First call for `"docker run --"` triggers an LLM call. Returns empty immediately (non-blocking).
- [ ] **1.11** `TestHintProviderCacheHit` — After a cache miss resolves, second call for same prefix returns cached results without LLM call.
- [ ] **1.12** `TestHintProviderPromptFormat` — The prompt sent to the LLM includes the command, current args, and requests flag completions in a structured format.
- [ ] **1.13** `TestHintProviderConcurrency` — Multiple concurrent calls for different commands don't race. Uses sync.RWMutex correctly.
- [ ] **1.14** `TestHintProviderDisabled` — When `agentTurn` is nil, `Hint()` returns empty slice (no panic).

### 1C. Readline Integration

- [ ] **1.15** `TestShellHostReadlineCompletion` — ShellHost wired with Completer provides tab completion. Typing `"ls"` + Tab produces completion candidates.
- [ ] **1.16** `TestShellHostHistoryNavigation` — Up/down arrows navigate command history (provided by readline library, verified via integration test).

---

## Feature 2: Error Analysis

- [ ] **2.1** `TestErrorAnalyzerAnalyzesFailedCommand` — Given command `"go test ./..."` with exit code 1 and stderr `"undefined: Foo"`, sends an analysis prompt to the LLM and returns streamed events.
- [ ] **2.2** `TestErrorAnalyzerSkipsSuccessfulCommand` — Exit code 0 does not trigger analysis. `Analyze()` is not called.
- [ ] **2.3** `TestErrorAnalyzerTruncatesLargeOutput` — When combined output exceeds `maxOutput`, it is truncated before sending to LLM. Truncation indicator is appended.
- [ ] **2.4** `TestErrorAnalyzerPromptFormat` — The prompt sent to the LLM contains command, exit code, truncated output, and asks for a concise fix suggestion.
- [ ] **2.5** `TestErrorAnalyzerDisabled` — When `enabled` is false, `Analyze()` returns nil channel (no LLM call).
- [ ] **2.6** `TestErrorAnalyzerNilSafe` — When `errorAnalyzer` is nil on ShellHost, failed commands behave as before (no panic, no analysis).
- [ ] **2.7** `TestShellHostErrorAnalysisIntegration` — Full integration: shell command fails → error output shown → "Analyzing..." indicator → LLM suggestion streamed → prompt re-rendered.
- [ ] **2.8** `TestErrorAnalyzerContextStillRecorded` — After error analysis, the failed command is still available in `ContextTracker` for manual `?why` follow-up.

---

## Feature 3: Missing Tool Installer

### 3A. Platform Detection

- [ ] **3.1** `TestDetectPackageManagerBrew` — On system with `brew` in PATH, returns PackageManager with Name="brew", InstallCmd="brew install".
- [ ] **3.2** `TestDetectPackageManagerApt` — On system with `apt` in PATH (but no brew), returns PackageManager with Name="apt", InstallCmd="sudo apt install -y".
- [ ] **3.3** `TestDetectPackageManagerNone` — On system with no known package manager, returns nil.
- [ ] **3.4** `TestDetectPackageManagerPriority` — When both `brew` and `apt` exist, `brew` takes precedence (ordered detection).

### 3B. Command-Not-Found Detection

- [ ] **3.5** `TestIsCommandNotFound_ExitCode127` — Exit code 127 with any stderr is detected as command-not-found.
- [ ] **3.6** `TestIsCommandNotFound_StderrPattern` — Exit code 1 with stderr containing "command not found" or "not found" is detected.
- [ ] **3.7** `TestIsCommandNotFound_NormalFailure` — Exit code 1 with stderr "test failed" is NOT detected as command-not-found.

### 3C. PackageInstaller

- [ ] **3.8** `TestPackageInstallerDetectsAndSuggests` — When a command-not-found is detected, the LLM is asked for the package name. The install command is shown to the user.
- [ ] **3.9** `TestPackageInstallerApprovalRequired` — The install command is only executed after user approval. Rejection skips installation.
- [ ] **3.10** `TestPackageInstallerRerunsOnSuccess` — After successful installation, the original command is re-executed automatically.
- [ ] **3.11** `TestPackageInstallerCachesMapping` — After resolving `rg` → `ripgrep`, a second `rg` not-found uses the cached mapping without LLM call.
- [ ] **3.12** `TestPackageInstallerNilSafe` — When `pkgInstaller` is nil on ShellHost, command-not-found errors show normally (no panic).
- [ ] **3.13** `TestPackageInstallerNoPkgManager` — When no package manager is detected, the installer suggests but doesn't offer to install.
- [ ] **3.14** `TestShellHostPackageInstallerIntegration` — Full integration: command not found → detection → LLM package lookup → approval prompt → install → re-run → output shown.

---

## Feature 4: Smart Script

### 4A. ScriptGenerator

- [ ] **4.1** `TestScriptGeneratorBasicGeneration` — Prompt `"list all Go files over 500 lines"` sends a script-generation prompt to the LLM and returns a bash script string.
- [ ] **4.2** `TestScriptGeneratorPromptFormat` — The prompt includes working directory, platform, and explicit rules (set -euo pipefail, no explanation, etc.).
- [ ] **4.3** `TestScriptGeneratorExtractsCodeBlock` — If the LLM wraps the script in ```bash fences, the fences are stripped. If no fences, the response is used as-is.
- [ ] **4.4** `TestScriptGeneratorEmptyPrompt` — Empty or whitespace-only prompt returns an error.

### 4B. Script Approval & Execution

- [ ] **4.5** `TestScriptApprovalApproved` — User responds `y` → script is executed via ShellExecFunc.
- [ ] **4.6** `TestScriptApprovalRejected` — User responds `n` → script is discarded, no execution.
- [ ] **4.7** `TestScriptApprovalEdited` — User responds `edit` → modified script returned → re-prompted → approved → executed.
- [ ] **4.8** `TestScriptExecutionOutput` — Executed script's stdout/stderr are displayed. Exit code is captured.

### 4C. ShellHost Integration

- [ ] **4.9** `TestShellHostSmartScriptRouting` — Input `"? find all TODO comments"` is classified as LLMQuery, then intercepted by the smart script handler (not sent as a conversational query).
- [ ] **4.10** `TestShellHostSmartScriptQuestionPassthrough` — Input `"? what is a goroutine"` (a question, not an action) is NOT intercepted by smart script — it goes through normal LLM query path.
- [ ] **4.11** `TestShellHostSmartScriptDisabled` — When `scriptGen` is nil, `?`-prefixed input behaves as before (LLM query).
- [ ] **4.12** `TestSmartScriptContextRecorded` — After script execution, the script and its output are recorded in `ContextTracker` for follow-up queries.

---

## Feature 5: Status Bar

### 5A. StatusBar Core

- [ ] **5.1** `TestStatusBarRender` — A status bar with segments `{cwd: "~/project", branch: "main", exitcode: "0"}` renders a formatted line with correct width padding.
- [ ] **5.2** `TestStatusBarUpdate` — Calling `Update("branch", "feature")` changes the branch segment. Next `Render()` reflects the change.
- [ ] **5.3** `TestStatusBarWidth` — Rendered line is truncated/padded to terminal width. Long paths are shortened.
- [ ] **5.4** `TestStatusBarClear` — `Clear()` writes ANSI sequences to remove the status bar and restore scroll region.
- [ ] **5.5** `TestStatusBarDisabled` — When `enabled` is false, `Render()` and `Update()` are no-ops.
- [ ] **5.6** `TestStatusBarNilSafe` — When `statusBar` is nil on ShellHost, all rendering proceeds normally (no panic).

### 5B. Segment Formatting

- [ ] **5.7** `TestStatusSegmentCWD` — CWD segment shortens home dir to `~`, truncates long paths with `...`.
- [ ] **5.8** `TestStatusSegmentBranch` — Branch segment shows branch name. Empty branch (non-git dir) omits the segment entirely.
- [ ] **5.9** `TestStatusSegmentExitCode` — Exit code 0 shows `✓`. Non-zero shows `✗ N` in red styling.
- [ ] **5.10** `TestStatusSegmentModel` — Model segment shows shortened model name (e.g., `claude-sonnet-4-5` → `sonnet`).

### 5C. Terminal Integration

- [ ] **5.11** `TestStatusBarScrollRegion` — `Render()` sets ANSI scroll region to `[1, rows-1]` and draws the bar on the last line.
- [ ] **5.12** `TestStatusBarResize` — On terminal resize (width change), bar re-renders with new width.
- [ ] **5.13** `TestShellHostStatusBarIntegration` — Full integration: shell command updates exit code segment, `cd` updates CWD segment, `/model` updates model segment.

---

## Implementation Order

The recommended implementation order, balancing dependencies and value:

1. **Feature 2: Error Analysis** (8 tests) — Highest immediate value, smallest scope. Uses existing `AgentTurnFunc` and `ContextTracker`. No new dependencies.

2. **Feature 4: Smart Script** (12 tests) — Core "smart shell" feature. Uses existing `AgentTurnFunc` and `ShellExecFunc`. No new dependencies.

3. **Feature 3: Missing Tool Installer** (14 tests) — Builds on error detection patterns from Feature 2. No new dependencies.

4. **Feature 5: Status Bar** (13 tests) — Independent from other features. Uses `golang.org/x/term` (existing). Pure display feature.

5. **Feature 1: Auto-completion** (16 tests) — Largest scope. Requires readline library dependency. Benefits from having Features 2-4 already working.

**Total: 63 tests across 5 features.**

---

## TDD Rhythm

For each test:
1. **Red**: Write the failing test
2. **Green**: Write minimal code to pass
3. **Refactor**: Clean up, keeping tests green
4. Run `go test ./internal/shell/...` and `golangci-lint run ./internal/shell/...`
5. Mark test `[x]` in this plan
6. Commit with `[BEHAVIORAL]` or `[STRUCTURAL]` prefix
7. Repeat
