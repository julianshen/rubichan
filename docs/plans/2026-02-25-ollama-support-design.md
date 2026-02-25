# Ollama Support Enhancement Design

**Date:** 2026-02-25
**Status:** Approved

## Context

The Ollama provider is already fully implemented (`internal/provider/ollama/`) with streaming, tool calling, and comprehensive tests. However, the user experience around Ollama is lacking: there's no model management CLI, no auto-detection of a running Ollama instance, no interactive model picker, and no integration tests against a real server.

## Goals

1. CLI subcommands for model management (`rubichan ollama list/pull/rm/status`)
2. Auto-detect running Ollama when no API key is configured
3. Interactive model picker when multiple local models are available
4. Integration tests gated behind a build tag

## Non-Goals

- Agent tools for model management (CLI only)
- Model recommendation engine
- Ollama download/installation helper
- GPU detection or hardware profiling

---

## 1. CLI Subcommands — `rubichan ollama`

New cobra subcommand tree in `cmd/rubichan/ollama.go`:

```
rubichan ollama list          # List locally available models
rubichan ollama pull <model>  # Pull a model (shows progress)
rubichan ollama rm <model>    # Remove a model
rubichan ollama status        # Check if Ollama is running
```

### API Client

Create `internal/provider/ollama/client.go` with a thin HTTP client for the Ollama REST API:

- `ListModels(ctx) ([]ModelInfo, error)` — GET `/api/tags`
- `PullModel(ctx, name) (<-chan PullProgress, error)` — POST `/api/pull` (streaming NDJSON)
- `DeleteModel(ctx, name) error` — DELETE `/api/delete`
- `Version(ctx) (string, error)` — GET `/api/version`
- `IsRunning(ctx) bool` — probe with 1-second timeout

### Types

```go
type ModelInfo struct {
    Name       string    // e.g. "llama3.2:latest"
    Size       int64     // bytes
    ModifiedAt time.Time
    Digest     string
}

type PullProgress struct {
    Status    string // "pulling manifest", "downloading", "verifying", "success"
    Total     int64
    Completed int64
}
```

### CLI Flags

The `ollama` subcommand accepts `--base-url` to override the configured Ollama URL (default `localhost:11434`).

---

## 2. Auto-Detection

In `loadConfig()` or a new `autoDetectProvider()` helper:

1. If `--provider` flag is set explicitly, honor it (skip auto-detection)
2. If `cfg.Provider.Default == "anthropic"` (the default) AND no `ANTHROPIC_API_KEY` env var is set:
   - Call `client.IsRunning(ctx)` with 1-second timeout on `localhost:11434`
   - If Ollama responds: set `cfg.Provider.Default = "ollama"`, print `Using local Ollama (no API key configured)`
   - If Ollama isn't running: return the normal "no API key" error
3. All other cases: proceed with configured provider

This means `ollama serve` + `rubichan` works with zero config.

---

## 3. Interactive Model Picker

When using Ollama with no `--model` flag and no model in config:

1. Call `client.ListModels(ctx)` to get locally available models
2. If exactly one model: use it automatically, print model name
3. If multiple models: show a Bubble Tea selection list
4. If zero models: show error `No models found. Run 'rubichan ollama pull llama3.2' first.`

This only applies to **interactive mode**. In headless mode, if no model is specified, return an error requiring `--model`.

### TUI Component

Create `internal/tui/modelpicker.go` — a minimal Bubble Tea list model that accepts `[]ModelInfo` and returns the selected model name.

---

## 4. Integration Tests

File: `internal/provider/ollama/integration_test.go`

Build tag: `//go:build integration`

Tests:
- Stream a simple text completion and verify response events
- List models and verify response parsing
- Check version endpoint
- Skip gracefully if Ollama isn't reachable at `localhost:11434`

These tests run locally only, never in CI (no Ollama in CI environment).

---

## Architecture

```
cmd/rubichan/
  ollama.go              # CLI subcommands (list, pull, rm, status)
  main.go                # Auto-detection in loadConfig(), model picker in runInteractive()

internal/provider/ollama/
  client.go              # HTTP client for Ollama REST API
  client_test.go         # Unit tests with httptest server
  provider.go            # (existing) LLM streaming provider
  provider_test.go       # (existing) provider tests
  integration_test.go    # Integration tests (build tag gated)

internal/tui/
  modelpicker.go         # Bubble Tea model selection component
  modelpicker_test.go    # Component tests
```

## Dependencies

No new external dependencies. Uses `net/http`, `encoding/json`, existing Bubble Tea framework.

## Testing Strategy

- `client.go` — unit tests with `httptest.NewServer` mocking Ollama API responses
- `ollama.go` CLI — test flag parsing and error handling
- `modelpicker.go` — test component initialization and model rendering
- `integration_test.go` — real Ollama tests behind build tag
- Target: >90% coverage on new code
