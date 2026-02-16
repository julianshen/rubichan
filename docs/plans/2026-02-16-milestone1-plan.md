# Milestone 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a working interactive CLI agent (`rubichan`) that supports multi-turn LLM conversations with file and shell tools.

**Architecture:** Bottom-up build across 6 layers: Config → Provider → Tools → Agent Core → TUI → CLI. Each layer is independently testable. See `docs/plans/2026-02-16-milestone1-design.md` for full design.

**Tech Stack:** Go, Cobra, Bubble Tea, Lipgloss, Glamour, BurntSushi/toml, tiktoken-go, zalando/go-keyring, testify

---

## Task 0: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `cmd/rubichan/main.go` (minimal placeholder)
- Create: `internal/config/.gitkeep` (placeholder)
- Create: `internal/provider/.gitkeep` (placeholder)
- Create: `internal/tools/.gitkeep` (placeholder)
- Create: `internal/agent/.gitkeep` (placeholder)
- Create: `internal/tui/.gitkeep` (placeholder)

**Step 1: Initialize Go module**

Run: `go mod init github.com/julianshen/rubichan`
Expected: `go.mod` created

**Step 2: Create directory structure**

Run:
```bash
mkdir -p cmd/rubichan internal/config internal/provider/anthropic internal/provider/openai internal/tools internal/agent internal/tui
```

**Step 3: Create minimal main.go**

```go
// cmd/rubichan/main.go
package main

import "fmt"

func main() {
	fmt.Println("rubichan")
}
```

**Step 4: Verify it compiles**

Run: `go build ./cmd/rubichan`
Expected: Binary created, no errors

**Step 5: Commit**

```
[STRUCTURAL] Scaffold project structure with Go module and directory layout
```

---

## Task 1: Config Types and Defaults

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write failing test for default config**

```go
// internal/config/config_test.go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Provider.Model)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
	assert.Equal(t, "prompt", cfg.Agent.ApprovalMode)
	assert.Equal(t, 100000, cfg.Agent.ContextBudget)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestDefaultConfig -v`
Expected: FAIL — `DefaultConfig` not defined

**Step 3: Write minimal implementation**

```go
// internal/config/config.go
package config

// Config holds all application configuration.
type Config struct {
	Provider ProviderConfig `toml:"provider"`
	Agent    AgentConfig    `toml:"agent"`
}

// ProviderConfig holds LLM provider settings.
type ProviderConfig struct {
	Default   string                   `toml:"default"`
	Model     string                   `toml:"model"`
	Anthropic AnthropicProviderConfig  `toml:"anthropic"`
	OpenAI    []OpenAICompatibleConfig `toml:"openai_compatible"`
}

// AnthropicProviderConfig holds Anthropic-specific settings.
type AnthropicProviderConfig struct {
	APIKeySource string `toml:"api_key_source"`
	APIKey       string `toml:"api_key"`
}

// OpenAICompatibleConfig holds settings for OpenAI-compatible providers.
type OpenAICompatibleConfig struct {
	Name         string            `toml:"name"`
	BaseURL      string            `toml:"base_url"`
	APIKeySource string            `toml:"api_key_source"`
	APIKey       string            `toml:"api_key"`
	ExtraHeaders map[string]string `toml:"extra_headers"`
}

// AgentConfig holds agent behavior settings.
type AgentConfig struct {
	MaxTurns      int    `toml:"max_turns"`
	ApprovalMode  string `toml:"approval_mode"`
	ContextBudget int    `toml:"context_budget"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider: ProviderConfig{
			Default: "anthropic",
			Model:   "claude-sonnet-4-5",
			Anthropic: AnthropicProviderConfig{
				APIKeySource: "env",
			},
		},
		Agent: AgentConfig{
			MaxTurns:      50,
			ApprovalMode:  "prompt",
			ContextBudget: 100000,
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestDefaultConfig -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add config types with default values
```

---

## Task 2: Config TOML Loading

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write failing test for loading TOML config**

```go
func TestLoadFromFile(t *testing.T) {
	tomlContent := `
[provider]
default = "openai"
model = "gpt-4o"

[provider.anthropic]
api_key_source = "keyring"

[agent]
max_turns = 30
approval_mode = "auto"
context_budget = 50000
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.Provider.Default)
	assert.Equal(t, "gpt-4o", cfg.Provider.Model)
	assert.Equal(t, "keyring", cfg.Provider.Anthropic.APIKeySource)
	assert.Equal(t, 30, cfg.Agent.MaxTurns)
	assert.Equal(t, "auto", cfg.Agent.ApprovalMode)
	assert.Equal(t, 50000, cfg.Agent.ContextBudget)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadFromFile -v`
Expected: FAIL — `Load` not defined

**Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Load reads config from a TOML file, applying defaults for missing values.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}
```

**Step 4: Install dependency and run test**

Run: `go get github.com/BurntSushi/toml && go test ./internal/config/ -run TestLoadFromFile -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add TOML config file loading with defaults
```

---

## Task 3: Config Missing File Fallback

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

**Step 1: Write failing test for missing config file**

```go
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err)

	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadMissingFileReturnsDefaults -v`
Expected: FAIL — returns error for missing file

**Step 3: Update Load to return defaults when file is missing**

Update `Load` in `internal/config/config.go`:

```go
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Return default config when config file is missing
```

---

## Task 4: Config OpenAI-Compatible Provider Entries

**Files:**
- Modify: `internal/config/config_test.go`

**Step 1: Write failing test for OpenAI-compatible entries including OpenRouter**

```go
func TestLoadOpenAICompatibleProviders(t *testing.T) {
	tomlContent := `
[provider]
default = "openrouter"
model = "anthropic/claude-sonnet-4-5"

[[provider.openai_compatible]]
name = "openai"
base_url = "https://api.openai.com/v1"
api_key_source = "env"

[[provider.openai_compatible]]
name = "openrouter"
base_url = "https://openrouter.ai/api/v1"
api_key_source = "env"

[provider.openai_compatible.extra_headers]
HTTP-Referer = "https://github.com/user/rubichan"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)

	assert.Equal(t, "openrouter", cfg.Provider.Default)
	require.Len(t, cfg.Provider.OpenAI, 2)

	assert.Equal(t, "openai", cfg.Provider.OpenAI[0].Name)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Provider.OpenAI[0].BaseURL)

	assert.Equal(t, "openrouter", cfg.Provider.OpenAI[1].Name)
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.Provider.OpenAI[1].BaseURL)
	assert.Equal(t, "https://github.com/user/rubichan", cfg.Provider.OpenAI[1].ExtraHeaders["HTTP-Referer"])
}
```

**Step 2: Run test to verify it passes (should work with existing types)**

Run: `go test ./internal/config/ -run TestLoadOpenAICompatibleProviders -v`
Expected: PASS (the TOML struct tags and types already support this)

If it fails, adjust the struct tags or types to match the TOML structure.

**Step 3: Commit**

```
[BEHAVIORAL] Verify OpenAI-compatible provider config with OpenRouter support
```

---

## Task 5: API Key Resolution

**Files:**
- Create: `internal/config/apikey.go`
- Create: `internal/config/apikey_test.go`

**Step 1: Write failing test for env-based key resolution**

```go
// internal/config/apikey_test.go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-12345")

	key, err := ResolveAPIKey("env", "", "TEST_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-12345", key)
}

func TestResolveAPIKeyFromConfig(t *testing.T) {
	key, err := ResolveAPIKey("config", "sk-from-config", "")
	require.NoError(t, err)
	assert.Equal(t, "sk-from-config", key)
}

func TestResolveAPIKeyMissingEnvVar(t *testing.T) {
	_, err := ResolveAPIKey("env", "", "NONEXISTENT_KEY_VAR")
	assert.Error(t, err)
}

func TestResolveAPIKeyEmptyConfig(t *testing.T) {
	_, err := ResolveAPIKey("config", "", "")
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestResolveAPIKey -v`
Expected: FAIL — `ResolveAPIKey` not defined

**Step 3: Write minimal implementation**

```go
// internal/config/apikey.go
package config

import (
	"fmt"
	"os"
)

// ResolveAPIKey resolves an API key from the specified source.
// Priority: keyring > env > config value.
func ResolveAPIKey(source, configValue, envVar string) (string, error) {
	switch source {
	case "keyring":
		// Keyring support deferred — fall through to env
		return resolveFromEnv(envVar)
	case "env":
		return resolveFromEnv(envVar)
	case "config":
		if configValue == "" {
			return "", fmt.Errorf("api_key_source is 'config' but no api_key value provided")
		}
		return configValue, nil
	default:
		return "", fmt.Errorf("unknown api_key_source: %q", source)
	}
}

func resolveFromEnv(envVar string) (string, error) {
	if envVar == "" {
		return "", fmt.Errorf("no environment variable name specified")
	}
	val := os.Getenv(envVar)
	if val == "" {
		return "", fmt.Errorf("environment variable %s is not set", envVar)
	}
	return val, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add API key resolution from env and config sources
```

---

## Task 6: Provider Interface and Types

**Files:**
- Create: `internal/provider/types.go`
- Create: `internal/provider/types_test.go`

**Step 1: Write failing test for message construction**

```go
// internal/provider/types_test.go
package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTextMessage(t *testing.T) {
	msg := NewUserMessage("hello world")

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "text", msg.Content[0].Type)
	assert.Equal(t, "hello world", msg.Content[0].Text)
}

func TestNewToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("tool-123", "file contents here", false)

	assert.Equal(t, "user", msg.Role)
	require.Len(t, msg.Content, 1)
	assert.Equal(t, "tool_result", msg.Content[0].Type)
	assert.Equal(t, "tool-123", msg.Content[0].ToolUseID)
	assert.Equal(t, "file contents here", msg.Content[0].Text)
	assert.False(t, msg.Content[0].IsError)
}

func TestToolDefJSON(t *testing.T) {
	td := ToolDef{
		Name:        "file",
		Description: "Read and write files",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}

	data, err := json.Marshal(td)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"name":"file"`)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ -run Test -v`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

```go
// internal/provider/types.go
package provider

