# Z.ai Provider Design Specification

**Date:** 2026-04-04  
**Status:** Design Phase  
**Author:** Claude Code

---

## Overview

This specification defines the implementation of a dedicated Z.ai (Zhipu AI) provider for Rubichan, following the established pattern of Anthropic, OpenAI, and Ollama providers. The provider enables the agent to use GLM-series language models from Z.ai's API.

Z.ai is an OpenAI-compatible API provided by Zhipu AI at `https://api.z.ai/api/paas/v4/chat/completions` with support for streaming, tool use, and multiple GLM models (GLM-4, GLM-4V, GLM-5, etc.).

---

## Design Decisions

### Approach: Dedicated Provider vs. OpenAI-Compatible Config

**Decision:** Implement as a dedicated provider in `internal/provider/zai/`.

**Rationale:**
- Consistency with existing Anthropic/OpenAI/Ollama providers
- First-class support for z.ai in factory and config
- Room for z.ai-specific features (model validation, rate limiting) in the future
- Cleaner mental model for users: "z.ai is a built-in provider like Anthropic"

### Model Selection Strategy

**Decision:** Global config-based default model with support for multiple GLM models.

**Rationale:**
- Simple and predictable: users configure model once in `config.toml`
- Matches existing Anthropic/OpenAI/Ollama patterns (model chosen at startup)
- No per-request complexity

**Supported models:** GLM-4, GLM-4V (vision), GLM-5, and any future GLM variants (no hard-coded allowlist)

### Request/Response Format

**Decision:** Reuse OpenAI-compatible request/response serialization.

**Rationale:**
- Z.ai's API is fully OpenAI-compatible
- Avoids code duplication
- Simplifies maintenance and testing

**Shared types from OpenAI provider:**
- `apiRequest`, `apiMessage`, `apiTool`, `apiFunction` (request)
- `chatChunk`, `chunkChoice`, `chunkDelta` (SSE response)
- `apiToolCall`, `apiCallFunc` (tool use blocks)

---

## Architecture

### Directory Structure

```
internal/provider/zai/
├── provider.go          # Main Provider implementation
├── provider_test.go     # Unit tests (>90% coverage)
```

### Provider Struct

```go
type Provider struct {
    baseURL      string
    apiKey       string
    model        string              // default GLM model from config
    client       *http.Client
    extraHeaders map[string]string   // for custom headers if needed
}
```

### Interface Implementation

The Provider implements `agentsdk.LLMProvider` with two methods:

```go
// Complete makes a non-streaming completion request to Z.ai API
func (p *Provider) Complete(ctx context.Context, req *CompletionRequest) (*Response, error)

// Stream makes a streaming completion request to Z.ai API
func (p *Provider) Stream(ctx context.Context, req *CompletionRequest) (chan StreamEvent, error)
```

**Behavior:**
- Both methods use the `req.Model` field if set; otherwise fall back to `p.model` (config default)
- HTTP client is reused from `internal/provider` package (shared connection pooling, timeouts)
- SSE parsing reuses OpenAI's implementation
- Errors are wrapped with context and returned as `StreamEvent` with error flag set

### API Request Shape

Z.ai accepts OpenAI-compatible requests:

```json
{
  "model": "glm-5",
  "messages": [
    {
      "role": "user",
      "content": "..."
    }
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "tool_name",
        "description": "...",
        "parameters": {...}
      }
    }
  ],
  "max_tokens": 4096,
  "temperature": 0.7,
  "stream": true
}
```

### API Response Shape (Streaming)

Z.ai streams Server-Sent Events (SSE) in OpenAI format:

```
data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}
data: {"choices":[{"delta":{"content":" there"},"finish_reason":null}]}
data: [DONE]
```

Tool use blocks are returned in the same format as OpenAI.

---

## Configuration

### Config Structure

Add to `internal/config/config.go`:

```toml
[provider.zai]
api_key_source = "env"      # "env", "file", or inline
api_key = ""                # only if api_key_source = "env" (inline)
base_url = "https://api.z.ai/api/paas/v4"  # optional override
model = "glm-5"             # default model
```

### ProviderConfig Struct Extension

```go
type ProviderConfig struct {
    Default   string                   // "anthropic", "openai", "ollama", "zai"
    Model     string
    Anthropic AnthropicProviderConfig
    OpenAI    []OpenAICompatibleConfig
    Ollama    OllamaProviderConfig
    Zai       ZaiProviderConfig        // NEW
}

type ZaiProviderConfig struct {
    APIKeySource string `toml:"api_key_source"`
    APIKey       string `toml:"api_key"`
    BaseURL      string `toml:"base_url"`
    Model        string `toml:"model"`
}
```

### API Key Resolution

- If `api_key_source = "env"`: read from `Z_AI_API_KEY` environment variable
- If `api_key_source = "file"`: read from file path specified in `api_key` field
- Otherwise: use `api_key` field inline (not recommended for production)

**Error handling:** Factory returns error if API key cannot be resolved.

---

## Factory Integration

### Factory Logic (`internal/provider/factory.go`)

Add a new factory function:

```go
func newZaiProvider(cfg *config.Config) (LLMProvider, error) {
    // 1. Resolve API key
    apiKey, err := config.ResolveAPIKey(
        cfg.Provider.Zai.APIKeySource,
        cfg.Provider.Zai.APIKey,
        "Z_AI_API_KEY",
    )
    if err != nil {
        return nil, fmt.Errorf("resolving Z.ai API key: %w", err)
    }

    // 2. Use base URL from config or default
    baseURL := cfg.Provider.Zai.BaseURL
    if baseURL == "" {
        baseURL = "https://api.z.ai/api/paas/v4"
    }

    // 3. Create provider with default model
    p := New(baseURL, apiKey, cfg.Provider.Zai.Model, nil)
    return p, nil
}
```

