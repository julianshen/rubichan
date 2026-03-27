# Shell Mode — Design Document

> **Date:** 2026-03-27 · **Status:** Draft
> **Feature:** Fourth execution mode — AI-enhanced interactive shell

---

## 1. Motivation

Rubichan currently has three execution modes: Interactive (TUI), Headless (CI/CD), and Wiki Generator (batch). Users frequently switch between their terminal shell and the agent — copying commands from the agent, pasting them into a shell, then returning results. Shell mode eliminates this friction by providing an **AI-enhanced shell** where:

- Natural language and shell commands coexist in one prompt
- Commands execute directly with output streamed inline
- The AI assists with command generation, error diagnosis, and multi-step workflows
- There is no heavyweight TUI — just a readline-style prompt with minimal chrome

This is analogous to Claude Code's shell mode: a lightweight, command-first interface where the AI is a co-pilot rather than the driver.

---

## 2. User Experience

### 2.1 Entry

```bash
rubichan shell                    # enter shell mode
rubichan shell --model=claude-sonnet-4-5  # with model override
rubichan shell --auto-approve     # skip approval prompts
```

Or toggle from within interactive mode via the `/shell` slash command.

### 2.2 Prompt Behavior

The shell mode prompt looks like a standard shell with an AI indicator:

```
~/project (main) ai$ ls -la
... normal ls output ...

~/project (main) ai$ what files were changed in the last commit?
The last commit (abc1234) modified:
  - internal/agent/agent.go (refactored loop)
  - internal/tools/shell.go (added timeout)

~/project (main) ai$ fix the failing test in agent_test.go
I'll look at the test failure and fix it.
[Running: go test ./internal/agent/...]
... test output ...
[Editing: internal/agent/agent.go]
... diff preview ...
Applied fix. Tests now pass.

~/project (main) ai$ !git status
On branch main
...
```

### 2.3 Input Classification

Shell mode must distinguish between:

1. **Direct shell commands** — executed immediately, no LLM involved
2. **Natural language requests** — sent to the LLM agent for processing
3. **Hybrid** — natural language that results in the agent running commands

Classification strategy (ordered):

| Signal | Classification | Example |
|--------|---------------|---------|
| `!` prefix | Force shell execution | `!docker compose up` |
| `?` prefix | Force LLM query | `?what does this Makefile do` |
| Starts with known executable | Shell command | `ls`, `git status`, `go test` |
| Starts with `cd`, `export`, `alias` | Shell built-in (handled locally) | `cd src/` |
| Contains question words / natural language patterns | LLM query | `explain the auth flow` |
| Ambiguous | LLM with shell tool access | `run the tests` |

The classifier is a **fast, local heuristic** — no LLM call needed for classification. It uses:
- A configurable set of known executables (populated from `$PATH` scan at startup)
- Simple NLP heuristics (question words, imperative verbs without a path argument)
- User-overridable via `!` and `?` prefixes

### 2.4 Shell Built-in Handling

Certain commands must be handled by the shell mode process itself (not spawned):

- `cd <path>` — changes the working directory for subsequent commands
- `exit` / `quit` / Ctrl-D — exits shell mode
- `export VAR=val` — sets environment variable for session
- `/command` — delegates to the existing slash command system (`/clear`, `/model`, `/skill`, etc.)

### 2.5 Output Rendering

- **Shell command output**: Streamed verbatim to stdout (no markdown rendering)
- **LLM responses**: Rendered as styled markdown in the terminal (reusing existing `StyledMarkdownFormatter`)
- **Tool call output**: Displayed inline with `[Running: ...]` / `[Editing: ...]` indicators
- **Approval prompts**: Inline yes/no prompt (same as plain-interactive mode)

### 2.6 Session Behavior

- Shell mode maintains a conversation context across the session (multi-turn)
- Direct shell commands and their output are **not** added to the LLM conversation by default
- When a shell command fails, the user can say `why did that fail?` and the agent sees the last command + output
- Session persistence: shell mode sessions are stored in SQLite like other modes, resumable with `--resume`