import (
	"context"
	"encoding/json"
)

// LLMProvider streams completions from a language model.
type LLMProvider interface {
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}

// CompletionRequest holds parameters for an LLM completion.
type CompletionRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature,omitempty"`
}

// Message represents a conversation message.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a piece of content within a message.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ToolDef defines a tool the LLM can call.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolUseBlock holds a parsed tool call from the LLM.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// StreamEvent represents one event in a streaming LLM response.
type StreamEvent struct {
	Type    string
	Text    string
	ToolUse *ToolUseBlock
	Error   error
}

// NewUserMessage creates a text message from the user.
func NewUserMessage(text string) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

// NewToolResultMessage creates a tool result message.
func NewToolResultMessage(toolUseID, content string, isError bool) Message {
	return Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: toolUseID, Text: content, IsError: isError},
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add provider interface and message types
```

---

## Task 7: Anthropic Provider — SSE Parsing

**Files:**
- Create: `internal/provider/anthropic/sse.go`
- Create: `internal/provider/anthropic/sse_test.go`

**Step 1: Write failing test for SSE line parsing**

```go
// internal/provider/anthropic/sse_test.go
package anthropic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSSEEvents(t *testing.T) {
	input := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: message_stop
data: {"type":"message_stop"}

`
	events, err := collectSSEEvents(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, events, 3)

	assert.Equal(t, "content_block_delta", events[0].Event)
	assert.Contains(t, events[0].Data, "Hello")

	assert.Equal(t, "content_block_delta", events[1].Event)
	assert.Contains(t, events[1].Data, " world")

	assert.Equal(t, "message_stop", events[2].Event)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/anthropic/ -run TestParseSSEEvents -v`
Expected: FAIL — `collectSSEEvents` not defined

**Step 3: Write minimal SSE parser**

```go
// internal/provider/anthropic/sse.go
package anthropic

import (
	"bufio"
	"io"
	"strings"
)

type sseEvent struct {
	Event string
	Data  string
}

func collectSSEEvents(r io.Reader) ([]sseEvent, error) {
	var events []sseEvent
	scanner := bufio.NewScanner(r)

	var current sseEvent
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if current.Data != "" {
				events = append(events, current)
			}
			current = sseEvent{}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			current.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
		}
	}

	return events, scanner.Err()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/anthropic/ -run TestParseSSEEvents -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add SSE event parser for Anthropic streaming
```

---

## Task 8: Anthropic Provider — Stream Implementation

**Files:**
- Create: `internal/provider/anthropic/provider.go`
- Create: `internal/provider/anthropic/provider_test.go`

**Step 1: Write failing test using httptest server**

```go
// internal/provider/anthropic/provider_test.go
package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTextResponse(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_01","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: message_stop
data: {"type":"message_stop"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key")

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		System:    "You are helpful.",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should have: text "Hello", text " world", stop
	textEvents := filterByType(events, "text_delta")
	require.Len(t, textEvents, 2)
	assert.Equal(t, "Hello", textEvents[0].Text)
	assert.Equal(t, " world", textEvents[1].Text)

	stopEvents := filterByType(events, "stop")
	require.Len(t, stopEvents, 1)
}

func filterByType(events []provider.StreamEvent, typ string) []provider.StreamEvent {
	var filtered []provider.StreamEvent
	for _, e := range events {
		if e.Type == typ {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/anthropic/ -run TestStreamTextResponse -v`
Expected: FAIL — `New` not defined

**Step 3: Write the Anthropic provider**

```go
// internal/provider/anthropic/provider.go
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

const anthropicVersion = "2023-06-01"

// Provider implements the LLMProvider interface for Anthropic's Messages API.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates an Anthropic provider.
func New(baseURL, apiKey string) *Provider {
	return &Provider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		p.processStream(ctx, bufio.NewScanner(resp.Body), ch)
	}()

	return ch, nil
}

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Messages) > 0 {
		body["messages"] = req.Messages
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	return json.Marshal(body)
}

func (p *Provider) processStream(ctx context.Context, scanner *bufio.Scanner, ch chan<- provider.StreamEvent) {
	var currentEvent string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		ev := p.parseDataLine(currentEvent, data)
		if ev != nil {
			ch <- *ev
		}
	}
}

func (p *Provider) parseDataLine(eventType, data string) *provider.StreamEvent {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return &provider.StreamEvent{Type: "error", Error: err}
	}

	switch eventType {
	case "content_block_delta":
		var delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if d, ok := raw["delta"]; ok {
			json.Unmarshal(d, &delta)
		}
		if delta.Type == "text_delta" {
			return &provider.StreamEvent{Type: "text_delta", Text: delta.Text}
		}
		if delta.Type == "input_json_delta" {
			return &provider.StreamEvent{Type: "text_delta", Text: delta.Text}
		}

	case "content_block_start":
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if cb, ok := raw["content_block"]; ok {
			json.Unmarshal(cb, &block)
		}
		if block.Type == "tool_use" {
			return &provider.StreamEvent{
				Type: "tool_use",
				ToolUse: &provider.ToolUseBlock{
					ID:   block.ID,
					Name: block.Name,
				},
			}
		}

	case "message_stop":
		return &provider.StreamEvent{Type: "stop"}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/anthropic/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement Anthropic provider with SSE streaming
```

---

## Task 9: Anthropic Provider — Tool Use Streaming

**Files:**
- Modify: `internal/provider/anthropic/provider_test.go`

**Step 1: Write failing test for tool use response**

```go
func TestStreamToolUseResponse(t *testing.T) {
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_02","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","text":"{\"operation\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","text":"\"read\",\"path\":\"main.go\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key")
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("read main.go")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	toolEvents := filterByType(events, "tool_use")
	require.Len(t, toolEvents, 1)
	assert.Equal(t, "toolu_01", toolEvents[0].ToolUse.ID)
	assert.Equal(t, "file", toolEvents[0].ToolUse.Name)
}
```

**Step 2: Run test to verify it passes (should pass with existing implementation)**

Run: `go test ./internal/provider/anthropic/ -v`
Expected: PASS — tool_use parsing already handled in Task 8

If it fails, fix the `content_block_start` parsing for tool_use blocks.

**Step 3: Commit**

```
[BEHAVIORAL] Verify Anthropic provider handles tool use streaming
```

---

## Task 10: Anthropic Provider — Error Handling

**Files:**
- Modify: `internal/provider/anthropic/provider_test.go`

**Step 1: Write failing tests for error cases**

```go
func TestStreamAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	p := New(server.URL, "test-key")
	_, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestStreamContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write partial response then hang — context should cancel
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n"))
		w.(http.Flusher).Flush()
		// Block until request is cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := New(server.URL, "test-key")
	ch, err := p.Stream(ctx, provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	// Read first event
	ev := <-ch
	assert.Equal(t, "text_delta", ev.Type)

	// Cancel context
	cancel()

	// Should get error event and channel should close
	var gotError bool
	for ev := range ch {
		if ev.Type == "error" {
			gotError = true
		}
	}
	assert.True(t, gotError)
}
```

**Step 2: Run tests**

Run: `go test ./internal/provider/anthropic/ -v`
Expected: PASS

**Step 3: Commit**

```
[BEHAVIORAL] Verify Anthropic provider error handling and context cancellation
```

---

## Task 11: OpenAI Provider — Stream Implementation

**Files:**
- Create: `internal/provider/openai/provider.go`
- Create: `internal/provider/openai/provider_test.go`

**Step 1: Write failing test using httptest server**

```go
// internal/provider/openai/provider_test.go
package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamTextResponse(t *testing.T) {
	sseBody := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", nil)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "gpt-4o",
		System:    "You are helpful.",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	textEvents := filterByType(events, "text_delta")
	require.Len(t, textEvents, 2)
	assert.Equal(t, "Hello", textEvents[0].Text)
	assert.Equal(t, " world", textEvents[1].Text)

	stopEvents := filterByType(events, "stop")
	require.Len(t, stopEvents, 1)
}

func filterByType(events []provider.StreamEvent, typ string) []provider.StreamEvent {
	var filtered []provider.StreamEvent
	for _, e := range events {
		if e.Type == typ {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/openai/ -run TestStreamTextResponse -v`
Expected: FAIL — `New` not defined

**Step 3: Write the OpenAI provider**

```go
// internal/provider/openai/provider.go
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// Provider implements the LLMProvider interface for OpenAI-compatible APIs.
type Provider struct {
	baseURL      string
	apiKey       string
	extraHeaders map[string]string
	client       *http.Client
}

// New creates an OpenAI-compatible provider.
func New(baseURL, apiKey string, extraHeaders map[string]string) *Provider {
	return &Provider{
		baseURL:      baseURL,
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
		client:       &http.Client{},
	}
}

func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	for k, v := range p.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		p.processStream(ctx, bufio.NewScanner(resp.Body), ch)
	}()

	return ch, nil
}

func (p *Provider) buildRequestBody(req provider.CompletionRequest) ([]byte, error) {
	messages := p.convertMessages(req.System, req.Messages)

	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
		"messages":   messages,
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = p.convertTools(req.Tools)
	}
	return json.Marshal(body)
}

func (p *Provider) convertMessages(system string, messages []provider.Message) []map[string]any {
	var out []map[string]any

	if system != "" {
		out = append(out, map[string]any{
			"role":    "system",
			"content": system,
		})
	}

	for _, msg := range messages {
		converted := p.convertMessage(msg)
		out = append(out, converted...)
	}
	return out
}

func (p *Provider) convertMessage(msg provider.Message) []map[string]any {
	var out []map[string]any

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			out = append(out, map[string]any{
				"role":    msg.Role,
				"content": block.Text,
			})
		case "tool_use":
			out = append(out, map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{
					{
						"id":   block.ID,
						"type": "function",
						"function": map[string]any{
							"name":      block.Name,
							"arguments": string(block.Input),
						},
					},
				},
			})
		case "tool_result":
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": block.ToolUseID,
				"content":      block.Text,
			})
		}
	}
	return out
}

func (p *Provider) convertTools(tools []provider.ToolDef) []map[string]any {
	var out []map[string]any
	for _, t := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  json.RawMessage(t.InputSchema),
			},
		})
	}
	return out
}

func (p *Provider) processStream(ctx context.Context, scanner *bufio.Scanner, ch chan<- provider.StreamEvent) {
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.StreamEvent{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			ch <- provider.StreamEvent{Type: "stop"}
			return
		}

		ev := p.parseChunk(data)
		if ev != nil {
			ch <- *ev
		}
	}
}

func (p *Provider) parseChunk(data string) *provider.StreamEvent {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return &provider.StreamEvent{Type: "error", Error: err}
	}

	if len(chunk.Choices) == 0 {
		return nil
	}

	choice := chunk.Choices[0]

	if choice.Delta.Content != "" {
		return &provider.StreamEvent{Type: "text_delta", Text: choice.Delta.Content}
	}

	if len(choice.Delta.ToolCalls) > 0 {
		tc := choice.Delta.ToolCalls[0]
		if tc.ID != "" {
			return &provider.StreamEvent{
				Type: "tool_use",
				ToolUse: &provider.ToolUseBlock{
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				},
			}
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/openai/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement OpenAI-compatible provider with SSE streaming
```

---

## Task 12: OpenAI Provider — Tool Use and Extra Headers

**Files:**
- Modify: `internal/provider/openai/provider_test.go`

**Step 1: Write test for tool use streaming**

```go
func TestStreamToolCallResponse(t *testing.T) {
	sseBody := `data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"call_01","type":"function","function":{"name":"file","arguments":""}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"operation\":\"read\"}"}}]},"finish_reason":null}]}

