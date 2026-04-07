# Provider Layer Improvements Plan

**Date:** 2026-04-07
**Reference:** claurst `src-rust/crates/api` patterns
**Branch:** `claude/review-claurst-api-2e5dM`

---

## Motivation

The current provider layer has three structural problems:

1. **Massive duplication** — OpenAI, Z.ai, and Ollama all re-implement the same message conversion logic (`convertMessages`, `convertAssistantMessage`, `convertUserMessages`), the same SSE parsing loop, and the same `toolCallAccumulator`. Z.ai is a near-verbatim copy of OpenAI (compare `openai/provider.go` and `zai/provider.go`).

2. **Flat error handling** — `FormatAPIError` returns `error` strings. The retry layer (`DoWithRetry`) makes retry decisions based on HTTP status codes, but callers (agent loop) cannot distinguish rate-limits from auth failures from context overflow. The agent loop treats all provider errors the same way (abort the turn).

3. **Coarse stream events** — The 5 `StreamEvent` types collapse information. `input_json_delta` from Anthropic is silently converted to `text_delta` (anthropic/provider.go:350), which works but prevents the TUI from showing progressive tool-call arguments. There's no `MessageStart` event carrying initial usage/model info.

This plan addresses all four improvements in dependency order, each as an independent phase with its own commits. Every phase follows TDD: red-green-refactor.

---

## Phase 1: Typed ProviderError (STRUCTURAL then BEHAVIORAL)

**Why first:** ProviderError is a leaf type with no dependencies on other changes. Everything else benefits from it — the transformer can return typed errors, retry logic can use `IsRetryable()`, and the agent loop can react to `ContextOverflow`.

### 1.1 Define ProviderError type

**File:** `internal/provider/provider_error.go` (new)

```go
package provider

type ErrorKind int

const (
    ErrRateLimited ErrorKind = iota
    ErrAuthFailed
    ErrContextOverflow
    ErrModelNotFound
    ErrServerError
    ErrStreamError
    ErrContentFiltered
    ErrInvalidRequest
    ErrQuotaExceeded
    ErrOther
)

type ProviderError struct {
    Kind        ErrorKind
    Provider    string        // e.g. "anthropic", "openai"
    Message     string        // human-readable
    StatusCode  int           // HTTP status, 0 if not applicable
    RetryAfter  time.Duration // for RateLimited
    Suggestions []string      // for ModelNotFound
    Retryable   bool          // explicit override
}

func (e *ProviderError) Error() string { ... }
func (e *ProviderError) IsRetryable() bool { ... }
```

#### Tests (Red → Green)

- [ ] `TestProviderError_Error` — each ErrorKind produces a distinct message
- [ ] `TestProviderError_IsRetryable` — RateLimited and ServerError are retryable; AuthFailed, ModelNotFound, ContextOverflow are not
- [ ] `TestProviderError_Unwrap` — standard errors.As works to extract ProviderError from wrapped errors

### 1.2 Migrate FormatAPIError to return *ProviderError

**File:** `internal/provider/apierror.go`

Refactor `FormatAPIError` to return `*ProviderError` instead of `error`. Keep the existing function signature as a thin wrapper for backward compatibility during migration, then remove once all callers are updated.

New function: `ClassifyAPIError(statusCode int, body []byte, httpReq *http.Request, providerName string) *ProviderError`

- Parses HTTP status → ErrorKind (429 → RateLimited, 401 → AuthFailed, 404 → ModelNotFound, 413/400 with overflow patterns → ContextOverflow, 5xx → ServerError)
- Extracts `Retry-After` header → `RetryAfter` duration
- Extracts error message from Anthropic/OpenAI/Ollama JSON formats (existing logic)
- Detects context overflow from error message patterns (e.g. "maximum context length", "prompt is too long", "context_length_exceeded")

#### Tests