---

## 3. Architecture

### 3.1 Positioning in the Mode Hierarchy

Shell mode is the **fourth execution mode**, sharing the same Agent Core as the other three. It is a thin I/O adapter, consistent with ADR-002 (Shared Agent Core).

```
┌─────────────────────────────────────────────────┐
│              Execution Modes                     │
│  ┌───────────┐ ┌────────┐ ┌──────┐ ┌─────────┐ │
│  │Interactive│ │Headless│ │ Wiki │ │  Shell  │ │
│  │   (TUI)   │ │ (CI/CD)│ │(Batch)│ │  (REPL) │ │
│  └─────┬─────┘ └───┬────┘ └──┬───┘ └────┬────┘ │
│        └────────────┴─────────┴──────────┘      │
│                 Agent Core                       │
│        (Plan → Act → Observe loop)               │
│                                                  │
│     Tool Layer · Skill Runtime · Security        │
│     LLM Provider · Config · Storage              │
└─────────────────────────────────────────────────┘
```

### 3.2 Package Structure

```
internal/
  shell/                    # New package
    shell.go                # ShellHost — main REPL loop
    classifier.go           # InputClassifier — command vs. NL detection
    classifier_test.go
    prompt.go               # PromptRenderer — PS1-style prompt
    prompt_test.go
    history.go              # CommandHistory — readline history with persistence
    history_test.go
    context.go              # ContextTracker — manages last-command context for LLM
    context_test.go
    shell_test.go           # Integration tests for ShellHost
cmd/rubichan/
    main.go                 # Add `shell` subcommand routing
```

### 3.3 Key Types

```go
// InputClassification represents how the shell interprets user input.
type InputClassification int

const (
    ClassShellCommand   InputClassification = iota  // Execute directly
    ClassBuiltinCommand                              // Handle in-process (cd, export)
    ClassLLMQuery                                    // Send to agent
    ClassSlashCommand                                // Delegate to command system
)

// ClassifiedInput is the result of classifying a user input line.
type ClassifiedInput struct {
    Classification InputClassification
    Raw            string   // Original input
    Command        string   // Parsed command (for shell/builtin)
    Args           []string // Parsed arguments (for shell/builtin)
}

// InputClassifier determines whether input is a shell command or LLM query.
type InputClassifier struct {
    knownExecutables map[string]bool // Populated from $PATH at startup
}

// ShellHost runs the shell mode REPL loop.
type ShellHost struct {
    agent       *agent.Agent
    classifier  *InputClassifier
    history     *CommandHistory
    context     *ContextTracker
    workDir     string
    env         map[string]string
    approvalFn  agent.ApprovalFunc
    promptRender *PromptRenderer
}

// ContextTracker manages the "last command" context window for the LLM.
type ContextTracker struct {
    lastCommand string
    lastOutput  string
    lastExitCode int
    maxOutputTokens int // Truncate large outputs before injecting into context
}

// PromptRenderer generates the PS1-style shell prompt.
type PromptRenderer struct {
    showGitBranch bool
    showAIIndicator bool
}
```

### 3.4 REPL Loop

```
ShellHost.Run(ctx):
  1. Render prompt (CWD + git branch + "ai$")
  2. Read line from stdin (with readline support)
  3. Classify input → ClassifiedInput
  4. Switch on classification:
     a. ClassBuiltinCommand → handle internally (cd, export, exit)
     b. ClassShellCommand → execute via ShellTool, stream output
        → update ContextTracker with command + output
     c. ClassLLMQuery → build agent turn with ContextTracker context
        → stream response, handle tool calls with approval
     d. ClassSlashCommand → delegate to command registry
  5. Loop back to step 1
```

### 3.5 Agent Integration

When input is classified as an LLM query, the shell host:

1. Constructs a user message from the input
2. Optionally injects the last shell command + output as context (if recent and relevant)
3. Calls `agent.Turn()` with shell-mode-specific system prompt additions:
   - "You are in shell mode. The user is working in a terminal. Prefer running commands over explaining how to run them. Be concise."