data: {"id":"chatcmpl-2","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseBody))
	}))
	defer server.Close()

	p := New(server.URL, "test-key", nil)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []provider.Message{provider.NewUserMessage("read file")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	toolEvents := filterByType(events, "tool_use")
	require.Len(t, toolEvents, 1)
	assert.Equal(t, "call_01", toolEvents[0].ToolUse.ID)
	assert.Equal(t, "file", toolEvents[0].ToolUse.Name)
}

func TestExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "https://github.com/user/rubichan", r.Header.Get("HTTP-Referer"))
		assert.Equal(t, "rubichan", r.Header.Get("X-Title"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	headers := map[string]string{
		"HTTP-Referer": "https://github.com/user/rubichan",
		"X-Title":      "rubichan",
	}
	p := New(server.URL, "test-key", headers)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Model:     "anthropic/claude-sonnet-4-5",
		Messages:  []provider.Message{provider.NewUserMessage("Hi")},
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	var events []provider.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	assert.NotEmpty(t, events)
}
```

**Step 2: Run tests**

Run: `go test ./internal/provider/openai/ -v`
Expected: PASS

**Step 3: Commit**

```
[BEHAVIORAL] Verify OpenAI provider tool use and extra headers for OpenRouter
```

---

## Task 13: Provider Factory

**Files:**
- Create: `internal/provider/factory.go`
- Create: `internal/provider/factory_test.go`

**Step 1: Write failing test for provider creation**

```go
// internal/provider/factory_test.go
package provider

import (
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderAnthropic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "anthropic"
	cfg.Provider.Anthropic.APIKeySource = "config"
	cfg.Provider.Anthropic.APIKey = "sk-test"

	p, err := NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderOpenAI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openai"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{
			Name:         "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKeySource: "config",
			APIKey:       "sk-test",
		},
	}

	p, err := NewProvider(cfg)
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProviderUnknown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "unknown"

	_, err := NewProvider(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ -run TestNewProvider -v`
Expected: FAIL — `NewProvider` not defined

**Step 3: Write factory function**

```go
// internal/provider/factory.go
package provider

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider/anthropic"
	"github.com/julianshen/rubichan/internal/provider/openai"
)

