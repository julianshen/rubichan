# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rubichan is a Go-based AI Coding Agent specified in `spec.md` (v1.1, Feb 2026). It is currently in the **specification phase** — no implementation code exists yet. The spec defines a terminal-first CLI tool with three execution modes: Interactive (Bubble Tea TUI), Headless (CI/CD pipes), and Wiki Generator (batch documentation).

The spec is the single source of truth. Always consult `spec.md` before making architectural decisions.

## CRITICAL ARCHITECTURAL INVARIANTS — DO NOT VIOLATE

1. **Correctness Over Performance**: Always choose the correct solution over the fast one. Never sacrifice correctness for optimization.
2. **Performance Claims Require Validation**: Any claim that a change improves performance must be backed by benchmark data. No speculative optimization.
3. **Never Commit Directly to Main**: All changes go through feature branches and PRs. No exceptions.

## Architecture (from spec)

All three modes share a common **Agent Core** (`internal/agent/`) with mode-specific thin I/O adapters. Key subsystems:

- **Provider Layer** (`internal/provider/`): LLM abstraction over Anthropic, OpenAI, Ollama — custom HTTP+SSE, no vendor SDKs (ADR-006)
- **Tool Layer** (`internal/tools/`): Interface-based tool registry; tools implement `Tool` interface with `Name()`, `Description()`, `InputSchema()`, `Execute()`
- **Skill Runtime** (`internal/skills/`): Three backends — Starlark (sandboxed scripting), Go plugins, external processes via JSON-RPC. Five skill types: tool, prompt, workflow, security-rule, transform
- **Security Engine** (`internal/security/`): Two-phase hybrid — fast static scanners then LLM-powered analyzers on prioritized segments
- **Wiki Pipeline** (`internal/wiki/`): Batch analysis producing static docs with Mermaid diagrams

Public SDK lives in `pkg/skillsdk/` — stable interface for skill authors. Everything in `internal/` is private.

## ACP Protocol Integration — a working protocol layer, still no production caller

**This section previously described ACP as "the standardized backbone for agent-mode communication." That was aspirational, not descriptive.** A Phase 3 audit (see `docs/MODULAR_CORE_REDESIGN.md`) established that the mode adapters that were supposed to be ACP's consumers could not complete a single operation against the server, and they have since been deleted.

Current state, so nobody plans against the old claim:

- **`internal/acp` still has no production caller.** `agent.ServeACP` is the composition root — registry, connection and session wiring in one call — but nothing in `cmd/rubichan/main.go` invokes it. There is no `--acp` flag. `grep -rn "ServeACP" --include=*.go . | grep -v _test.go` returns its own definition and nothing else.
- **The three execution modes do not use ACP.** All are implemented inline in `cmd/rubichan/main.go`, but they do not share a shape: interactive and headless bind directly to `internal/agent`, while `--wiki` calls `runWikiHeadless`, which drives `internal/wiki.Run` and never constructs an agent at all. Do not look for a wiki agent integration point; there isn't one.
- **Registered methods are `initialize`, `session/new`, `session/prompt`, `agent/prompt`, `tool/execute`, `skill/{invoke,list,manifest}`, `security/{scan,approve}`, and `shutdown`.** `tool/execute` is an explicit `not_implemented` stub. `agent/prompt` predates the session methods and is not an ACP method at all — `session/prompt` is the spec-defined one.
- **A turn can now be conducted end to end.** `session/new` mints a session against an absolute cwd; `session/prompt` decodes content, runs the turn, streams `session/update` notifications, and closes with a `stopReason`. `internal/agent/acp_serve_internal_test.go` drives that whole sequence over a pipe.
- **Server-initiated dispatch exists in one direction only.** `Conn.Notify` sends notifications — that is what carries `session/update` — so streaming output works. There is still no mid-turn `session/request_permission`: `Conn.Request` can express it, but nothing calls it.
- **A turn cannot be cancelled.** `Handler` takes only `params`, so there is no request context to derive from and `session/cancel` is unimplementable until a context is threaded through every handler in the package.
- **Declared capabilities are all false, and that is enforced, not just documented.** `acpAgentCapabilities()` is the single source: the handshake publishes it and `session/prompt` gates incoming content on it. So `image`, `audio` and `resource` content blocks are refused, and `session/new` refuses `mcpServers` and `additionalDirectories` rather than accepting and ignoring them.
- **The method constants in `types.go` are largely unwired**, in two degrees. `tools/list`, `tools/call` and `resources/list` are referenced only by `methods_test.go`, which round-trips them through JSON. `resources/read`, `prompts/list`, `prompts/call` and `sampling/createMessage` have **no reference anywhere in the tree** — not even a test. None of the seven is registered or sent.

Do not add capabilities to ACP on the assumption that something consumes them — flip a capability flag on in the same change that registers the method behind it, never before. Whether ACP should get a mode that actually calls `ServeACP`, or be removed, is still an open decision.

Note also that `internal/acp` is **not** the repository's MCP support: that lives in `internal/tools/mcp` and `internal/skills/mcpbackend`.

## Build Commands (planned)

