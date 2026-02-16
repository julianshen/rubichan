# Milestone 1 Design: Core Agent with Interactive Mode

> **Date:** 2026-02-16
> **Status:** Approved
> **Goal:** Interactive mode with basic tool support, Anthropic + OpenAI providers, TUI

---

## Scope

Milestone 1 delivers a working interactive CLI agent. A user can launch `rubichan`, have a multi-turn conversation with an LLM, and the agent can read/write files and execute shell commands with user approval.

### In Scope

- Config loading (TOML) with API key resolution (keyring, env, config)
- LLM provider interface with Anthropic and OpenAI implementations
- OpenRouter support via OpenAI-compatible config with custom base_url
- Tool interface + registry with file and shell tools
- Agent core: conversation manager, context window manager, Plan-Act-Observe loop
- Bubble Tea TUI: streaming output, markdown rendering, approval prompts
- Cobra CLI entrypoint with flag overrides

### Out of Scope (deferred to later milestones)

- Ollama provider
- SQLite persistence / session resume
- Skill system (runtime, loader, hooks, manifests)
- Security analysis engine
- Headless mode / wiki generator
- Git, search, LSP, MCP tools
- Theming / color customization

---

## Build Order

Bottom-up: each layer is independently testable before the next layer depends on it.

1. Config
2. Provider Interface + Anthropic + OpenAI
3. Tool Interface + File Tool + Shell Tool
4. Agent Core (conversation, context, loop)
5. TUI
6. CLI entrypoint

---

## Layer 1: Config

**Package:** `internal/config`

### Types

```go
type Config struct {
    Provider ProviderConfig
    Agent    AgentConfig
}

type ProviderConfig struct {
    Default   string
    Model     string
    Providers []OpenAICompatibleConfig  // includes Anthropic as a special case
    Anthropic AnthropicProviderConfig
}

type AnthropicProviderConfig struct {
    APIKeySource string  // "keyring", "env", "config"
    APIKey       string  // only if source is "config"
}

type OpenAICompatibleConfig struct {
    Name         string
    BaseURL      string
    APIKeySource string
    APIKey       string
    ExtraHeaders map[string]string
}

type AgentConfig struct {
    MaxTurns      int     // default: 50
    ApprovalMode  string  // "prompt"
    ContextBudget int     // default: 100000
}
```

### API Key Resolution Order

1. OS keyring (`zalando/go-keyring`)
2. Environment variable (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.)
3. Config file value

### Design Decisions

- No global singleton. `Load(path string) (*Config, error)` returns a value.
- Sensible defaults applied when config file is missing or incomplete.
- Validation on load: unknown provider name returns error.

---

## Layer 2: LLM Provider

**Packages:** `internal/provider`, `internal/provider/anthropic`, `internal/provider/openai`

### Interface

```go
type LLMProvider interface {
    Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}

type CompletionRequest struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolDef
    MaxTokens   int
    Temperature float64
}

type Message struct {
    Role    string          // "user", "assistant"
    Content []ContentBlock  // text, tool_use, tool_result
}

type StreamEvent struct {
    Type    string          // "text_delta", "tool_use", "stop", "error"
    Text    string
    ToolUse *ToolUseBlock
    Error   error
}
```

### Anthropic Provider

- `POST https://api.anthropic.com/v1/messages` with `stream: true`
- SSE parsing via `bufio.Scanner`
- Maps `content_block_delta` -> text_delta, `content_block_start` (tool_use) -> tool_use, `message_stop` -> stop
- Handles overloaded / rate-limit errors with backoff
- ~300 LOC target (ADR-006)

### OpenAI Provider

- `POST /v1/chat/completions` with `stream: true`
- SSE parsing, maps `choices[0].delta` -> StreamEvent
- Translates between our canonical message format and OpenAI chat format internally
- Supports custom `base_url` for OpenAI-compatible APIs

### OpenRouter Support

No separate provider. OpenRouter is an OpenAI-compatible entry with:

```toml
[provider.openrouter]
base_url = "https://openrouter.ai/api/v1"
api_key_source = "env"
extra_headers = { "HTTP-Referer" = "https://github.com/user/rubichan" }
```

### Factory

```go
func NewProvider(cfg *config.Config) (LLMProvider, error)
```

### Design Decisions

- No retry/backoff in providers. Providers surface errors; the agent loop decides retry policy.
- Testing via `httptest.Server` with recorded SSE responses. No live API calls in tests.

---

## Layer 3: Tool Layer

**Package:** `internal/tools`

### Interface

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() json.RawMessage
    Execute(ctx context.Context, input json.RawMessage) (ToolResult, error)
}

type ToolResult struct {
    Content string
    IsError bool
}
```

### Registry

```go
type Registry struct { tools map[string]Tool }

func NewRegistry() *Registry
func (r *Registry) Register(t Tool) error
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) All() []ToolDef
```

### File Tool (`file.go`)

Three operations via `operation` field: `read`, `write`, `patch`.

- Paths resolved relative to working directory
- Path traversal prevention: reject paths escaping project root
- `write` and `patch` require approval (enforced by agent loop, not the tool)

### Shell Tool (`shell.go`)

- `os/exec.CommandContext` with configurable timeout (default 120s)
- Captures combined stdout/stderr, truncates at 30KB
- Always requires approval
- Runs via `sh -c <command>`

### Design Decisions

- Tools don't know about approval. The agent loop wraps execution with the approval callback.
- Tools are pure: given input, produce output. No side-channel dependencies.

---

## Layer 4: Agent Core

**Package:** `internal/agent`

### Conversation Manager (`conversation.go`)

```go
type Conversation struct {
    system   string
    messages []provider.Message
}