// NewProvider creates an LLMProvider based on config.
func NewProvider(cfg *config.Config) (LLMProvider, error) {
	switch cfg.Provider.Default {
	case "anthropic":
		apiKey, err := config.ResolveAPIKey(
			cfg.Provider.Anthropic.APIKeySource,
			cfg.Provider.Anthropic.APIKey,
			"ANTHROPIC_API_KEY",
		)
		if err != nil {
			return nil, fmt.Errorf("resolving Anthropic API key: %w", err)
		}
		return anthropic.New("https://api.anthropic.com", apiKey), nil

	default:
		// Check OpenAI-compatible providers
		for _, oc := range cfg.Provider.OpenAI {
			if oc.Name == cfg.Provider.Default {
				envVar := fmt.Sprintf("%s_API_KEY", strings.ToUpper(strings.ReplaceAll(oc.Name, "-", "_")))
				apiKey, err := config.ResolveAPIKey(oc.APIKeySource, oc.APIKey, envVar)
				if err != nil {
					return nil, fmt.Errorf("resolving %s API key: %w", oc.Name, err)
				}
				return openai.New(oc.BaseURL, apiKey, oc.ExtraHeaders), nil
			}
		}
		return nil, fmt.Errorf("unknown provider: %q", cfg.Provider.Default)
	}
}
```

Note: Add `"strings"` to the imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add provider factory with Anthropic and OpenAI-compatible support
```

---

## Task 14: Tool Interface and Registry

**Files:**
- Create: `internal/tools/interface.go`
- Create: `internal/tools/registry.go`
- Create: `internal/tools/registry_test.go`

**Step 1: Write failing test for registry**

```go
// internal/tools/registry_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string          { return "A mock tool" }
func (m *mockTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	return ToolResult{Content: "ok"}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test"}

	err := r.Register(tool)
	require.NoError(t, err)

	got, ok := r.Get("test")
	assert.True(t, ok)
	assert.Equal(t, "test", got.Name())
}

func TestRegistryDuplicateReturnsError(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test"}

	require.NoError(t, r.Register(tool))
	err := r.Register(tool)
	assert.Error(t, err)
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&mockTool{name: "alpha"}))
	require.NoError(t, r.Register(&mockTool{name: "beta"}))

	defs := r.All()
	assert.Len(t, defs, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -v`
Expected: FAIL — types not defined

**Step 3: Write interface and registry**

```go
// internal/tools/interface.go
package tools

import (
	"context"
	"encoding/json"

	"github.com/julianshen/rubichan/internal/provider"
)

// Tool defines the interface for agent tools.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	Content string
	IsError bool
}
```

```go
// internal/tools/registry.go
package tools

import (
	"fmt"

	"github.com/julianshen/rubichan/internal/provider"
)

// Registry manages available tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Returns error if name is already registered.
func (r *Registry) Register(t Tool) error {
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tool already registered: %s", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns ToolDefs for all registered tools (for LLM requests).
func (r *Registry) All() []provider.ToolDef {
	var defs []provider.ToolDef
	for _, t := range r.tools {
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add tool interface and registry
```

---

## Task 15: File Tool

**Files:**
- Create: `internal/tools/file.go`
- Create: `internal/tools/file_test.go`

**Step 1: Write failing test for file read**

```go
// internal/tools/file_test.go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileToolReadFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644))

	tool := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "hello.txt",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "hello world", result.Content)
}

func TestFileToolWriteFile(t *testing.T) {
	dir := t.TempDir()
	tool := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "write",
		"path":      "new.txt",
		"content":   "new content",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new content", string(data))
}

func TestFileToolPatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "code.go"), []byte("func old() {}"), 0644))

	tool := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation":  "patch",
		"path":       "code.go",
		"old_string": "old",
		"new_string": "new",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	data, err := os.ReadFile(filepath.Join(dir, "code.go"))
	require.NoError(t, err)
	assert.Equal(t, "func new() {}", string(data))
}

func TestFileToolPathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "../../../etc/passwd",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "outside project root")
}

func TestFileToolReadMissing(t *testing.T) {
	dir := t.TempDir()
	tool := NewFileTool(dir)

	input, _ := json.Marshal(map[string]string{
		"operation": "read",
		"path":      "nonexistent.txt",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestFileTool -v`
Expected: FAIL — `NewFileTool` not defined

**Step 3: Write file tool implementation**

```go
// internal/tools/file.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileTool provides file read, write, and patch operations.
type FileTool struct {
	rootDir string
}

// NewFileTool creates a file tool rooted at the given directory.
func NewFileTool(rootDir string) *FileTool {
	return &FileTool{rootDir: rootDir}
}

func (f *FileTool) Name() string        { return "file" }
func (f *FileTool) Description() string { return "Read, write, and patch files" }
func (f *FileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["read", "write", "patch"]},
			"path": {"type": "string"},
			"content": {"type": "string"},
			"old_string": {"type": "string"},
			"new_string": {"type": "string"}
		},
		"required": ["operation", "path"]
	}`)
}

func (f *FileTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var params struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	fullPath, err := f.resolvePath(params.Path)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}

	switch params.Operation {
	case "read":
		return f.read(fullPath)
	case "write":
		return f.write(fullPath, params.Content)
	case "patch":
		return f.patch(fullPath, params.OldString, params.NewString)
	default:
		return ToolResult{Content: fmt.Sprintf("unknown operation: %s", params.Operation), IsError: true}, nil
	}
}

func (f *FileTool) resolvePath(path string) (string, error) {
	fullPath := filepath.Join(f.rootDir, path)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	absRoot, err := filepath.Abs(f.rootDir)
	if err != nil {
		return "", fmt.Errorf("resolving root: %w", err)
	}
	if !strings.HasPrefix(absPath, absRoot) {
		return "", fmt.Errorf("path %q is outside project root", path)
	}
	return absPath, nil
}

func (f *FileTool) read(path string) (ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error reading file: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: string(data)}, nil
}