```bash
go build ./cmd/agent              # Build binary
go test ./...                     # Run all tests
go test -cover ./...              # Tests with coverage
go test ./internal/agent/...      # Test specific package
go test -run TestFunctionName     # Run single test
golangci-lint run ./...           # Lint
gofmt -l .                        # Check formatting
go run ./cmd/agent                # Run interactive mode
go run ./cmd/agent --headless     # Run headless mode
go run ./cmd/agent wiki           # Run wiki generator
```

## Key Design Decisions

- **Go** chosen for single-binary distribution, goroutine concurrency, Charm TUI ecosystem (ADR-001)
- **No vendor LLM SDKs**: each provider is ~300 LOC custom HTTP+SSE (ADR-006)
- **Tree-sitter** for multi-language AST parsing, 50+ languages (ADR-007)
- **Starlark** for skill scripting — deterministic, sandboxed, Python-like (ADR-008)
- **Mermaid** for diagrams — GitHub-native, LLM-friendly (ADR-005)
- **Pure Go SQLite** (`modernc.org/sqlite`) for persistence — no CGO dependency
- **Structured concurrency** via `sourcegraph/conc`

## Development Rules

- **TDD strictly**: One test at a time. The rhythm is: **Red** (add a failing test) → **Green** (make it pass with minimal code) → **Refactor** (clean up) → **Commit** → **Repeat**. Never write implementation before the test. Run all tests (except those marked long-running) after every change.
- **Branch-based workflow**: Always work on feature/fix branches. Create PRs for merging into main.
- **Small PRs**: Keep pull requests focused and reviewable. Split large changes into incremental PRs.
- **Test coverage >90%**: All new code must have >90% test coverage. Check with `go test -cover`.

### Tidy First: Separate Structural from Behavioral Changes

Every change is either **structural** (renaming, extracting methods, moving code, reformatting — no behavior change) or **behavioral** (new features, bug fixes, logic changes).

- **Never mix** structural and behavioral changes in the same commit.
- **Structural changes first** when both are needed — tidy the code, then add behavior.
- **Validate structural changes** don't alter behavior: run tests before and after. If tests break, revert and investigate.

### Refactoring Guidelines

- **Only refactor in Green**: Tests must be passing before starting any refactoring.
- **One refactoring at a time**: Small, safe steps — Extract Method, Rename Variable, Move Method, Inline Method, Extract Interface, etc. Use the proper pattern names in commits.
- **Run tests after each step**: Continuous validation. If tests fail, revert immediately.
- **Prioritize** refactorings that: remove duplication, improve clarity, reduce complexity, or make future changes easier.

### Commit Discipline

Only commit when **all** of these are true:
- All tests passing — no exceptions
- Zero compiler/linter warnings
- Code is properly formatted (`gofmt`)
- The change is a single logical unit of work

**Commit message format** — prefix with change type:
```
[STRUCTURAL] Extract validation logic into separate method
[BEHAVIORAL] Add support for JSON input format
```

Prefer small, frequent commits. Each commit should tell a story.

### Implementation Workflow

When executing from a plan (`plan.md`):
1. Read `plan.md`, find next unchecked (`[ ]`) test
2. Write failing test (Red)
3. Write minimum code to pass (Green)
4. Refactor if needed (keep green)
5. Run `go test ./...`, `golangci-lint run`, `gofmt -l .`
6. Mark test `[x]` in `plan.md`
7. Commit with `[BEHAVIORAL]` or `[STRUCTURAL]` prefix
8. Repeat

Quality is not negotiable — always prioritize clean, well-tested code over quick implementation.

## Code Quality Standards

- **DRY**: Eliminate duplication ruthlessly
- **Intent-revealing names**: Code should read like prose — choose names and structure that make purpose obvious
- **Explicit dependencies**: No hidden coupling between packages or components
- **Small, focused methods**: Single responsibility — one reason to change per function
- **Minimize state and side effects**: Prefer pure functions when possible
- **YAGNI**: Use the simplest solution that works — don't build for hypothetical futures
- **Comments explain why, not what**: The code shows what; comments explain the reasoning

## Code Conventions

- Standard Go project layout: `cmd/` for entrypoints, `internal/` for private packages, `pkg/` for public API
- CLI built with `spf13/cobra`
- Errors wrapped with context: `fmt.Errorf("operation failed: %w", err)`
- Interfaces define boundaries between subsystems (provider, tool, scanner, analyzer)
- Skill permissions are declarative (file:read, shell:exec, net:fetch, etc.) and user-approved
- Config format is TOML (`~/.config/aiagent/config.toml`), project rules in `.security.yaml` and `AGENT.md`

## Implementation Roadmap

1. **Milestone 1** (Weeks 1–4): CLI entrypoint, TUI, Anthropic provider, agent loop, file/shell tools, config
2. **Milestone 2** (Weeks 5–8): Headless runner, output formatters, code review pipeline, security scanners, Xcode tools
3. **Milestone 3** (Weeks 9–12): Skill system — manifest, loader, Starlark engine, permissions, hooks, CLI
4. **Milestone 4** (Weeks 13–16): Wiki generator, LLM analyzers, skill registry, MCP client, SQLite, >90% test coverage
