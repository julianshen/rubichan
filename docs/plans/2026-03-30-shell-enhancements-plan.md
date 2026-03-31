# Shell Enhancements — TDD Implementation Plan

> **Date:** 2026-03-30 · **Status:** Draft
> **Design:** `2026-03-30-shell-enhancements-design.md`

---

## Feature 1: Auto-completion & Argument Hints

### 1A. LineReader Abstraction

- [ ] **1.1** `TestSimpleLineReaderReadsLines` — `SimpleLineReader` wrapping a `strings.NewReader` returns lines sequentially. Returns `io.EOF` at end.
- [ ] **1.2** `TestSimpleLineReaderPromptIgnored` — `SimpleLineReader.ReadLine(prompt)` ignores the prompt argument (used only for display in real readline).
- [ ] **1.3** `TestShellHostUsesLineReader` — `ShellHost` configured with a `LineReader` uses it instead of `bufio.Scanner`. Existing behavior is preserved.

### 1B. Completer (local, synchronous)

- [ ] **1.4** `TestCompleterExecutableCompletion` — Given executables `{ls, lsof, lsblk}` and input `"ls"` at pos 2, returns all three as candidates. Input `"lsb"` returns only `lsblk`.
- [ ] **1.5** `TestCompleterFilePathCompletion` — Given workDir with files `{main.go, main_test.go, README.md}`, input `"cat ma"` returns `main.go` and `main_test.go`. Directories include trailing `/`.
- [ ] **1.6** `TestCompleterDirectoryCompletion` — Input `"cd sr"` with subdirectory `src/` completes to `src/`. Only directories are returned for `cd`.
- [ ] **1.7** `TestCompleterSlashCommandCompletion` — Input `"/mo"` returns `/model` from registered slash commands `{model, quit, help}`.
- [ ] **1.8** `TestCompleterBuiltinCompletion` — Input `"ex"` returns `exit` from builtins. Input `"cd"` does not complete (it's already a full command).
- [ ] **1.9** `TestCompleterEmptyInput` — Empty input or whitespace-only returns no candidates.
- [ ] **1.10** `TestCompleterSecondArgFilePath` — Input `"cat "` (with trailing space) completes file paths for the second argument. Input `"ls src/"` completes paths within `src/`.
- [ ] **1.11** `TestCompleterGitBranchCompletion` — Input `"git checkout ma"` returns branches matching `ma*` (e.g., `main`, `major-fix`). Only triggers for git checkout/switch/branch.
- [ ] **1.12** `TestCompleterNoLLMCall` — Verify that `Complete()` never invokes `AgentTurnFunc`. Completion is purely local.

### 1C. HintProvider (async, LLM-powered)

- [ ] **1.13** `TestHintProviderCacheMiss` — First call for `"docker run --"` triggers an LLM call. Returns empty immediately (non-blocking).
- [ ] **1.14** `TestHintProviderCacheHit` — After a cache miss resolves, second call for same prefix returns cached results without LLM call.
- [ ] **1.15** `TestHintProviderPromptFormat` — The prompt sent to the LLM includes the command, current args, and requests flag completions in a structured format.
- [ ] **1.16** `TestHintProviderConcurrency` — Multiple concurrent calls for different commands don't race. Uses sync.RWMutex correctly.
- [ ] **1.17** `TestHintProviderDisabled` — When `agentTurn` is nil, `Hint()` returns empty slice (no panic).

### 1D. Readline Integration

- [ ] **1.18** `TestReadlineLineReaderCompletion` — `ReadlineLineReader` wired with a `Completer` provides tab completion. Typing `"ls"` + Tab produces completion candidates.
- [ ] **1.19** `TestReadlineLineReaderHistory` — Up/down arrows navigate command history (provided by readline library, verified via integration test).

---

## Feature 2: Error Analysis

- [ ] **2.1** `TestErrorAnalyzerAnalyzesFailedCommand` — Given command `"go test ./..."` with exit code 1 and stderr `"undefined: Foo"`, sends an analysis prompt to the LLM and returns streamed events.
- [ ] **2.2** `TestErrorAnalyzerAnalyzesBenignFailure` — Given command `"grep foo bar.txt"` with exit code 1 (no match), still triggers analysis. LLM receives the context and can suggest alternatives (e.g., typo, wrong file).
- [ ] **2.3** `TestErrorAnalyzerSkipsSuccessfulCommand` — Exit code 0 does not trigger analysis. `Analyze()` is not called.
- [ ] **2.4** `TestErrorAnalyzerTruncatesLargeOutput` — When combined output exceeds `maxOutput`, it is truncated before sending to LLM. Truncation indicator is appended.
- [ ] **2.5** `TestErrorAnalyzerPromptFormat` — The prompt sent to the LLM contains command, exit code, truncated output, and asks for a concise fix suggestion.
- [ ] **2.6** `TestErrorAnalyzerDisabled` — When `enabled` is false, `Analyze()` returns nil channel (no LLM call).
- [ ] **2.7** `TestErrorAnalyzerNilSafe` — When `errorAnalyzer` is nil on ShellHost, failed commands behave as before (no panic, no analysis).
- [ ] **2.8** `TestShellHostErrorAnalysisIntegration` — Full integration: shell command fails → error output shown → "Analyzing..." indicator → LLM suggestion streamed → prompt re-rendered.
- [ ] **2.9** `TestErrorAnalyzerContextStillRecorded` — After error analysis, the failed command is still available in `ContextTracker` for manual `?why` follow-up.

---

## Feature 3: Missing Tool Installer

### 3A. Platform Detection

- [ ] **3.1** `TestDetectPackageManagerBrew` — When `brew` is in the lookup path, returns PackageManager with Name="brew", InstallCmd="brew install".
- [ ] **3.2** `TestDetectPackageManagerApt` — When `apt` is available (but not brew), returns PackageManager with Name="apt", InstallCmd="sudo apt install -y".
- [ ] **3.3** `TestDetectPackageManagerNone` — When no known package manager is found, returns nil.
- [ ] **3.4** `TestDetectPackageManagerPriority` — When both `brew` and `apt` exist, `brew` takes precedence (ordered detection).

### 3B. Command-Not-Found Detection

- [ ] **3.5** `TestIsCommandNotFound_ExitCode127` — Exit code 127 with any stderr is detected as command-not-found.
- [ ] **3.6** `TestIsCommandNotFound_StderrPattern` — Exit code 1 with stderr containing "command not found" or "not found" is detected.
- [ ] **3.7** `TestIsCommandNotFound_NormalFailure` — Exit code 1 with stderr "test failed" is NOT detected as command-not-found.

### 3C. Built-in Lookup Table

- [ ] **3.8** `TestBuiltinLookupKnownTool` — `LookupPackage("jq", "brew")` returns `"jq"`. `LookupPackage("rg", "apt")` returns `"ripgrep"`. `LookupPackage("fd", "apt")` returns `"fd-find"`.
- [ ] **3.9** `TestBuiltinLookupUnknownTool` — `LookupPackage("obscuretool", "brew")` returns `""` (empty, not in table).
- [ ] **3.10** `TestBuiltinLookupPlatformVariants` — `LookupPackage("fd", "brew")` returns `"fd"` while `LookupPackage("fd", "apt")` returns `"fd-find"` (platform-specific package names).

### 3D. PackageInstaller (three-tier resolution)

- [ ] **3.11** `TestPackageInstallerUsesBuiltinFirst` — For `jq` (in built-in table), resolves instantly without LLM call or package manager search.
- [ ] **3.12** `TestPackageInstallerFallsThroughToPkgSearch` — For a tool not in built-in table, invokes the package manager's search command (e.g., `brew search`).
- [ ] **3.13** `TestPackageInstallerFallsThroughToLLM` — For a tool not in built-in table and not found by package manager search, asks the LLM.
- [ ] **3.14** `TestPackageInstallerApprovalRequired` — The install command is only executed after user approval. Rejection skips installation.
- [ ] **3.15** `TestPackageInstallerRerunsOnSuccess` — After successful installation, the original command is re-executed automatically.
- [ ] **3.16** `TestPackageInstallerCachesMapping` — After resolving `rg` → `ripgrep` (from any tier), a second `rg` not-found uses the cached mapping.
- [ ] **3.17** `TestPackageInstallerNilSafe` — When `pkgInstaller` is nil on ShellHost, command-not-found errors show normally (no panic).
- [ ] **3.18** `TestPackageInstallerNoPkgManager` — When no package manager is detected, the installer suggests the package name but doesn't offer to install.
- [ ] **3.19** `TestShellHostPackageInstallerIntegration` — Full integration: command not found → detection → three-tier resolution → approval prompt → install → re-run → output shown.

---

## Feature 4: Smart Script

### 4A. IntentClassifier (two-pass routing)

- [ ] **4.1** `TestIntentClassifierAction` — Input `"find all TODO comments"` is classified as `IntentAction` by the LLM.
- [ ] **4.2** `TestIntentClassifierQuestion` — Input `"what is a goroutine"` is classified as `IntentQuestion` by the LLM.
- [ ] **4.3** `TestIntentClassifierPromptFormat` — The classification prompt uses few-shot examples and requests only `"question"` or `"action"` as output.
- [ ] **4.4** `TestIntentClassifierDefaultsToQuestion` — When the LLM returns an ambiguous or malformed response, defaults to `IntentQuestion` (safe fallback).
- [ ] **4.5** `TestIntentClassifierNilAgent` — When `agentTurn` is nil, returns `IntentQuestion` (no panic).

### 4B. ScriptGenerator

- [ ] **4.6** `TestScriptGeneratorBasicGeneration` — Prompt `"list all Go files over 500 lines"` sends a script-generation prompt to the LLM and returns a bash script string.
- [ ] **4.7** `TestScriptGeneratorPromptFormat` — The prompt includes working directory, platform, and explicit rules (set -euo pipefail, no explanation, etc.).
- [ ] **4.8** `TestScriptGeneratorExtractsCodeBlock` — If the LLM wraps the script in ```bash fences, the fences are stripped. If no fences, the response is used as-is.
- [ ] **4.9** `TestScriptGeneratorEmptyPrompt` — Empty or whitespace-only prompt returns an error.

### 4C. Script Approval & Execution

- [ ] **4.10** `TestScriptApprovalApproved` — User responds `y` → script is executed via ShellExecFunc.
- [ ] **4.11** `TestScriptApprovalRejected` — User responds `n` → script is discarded, no execution.
- [ ] **4.12** `TestScriptApprovalEdited` — User responds `edit` → modified script returned → re-prompted → approved → executed.
- [ ] **4.13** `TestScriptExecutionOutput` — Executed script's stdout/stderr are displayed. Exit code is captured.

### 4D. ShellHost Integration

- [ ] **4.14** `TestShellHostSmartScriptRouting` — Input `"? find all TODO comments"` is classified as LLMQuery, then two-pass routes to script generation (IntentAction).
- [ ] **4.15** `TestShellHostSmartScriptQuestionPassthrough` — Input `"? what is a goroutine"` — two-pass classifies as IntentQuestion → normal LLM conversational response (not script generation).
- [ ] **4.16** `TestShellHostSmartScriptDisabled` — When `scriptGen` is nil, `?`-prefixed input behaves as before (direct LLM query, no intent classification).
- [ ] **4.17** `TestSmartScriptContextRecorded` — After script execution, the script and its output are recorded in `ContextTracker` for follow-up queries.

---

## Feature 5: Status Line

### 5A. StatusLine Core

- [ ] **5.1** `TestStatusLineRender` — A status line with segments `{cwd: "~/project", branch: "main", exitcode: "0"}` renders a formatted string with segments separated by `|`.
- [ ] **5.2** `TestStatusLineUpdate` — Calling `Update("branch", "feature")` changes the branch segment. Next `Render()` reflects the change.
- [ ] **5.3** `TestStatusLineWidth` — Rendered line is truncated to terminal width. Long CWD paths are shortened with `...`.
- [ ] **5.4** `TestStatusLineDisabled` — When `enabled` is false, `Render()` returns empty string. `Update()` is a no-op.
- [ ] **5.5** `TestStatusLineNilSafe` — When `statusLine` is nil on ShellHost, prompt renders as before (no panic).

### 5B. Segment Formatting

- [ ] **5.6** `TestStatusSegmentCWD` — CWD segment shortens home dir to `~`, truncates long paths with `...`.
- [ ] **5.7** `TestStatusSegmentBranch` — Branch segment shows branch name. Empty branch (non-git dir) omits the segment entirely.
- [ ] **5.8** `TestStatusSegmentExitCode` — Exit code 0 shows `✓`. Non-zero shows `✗ N`.
- [ ] **5.9** `TestStatusSegmentModel` — Model segment shows shortened model name (e.g., `claude-sonnet-4-5` → `sonnet`).

### 5C. Prompt Integration

- [ ] **5.10** `TestPromptRendererWithStatusLine` — `PromptRenderer` configured with a `StatusLine` prepends the status line before the `ai$` prompt.
- [ ] **5.11** `TestPromptRendererWithoutStatusLine` — `PromptRenderer` with nil `StatusLine` renders the same as before (backward compatible).
- [ ] **5.12** `TestShellHostStatusLineIntegration` — Full integration: shell command updates exit code segment, `cd` updates CWD segment, git command updates branch segment.

---

## Implementation Order

The recommended implementation order, balancing dependencies and value:

1. **Feature 2: Error Analysis** (9 tests) — Highest immediate value, smallest scope. Uses existing `AgentTurnFunc` and `ContextTracker`. No new dependencies.

2. **Feature 4: Smart Script** (17 tests) — Core "smart shell" feature. Two-pass intent classification + script generation. Uses existing `AgentTurnFunc` and `ShellExecFunc`. No new dependencies.

3. **Feature 3: Missing Tool Installer** (19 tests) — Three-tier resolution (built-in table → pkg manager search → LLM). No new dependencies.

4. **Feature 5: Status Line** (12 tests) — Prompt-integrated status display. Independent from other features. No new dependencies.

5. **Feature 1: Auto-completion** (19 tests) — Largest scope. Requires `LineReader` abstraction and readline library dependency. Benefits from having Features 2-4 already working.

**Total: 76 tests across 5 features.**

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