func (f *FileTool) write(path, content string) (ToolResult, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{Content: fmt.Sprintf("error creating directory: %s", err), IsError: true}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ToolResult{Content: fmt.Sprintf("error writing file: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: fmt.Sprintf("wrote %d bytes to %s", len(content), filepath.Base(path))}, nil
}

func (f *FileTool) patch(path, oldStr, newStr string) (ToolResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("error reading file: %s", err), IsError: true}, nil
	}

	content := string(data)
	if !strings.Contains(content, oldStr) {
		return ToolResult{Content: "old_string not found in file", IsError: true}, nil
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return ToolResult{Content: fmt.Sprintf("error writing file: %s", err), IsError: true}, nil
	}
	return ToolResult{Content: "patch applied successfully"}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -run TestFileTool -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement file tool with read, write, patch, and path traversal prevention
```

---

## Task 16: Shell Tool

**Files:**
- Create: `internal/tools/shell.go`
- Create: `internal/tools/shell_test.go`

**Step 1: Write failing test for shell execution**

```go
// internal/tools/shell_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellToolExecute(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "echo hello",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

func TestShellToolTimeout(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellTool(dir, 100*time.Millisecond)

	input, _ := json.Marshal(map[string]string{
		"command": "sleep 10",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "timed out")
}

func TestShellToolExitCode(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellTool(dir, 30*time.Second)

	input, _ := json.Marshal(map[string]string{
		"command": "exit 1",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestShellToolOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	tool := NewShellTool(dir, 30*time.Second)

	// Generate output larger than 30KB
	input, _ := json.Marshal(map[string]string{
		"command": "yes | head -n 50000",
	})

	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Content), 31000) // 30KB + truncation message
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/ -run TestShellTool -v`
Expected: FAIL — `NewShellTool` not defined

**Step 3: Write shell tool implementation**

```go
// internal/tools/shell.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const maxOutputBytes = 30 * 1024 // 30KB

// ShellTool executes shell commands.
type ShellTool struct {
	workDir string
	timeout time.Duration
}

// NewShellTool creates a shell tool with the given working directory and timeout.
func NewShellTool(workDir string, timeout time.Duration) *ShellTool {
	return &ShellTool{workDir: workDir, timeout: timeout}
}

func (s *ShellTool) Name() string        { return "shell" }
func (s *ShellTool) Description() string { return "Execute shell commands" }
func (s *ShellTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Shell command to execute"}
		},
		"required": ["command"]
	}`)
}

func (s *ShellTool) Execute(ctx context.Context, input json.RawMessage) (ToolResult, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{Content: fmt.Sprintf("invalid input: %s", err), IsError: true}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", params.Command)
	cmd.Dir = s.workDir

	output, err := cmd.CombinedOutput()
	content := string(output)

	if len(content) > maxOutputBytes {
		content = content[:maxOutputBytes] + "\n... (output truncated)"
	}

	if ctx.Err() == context.DeadlineExceeded {
		return ToolResult{
			Content: fmt.Sprintf("command timed out after %s\n%s", s.timeout, content),
			IsError: true,
		}, nil
	}

	if err != nil {
		return ToolResult{
			Content: strings.TrimSpace(content),
			IsError: true,
		}, nil
	}

	return ToolResult{Content: strings.TrimSpace(content)}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement shell tool with timeout and output truncation
```

---

## Task 17: Conversation Manager

**Files:**
- Create: `internal/agent/conversation.go`
- Create: `internal/agent/conversation_test.go`

**Step 1: Write failing test for conversation operations**

```go
// internal/agent/conversation_test.go
package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationAddUser(t *testing.T) {
	conv := NewConversation("You are helpful.")

	conv.AddUser("Hello")
	msgs := conv.Messages()

	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello", msgs[0].Content[0].Text)
}

func TestConversationAddAssistant(t *testing.T) {
	conv := NewConversation("system prompt")
	conv.AddUser("Hi")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "text", Text: "Hello back!"},
	})

	msgs := conv.Messages()
	require.Len(t, msgs, 2)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestConversationAddToolResult(t *testing.T) {
	conv := NewConversation("system prompt")
	conv.AddUser("read a file")
	conv.AddAssistant([]provider.ContentBlock{
		{Type: "tool_use", ID: "tool-1", Name: "file"},
	})
	conv.AddToolResult("tool-1", "file contents", false)

	msgs := conv.Messages()
	require.Len(t, msgs, 3)
	assert.Equal(t, "user", msgs[2].Role)
	assert.Equal(t, "tool_result", msgs[2].Content[0].Type)
	assert.Equal(t, "tool-1", msgs[2].Content[0].ToolUseID)
}

func TestConversationSystemPrompt(t *testing.T) {
	conv := NewConversation("You are a coding assistant.")
	assert.Equal(t, "You are a coding assistant.", conv.SystemPrompt())
}

func TestConversationClear(t *testing.T) {
	conv := NewConversation("system")
	conv.AddUser("hello")
	conv.AddAssistant([]provider.ContentBlock{{Type: "text", Text: "hi"}})

	conv.Clear()
	assert.Empty(t, conv.Messages())
	assert.Equal(t, "system", conv.SystemPrompt())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestConversation -v`
Expected: FAIL — `NewConversation` not defined

**Step 3: Write conversation manager**

```go
// internal/agent/conversation.go
package agent

import (
	"github.com/julianshen/rubichan/internal/provider"
)

// Conversation manages the message history for an agent session.
type Conversation struct {
	system   string
	messages []provider.Message
}

// NewConversation creates a conversation with the given system prompt.
func NewConversation(systemPrompt string) *Conversation {
	return &Conversation{system: systemPrompt}
}

// SystemPrompt returns the system prompt.
func (c *Conversation) SystemPrompt() string {
	return c.system
}

// Messages returns the conversation messages (excludes system prompt).
func (c *Conversation) Messages() []provider.Message {
	return c.messages
}

// AddUser appends a user text message.
func (c *Conversation) AddUser(text string) {
	c.messages = append(c.messages, provider.NewUserMessage(text))
}

// AddAssistant appends an assistant message with the given content blocks.
func (c *Conversation) AddAssistant(blocks []provider.ContentBlock) {
	c.messages = append(c.messages, provider.Message{
		Role:    "assistant",
		Content: blocks,
	})
}

// AddToolResult appends a tool result message.
func (c *Conversation) AddToolResult(toolUseID, content string, isError bool) {
	c.messages = append(c.messages, provider.NewToolResultMessage(toolUseID, content, isError))
}

// Clear removes all messages but preserves the system prompt.
func (c *Conversation) Clear() {
	c.messages = nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement conversation manager with message history
```

---

## Task 18: Context Window Manager

**Files:**
- Create: `internal/agent/context.go`
- Create: `internal/agent/context_test.go`

**Step 1: Write failing test for token counting and truncation**

```go
// internal/agent/context_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextManagerExceedsBudget(t *testing.T) {
	cm := NewContextManager(100) // very small budget

	conv := NewConversation("system prompt")
	for i := 0; i < 20; i++ {
		conv.AddUser("This is a message that uses up some tokens in the context window")
		conv.AddAssistant(nil)
	}

	assert.True(t, cm.ExceedsBudget(conv))
}

func TestContextManagerTruncate(t *testing.T) {
	cm := NewContextManager(100) // very small budget

	conv := NewConversation("system")
	for i := 0; i < 20; i++ {
		conv.AddUser("This is message content that takes up tokens")
		conv.AddAssistant(nil)
	}

	cm.Truncate(conv)

	// After truncation, should be within budget
	assert.False(t, cm.ExceedsBudget(conv))
	// Should still have some messages
	assert.NotEmpty(t, conv.Messages())
}

func TestContextManagerSmallConversationNoTruncation(t *testing.T) {
	cm := NewContextManager(100000)

	conv := NewConversation("system")
	conv.AddUser("hello")

	cm.Truncate(conv)
	assert.Len(t, conv.Messages(), 1)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestContextManager -v`
Expected: FAIL — `NewContextManager` not defined

**Step 3: Write context manager**

```go
// internal/agent/context.go
package agent

// ContextManager tracks token usage and truncates conversations to stay within budget.
type ContextManager struct {
	budget int
}

// NewContextManager creates a context manager with the given token budget.
func NewContextManager(budget int) *ContextManager {
	return &ContextManager{budget: budget}
}

// EstimateTokens provides a rough token count for a conversation.
// Uses a simple heuristic: ~4 chars per token (conservative).
func (cm *ContextManager) EstimateTokens(conv *Conversation) int {
	total := len(conv.SystemPrompt()) / 4

	for _, msg := range conv.Messages() {
		for _, block := range msg.Content {
			total += len(block.Text) / 4
			total += len(block.Input) / 4
			total += 10 // overhead per block (role, type, etc.)
		}
	}
	return total
}

// ExceedsBudget returns true if the conversation exceeds the token budget.
func (cm *ContextManager) ExceedsBudget(conv *Conversation) bool {
	return cm.EstimateTokens(conv) > cm.budget
}

// Truncate removes oldest message pairs to fit within budget.
// Keeps the system prompt and at least the most recent 2 messages.
func (cm *ContextManager) Truncate(conv *Conversation) {
	for cm.ExceedsBudget(conv) && len(conv.messages) > 2 {
		// Remove the oldest two messages (user + assistant pair)
		if len(conv.messages) >= 2 {
			conv.messages = conv.messages[2:]
		} else {
			conv.messages = conv.messages[1:]
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement context window manager with token estimation and truncation
```

---

## Task 19: Agent Loop — Types and Constructor

**Files:**
- Create: `internal/agent/agent.go`
- Create: `internal/agent/agent_test.go`

**Step 1: Write failing test for agent construction**

```go
// internal/agent/agent_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider returns pre-scripted stream events.
type mockProvider struct {
	events []provider.StreamEvent
}

func (m *mockProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		for _, ev := range m.events {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

func autoApprove(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return true, nil
}

func autoDeny(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
	return false, nil
}

func TestNewAgent(t *testing.T) {
	p := &mockProvider{}
	r := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(p, r, autoApprove, cfg)
	assert.NotNil(t, a)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestNewAgent -v`
Expected: FAIL — `New` not defined

**Step 3: Write agent types and constructor**

```go
// internal/agent/agent.go
package agent

import (
	"context"
	"encoding/json"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
)

// ApprovalFunc is called before executing a tool that requires approval.
type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

// TurnEvent represents an event emitted during an agent turn.
type TurnEvent struct {
	Type       string
	Text       string
	ToolCall   *ToolCallEvent
	ToolResult *ToolResultEvent
	Error      error
}

// ToolCallEvent holds information about a tool call.
type ToolCallEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent holds the result of a tool execution.
type ToolResultEvent struct {
	ID      string
	Name    string
	Content string
	IsError bool
}

// Agent orchestrates the LLM conversation loop.
type Agent struct {
	provider     provider.LLMProvider
	tools        *tools.Registry
	conversation *Conversation
	context      *ContextManager
	approve      ApprovalFunc
	model        string
	maxTurns     int
}

// New creates an agent with the given dependencies.
func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config) *Agent {
	systemPrompt := buildSystemPrompt(cfg)
	return &Agent{
		provider:     p,
		tools:        t,
		conversation: NewConversation(systemPrompt),
		context:      NewContextManager(cfg.Agent.ContextBudget),
		approve:      approve,
		model:        cfg.Provider.Model,
		maxTurns:     cfg.Agent.MaxTurns,
	}
}

func buildSystemPrompt(cfg *config.Config) string {
	return "You are a helpful coding assistant. You have access to tools for reading and writing files and executing shell commands."
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestNewAgent -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add agent types, events, and constructor
```

---

## Task 20: Agent Loop — Turn Execution

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write failing test for text-only turn**

```go
func TestTurnTextOnly(t *testing.T) {
	p := &mockProvider{
		events: []provider.StreamEvent{
			{Type: "text_delta", Text: "Hello "},
			{Type: "text_delta", Text: "there!"},
			{Type: "stop"},
		},
	}
	r := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(p, r, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "Hi")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Should get text deltas then done
	textEvents := filterTurnEvents(events, "text_delta")
	assert.Len(t, textEvents, 2)
	assert.Equal(t, "Hello ", textEvents[0].Text)
	assert.Equal(t, "there!", textEvents[1].Text)

	doneEvents := filterTurnEvents(events, "done")
	assert.Len(t, doneEvents, 1)
}

func filterTurnEvents(events []TurnEvent, typ string) []TurnEvent {
	var filtered []TurnEvent
	for _, e := range events {
		if e.Type == typ {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestTurnTextOnly -v`
Expected: FAIL — `Turn` not defined

**Step 3: Write the Turn method**

Add to `internal/agent/agent.go`:

```go
// Turn processes one user message through potentially multiple LLM round-trips.
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error) {
	a.conversation.AddUser(userMessage)
	a.context.Truncate(a.conversation)

	ch := make(chan TurnEvent)
	go func() {
		defer close(ch)
		a.runLoop(ctx, ch, 0)
	}()

	return ch, nil
}

func (a *Agent) runLoop(ctx context.Context, ch chan<- TurnEvent, turnCount int) {
	if turnCount >= a.maxTurns {
		ch <- TurnEvent{Type: "error", Error: fmt.Errorf("max turns (%d) exceeded", a.maxTurns)}
		ch <- TurnEvent{Type: "done"}
		return
	}

	req := provider.CompletionRequest{
		Model:     a.model,
		System:    a.conversation.SystemPrompt(),
		Messages:  a.conversation.Messages(),
		Tools:     a.tools.All(),
		MaxTokens: 4096,
	}

	stream, err := a.provider.Stream(ctx, req)
	if err != nil {
		ch <- TurnEvent{Type: "error", Error: err}
		ch <- TurnEvent{Type: "done"}
		return
	}

	var assistantBlocks []provider.ContentBlock
	var pendingToolCalls []provider.ToolUseBlock
	var toolInputBuffer string

	for ev := range stream {
		switch ev.Type {
		case "text_delta":
			ch <- TurnEvent{Type: "text_delta", Text: ev.Text}
			if len(assistantBlocks) == 0 || assistantBlocks[len(assistantBlocks)-1].Type != "text" {
				assistantBlocks = append(assistantBlocks, provider.ContentBlock{Type: "text"})
			}
			assistantBlocks[len(assistantBlocks)-1].Text += ev.Text

		case "tool_use":
			toolInputBuffer = ""
			pendingToolCalls = append(pendingToolCalls, *ev.ToolUse)
			assistantBlocks = append(assistantBlocks, provider.ContentBlock{
				Type: "tool_use",
				ID:   ev.ToolUse.ID,
				Name: ev.ToolUse.Name,
			})

		case "error":
			ch <- TurnEvent{Type: "error", Error: ev.Error}

		case "stop":
			// handled below
		}
	}

	a.conversation.AddAssistant(assistantBlocks)

	if len(pendingToolCalls) == 0 {
		ch <- TurnEvent{Type: "done"}
		return
	}

	// Execute tool calls
	for _, tc := range pendingToolCalls {
		ch <- TurnEvent{
			Type:     "tool_call",
			ToolCall: &ToolCallEvent{ID: tc.ID, Name: tc.Name, Input: tc.Input},
		}

		approved, err := a.approve(ctx, tc.Name, tc.Input)
		if err != nil {
			ch <- TurnEvent{Type: "error", Error: err}
			a.conversation.AddToolResult(tc.ID, "approval error: "+err.Error(), true)
			continue
		}

		if !approved {
			a.conversation.AddToolResult(tc.ID, "Tool use denied by user", true)
			ch <- TurnEvent{
				Type:       "tool_result",
				ToolResult: &ToolResultEvent{ID: tc.ID, Name: tc.Name, Content: "Tool use denied by user", IsError: true},
			}
			continue
		}

		tool, ok := a.tools.Get(tc.Name)
		if !ok {
			result := "unknown tool: " + tc.Name
			a.conversation.AddToolResult(tc.ID, result, true)
			ch <- TurnEvent{
				Type:       "tool_result",
				ToolResult: &ToolResultEvent{ID: tc.ID, Name: tc.Name, Content: result, IsError: true},
			}
			continue
		}

		result, err := tool.Execute(ctx, tc.Input)
		if err != nil {
			errMsg := "tool execution error: " + err.Error()
			a.conversation.AddToolResult(tc.ID, errMsg, true)
			ch <- TurnEvent{
				Type:       "tool_result",
				ToolResult: &ToolResultEvent{ID: tc.ID, Name: tc.Name, Content: errMsg, IsError: true},
			}
			continue
		}

		a.conversation.AddToolResult(tc.ID, result.Content, result.IsError)
		ch <- TurnEvent{
			Type:       "tool_result",
			ToolResult: &ToolResultEvent{ID: tc.ID, Name: tc.Name, Content: result.Content, IsError: result.IsError},
		}
	}

	// After tool results, loop back to let LLM see them
	a.context.Truncate(a.conversation)
	a.runLoop(ctx, ch, turnCount+1)
}
```

Add `"fmt"` to the imports in `agent.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestTurnTextOnly -v`
Expected: PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement agent turn loop with streaming and text responses
```

---

## Task 21: Agent Loop — Tool Execution and Approval

**Files:**
- Modify: `internal/agent/agent_test.go`

**Step 1: Write test for tool call turn**

```go
func TestTurnWithToolCall(t *testing.T) {
	callCount := 0
	p := &mockProvider{}

	// First call: LLM requests a tool call
	// Second call: LLM responds with text after seeing tool result
	p.events = nil // will be set dynamically
	dynamicProvider := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "t1", Name: "file", Input: json.RawMessage(`{"operation":"read","path":"hello.txt"}`),
				}},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "File contains: hello"},
				{Type: "stop"},
			},
		},
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644)

	r := tools.NewRegistry()
	r.Register(tools.NewFileTool(dir))

	cfg := config.DefaultConfig()
	a := New(dynamicProvider, r, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "Read hello.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	toolCallEvents := filterTurnEvents(events, "tool_call")
	require.Len(t, toolCallEvents, 1)
	assert.Equal(t, "file", toolCallEvents[0].ToolCall.Name)

	toolResultEvents := filterTurnEvents(events, "tool_result")
	require.Len(t, toolResultEvents, 1)
	assert.Contains(t, toolResultEvents[0].ToolResult.Content, "hello world")

	doneEvents := filterTurnEvents(events, "done")
	assert.Len(t, doneEvents, 1)

	_ = callCount
}

