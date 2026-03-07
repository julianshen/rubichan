# AGENTS.md

This file provides guidance to coding agents working in this repository.

## Project Overview

Rubichan is a Go-based AI coding agent specified in `spec.md` (v1.1, Feb 2026).
Treat `spec.md` as the source of truth for architecture and behavior.

## Critical Invariants

1. Correctness over performance.
2. Do not claim performance improvements without benchmark data.
3. Do not commit directly to `main`; use feature/fix branches and PRs.

## Architecture

All modes share a common Agent Core in `internal/agent/` with mode-specific I/O adapters.

- Provider layer: `internal/provider/` (Anthropic/OpenAI/Ollama via custom HTTP+SSE; no vendor SDKs)
- Tool layer: `internal/tools/` (`Tool` interface: `Name`, `Description`, `InputSchema`, `Execute`)
- Skill runtime: `internal/skills/` (Starlark, Go plugins, JSON-RPC external processes)
- Security engine: `internal/security/` (static scanners + LLM analyzers)
- Wiki pipeline: `internal/wiki/` (batch docs + Mermaid)
- Public SDK: `pkg/skillsdk/` (stable for skill authors)

## Development Workflow

- Follow strict TDD: Red -> Green -> Refactor.
- One change at a time; run tests continuously.
- Keep structural changes separate from behavioral changes.
- Refactor only when tests are green.
- Prefer small, reviewable PRs.

When executing from `plan.md`:
1. Pick the next unchecked (`[ ]`) test.
2. Add a failing test.
3. Implement the minimal passing code.
4. Refactor while keeping tests green.
5. Run validation commands.
6. Mark the plan item complete (`[x]`).
7. Commit with the required prefix.

## Validation Commands

```bash
go test ./...
go test -cover ./...
golangci-lint run ./...
gofmt -l .
```

Useful package-level commands:

```bash
go test ./internal/agent/...
go test -run TestFunctionName
go build ./cmd/agent
go run ./cmd/agent
go run ./cmd/agent --headless
go run ./cmd/agent wiki
```

## Commit Rules

Commit only when:
- tests pass
- formatting is clean
- lint/compile warnings are resolved
- changes form one logical unit

Commit prefixes:

```text
[STRUCTURAL] ...
[BEHAVIORAL] ...
```

Do not mix structural and behavioral work in one commit.

## Code Quality Standards

- DRY
- Intent-revealing names
- Explicit dependencies
- Small focused functions
- Minimal side effects
- YAGNI
- Comments explain why, not what

## Conventions

- Standard Go layout (`cmd/`, `internal/`, `pkg/`)
- `spf13/cobra` for CLI
- Wrap errors with context (`fmt.Errorf("...: %w", err)`)
- Config in TOML; security/project rules in `.security.yaml` and `AGENT.md`
