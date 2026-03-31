# Shell Enhancements — Design Document

> **Date:** 2026-03-30 · **Status:** Draft
> **Feature:** Five AI-powered enhancements to shell mode

---

## 1. Overview

Five enhancements to the existing shell mode that make it a truly intelligent shell:

| # | Feature | Description |
|---|---------|-------------|
| 1 | Auto-completion & Argument Hints | LLM-powered tab completion for commands, flags, and file paths |
| 2 | Error Analysis | When a command fails, analyze the error and suggest fixes |
| 3 | Missing Tool Installer | Detect "command not found" and offer to install via the platform package manager |
| 4 | Smart Script | `? prompt` generates a script, shows it for approval, then executes |
| 5 | Status Bar | Persistent bottom bar showing CWD, git branch, last exit code, model |

All five features are independent and can be implemented in any order. They share the existing shell infrastructure and follow the thin-adapter pattern.

---

## 2. Feature 1: Auto-completion & Argument Hints

### 2.1 Motivation

The current shell reads input via `bufio.Scanner` — no tab completion exists. Users expect shell-grade completion for commands and file paths, plus AI-powered hints for less common flags and subcommands.

### 2.2 Design

**Two-tier completion system:**

1. **Local completions** (< 1ms) — handled synchronously on Tab press:
   - Executable names from `knownExecutables` map (already scanned at startup)
   - File/directory paths relative to `workDir`
   - Shell builtins (`cd`, `exit`, `quit`)
   - Slash commands from the command registry
   - Git branches (for `git checkout`, `git switch`, etc.)

2. **LLM-powered hints** (async, displayed as ghost text or inline suggestion):
   - Flag/argument completion for known CLI tools (`go test -run`, `docker run --`)
   - Triggered after a brief pause (300ms debounce) when the cursor is after a `-` or `--`
   - Results cached per (command, prefix) pair to avoid repeated LLM calls

### 2.3 Architecture

```go
// Completer provides tab-completion candidates for shell input.
type Completer struct {
    executables    map[string]bool
    workDir        *string // pointer to ShellHost.workDir for live tracking
    slashCommands  func() []string
    gitBranchFn    func(string) string
}

// Complete returns completion candidates for the given input and cursor position.
func (c *Completer) Complete(input string, pos int) []Completion

// Completion is a single completion candidate.
type Completion struct {
    Text        string // The completion text to insert
    Display     string // What to show in the menu (may include description)
    Description string // Optional short description (e.g., flag meaning)
}
```

**Hint provider** (async, LLM-powered):

```go
// HintProvider generates argument hints via the LLM.
type HintProvider struct {
    agentTurn AgentTurnFunc
    cache     map[string][]Completion // keyed by "command:prefix"
    mu        sync.RWMutex
}

// Hint returns argument hints for the current input. Non-blocking; returns
// cached results or empty if no hint is available yet. Triggers background
// LLM call if cache miss.
func (hp *HintProvider) Hint(input string) []Completion
```

### 2.4 Readline Integration

Replace `bufio.Scanner` with a readline library that supports custom completers. Options:

- `github.com/chzyer/readline` — mature, supports custom `AutoCompleter`
- `github.com/peterh/liner` — simpler, supports tab completion callbacks

The readline library provides line editing (arrow keys, Ctrl-A/E, history navigation) for free, which also improves the base shell experience.

**Testability — `LineReader` interface:**

To avoid breaking the existing test infrastructure (which uses `strings.NewReader` for stdin), the readline dependency is abstracted behind a `LineReader` interface:

```go
// LineReader abstracts line input with optional completion support.
type LineReader interface {
    ReadLine(prompt string) (string, error)
    Close() error
}

// SimpleLineReader wraps bufio.Scanner for testing (no completion).
type SimpleLineReader struct { ... }

// ReadlineLineReader wraps chzyer/readline for production (with completion).
type ReadlineLineReader struct { ... }
```

`ShellHost` uses `LineReader` instead of `bufio.Scanner` directly. Tests inject `SimpleLineReader` (backed by `strings.NewReader`), production uses `ReadlineLineReader`. Existing tests remain unchanged.

### 2.5 Key Decisions

- **Local-first**: Tab always completes locally (instant). LLM hints are supplemental.
- **No LLM for basic completion**: File paths and executables never hit the LLM.
- **Cache aggressively**: LLM hint results cached for the session to avoid redundant calls.
- **Graceful degradation**: If LLM is slow/unavailable, local completion still works.
- **Interface-based readline**: `LineReader` interface keeps tests simple. Production gets full readline; tests use a mock.