### Provider Factory Registration

In `internal/provider/zai/provider.go`, add an `init()` function:

```go
func init() {
    provider.RegisterProvider("zai", func(baseURL, apiKey string, extraHeaders map[string]string) provider.LLMProvider {
        return New(baseURL, apiKey, "glm-5", extraHeaders)
    })
}
```

### Factory Router

Update `internal/provider/factory.go` `NewProvider()` to handle z.ai:

```go
func NewProvider(cfg *config.Config) (LLMProvider, error) {
    switch cfg.Provider.Default {
    case "anthropic":
        return newAnthropicProvider(cfg)
    case "ollama":
        return newOllamaProvider(cfg)
    case "zai":
        return newZaiProvider(cfg)    // NEW
    default:
        return newOpenAIProvider(cfg)
    }
}
```

---

## Implementation Details

### HTTP Client

Reuse `provider.NewHTTPClient()` from `internal/provider/httpclient.go` for:
- Connection pooling
- Timeout management (default 30s)
- TLS verification

### Streaming (SSE Parsing)

Reuse OpenAI provider's SSE parsing logic from `internal/provider/openai/provider.go`:
- Read SSE chunks line-by-line
- Parse JSON delta objects
- Handle `[DONE]` sentinel
- Emit `StreamEvent` for each chunk

### Error Handling

**HTTP-level errors:**
- 401 Unauthorized → "invalid API key"
- 429 Too Many Requests → "rate limited"
- 5xx Server errors → "Z.ai service error"

**Response parsing errors:**
- Malformed JSON → log warning, skip chunk, continue
- Missing required fields → return error event

**Example:**

```go
resp, err := p.client.Do(req)
if err != nil {
    return nil, fmt.Errorf("Z.ai request failed: %w", err)
}
if resp.StatusCode == 401 {
    return nil, fmt.Errorf("Z.ai API key invalid or expired")
}
if resp.StatusCode >= 500 {
    return nil, fmt.Errorf("Z.ai service error: %d", resp.StatusCode)
}
```

### Tool Use Support

Z.ai supports tool use (function calling) in the same format as OpenAI:

```go
// Request includes tools array
apiReq := &apiRequest{
    Model: p.model,
    Tools: convertTools(req.Tools),  // reuse OpenAI's conversion
}

// Response includes tool_calls in delta
// SSE chunks deliver tool_calls just like OpenAI
```

No special handling needed; existing code works as-is.

---

## Testing Strategy

### Test Coverage Target: >90%

Tests in `internal/provider/zai/provider_test.go`:

#### Construction & Setup (3 tests)
- `TestNew` — creates provider with correct fields
- `TestNewWithNilExtraHeaders` — handles nil headers gracefully
- `TestSetHTTPClient` — client can be replaced for testing

#### Complete (Non-streaming) (4 tests)
- `TestCompletionSuccess` — successful request/response
- `TestCompletionWithToolUse` — tool calls in response
- `TestCompletionAPIKeyError` — 401 error handling
- `TestCompletionServerError` — 5xx error handling

#### Streaming (6 tests)
- `TestStreamSuccess` — stream opens, chunks parse, emits events
- `TestStreamWithToolUse` — tool use blocks stream correctly
- `TestStreamDone` — [DONE] sentinel closes channel
- `TestStreamNetworkError` — error during streaming
- `TestStreamMalformedJSON` — skips invalid chunks, continues
- `TestStreamContextCancellation` — respects context.Done()

#### Model Selection (2 tests)
- `TestCompletionUsesDefaultModel` — falls back to config model
- `TestCompletionUsesRequestModel` — uses req.Model if set

#### Message & Tool Conversion (3 tests)
- `TestConvertMessagesUserMessage` — user message serializes
- `TestConvertMessagesToolResult` — tool result serializes
- `TestConvertToolsFunction` — tools array serializes with correct schema

#### Edge Cases (2 tests)
- `TestEmptyStreamResponse` — handles empty response gracefully
- `TestConcurrentRequests` — concurrent requests don't race

**Total: ~20 focused, non-overlapping tests**

---

## Success Criteria

✅ Provider implements `agentsdk.LLMProvider` interface completely  
✅ Config supports `[provider.zai]` with model selection  
✅ Factory routes `default = "zai"` correctly  
✅ Streaming works end-to-end with SSE parsing  
✅ Tool use / function calling works  
✅ All tests pass  
✅ Test coverage >90%  
✅ No linter warnings (`golangci-lint run ./internal/provider/zai`)  
✅ Code properly formatted (`gofmt`)  
✅ No breaking changes to existing providers  

---

## Rollout Plan

1. **Phase 1:** Implement core provider + tests (Red → Green → Refactor)
2. **Phase 2:** Integrate into factory and config
3. **Phase 3:** Add documentation (README, example config)
4. **Phase 4:** Commit and create PR to main

---

## Future Extensions (Not in Scope)

- Vision model support (GLM-4V) with image embeddings
- Streaming response with vision image input
- Z.ai-specific rate limiting or quota management
- Custom Z.ai headers for billing/project tracking
- Vision-capable tool calling

These can be added as follow-up PRs once the base provider is merged.

---

## Appendix: Example Configuration

```toml
[provider]
default = "zai"
model = "glm-5"

[provider.zai]
api_key_source = "env"
base_url = "https://api.z.ai/api/paas/v4"
model = "glm-5"
```

User sets environment variable:
```bash
export Z_AI_API_KEY="your-api-key-here"
```

Then uses agent:
```bash
rubichan              # Uses Z.ai with glm-5
```