func NewConversation(systemPrompt string) *Conversation
func (c *Conversation) AddUser(text string)
func (c *Conversation) AddAssistant(blocks []provider.ContentBlock)
func (c *Conversation) AddToolResult(toolUseID string, result tools.ToolResult)
func (c *Conversation) Messages() []provider.Message
```

System prompt assembled from base instructions + `AGENT.md` (if present).

### Context Window Manager (`context.go`)

```go
type ContextManager struct {
    budget    int
    tokenizer *tiktoken.Tiktoken
}

func (cm *ContextManager) TokenCount(messages []provider.Message) int
func (cm *ContextManager) Truncate(conv *Conversation) *Conversation
```

Truncation: oldest-first message pair dropping (keeps system prompt + recent N messages).

### Agent Loop (`loop.go`)

```go
type Agent struct {
    provider     provider.LLMProvider
    tools        *tools.Registry
    conversation *Conversation
    context      *ContextManager
    approve      ApprovalFunc
    config       *config.AgentConfig
}

type ApprovalFunc func(ctx context.Context, tool string, input json.RawMessage) (bool, error)

func New(p provider.LLMProvider, t *tools.Registry, approve ApprovalFunc, cfg *config.Config) *Agent
func (a *Agent) Turn(ctx context.Context, userMessage string) (<-chan TurnEvent, error)
```

### TurnEvent

```go
type TurnEvent struct {
    Type       string   // "text_delta", "tool_call", "tool_result", "approval_request", "error", "done"
    Text       string
    ToolCall   *ToolCallEvent
    ToolResult *ToolResultEvent
    Error      error
}
```

### Loop Logic (one turn)

1. Add user message to conversation
2. Check context budget, truncate if needed
3. Stream LLM response via `provider.Stream()`
4. Forward `text_delta` events to TurnEvent channel
5. If tool_use blocks in response:
   - Emit `tool_call` event
   - Call `approve()` — if denied, add denial as tool result
   - If approved, execute tool, emit `tool_result` event
   - Add tool result to conversation, go to step 2
6. If text-only response, emit `done`
7. Enforce `max_turns` limit

### Design Decisions

- `Turn()` returns `<-chan TurnEvent` for non-blocking streaming to TUI.
- `ApprovalFunc` is injected — TUI provides interactive prompt, tests provide auto-approve/deny.
- Tool calls executed sequentially within a turn (no parallel execution in M1).

---

## Layer 5: TUI

**Package:** `internal/tui`

### Model

```go
type Model struct {
    agent       *agent.Agent
    input       textinput.Model
    viewport    viewport.Model
    content     strings.Builder
    spinner     spinner.Model
    state       UIState   // StateInput, StateStreaming, StateAwaitingApproval
    approval    *pendingApproval
    width, height int
}
```

### Rendering

| TurnEvent Type | Render |
|---|---|
| `text_delta` | Append to viewport, render markdown via glamour |
| `tool_call` | Show tool name + input summary |
| `tool_result` | Dimmed/indented output block, truncated if long |
| `approval_request` | Show tool + input preview, `[Y]es / [N]o` prompt |
| `error` | Red-styled error |
| `done` | Return to StateInput |

### Approval Flow

1. Agent emits `approval_request` with a callback channel
2. TUI switches to `StateAwaitingApproval`, shows `[Y/N]` prompt
3. User presses `y` or `n`
4. Response sent back via callback channel
5. Agent continues

### Layout

```
┌─────────────────────────────────────────┐
│ rubichan v0.1.0 · claude-sonnet-4-5     │  header
├─────────────────────────────────────────┤
│ [conversation viewport - scrollable]    │  viewport
├─────────────────────────────────────────┤
│ > _                                     │  input
└─────────────────────────────────────────┘
```

### Built-in Commands

| Command | Action |
|---|---|
| `/quit`, `/exit` | Exit |
| `/clear` | Clear conversation |
| `/model <name>` | Switch model |
| `/help` | Show commands |

### Design Decisions

- No theming in M1. Hardcoded `lipgloss.AdaptiveColor` for light/dark terminal compatibility.
- Multi-line input: Shift+Enter for newline, Enter to submit.
- Glamour re-renders accumulated text on each delta (fast enough for streaming).

---

## Layer 6: CLI Entrypoint

**Package:** `cmd/rubichan`

### Commands

```
rubichan                      # start interactive TUI
rubichan version              # print version
```

### Flags

```
--config    custom config path
--model     override model
--provider  override provider
--verbose   enable debug logging
```

### Startup Sequence

1. Parse flags (Cobra)
2. Load config, apply flag overrides
3. Resolve API key
4. Create LLMProvider
5. Create Tool Registry, register file + shell tools
6. Build system prompt (base + AGENT.md)
7. Create Agent
8. Create TUI Model, inject agent
9. Run `tea.NewProgram`

### Version Info

Embedded via ldflags at build time:

```bash
go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/rubichan
```

### Design Decisions

- `main.go` is a wiring function only — no logic.
- Startup errors (bad config, missing key) printed to stderr, exit 1. No TUI launched on failure.
