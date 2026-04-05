# TUI Shell Escape (`!` Prefix) — Design Spec

## Goal

Add `! <command>` shell escape to the main interactive TUI, allowing users to run shell commands directly from the chat prompt with output captured and injected into the conversation context.

## Behavior

When the user types `! git status` and presses Enter:

1. The TUI detects the `!` prefix before checking for slash commands or agent routing
2. Strips the `!` prefix and executes the command via `os/exec`
3. Captures combined stdout+stderr
4. Renders the result as a `CollapsibleToolResult` with the `❯` shell icon — identical to agent shell tool results
5. Injects the command and output into the conversation as a user message so the LLM can reference it on subsequent turns
6. No approval prompt — user explicitly typed `!`, this is intentional

## Context Injection Format

The command and output are added to the conversation as a user message:

```
[User ran shell command]
$ git status
[Output]
On branch main
nothing to commit, working tree clean
```

This ensures the LLM sees what was run and can reference it (e.g., "based on the git status you just ran...").

## Display

- Success: normal tool box with `❯` icon, `✓` status
- Non-zero exit: error tool box (red border) with `✗` status
- Output is collapsible/expandable via existing Ctrl+T/Ctrl+E
- Truncation at 20 lines with expand hint (same as tool results)

## Error Handling

- Empty command (`!` with no text): ignore, do nothing
- Command not found: show the shell error in the error box
- Timeout (30 seconds): kill the process, show timeout error
- Non-zero exit: show output in error style, include exit code

## Scope

- Simple command execution only
- No piping through a shell interpreter — uses `exec.Command("sh", "-c", command)` for shell feature support (pipes, redirects, env vars)
- No interactive commands (vim, less, top) — no PTY allocation
- No background execution

## Files

| File | Change |
|------|--------|
| `internal/tui/update.go` | Add `!` prefix check in input submission handler |
| `internal/tui/shellexec.go` | New: execute command, capture output, return result |
| `internal/tui/shellexec_test.go` | Tests for execution and output capture |