---

## 3. Feature 2: Error Analysis

### 3.1 Motivation

When a shell command fails (non-zero exit code), the user currently sees the raw error output. The AI can analyze the error and suggest a fix — bridging the gap between "command failed" and "here's what to do."

### 3.2 Design

**ErrorAnalyzer** hooks into the shell command execution path. After a command fails:

1. Capture command, exit code, stdout, and stderr
2. Send to the LLM with a focused prompt: "The command `X` failed with exit code N. Analyze the error and suggest a concise fix."
3. Display the suggestion below the error output, clearly delineated

**Behavior:**
- Triggers on **any non-zero exit code** (exit > 0), including benign ones like `grep` returning 1 (no match). Even "expected" failures can be useful to analyze — a grep with no matches might indicate a typo, a wrong directory, or a misremembered flag. The LLM is smart enough to give context-appropriate suggestions (e.g., "No matches found — did you mean X?" vs. "Compilation failed because...").
- Respects a configurable opt-out (`--no-error-analysis` flag or config)
- Truncates large output before sending to LLM (reuses `ContextTracker.maxOutputSize`)
- Shows a brief "Analyzing..." indicator while the LLM responds
- Suggestion is streamed inline (same event streaming as LLM queries)

### 3.3 Architecture

```go
// ErrorAnalyzer provides AI-powered analysis of failed shell commands.
type ErrorAnalyzer struct {
    agentTurn AgentTurnFunc
    enabled   bool
    maxOutput int // max bytes of output to send to LLM
}

// Analyze sends a failed command's output to the LLM for diagnosis.
// Returns a channel of TurnEvents for streaming the suggestion.
func (ea *ErrorAnalyzer) Analyze(ctx context.Context, command string, stdout string, stderr string, exitCode int) (<-chan TurnEvent, error)
```

### 3.4 Integration Point

In `ShellHost.handleShellCommand()`, after execution completes with `exitCode != 0`:

```go
if exitCode != 0 && h.errorAnalyzer != nil && h.errorAnalyzer.enabled {
    fmt.Fprintf(h.stderr, "\n💡 Analyzing error...\n")
    events, err := h.errorAnalyzer.Analyze(ctx, input.Command, stdout, stderr, exitCode)
    // stream events...
}
```

### 3.5 Key Decisions

- **Analyze all failures (exit > 0)**: Even benign exit codes (grep no-match = 1) get analysis. The LLM adapts its suggestion to the severity — a typo hint for grep, a fix for a build failure. Users can disable if too noisy.
- **Non-blocking**: The error output is shown immediately; analysis streams after.
- **Enabled by default**: On by default, can be disabled per-session or globally via opt-out flags.
- **Concise prompt**: The analysis prompt focuses on actionable suggestions, not verbose explanations.
- **Context tracker still records**: The failed command is still recorded in `ContextTracker` for manual `?why` follow-ups.

---

## 4. Feature 3: Missing Tool Installer

### 4.1 Motivation

"command not found" is a common shell frustration. The AI shell can detect this, identify the package that provides the command, and offer to install it using the platform's package manager.

### 4.2 Design

**PackageInstaller** detects "command not found" errors and offers installation:

1. Detect the pattern: exit code 127 or stderr containing "command not found" / "not found"
2. Identify the platform package manager (detect once at startup)
3. Ask the LLM: "The command `X` is not found. What package provides it on [platform]? Give only the install command."
4. Show the suggested install command and prompt for confirmation
5. Execute the install command if approved
6. Re-run the original command if installation succeeded

### 4.3 Platform Detection

```go
// PackageManager represents a system package manager.
type PackageManager struct {
    Name       string // "brew", "apt", "dnf", "pacman", "apk", etc.
    InstallCmd string // "brew install", "apt install -y", etc.
    SearchCmd  string // "brew search", "apt search", etc.
}

// DetectPackageManager identifies the available package manager on the system.
func DetectPackageManager() *PackageManager
```

Detection order:
1. `brew` (macOS, Linuxbrew)
2. `apt` / `apt-get` (Debian/Ubuntu)
3. `dnf` (Fedora/RHEL 8+)
4. `yum` (RHEL 7/CentOS)
5. `pacman` (Arch)
6. `apk` (Alpine)
7. `zypper` (openSUSE)
8. `dpkg` (fallback Debian)
9. `nix-env` (NixOS)

### 4.4 Architecture