- [ ] `TestClassifyAPIError_RateLimited` — 429 status → ErrRateLimited, IsRetryable true
- [ ] `TestClassifyAPIError_RateLimited_RetryAfter` — parses Retry-After header into duration
- [ ] `TestClassifyAPIError_AuthFailed` — 401 → ErrAuthFailed, not retryable
- [ ] `TestClassifyAPIError_ModelNotFound` — 404 → ErrModelNotFound with request details
- [ ] `TestClassifyAPIError_ContextOverflow_Status413` — 413 → ErrContextOverflow
- [ ] `TestClassifyAPIError_ContextOverflow_MessagePattern` — 400 with "maximum context length" → ErrContextOverflow
- [ ] `TestClassifyAPIError_ServerError` — 500/502/503 → ErrServerError, retryable
- [ ] `TestClassifyAPIError_ContentFiltered` — provider-specific filter messages → ErrContentFiltered
- [ ] `TestClassifyAPIError_UnknownStatus` — fallback → ErrOther with raw body excerpt
- [ ] `TestClassifyAPIError_PreservesLegacyBehavior` — FormatAPIError wrapper still returns same user-friendly strings

### 1.3 Update DoWithRetry to use ProviderError

**File:** `internal/provider/retry.go`

Change retry logic to use `ProviderError.IsRetryable()` when available. For non-`ProviderError` errors (network errors), keep the existing `shouldRetryError` logic.

Add `RetryAfter` awareness: if the `ProviderError` carries a `RetryAfter` duration, use it instead of exponential backoff.

#### Tests

- [ ] `TestDoWithRetry_UsesProviderErrorRetryAfter` — when server returns 429 with Retry-After, delay matches
- [ ] `TestDoWithRetry_SkipsRetryOnAuthFailed` — 401 response is not retried
- [ ] `TestDoWithRetry_SkipsRetryOnContextOverflow` — 413/overflow is not retried

### 1.4 Update all provider Stream() methods

**Files:** `anthropic/provider.go`, `openai/provider.go`, `ollama/provider.go`, `zai/provider.go`

Replace `provider.FormatAPIError(...)` calls with `provider.ClassifyAPIError(...)`. Pass provider name string.

#### Tests

- [ ] `TestAnthropicProvider_Stream_AuthError` — returns *ProviderError with ErrAuthFailed
- [ ] `TestOpenAIProvider_Stream_RateLimited` — returns *ProviderError with ErrRateLimited
- [ ] `TestOllamaProvider_Stream_ServerError` — returns *ProviderError with ErrServerError

### 1.5 Update agent loop to react to ProviderError

**File:** `pkg/agentsdk/agent.go`

In `runLoop`, after `a.provider.Stream()` returns an error, check if it's a `*ProviderError`:
- `ErrContextOverflow` → emit a `TurnEvent{Type: "context_overflow"}` (future: trigger compaction)
- `ErrRateLimited` → emit event with retry info for TUI display
- Others → existing behavior (emit error, stop)

#### Tests

- [ ] `TestAgent_Turn_ContextOverflowEvent` — provider returns ContextOverflow → TurnEvent type is "context_overflow"
- [ ] `TestAgent_Turn_ProviderErrorPreservesKind` — agent emits error event; errors.As extracts ProviderError

---

## Phase 2: MessageTransformer Interface (STRUCTURAL)

**Why second:** This is a pure structural refactoring. Extracts JSON serialization from providers into a composable interface. Depends on Phase 1 for typed error returns.

### 2.1 Define MessageTransformer interface

**File:** `internal/provider/transformer.go` (new)

```go
package provider

type MessageTransformer interface {
    // ToProviderJSON converts a CompletionRequest into the provider-specific
    // JSON request body.
    ToProviderJSON(req CompletionRequest) ([]byte, error)

    // ParseResponse converts a provider-specific JSON response into a
    // ProviderResponse. (Used for non-streaming; streaming uses the existing
    // SSE/NDJSON parsers.)
    // Reserved for future non-streaming support.
}
```

