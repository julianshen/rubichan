# Shell Mode — TDD Implementation Plan

> **Date:** 2026-03-27 · **Design:** `2026-03-27-shell-mode-design.md`
> **Estimated Tests:** 42 · **Packages:** `internal/shell/`, `cmd/rubichan/`

---

## Feature 1: Input Classifier

The classifier determines whether user input is a shell command, built-in, LLM query, or slash command. This is the core heuristic that makes shell mode feel right.

**Package:** `internal/shell/`
**Files:** `classifier.go`, `classifier_test.go`

### Tests

- [ ] **Test 1.1: Force shell prefix** — Input starting with `!` is classified as `ClassShellCommand` with the `!` stripped from the command.
  ```go
  // "!docker compose up" → ClassShellCommand, Command: "docker compose up"
  ```

- [ ] **Test 1.2: Force LLM prefix** — Input starting with `?` is classified as `ClassLLMQuery` with the `?` stripped.
  ```go
  // "?what does this Makefile do" → ClassLLMQuery, Raw: "what does this Makefile do"
  ```

- [ ] **Test 1.3: Slash command detection** — Input starting with `/` is classified as `ClassSlashCommand`.
  ```go
  // "/model claude-sonnet-4-5" → ClassSlashCommand, Command: "model", Args: ["claude-sonnet-4-5"]
  ```

- [ ] **Test 1.4: Known executable detection** — Input starting with a known executable (from `$PATH` scan) is classified as `ClassShellCommand`.
  ```go
  // With knownExecutables: {"ls": true, "git": true}
  // "ls -la" → ClassShellCommand
  // "git status" → ClassShellCommand
  ```

- [ ] **Test 1.5: Built-in command detection** — `cd`, `export`, `exit`, `quit` are classified as `ClassBuiltinCommand`.
  ```go
  // "cd src/" → ClassBuiltinCommand, Command: "cd", Args: ["src/"]
  // "export FOO=bar" → ClassBuiltinCommand, Command: "export", Args: ["FOO=bar"]
  // "exit" → ClassBuiltinCommand, Command: "exit"
  ```

- [ ] **Test 1.6: Natural language with question words** — Input containing question words (`what`, `why`, `how`, `explain`, `describe`) without matching a known executable is classified as `ClassLLMQuery`.
  ```go
  // "what files changed?" → ClassLLMQuery
  // "explain the auth flow" → ClassLLMQuery
  // "how do I run tests" → ClassLLMQuery
  ```

- [ ] **Test 1.7: Imperative natural language** — Input with imperative verbs like `fix`, `refactor`, `add`, `create`, `update`, `find bugs` is classified as `ClassLLMQuery`.
  ```go
  // "fix the failing test" → ClassLLMQuery
  // "refactor the auth module" → ClassLLMQuery
  ```

- [ ] **Test 1.8: Ambiguous input defaults to LLM** — Input that doesn't match any known executable or clear pattern defaults to `ClassLLMQuery`.
  ```go
  // "run the tests" → ClassLLMQuery (not a direct executable)
  // "deploy to staging" → ClassLLMQuery
  ```

- [ ] **Test 1.9: Empty input** — Empty string or whitespace-only input returns a zero-value `ClassifiedInput` with empty classification (handled by caller as no-op).

- [ ] **Test 1.10: PATH scanning** — `NewInputClassifier()` scans `$PATH` directories and populates `knownExecutables` with found binaries.
  ```go
  // Given PATH="/usr/bin:/usr/local/bin", with /usr/bin containing "ls", "grep"
  // classifier.knownExecutables should contain "ls", "grep"
  ```

- [ ] **Test 1.11: Command with env prefix** — `VAR=val command args` is classified as `ClassShellCommand` when `command` is a known executable.
  ```go
  // "GOFLAGS=-v go test ./..." → ClassShellCommand
  ```