```go
// PackageInstaller detects missing commands and offers to install them.
type PackageInstaller struct {
    pkgManager *PackageManager
    agentTurn  AgentTurnFunc
    shellExec  ShellExecFunc
    approvalFn func(ctx context.Context, action string) (bool, error)
}

// HandleCommandNotFound checks if a command failure is due to a missing tool
// and offers to install it. Returns true if it handled the error.
func (pi *PackageInstaller) HandleCommandNotFound(ctx context.Context, command string, stderr string, exitCode int) (handled bool, err error)
```

### 4.5 User Experience

```
~/project (main) ai$ jq '.name' package.json
bash: jq: command not found

📦 jq is not installed. Install with: brew install jq
   Install? (y/n): y
   [Installing: brew install jq]
   ✓ jq installed successfully.
   [Re-running: jq '.name' package.json]
   "rubichan"
```

### 4.6 Package Resolution Strategy

**Three-tier resolution** — fast local lookup first, package manager search second, LLM as last resort:

1. **Built-in lookup table** (instant, ~100 common tools): A hardcoded map of the most common developer tools and their package names across package managers. Examples:
   - `jq` → `jq` (all), `rg` → `ripgrep` (all), `fd` → `fd-find` (apt) / `fd` (brew)
   - `htop`, `tree`, `wget`, `curl`, `tmux`, `make`, `gcc`, `python3`, etc.
   - Covers ~90% of real-world "command not found" cases with zero latency.

2. **Package manager search** (fast, local): If not in the built-in table, use the package manager's native search:
   - `brew search <cmd>`, `apt-file search /usr/bin/<cmd>`, `dnf provides */bin/<cmd>`
   - Requires `apt-file` to be installed on Debian/Ubuntu (common in dev environments).
   - Returns exact matches preferred, then fuzzy matches.

3. **LLM fallback** (async): If both above fail, ask the LLM for the package name. This handles edge cases, renamed packages, and platform-specific alternatives.

### 4.7 Key Decisions

- **Always ask before installing**: Never auto-install. Approval prompt required.
- **Three-tier resolution**: Built-in table → package manager search → LLM. Fast for common tools, comprehensive for obscure ones.
- **Re-run on success**: After successful installation, automatically re-run the original command.
- **Sudo awareness**: If the package manager requires sudo (apt, dnf), include it in the suggested command.
- **Cache package mappings**: Cache `command → package` mappings for the session (all tiers contribute to cache).

---

## 5. Feature 4: Smart Script

### 5.1 Motivation

The `?` prefix currently routes to the LLM for a conversational response. Smart Script extends this: `? <natural language prompt>` generates an executable script, shows it for review, and runs it on approval. This is the most powerful "AI shell" feature — turning intent into action.

### 5.2 Design

The existing `?` prefix already forces LLM routing. Smart Script uses a **two-pass approach**: the LLM first classifies whether the `?` input is a question or an action, then routes accordingly.

**Two-pass flow:**

**Pass 1 — Intent Classification (fast, small prompt):**
The LLM receives the `?`-prefixed input and classifies it as `"question"` or `"action"`:
- `? what is a goroutine` → `"question"` → normal conversational LLM response
- `? find all TODO comments` → `"action"` → proceed to script generation

This uses a lightweight classification prompt (few-shot, ~200 tokens) for fast turnaround. The classification result is cached for the session to avoid re-classifying similar patterns.

**Pass 2 — Script Generation (only for actions):**
1. User types: `? find all TODO comments and count them by file`
2. Classifier routes to `ClassLLMQuery` (existing behavior)
3. Two-pass: LLM classifies as `"action"`
4. Sends a script-generation prompt to the LLM
5. LLM returns a shell script
6. Script is displayed in a code block with syntax highlighting
7. User is prompted: `Run this script? (y/n/edit)`
8. On `y`: execute the script via `ShellExecFunc`
9. On `e`/`edit`: open in `$EDITOR` for modification, then re-prompt
10. On `n`: discard

**Fallback:** If intent classification fails or is ambiguous, default to conversational (question) mode — safer than generating an unwanted script.

### 5.3 Architecture