Keep it minimal for now — only `ToProviderJSON` is needed since all providers are streaming-only. `ParseResponse` is a placeholder method for future non-streaming support (do not implement yet).

#### Tests

- [ ] `TestMessageTransformer_InterfaceCompliance` — verify concrete types satisfy the interface

### 2.2 Extract AnthropicTransformer

**File:** `internal/provider/anthropic/transformer.go` (new)

Extract `buildRequestBody` and all its helpers (`apiRequest`, `apiMessage`, `apiTool`, `apiContentBlock`, `convertContentBlocks`, `buildCachedSystemBlocks`) from `provider.go` into a `Transformer` struct that implements `MessageTransformer`.

The `Provider.Stream()` method calls `t.ToProviderJSON(req)` instead of `p.buildRequestBody(req)`.

#### Tests

- [ ] `TestAnthropicTransformer_BasicRequest` — model, max_tokens, stream:true, messages
- [ ] `TestAnthropicTransformer_SystemPrompt` — system field is top-level string
- [ ] `TestAnthropicTransformer_CachedSystemBlocks` — cache breakpoints produce blocks with cache_control
- [ ] `TestAnthropicTransformer_Tools` — tools serialized with input_schema, last tool has cache_control
- [ ] `TestAnthropicTransformer_ToolResultBlock` — tool_result uses "content" not "text"
- [ ] `TestAnthropicTransformer_EmptyTextBlocksSkipped` — empty text blocks filtered
- [ ] `TestAnthropicTransformer_Temperature` — temperature included when non-nil

### 2.3 Extract OpenAIChatTransformer

**File:** `internal/provider/openai/transformer.go` (new)

Extract `buildRequestBody`, `convertMessages`, `convertAssistantMessage`, `convertUserMessages`, all API types (`apiRequest`, `apiMessage`, `apiTool`, `apiFunction`, `apiToolCall`, `apiCallFunc`) from `provider.go` into a `Transformer` struct.

#### Tests

- [ ] `TestOpenAITransformer_BasicRequest` — model, messages, stream:true
- [ ] `TestOpenAITransformer_SystemAsMessage` — system prompt becomes role:"system" message at index 0
- [ ] `TestOpenAITransformer_Tools` — tools wrapped in {type:"function", function:{...}} format
- [ ] `TestOpenAITransformer_ToolsSorted` — tools sorted alphabetically
- [ ] `TestOpenAITransformer_AssistantToolCalls` — tool_use blocks become tool_calls array
- [ ] `TestOpenAITransformer_UserToolResults` — tool_result blocks become separate role:"tool" messages
- [ ] `TestOpenAITransformer_MixedUserContent` — text + tool_result in same message handled correctly

### 2.4 Deduplicate Z.ai via OpenAIChatTransformer

**File:** `internal/provider/zai/provider.go`

Z.ai is OpenAI-compatible. Replace the entire `buildRequestBody` / `convertMessages` / `convertAssistantMessage` / `convertUserMessages` block with the `openai.Transformer`. The Z.ai provider becomes:
- HTTP transport (auth headers, endpoint URL)
- SSE stream processing (delegate to shared code, see Phase 2.5)
- Default model resolution

Delete: ~180 lines of duplicated message conversion code.

#### Tests

- [ ] `TestZaiProvider_UsesOpenAITransformer` — request body matches OpenAI format
- [ ] Existing Z.ai tests continue to pass (regression)

### 2.5 Extract shared OpenAI-compatible SSE processor

**File:** `internal/provider/ssecompat/processor.go` (new)

Extract the common SSE `data: ` line parsing, `[DONE]` detection, `chatChunk` parsing, and `toolCallAccumulator` into a shared package. This code is currently duplicated verbatim in `openai/provider.go` and `zai/provider.go`.

```go
package ssecompat

// ProcessSSE reads OpenAI-compatible SSE lines from a reader and sends
// StreamEvents to the channel. Handles text deltas, tool call accumulation,
// and [DONE] detection.
func ProcessSSE(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent)
```