// dynamicMockProvider returns different responses on successive calls.
type dynamicMockProvider struct {
	responses [][]provider.StreamEvent
	callIdx   int
}

func (d *dynamicMockProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	idx := d.callIdx
	if idx >= len(d.responses) {
		idx = len(d.responses) - 1
	}
	d.callIdx++

	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		for _, ev := range d.responses[idx] {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

func TestTurnWithDeniedTool(t *testing.T) {
	p := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "t1", Name: "shell", Input: json.RawMessage(`{"command":"rm -rf /"}`),
				}},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "Understood, I won't do that."},
				{Type: "stop"},
			},
		},
	}

	r := tools.NewRegistry()
	r.Register(tools.NewShellTool(t.TempDir(), 30*time.Second))

	cfg := config.DefaultConfig()
	a := New(p, r, autoDeny, cfg)

	ch, err := a.Turn(context.Background(), "Delete everything")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	toolResultEvents := filterTurnEvents(events, "tool_result")
	require.Len(t, toolResultEvents, 1)
	assert.True(t, toolResultEvents[0].ToolResult.IsError)
	assert.Contains(t, toolResultEvents[0].ToolResult.Content, "denied")
}
```

**Step 2: Run test — add missing imports**

Add to test imports: `"os"`, `"path/filepath"`, `"time"`

Run: `go test ./internal/agent/ -v`
Expected: All tests PASS

**Step 3: Commit**

```
[BEHAVIORAL] Verify agent loop handles tool calls and approval denial
```

---

## Task 22: Agent — Clear Conversation and Model Switch

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

**Step 1: Write failing tests**

```go
func TestClearConversation(t *testing.T) {
	p := &mockProvider{events: []provider.StreamEvent{
		{Type: "text_delta", Text: "Hi"},
		{Type: "stop"},
	}}
	r := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(p, r, autoApprove, cfg)

	ch, _ := a.Turn(context.Background(), "Hello")
	for range ch {} // drain

	a.ClearConversation()
	assert.Empty(t, a.conversation.Messages())
}