- [ ] **Test 1.12: Pipe chains classified as shell** — Input containing `|` where the first segment starts with a known executable is classified as `ClassShellCommand`.
  ```go
  // "ls -la | grep test" → ClassShellCommand
  // "cat file.go | head -20" → ClassShellCommand
  ```

---

## Feature 2: Prompt Renderer

Generates the PS1-style prompt showing CWD, git branch, and AI indicator.

**Package:** `internal/shell/`
**Files:** `prompt.go`, `prompt_test.go`

### Tests

- [ ] **Test 2.1: Basic prompt with CWD** — Renders working directory in prompt.
  ```go
  // workDir: "/home/user/project" → "~/project ai$ "
  ```

- [ ] **Test 2.2: Home directory shortening** — `$HOME` prefix is replaced with `~`.
  ```go
  // workDir: "/home/user/project", HOME: "/home/user" → "~/project ai$ "
  ```

- [ ] **Test 2.3: Git branch display** — When in a git repo, shows branch name.
  ```go
  // workDir: "/home/user/project", branch: "main" → "~/project (main) ai$ "
  ```

- [ ] **Test 2.4: No git branch outside repo** — When not in a git repo, omits branch.
  ```go
  // workDir: "/tmp", no git → "/tmp ai$ "
  ```

- [ ] **Test 2.5: Root directory** — Handles filesystem root correctly.
  ```go
  // workDir: "/" → "/ ai$ "
  ```

- [ ] **Test 2.6: Detached HEAD** — Shows short SHA when HEAD is detached.
  ```go
  // detached at abc1234 → "~/project (abc1234) ai$ "
  ```

---

## Feature 3: Context Tracker

Manages the sliding window of last shell command + output for LLM context injection.

**Package:** `internal/shell/`
**Files:** `context.go`, `context_test.go`

### Tests

- [ ] **Test 3.1: Record command and output** — `Record(command, output, exitCode)` stores the last command context.
  ```go
  // tracker.Record("ls -la", "file1.go\nfile2.go", 0)
  // tracker.LastCommand() == "ls -la"
  // tracker.LastOutput() == "file1.go\nfile2.go"
  ```

- [ ] **Test 3.2: Output truncation** — Output exceeding `MaxOutputTokens` is truncated with a marker.
  ```go
  // tracker with MaxOutputTokens=100
  // Record("cmd", longOutput, 0)
  // tracker.LastOutput() ends with "\n... (truncated, 5000 more bytes)"
  ```

- [ ] **Test 3.3: Context message generation** — `ContextMessage()` returns a formatted context block for LLM injection.
  ```go
  // "The user just ran `ls -la` (exit code 0) with output:\n```\nfile1.go\n```"
  ```

- [ ] **Test 3.4: No context when empty** — `ContextMessage()` returns empty string when no command has been recorded.

- [ ] **Test 3.5: Failed command context** — Non-zero exit code is included in context message.
  ```go
  // Record("go test", "FAIL ...", 1)
  // ContextMessage() includes "exit code 1"
  ```

- [ ] **Test 3.6: Clear context** — `Clear()` resets the tracker. Subsequent `ContextMessage()` returns empty.

- [ ] **Test 3.7: Overwrite on new record** — Recording a new command replaces the previous one.

---

## Feature 4: Command History

Persistent readline history for shell mode sessions.

**Package:** `internal/shell/`
**Files:** `history.go`, `history_test.go`

### Tests

- [ ] **Test 4.1: Add and retrieve entries** — `Add(line)` stores entries, `Entries()` returns them in order.

- [ ] **Test 4.2: Duplicate suppression** — Consecutive identical entries are stored only once.

- [ ] **Test 4.3: Max history size** — History is capped at a configurable maximum. Oldest entries are evicted.

- [ ] **Test 4.4: Persistence to file** — `Save(path)` writes history to disk, `Load(path)` reads it back.

- [ ] **Test 4.5: Empty history** — `Entries()` on fresh history returns empty slice. `Save` on empty history creates empty file.