Also extract `toolCallAccumulator` into this package.

#### Tests

- [ ] `TestProcessSSE_TextDelta` — text content emitted as text_delta events
- [ ] `TestProcessSSE_ToolCallAccumulation` — fragmented tool calls accumulated and flushed
- [ ] `TestProcessSSE_Done` — [DONE] emits stop event
- [ ] `TestProcessSSE_ParseError` — malformed JSON emits error event
- [ ] `TestProcessSSE_ContextCancellation` — context cancel stops processing
- [ ] `TestToolCallAccumulator_MultipleIndexes` — interleaved tool calls tracked by index

### 2.6 Migrate OpenAI and Z.ai to shared SSE processor

**Files:** `openai/provider.go`, `zai/provider.go`

Replace `processStream` in both with `ssecompat.ProcessSSE`. Delete duplicated `toolCallAccumulator` from both files.

#### Tests

- [ ] Existing OpenAI streaming tests pass
- [ ] Existing Z.ai streaming tests pass

---

## Phase 3: Message Normalization (STRUCTURAL then BEHAVIORAL)

**Why third:** Now that transformers are extracted, normalization can be applied as a pre-processing step before `ToProviderJSON`, shared across all providers.

### 3.1 Create normalization module

**File:** `internal/provider/normalize/normalize.go` (new)

Functions:
- `RemoveEmptyMessages(msgs []Message) []Message` — filters empty text/thinking blocks, removes messages with no content
- `ScrubToolIDs(msgs []Message, scrub func(string) string) []Message` — applies a scrubbing function to all tool_use IDs and tool_result tool_use_ids
- `ScrubAnthropicToolID(id string) string` — replaces non-alphanumeric chars (except `_` and `-`) with `_`
- `InsertAssistantBetweenToolAndUser(msgs []Message) []Message` — for providers that require an assistant message between tool results and the next user message

#### Tests

- [ ] `TestRemoveEmptyMessages_EmptyText` — empty text messages removed
- [ ] `TestRemoveEmptyMessages_EmptyThinking` — empty thinking blocks removed
- [ ] `TestRemoveEmptyMessages_PreservesToolUse` — tool_use blocks never removed
- [ ] `TestRemoveEmptyMessages_MixedBlocks` — message with some empty and some non-empty blocks keeps non-empty
- [ ] `TestScrubAnthropicToolID` — "call:1/abc" → "call_1_abc"
- [ ] `TestScrubToolIDs_BothDirections` — scrubs both tool_use.id and tool_result.tool_use_id
- [ ] `TestInsertAssistantBetweenToolAndUser` — inserts filler assistant message when tool result is followed by user

### 3.2 Integrate normalization into Anthropic transformer

Apply `RemoveEmptyMessages` and `ScrubAnthropicToolID` in `AnthropicTransformer.ToProviderJSON` before serialization. Remove the existing empty-text-block skip logic from `convertContentBlocks`.

#### Tests

- [ ] `TestAnthropicTransformer_NormalizesEmptyBlocks` — empty blocks filtered before serialization
- [ ] `TestAnthropicTransformer_ScrubsToolIDs` — special chars in tool IDs replaced

### 3.3 Integrate normalization into OpenAI transformer (optional quirks)

Add a `Quirks` config to `openai.Transformer`:

```go
type Quirks struct {
    // MaxToolIDLength truncates tool IDs to this length (0 = no limit).
    MaxToolIDLength int
    // AlphanumericToolIDs restricts tool IDs to [a-zA-Z0-9_-].
    AlphanumericToolIDs bool
    // InsertAssistantAfterTool inserts a filler assistant message between
    // tool results and user messages for providers that require it.
    InsertAssistantAfterTool bool
}
```

#### Tests