func TestSetModel(t *testing.T) {
	p := &mockProvider{}
	r := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(p, r, autoApprove, cfg)

	a.SetModel("gpt-4o")
	assert.Equal(t, "gpt-4o", a.model)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestClear -v`
Expected: FAIL — `ClearConversation` not defined

**Step 3: Add methods to agent**

Add to `internal/agent/agent.go`:

```go
// ClearConversation resets the conversation history.
func (a *Agent) ClearConversation() {
	a.conversation.Clear()
}

// SetModel changes the model used for completions.
func (a *Agent) SetModel(model string) {
	a.model = model
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add conversation clear and model switch to agent
```

---

## Task 23: TUI — Model and Basic Input/Output

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/model_test.go`

**Step 1: Write failing test for TUI model initialization**

```go
// internal/tui/model_test.go
package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewModel(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5")
	assert.Equal(t, StateInput, m.state)
	assert.NotNil(t, m.input)
	assert.NotNil(t, m.viewport)
}

func TestModelHandleSlashQuit(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5")
	cmd := m.handleCommand("/quit")
	assert.NotNil(t, cmd)
}

func TestModelHandleSlashClear(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5")
	cmd := m.handleCommand("/clear")
	assert.Nil(t, cmd) // doesn't quit
}

func TestModelHandleSlashHelp(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5")
	cmd := m.handleCommand("/help")
	assert.Nil(t, cmd)
	assert.Contains(t, m.content.String(), "/quit")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run Test -v`
Expected: FAIL — `NewModel` not defined

**Step 3: Write TUI model**

```go
// internal/tui/model.go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/agent"
)

// UIState represents the current TUI state.
type UIState int

const (
	StateInput UIState = iota
	StateStreaming
	StateAwaitingApproval
)

// Model is the Bubble Tea model for the interactive TUI.
type Model struct {
	agent     *agent.Agent
	input     textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model
	content   strings.Builder
	state     UIState
	approval  *pendingApproval
	appName   string
	modelName string
	width     int
	height    int
	quitting  bool
}

type pendingApproval struct {
	toolName string
	input    string
	respond  chan<- bool
}

// NewModel creates a new TUI model.
func NewModel(a *agent.Agent, appName, modelName string) *Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return &Model{
		agent:     a,
		input:     ti,
		viewport:  vp,
		spinner:   sp,
		state:     StateInput,
		appName:   appName,
		modelName: modelName,
		width:     80,
		height:    24,
	}
}

func (m *Model) handleCommand(cmd string) tea.Cmd {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "/quit", "/exit":
		m.quitting = true
		return tea.Quit
	case "/clear":
		if m.agent != nil {
			m.agent.ClearConversation()
		}
		m.content.Reset()
		m.viewport.SetContent("")
		return nil
	case "/model":
		if len(parts) > 1 {
			m.modelName = parts[1]
			if m.agent != nil {
				m.agent.SetModel(parts[1])
			}
			m.content.WriteString(fmt.Sprintf("\nModel switched to %s\n\n", parts[1]))
			m.viewport.SetContent(m.content.String())
		}
		return nil
	case "/help":
		help := `
Available commands:
  /quit, /exit  - Exit rubichan
  /clear        - Clear conversation history
  /model <name> - Switch to a different model
  /help         - Show this help message
`
		m.content.WriteString(help)
		m.viewport.SetContent(m.content.String())
		return nil
	default:
		return nil
	}
}
```

**Step 4: Install Bubble Tea dependencies and run test**

Run:
```bash
go get github.com/charmbracelet/bubbletea github.com/charmbracelet/bubbles github.com/charmbracelet/lipgloss github.com/charmbracelet/glamour
go test ./internal/tui/ -v
```
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Add TUI model with input state and slash command handling
```

---

## Task 24: TUI — Bubble Tea Init, Update, View

**Files:**
- Modify: `internal/tui/model.go`
- Create: `internal/tui/update.go`
- Create: `internal/tui/view.go`

**Step 1: Write failing test for View output**

```go
func TestModelView(t *testing.T) {
	m := NewModel(nil, "rubichan", "claude-sonnet-4-5")
	m.width = 80
	m.height = 24

	view := m.View()
	assert.Contains(t, view, "rubichan")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestModelView -v`
Expected: FAIL — `View` not defined on Model

**Step 3: Implement Init, Update, View**

```go
// internal/tui/update.go
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/julianshen/rubichan/internal/agent"
)

// TurnEventMsg wraps an agent TurnEvent as a Bubble Tea message.
type TurnEventMsg agent.TurnEvent

func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 2
		inputHeight := 3
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight - inputHeight
		return m, nil

	case TurnEventMsg:
		return m.handleTurnEvent(agent.TurnEvent(msg))

	case spinner.TickMsg:
		if m.state == StateStreaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		if m.state == StateAwaitingApproval {
			return m, nil // handled by y/n below
		}
		if m.state != StateInput {
			return m, nil
		}

		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")

		if strings.HasPrefix(text, "/") {
			cmd := m.handleCommand(text)
			return m, cmd
		}

		m.content.WriteString(fmt.Sprintf("\n> %s\n\n", text))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		m.state = StateStreaming

		return m, tea.Batch(m.spinner.Tick, m.startTurn(text))
	}

	if m.state == StateAwaitingApproval {
		switch msg.String() {
		case "y", "Y":
			if m.approval != nil {
				m.approval.respond <- true
				m.approval = nil
			}
			m.content.WriteString("Approved\n")
			m.viewport.SetContent(m.content.String())
			m.state = StateStreaming
			return m, m.spinner.Tick
		case "n", "N":
			if m.approval != nil {
				m.approval.respond <- false
				m.approval = nil
			}
			m.content.WriteString("Denied\n")
			m.viewport.SetContent(m.content.String())
			m.state = StateStreaming
			return m, m.spinner.Tick
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) startTurn(text string) tea.Cmd {
	return func() tea.Msg {
		if m.agent == nil {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}

		ch, err := m.agent.Turn(context.Background(), text)
		if err != nil {
			return TurnEventMsg(agent.TurnEvent{Type: "error", Error: err})
		}

		// Read first event and return it; subsequent events via waitForEvent
		ev, ok := <-ch
		if !ok {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}

		// Store channel for subsequent reads
		m.eventCh = ch
		return TurnEventMsg(ev)
	}
}

func (m *Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		if m.eventCh == nil {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		ev, ok := <-m.eventCh
		if !ok {
			return TurnEventMsg(agent.TurnEvent{Type: "done"})
		}
		return TurnEventMsg(ev)
	}
}

func (m *Model) handleTurnEvent(ev agent.TurnEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case "text_delta":
		m.content.WriteString(ev.Text)
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_call":
		m.content.WriteString(fmt.Sprintf("\n[Tool: %s]\n", ev.ToolCall.Name))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "tool_result":
		content := ev.ToolResult.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		m.content.WriteString(fmt.Sprintf("  Result: %s\n\n", content))
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		return m, m.waitForEvent()

	case "error":
		m.content.WriteString(fmt.Sprintf("\nError: %s\n", ev.Error))
		m.viewport.SetContent(m.content.String())
		return m, m.waitForEvent()

	case "done":
		m.content.WriteString("\n")
		m.viewport.SetContent(m.content.String())
		m.viewport.GotoBottom()
		m.state = StateInput
		m.eventCh = nil
		return m, nil
	}

	return m, m.waitForEvent()
}
```

Add `eventCh` field to Model in `model.go`:

```go
// Add to Model struct
eventCh <-chan agent.TurnEvent
```

Add spinner import to update.go:
```go
"github.com/charmbracelet/bubbles/spinner"
```

```go
// internal/tui/view.go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#EEEEEE"}).
			Padding(0, 1)

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"})
)