- [ ] **Test 4.6: Previous/Next navigation** — `Previous()` and `Next()` navigate the history stack for readline integration.

---

## Feature 5: Shell Host (REPL Loop)

The main orchestrator that ties classifier, prompt, context, and agent together.

**Package:** `internal/shell/`
**Files:** `shell.go`, `shell_test.go`

### Tests

- [ ] **Test 5.1: Shell command execution** — Input classified as shell command is executed via ShellTool, output appears on stdout.
  ```go
  // Input: "echo hello"
  // stdout: "hello\n"
  // No LLM call made
  ```

- [ ] **Test 5.2: LLM query execution** — Input classified as LLM query is sent to the agent, response streamed to stdout.
  ```go
  // Input: "explain this codebase"
  // Agent.Turn() called with user message "explain this codebase"
  ```

- [ ] **Test 5.3: Built-in cd changes working directory** — `cd src/` updates `ShellHost.workDir` and subsequent commands run in new dir.
  ```go
  // Initial: workDir="/project"
  // Input: "cd src"
  // workDir="/project/src"
  // Next shell command runs in /project/src
  ```

- [ ] **Test 5.4: Built-in cd with invalid path** — `cd nonexistent/` prints error, workDir unchanged.

- [ ] **Test 5.5: Built-in export sets env** — `export FOO=bar` sets environment variable for subsequent shell commands.

- [ ] **Test 5.6: Exit terminates loop** — `exit` (or `quit`) causes `Run()` to return nil.

- [ ] **Test 5.7: EOF (Ctrl-D) terminates loop** — EOF on stdin causes `Run()` to return nil.

- [ ] **Test 5.8: Empty input renders new prompt** — Empty line input does not execute anything, just re-renders prompt.

- [ ] **Test 5.9: Context injection on LLM query after shell command** — After a direct shell command, the next LLM query includes the command context.
  ```go
  // Input 1: "go test ./..." (shell command, fails)
  // Input 2: "why did that fail?" (LLM query)
  // Agent.Turn() receives context about "go test ./..." output
  ```

- [ ] **Test 5.10: Slash command delegation** — `/model claude-sonnet-4-5` is passed to the command registry.

- [ ] **Test 5.11: Agent mode label** — Agent is initialized with `WithMode("shell")`.

---

## Feature 6: CLI Integration

Wire shell mode into the Cobra command tree and main.go initialization.

**Package:** `cmd/rubichan/`
**Files:** `main.go` (modified), `shell.go` (new — thin command wiring)

### Tests

- [ ] **Test 6.1: Shell subcommand exists** — `rubichan shell --help` shows shell mode help text.

- [ ] **Test 6.2: Shell inherits global flags** — `rubichan shell --model=X --provider=Y` passes config to shell host.

- [ ] **Test 6.3: Shell with --auto-approve** — Auto-approve flag skips approval prompts in shell mode.

- [ ] **Test 6.4: Shell with --resume** — `rubichan shell --resume=<id>` restores previous session.

---

## Implementation Order

The features should be implemented in this order (each builds on the previous):

1. **Input Classifier** (Feature 1) — No dependencies, pure logic
2. **Prompt Renderer** (Feature 2) — No dependencies, pure logic
3. **Context Tracker** (Feature 3) — No dependencies, pure logic
4. **Command History** (Feature 4) — No dependencies, file I/O
5. **Shell Host** (Feature 5) — Depends on 1-4 + agent/tools integration
6. **CLI Integration** (Feature 6) — Depends on 5, wires into cmd/

Features 1-4 can be developed in parallel as they have no interdependencies.

---

## Test Infrastructure Notes

- Shell host tests use a mock agent (mock `Turn()` function) and mock ShellTool
- Classifier tests use a synthetic `knownExecutables` map (no real PATH scan)
- History persistence tests use `t.TempDir()`
- Integration tests (Feature 6) use the existing `coverage_test.go` pattern from `cmd/rubichan/`