- [ ] `TestOpenAITransformer_Quirks_MaxToolIDLength` — IDs truncated
- [ ] `TestOpenAITransformer_Quirks_AlphanumericToolIDs` — special chars scrubbed
- [ ] `TestOpenAITransformer_Quirks_InsertAssistant` — filler message inserted

---

## Phase 4: Richer StreamEvent (BEHAVIORAL)

**Why last:** This is the most visible change — it touches the `StreamEvent` type in `pkg/agentsdk`, the agent loop's `consumeStream`, and all four providers. The structural groundwork from Phases 1–3 makes this much safer.

### 4.1 Add new StreamEvent types

**File:** `pkg/agentsdk/types.go`

Add new event type constants:

```go
const (
    EventTextDelta      = "text_delta"
    EventThinkingDelta  = "thinking_delta"
    EventInputJsonDelta = "input_json_delta"  // NEW
    EventToolUse        = "tool_use"
    EventMessageStart   = "message_start"     // NEW
    EventStop           = "stop"
    EventError          = "error"
)
```

Extend `StreamEvent`:

```go
type StreamEvent struct {
    Type         string
    Text         string
    ToolUse      *ToolUseBlock
    Error        error
    InputTokens  int
    OutputTokens int
    Model        string // NEW: populated on message_start
    MessageID    string // NEW: populated on message_start
}
```

#### Tests

- [ ] `TestStreamEvent_NewTypes` — new constants exist and are distinct
- [ ] `TestStreamEvent_ModelField` — Model field serializes/deserializes

### 4.2 Emit input_json_delta from Anthropic provider

**File:** `internal/provider/anthropic/provider.go` (or the SSE handler)

Change `handleContentBlockDelta` to emit `input_json_delta` for `input_json_delta` SSE events instead of converting to `text_delta`.

Change `convertSSEEvent` to handle `message_start` SSE events and emit `StreamEvent{Type: "message_start", ...}` with model/id/usage from the event data.

#### Tests

- [ ] `TestAnthropicSSE_InputJsonDelta` — input_json_delta SSE event → StreamEvent type "input_json_delta"
- [ ] `TestAnthropicSSE_MessageStart` — message_start SSE event → StreamEvent with model and message ID

### 4.3 Emit message_start from OpenAI-compatible SSE

**File:** `internal/provider/ssecompat/processor.go`

Emit a synthetic `message_start` event from the first chunk that contains a model field. OpenAI SSE chunks include `"model": "..."` in every chunk; use the first one.

#### Tests

- [ ] `TestProcessSSE_MessageStart` — first chunk with model field emits message_start

### 4.4 Update agent loop to handle new events

**File:** `pkg/agentsdk/agent.go`

In `consumeStream`:
- `input_json_delta` → accumulate into `toolInputBuf` (same as current text_delta behavior during tool accumulation), AND forward as `TurnEvent{Type: "input_json_delta", Text: event.Text}` for TUI rendering
- `message_start` → forward as `TurnEvent{Type: "message_start", Model: event.Model}`
- Keep existing `text_delta` behavior unchanged

#### Tests

- [ ] `TestAgent_ConsumeStream_InputJsonDelta_AccumulatesTool` — input_json_delta events accumulate into tool input buffer
- [ ] `TestAgent_ConsumeStream_InputJsonDelta_ForwardedToTUI` — TurnEvent with type "input_json_delta" emitted
- [ ] `TestAgent_ConsumeStream_MessageStart` — message_start forwarded as TurnEvent

### 4.5 Add TurnEvent types for new events

**File:** `pkg/agentsdk/turn_event.go` (or wherever TurnEvent is defined)

Add new TurnEvent fields/types:
- `Type: "input_json_delta"` with `Text` field (partial JSON for tool arguments)
- `Type: "message_start"` with `Model` and `MessageID` fields

#### Tests

- [ ] `TestTurnEvent_InputJsonDelta` — type and text field populated
- [ ] `TestTurnEvent_MessageStart` — type, model, and message ID populated

---

## Dependency Graph