func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	header := headerStyle.Render(fmt.Sprintf("%s · %s", m.appName, m.modelName))
	divider := lipgloss.NewStyle().Width(m.width).Render("─")

	var statusLine string
	switch m.state {
	case StateStreaming:
		statusLine = m.spinner.View() + " Thinking..."
	case StateAwaitingApproval:
		statusLine = "Approve? [Y]es / [N]o"
	default:
		statusLine = ""
	}

	inputLine := inputPromptStyle.Render("> ") + m.input.View()

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		header, divider, m.viewport.View(), divider, statusLine, inputLine)
}
```

**Step 4: Run tests**

Run: `go test ./internal/tui/ -v`
Expected: All tests PASS

**Step 5: Commit**

```
[BEHAVIORAL] Implement TUI Init, Update, View with streaming and approval flow
```

---

## Task 25: CLI Entrypoint

**Files:**
- Modify: `cmd/rubichan/main.go`

**Step 1: Write failing test for version command**

```go
// cmd/rubichan/main_test.go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionString(t *testing.T) {
	s := versionString()
	assert.Contains(t, s, "rubichan")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/rubichan/ -run TestVersionString -v`
Expected: FAIL — `versionString` not defined

**Step 3: Write the CLI entrypoint**

```go
// cmd/rubichan/main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/tui"
)

var (
	version    = "dev"
	commit     = "none"
	date       = "unknown"

	configPath   string
	modelFlag    string
	providerFlag string
	verbose      bool
)

func versionString() string {
	return fmt.Sprintf("rubichan %s (commit: %s, built: %s)", version, commit, date)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "rubichan",
		Short: "AI coding assistant",
		RunE:  runInteractive,
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file path")
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "override model")
	rootCmd.PersistentFlags().StringVar(&providerFlag, "provider", "", "override provider")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug logging")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(versionString())
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runInteractive(cmd *cobra.Command, args []string) error {
	// Load config
	cfgPath := configPath
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides
	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry
	cwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	registry.Register(tools.NewFileTool(cwd))
	registry.Register(tools.NewShellTool(cwd, 120*time.Second))

	// Create approval function (TUI-based)
	approveCh := make(chan bool)
	approveFunc := tui.NewApprovalFunc(approveCh)

	// Create agent
	a := agent.New(p, registry, approveFunc, cfg)

	// Create and run TUI
	m := tui.NewModel(a, "rubichan", cfg.Provider.Model)
	prog := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}
```

Note: `tui.NewApprovalFunc` needs to be added to the TUI package. Add to `internal/tui/model.go`:

```go
// NewApprovalFunc creates an ApprovalFunc that integrates with the TUI.
func NewApprovalFunc(ch chan bool) agent.ApprovalFunc {
	return func(ctx context.Context, tool string, input json.RawMessage) (bool, error) {
		// In the actual TUI, this will send an approval_request event
		// and wait for user response. For now, auto-approve.
		return true, nil
	}
}
```

**Step 4: Install cobra and run test**

Run: `go get github.com/spf13/cobra && go test ./cmd/rubichan/ -v`
Expected: PASS

**Step 5: Verify full build**

Run: `go build ./cmd/rubichan && ./rubichan version`
Expected: Prints version string

**Step 6: Commit**

```
[BEHAVIORAL] Implement CLI entrypoint with Cobra, config loading, and TUI launch
```

---

## Task 26: Integration Smoke Test

**Files:**
- Create: `internal/agent/integration_test.go`

**Step 1: Write an end-to-end test with mock provider**

```go
// internal/agent/integration_test.go
package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegrationFileReadWriteFlow(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("original content"), 0644)

	// Mock provider: first asks to read the file, then writes new content
	p := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			// Turn 1: LLM asks to read the file
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "t1", Name: "file",
					Input: json.RawMessage(`{"operation":"read","path":"test.txt"}`),
				}},
				{Type: "stop"},
			},
			// Turn 2: LLM writes modified content
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "t2", Name: "file",
					Input: json.RawMessage(`{"operation":"write","path":"test.txt","content":"modified content"}`),
				}},
				{Type: "stop"},
			},
			// Turn 3: LLM responds with text
			{
				{Type: "text_delta", Text: "Done! I've updated the file."},
				{Type: "stop"},
			},
		},
	}

	r := tools.NewRegistry()
	r.Register(tools.NewFileTool(dir))
	r.Register(tools.NewShellTool(dir, 30*time.Second))

	cfg := config.DefaultConfig()
	a := New(p, r, autoApprove, cfg)

	ch, err := a.Turn(context.Background(), "Update test.txt")
	require.NoError(t, err)

	var events []TurnEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify file was modified
	data, err := os.ReadFile(filepath.Join(dir, "test.txt"))
	require.NoError(t, err)
	assert.Equal(t, "modified content", string(data))

	// Verify we got the expected flow
	doneEvents := filterTurnEvents(events, "done")
	assert.Len(t, doneEvents, 1)
}
```

**Step 2: Run test**

Run: `go test ./internal/agent/ -run TestIntegration -v`
Expected: PASS

**Step 3: Run full test suite with coverage**

Run: `go test -cover ./...`
Expected: All PASS, coverage >90% for internal packages

**Step 4: Commit**

```
[BEHAVIORAL] Add integration smoke test for full agent file read/write flow
```

---

## Final Checklist

- [ ] Task 0: Project scaffolding
- [ ] Task 1: Config types and defaults
- [ ] Task 2: Config TOML loading
- [ ] Task 3: Config missing file fallback
- [ ] Task 4: Config OpenAI-compatible providers
- [ ] Task 5: API key resolution
- [ ] Task 6: Provider interface and types
- [ ] Task 7: Anthropic SSE parsing
- [ ] Task 8: Anthropic Stream implementation
- [ ] Task 9: Anthropic tool use streaming
- [ ] Task 10: Anthropic error handling
- [ ] Task 11: OpenAI Stream implementation
- [ ] Task 12: OpenAI tool use and extra headers
- [ ] Task 13: Provider factory
- [ ] Task 14: Tool interface and registry
- [ ] Task 15: File tool
- [ ] Task 16: Shell tool
- [ ] Task 17: Conversation manager
- [ ] Task 18: Context window manager
- [ ] Task 19: Agent types and constructor
- [ ] Task 20: Agent turn loop
- [ ] Task 21: Agent tool execution and approval
- [ ] Task 22: Agent clear and model switch
- [ ] Task 23: TUI model and slash commands
- [ ] Task 24: TUI Init/Update/View
- [ ] Task 25: CLI entrypoint
- [ ] Task 26: Integration smoke test