4. Streams the response, intercepting tool calls for inline display
5. The agent has full access to all tools (file, shell, search, etc.)

### 3.6 Direct Shell Execution

When input is classified as a shell command:

1. The command is executed via the existing `ShellTool` infrastructure (security interceptors, sandbox, etc.)
2. Output streams directly to the terminal (stdout/stderr passthrough)
3. The command and truncated output are stored in `ContextTracker`
4. No LLM involvement — zero latency overhead

### 3.7 Command-Line Integration

```go
// In cmd/rubichan/main.go — add shell subcommand
shellCmd := &cobra.Command{
    Use:   "shell",
    Short: "Start AI-enhanced interactive shell",
    RunE: func(cmd *cobra.Command, args []string) error {
        return runShell()
    },
}
```

The `runShell()` function follows the same initialization pattern as `runInteractive()` but creates a `ShellHost` instead of a TUI model. It reuses:
- Config loading
- Provider creation
- Tool registry setup
- Skill runtime
- Approval checker (simplified — inline yes/no, not TUI overlay)
- Session persistence

---

## 4. Design Decisions

### 4.1 Why a Separate Mode (Not a TUI Feature)?

Shell mode has fundamentally different I/O semantics:
- Input is line-oriented (readline), not textarea-based
- Output is a scrolling terminal stream, not a managed viewport
- Direct command execution bypasses the LLM entirely
- The working directory changes during the session

These differ enough from the TUI model that a thin adapter over `ShellHost` is cleaner than bolting shell semantics onto the Bubble Tea model.

### 4.2 Why Local Classification (Not LLM)?

Sending every input line to the LLM for classification would add 200-500ms latency to direct shell commands. Users expect sub-millisecond response for `ls` or `git status`. The local heuristic classifier ensures shell commands feel instant while only routing ambiguous or clearly natural-language input to the LLM.

### 4.3 Context Injection Strategy

Rather than adding every shell command to the conversation history (which would bloat context), we use a sliding window approach:
- Only the **last** direct shell command + output is kept
- It is injected as a `system` context block (not a user message) when the next LLM query occurs
- Large outputs are truncated to a configurable token limit (default: 2000 tokens)
- The user can explicitly reference previous commands: `explain the output of my last command`

---

## 5. Edge Cases

| Scenario | Behavior |
|----------|----------|
| Empty input | Re-render prompt (no action) |
| Ctrl-C during shell command | Kill the running process, return to prompt |
| Ctrl-C during LLM streaming | Cancel the agent turn, return to prompt |
| Ctrl-D on empty line | Exit shell mode |
| Command not found | Show shell error, store in context |
| `cd` to nonexistent dir | Show error, keep current dir |
| Very long output (>100KB) | Stream all to terminal, truncate for context |
| Pipe input (`echo "query" \| rubichan shell`) | Read stdin as single query, execute, exit (non-interactive) |
| `--resume` flag | Restore previous shell session (workdir, env, conversation) |

---

## 6. Security Considerations

- All shell commands pass through the existing `ShellTool` security interceptors (command substitution blocking, recursive rm guards, etc.)
- The sandbox configuration applies identically to shell mode
- Approval prompts are shown for write operations unless `--auto-approve` is set
- Environment variable modifications (`export`) are session-scoped, not persisted
- The `$PATH` scan for known executables uses `exec.LookPath`, not shell evaluation

---

## 7. Future Extensions (Out of Scope for v1)

- **Tab completion** for both shell commands and natural language (LLM-powered suggestions)
- **Command history search** (Ctrl-R) with semantic search via embeddings
- **Pipeline mode**: `rubichan shell -c "find bugs in the auth module"` for single-command non-interactive use
- **Shell aliases**: user-defined shortcuts (e.g., `t` → `go test ./...`)
- **Multi-line input**: heredoc-style or `\` continuation for complex queries