```go
// IntentKind represents whether a ? query is a question or an action.
type IntentKind int

const (
    IntentQuestion IntentKind = iota // Conversational response
    IntentAction                     // Generate and run a script
)

// IntentClassifier uses the LLM to classify ? queries as questions or actions.
type IntentClassifier struct {
    agentTurn AgentTurnFunc
}

// Classify determines if the input is a question or an action request.
// Returns IntentQuestion on error or ambiguity (safe default).
func (ic *IntentClassifier) Classify(ctx context.Context, input string) (IntentKind, error)

// ScriptGenerator generates shell scripts from natural language prompts.
type ScriptGenerator struct {
    intentClassifier *IntentClassifier
    agentTurn        AgentTurnFunc
    shellExec        ShellExecFunc
    approvalFn       func(ctx context.Context, script string) (approved bool, edited string, err error)
    workDir          *string
}

// Generate creates a shell script from a natural language prompt.
// Returns the generated script text.
func (sg *ScriptGenerator) Generate(ctx context.Context, prompt string) (string, error)

// Execute runs an approved script and returns its output.
func (sg *ScriptGenerator) Execute(ctx context.Context, script string) (stdout string, stderr string, exitCode int, err error)

// HandleQuery is the main entry point: classifies intent, then either
// generates a script (action) or returns nil to signal conversational mode.
func (sg *ScriptGenerator) HandleQuery(ctx context.Context, query string) (script *string, err error)
```

### 5.4 Script Generation Prompt

The LLM prompt is structured to produce clean, runnable scripts:

```
Generate a shell script that accomplishes the following task:
{user prompt}

Working directory: {workDir}
Shell: bash
Platform: {runtime.GOOS}/{runtime.GOARCH}

Rules:
- Output ONLY the script, no explanation
- Use bash with set -euo pipefail
- Use relative paths from the working directory
- Include brief comments for non-obvious steps
- Prefer standard Unix tools over exotic ones
```

### 5.5 Script Approval

```go
// ScriptApproval prompts the user to approve, edit, or reject a generated script.
type ScriptApproval int

const (
    ScriptApproved ScriptApproval = iota
    ScriptEdited   // User modified the script
    ScriptRejected
)
```

### 5.6 User Experience

```
~/project (main) ai$ ? find all Go files with more than 500 lines

Generated script:
┌──────────────────────────────────────────────────┐
│ #!/usr/bin/env bash                              │
│ set -euo pipefail                                │
│ find . -name '*.go' -exec awk '                  │
│   END { if (NR > 500) print FILENAME ": " NR }  │
│ ' {} \;                                          │
└──────────────────────────────────────────────────┘

Run this script? (y/n/edit): y
./cmd/rubichan/main.go: 2847
./cmd/rubichan/coverage_test.go: 1823
./internal/tools/shell.go: 1110
```

### 5.7 Key Decisions

- **`?` prefix only**: Smart Script only activates on `?`-prefixed input. Regular NL queries still get conversational responses.
- **Two-pass LLM routing**: First pass classifies intent (question vs action) via a lightweight LLM call. Questions go to normal conversational path. Actions trigger script generation. Ambiguous defaults to question (safe).
- **Always show before running**: The script is always displayed for review. No blind execution.
- **Approval required**: `y/n/edit` prompt. Never auto-execute.
- **Edit option**: Let users tweak the generated script before running.
- **Script not added to history**: The raw prompt is added to history, not the generated script.

---

## 6. Feature 5: Status Bar

### 6.1 Motivation

Users want persistent visibility of shell state: current directory, git branch, last command status, active model. Currently this information only appears in the prompt line, which scrolls away.

### 6.2 Design

Instead of a persistent bottom bar using ANSI scroll regions (which conflicts with streaming LLM output and shell command output), the status is displayed as an **enhanced prompt line** — a rich, multi-segment line rendered above or as part of the prompt on every iteration.

This is simpler, more compatible across terminals, and avoids visual glitches during long streaming outputs.

### 6.3 Architecture

```go
// StatusLine renders a rich status display as part of the shell prompt.
type StatusLine struct {
    segments []StatusSegment
    writer   io.Writer
    width    int // terminal width for truncation
    enabled  bool
}

// StatusSegment is a single section of the status line.
type StatusSegment struct {
    Key   string // identifier for updates
    Value string // display text
    Style string // ANSI style codes (color, bold, etc.)
}

// Update changes a segment's value.
func (sl *StatusLine) Update(key string, value string)

// Render writes the status line to the writer. Called before each prompt.
func (sl *StatusLine) Render() string
```

### 6.4 Segments

| Segment | Key | Example | Update Trigger |
|---------|-----|---------|----------------|
| Working directory | `cwd` | `~/project` | After `cd`, on startup |
| Git branch | `branch` | `main` | After `cd`, after git commands |
| Last exit code | `exitcode` | `✓` or `✗ 1` | After every shell command |
| Active model | `model` | `sonnet` | After `/model` command |
| Mode indicator | `mode` | `AI Shell` | Static |