```
Phase 1: ProviderError
   │
   ├── 1.1 Define type ──→ 1.2 Migrate FormatAPIError ──→ 1.3 Update retry
   │                                                          │
   │                                                          ├── 1.4 Update providers
   │                                                          │
   │                                                          └── 1.5 Update agent loop
   │
Phase 2: MessageTransformer (depends on 1.2 for error types)
   │
   ├── 2.1 Define interface
   ├── 2.2 Anthropic transformer ──→ 2.3 OpenAI transformer
   │                                      │
   │                                      ├── 2.4 Z.ai dedup
   │                                      │
   │                                      └── 2.5 Shared SSE ──→ 2.6 Migrate SSE
   │
Phase 3: Normalization (depends on 2.2, 2.3)
   │
   ├── 3.1 Normalization module ──→ 3.2 Anthropic integration
   │                              ──→ 3.3 OpenAI quirks integration
   │
Phase 4: Richer StreamEvent (depends on 2.5 for SSE, 1.5 for agent)
   │
   ├── 4.1 New types ──→ 4.2 Anthropic emits ──→ 4.4 Agent handles
   │                  ──→ 4.3 OpenAI emits    ──→ 4.5 TurnEvent types
```

---

## File Change Summary

| Phase | New Files | Modified Files | Deleted Lines (approx) |
|-------|-----------|----------------|----------------------|
| 1 | `provider_error.go`, `provider_error_test.go` | `apierror.go`, `retry.go`, `anthropic/provider.go`, `openai/provider.go`, `ollama/provider.go`, `zai/provider.go`, `agent.go` | ~0 (additive) |
| 2 | `transformer.go`, `anthropic/transformer.go`, `openai/transformer.go`, `ssecompat/processor.go` + tests | `anthropic/provider.go`, `openai/provider.go`, `zai/provider.go` | ~350 (dedup) |
| 3 | `normalize/normalize.go` + tests | `anthropic/transformer.go`, `openai/transformer.go` | ~20 (inline skip logic) |
| 4 | — | `types.go`, `agent.go`, `anthropic/provider.go`, `ssecompat/processor.go` | ~0 (additive) |

**Total estimated new lines:** ~800 code + ~1200 tests
**Total estimated deleted lines:** ~370 (duplication removed)
**Net complexity change:** Negative (less duplication, clearer boundaries)

---

## Commit Strategy

Each numbered subsection (1.1, 1.2, ...) is one commit. Structural changes get `[STRUCTURAL]` prefix, behavioral changes get `[BEHAVIORAL]` prefix. All tests must pass at every commit boundary.

Example sequence:
```
[BEHAVIORAL] Add ProviderError type with ErrorKind variants
[BEHAVIORAL] Add ClassifyAPIError returning typed ProviderError
[STRUCTURAL] Update DoWithRetry to use ProviderError.IsRetryable
[STRUCTURAL] Migrate anthropic/openai/ollama/zai to ClassifyAPIError
[BEHAVIORAL] Agent loop emits context_overflow TurnEvent on ContextOverflow
[STRUCTURAL] Define MessageTransformer interface
[STRUCTURAL] Extract AnthropicTransformer from anthropic/provider.go
[STRUCTURAL] Extract OpenAIChatTransformer from openai/provider.go
[STRUCTURAL] Deduplicate zai provider via OpenAIChatTransformer
[STRUCTURAL] Extract shared SSE processor from openai/zai
[STRUCTURAL] Migrate openai and zai to shared SSE processor
[BEHAVIORAL] Add message normalization module
[STRUCTURAL] Integrate normalization into Anthropic transformer
[STRUCTURAL] Add Quirks config to OpenAI transformer
[BEHAVIORAL] Add input_json_delta and message_start StreamEvent types
[BEHAVIORAL] Anthropic provider emits input_json_delta and message_start
[BEHAVIORAL] Shared SSE processor emits message_start
[BEHAVIORAL] Agent loop handles input_json_delta and message_start events
```