### 6.5 Prompt-Integrated Display

The status line renders as a colored line above the main prompt:

```
┌──────────────────────────────────────────────────┐
│ ~/project | main | ✓ | sonnet | AI Shell         │  ← status line (colored)
│ ai$ _                                            │  ← input prompt
└──────────────────────────────────────────────────┘
```

Or as a two-line prompt:

```
[~/project] (main) ✓ sonnet
ai$ git status
On branch main
...

[~/project] (main) ✗1 sonnet
ai$ _
```

Implementation approach:
- `StatusLine.Render()` returns a formatted string with ANSI colors
- `PromptRenderer` is enhanced to optionally prepend the status line
- Terminal width detected via `golang.org/x/term` for truncation
- Segments separated by `|` or spaces, with ANSI color codes
- No scroll regions, no cursor manipulation — just printed text

### 6.6 Key Decisions

- **Prompt-integrated**: Status is part of the prompt, not a persistent bar. No ANSI scroll regions means zero conflicts with streaming output, pipes, or terminal resizers.
- **Simple and robust**: Works in every terminal, over SSH, in tmux, in screen. No special terminal capability required.
- **ANSI colors**: Uses standard 16-color ANSI codes for broad compatibility. Configurable for 256-color or no-color.
- **Opt-in**: Can be disabled with `--no-status` or config.
- **No emoji by default**: Use text symbols (`*` for branch, `>` for CWD) unless unicode is detected via locale.

---

## 7. Integration with ShellHost

All five features integrate into `ShellHost` as optional components:

```go
type ShellHost struct {
    // existing fields...
    classifier      *InputClassifier
    history         *CommandHistory
    ctxTracker      *ContextTracker
    prompt          *PromptRenderer

    // new fields
    completer       *Completer       // Feature 1
    hintProvider    *HintProvider    // Feature 1
    errorAnalyzer   *ErrorAnalyzer   // Feature 2
    pkgInstaller    *PackageInstaller // Feature 3
    scriptGen       *ScriptGenerator // Feature 4
    statusBar       *StatusBar       // Feature 5
}
```

All new components are **nil-safe** — the shell works identically to v1 if none are configured. Each feature is wired via `ShellHostConfig`:

```go
type ShellHostConfig struct {
    // existing fields...

    // Feature 1: Auto-completion
    SlashCommandNames func() []string // for completion candidates
    LineReader        LineReader       // nil = use SimpleLineReader (bufio)

    // Feature 2: Error Analysis
    ErrorAnalysis bool // enable/disable

    // Feature 3: Package Installer
    PackageManager *PackageManager // nil = disabled

    // Feature 4: Smart Script
    ScriptApprovalFn func(ctx context.Context, script string) (approved bool, edited string, err error)

    // Feature 5: Status Line
    StatusLine bool // enable/disable
}
```

### Feature Flags (Config)

Each feature has an individual toggle in the TOML config file under `[shell]`:

```toml
[shell]
error_analysis = true    # Feature 2 (default: true)
package_installer = true # Feature 3 (default: true)
smart_script = true      # Feature 4 (default: true)
status_line = true       # Feature 5 (default: true)
completion = true        # Feature 1 (default: true)
```

All features default to **enabled**. Each can be overridden per-invocation via CLI flags (`--no-error-analysis`, `--no-status`, etc.). The nil-safe design ensures disabled features have zero overhead.

---

## 8. Dependencies

| Feature | New Dependencies | Existing Dependencies Used |
|---------|-----------------|---------------------------|
| Auto-completion | `github.com/chzyer/readline` (readline with completion) | `knownExecutables`, `CommandHistory` |
| Error Analysis | None | `AgentTurnFunc`, `ContextTracker` |
| Missing Tool Installer | None | `ShellExecFunc`, `AgentTurnFunc` |
| Smart Script | None | `AgentTurnFunc`, `ShellExecFunc` |
| Status Bar | None | `golang.org/x/term` (already present) |

Only Feature 1 requires a new dependency. Features 2-5 are pure additions to the existing package.

---

## 9. Security Considerations

- **Smart Script**: Scripts are shown before execution and require explicit approval. The generated script runs through the same `ShellExecFunc` with all security interceptors.
- **Package Installer**: Install commands require user approval. Sudo commands are shown explicitly.
- **Error Analysis**: Only sends truncated output to the LLM. No secrets detection needed beyond existing safeguards.
- **Auto-completion**: LLM hints are read-only — they never execute anything.
- **Status Bar**: Display-only. No user input processing.
