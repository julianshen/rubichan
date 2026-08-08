# A2A (Agent-to-Agent) Protocol Support Design

**Status:** Approved for implementation planning
**Date:** 2026-08-08

## Problem

rubichan has no way to discover, call, or be called by agents outside its own process. It already has two of the three pieces a modern agent stack needs: a production MCP client (`internal/tools/mcp`, "what tools can I use?") and local in-process subagent spawning (`internal/agent/subagent.go`'s `DefaultSubagentSpawner`/`SpawnParallel`, single-process fan-out). It has nothing for "who else can help me?" across process or machine boundaries, regardless of what framework the other agent is built on.

[A2A (Agent2Agent)](https://a2a-protocol.org) is Google's open protocol (Apache 2.0, Linux Foundation, IBM's competing ACP merged into it Aug 2025) for exactly this: framework-agnostic agent discovery and task delegation over HTTP. This design was prompted by researching [NousResearch/hermes-agent#514](https://github.com/NousResearch/hermes-agent/issues/514), where Hermes Agent (a comparable Python coding-agent project) proposed adding A2A support in three phases: client, server, orchestration. rubichan starts from a similar position (MCP done, local delegation done, no A2A), so this design follows the same phase split.

**Not to be confused with:** `internal/acp`, which implements Anthropic's unrelated *Agent Client Protocol* — a persistent bidirectional JSON-RPC stream between an editor and an agent, currently unwired to any of rubichan's three execution modes. A2A's transport (HTTP POST per call, SSE for streaming, no persistent connection) is different enough from ACP's that this design does not attempt to share transport code with it. See "Relationship to internal/acp" below.

## Scope

**In scope, this spec, all three phases:**
1. **Client** — discover an A2A agent's capabilities, call it, get results (sync or streaming).
2. **Server** — expose rubichan itself as a discoverable, callable A2A agent.
3. **Orchestration** — a registry of known agents and a tool that picks/fans out to them by capability match.

**Out of scope:**
- Any shared mutable state / conflict resolution between agents. Every technically-substantive comment on the Hermes issue thread flagged this as the actual hard problem multi-agent coordination has — and every one of them also confirmed A2A itself doesn't solve it (it solves discovery and routing only). This design does not attempt to solve it either.
- Non-standard transport bindings (a NATS-based bidirectional "steering" channel was proposed in the Hermes thread). Implementing it would break wire compatibility with real A2A agents built on the standard HTTP+JSON-RPC+SSE bindings. rubichan implements the spec as published, not a variant.
- Reviving or rewiring `internal/acp` into any execution mode. That remains the open decision CLAUDE.md already describes; this design does not resolve it and does not depend on it being resolved.
- Changing `internal/agent/subagent.go`'s local `SpawnParallel` to support remote targets. A2A is exposed purely as tools (`a2a_discover`, `a2a_call`, `a2a_dispatch`); the existing subagent system is untouched.
- gRPC transport. The A2A spec allows it as an alternative binding; this design implements only the HTTP+JSON-RPC+SSE binding, matching what Hermes chose and what the reference Python SDK defaults to.

## Relationship to internal/acp

`internal/acp` was recently reworked (outside this design's scope) into a genuine bidirectional JSON-RPC connection (`acp.Conn`): one persistent stream, request/response correlation by ID, ordered notification dispatch. It fits ACP's shape well — a long-lived stream where either side can call the other mid-turn.

A2A's shape is different: a client POSTs a JSON-RPC request per call over HTTP, and the server responds either with a single JSON-RPC response or an SSE event stream for that one call. There's no persistent bidirectional stream to correlate against — each HTTP request is its own round trip, and Task state (not a `Conn`'s in-memory pending map) is what lets a client resume polling later. Forcing `acp.Conn` to also serve A2A would mean either bolting HTTP+SSE semantics onto a stream-oriented abstraction, or extracting just its JSON message types (`Request`/`Response`/`RPCError`) — and even those are worth re-declaring rather than importing, since A2A's JSON-RPC methods, param shapes, and error codes are a different, independently-versioned contract from ACP's. `internal/a2a` is a new, independent package.

## Architecture

```
internal/a2a/                  Protocol layer — data model, client, server, registry
  types.go                     AgentCard, Task, Message, Part, Artifact, JSON-RPC envelope
  client.go                    Discover, Send, Stream, GetTask, CancelTask
  sse.go                       SSE event parsing (mirrors internal/tools/mcp/sse_transport.go's pattern)
  server.go                    HTTP handler: message/send, message/stream, tasks/get, tasks/cancel
  taskstore.go                 TaskStore interface
  sqlitetaskstore.go           SQLite-backed implementation
  agentcard.go                 Builds an AgentCard from a tool/skill registry (server side)
  registry.go                  Registry: known agents + capability-based selection (Phase 3)

internal/tools/a2a/            Agent-facing tools, thin wrappers over internal/a2a's client
  discover_tool.go              a2a_discover
  call_tool.go                  a2a_call
  dispatch_tool.go              a2a_dispatch (Phase 3, uses internal/a2a.Registry)

cmd/rubichan/
  a2a_serve.go                 `rubichan a2a serve` subcommand (Phase 2)

internal/config/
  config.go                    New A2AConfig section (agent registry entries)
```

Each phase adds files without modifying the previous phase's public surface: Phase 2's server doesn't change Phase 1's client types, Phase 3's registry consumes Phase 1's client and Phase 2's `AgentCard` type as-is.

## Components

### 1. Protocol types (`internal/a2a/types.go`)

Mirrors the A2A spec's data model directly — no abstraction beyond what the wire format needs:

```go
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	URL          string       `json:"url"`
	Skills       []Skill      `json:"skills"`
	Capabilities Capabilities `json:"capabilities"` // streaming, pushNotifications
	Auth         []AuthScheme `json:"authentication,omitempty"`
}

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

type TaskState string

const (
	TaskSubmitted     TaskState = "submitted"
	TaskWorking       TaskState = "working"
	TaskInputRequired TaskState = "input-required"
	TaskAuthRequired  TaskState = "auth-required"
	TaskCompleted     TaskState = "completed"
	TaskFailed        TaskState = "failed"
	TaskCanceled      TaskState = "canceled"
	TaskRejected      TaskState = "rejected"
)

type Task struct {
	ID        string    `json:"id"`
	ContextID string    `json:"contextId"`
	State     TaskState `json:"state"`
	Messages  []Message `json:"history,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type Message struct {
	Role  string `json:"role"` // "user" or "agent"
	Parts []Part `json:"parts"`
}

// Part is a tagged union over A2A's three part kinds. Exactly one of Text,
// DataJSON, or (FileURL or FileBytes) is set, selected by Kind.
type Part struct {
	Kind      string          `json:"kind"` // "text", "data", "file"
	Text      string          `json:"text,omitempty"`
	DataJSON  json.RawMessage `json:"data,omitempty"`
	FileURL   string          `json:"fileUrl,omitempty"`
	FileBytes []byte          `json:"fileBytes,omitempty"`
}

type Artifact struct {
	Name  string `json:"name"`
	Parts []Part `json:"parts"`
}
```

Plus the standard JSON-RPC 2.0 envelope (`RPCRequest`, `RPCResponse`, `RPCError`) — structurally identical to `internal/acp/types.go`'s, but declared fresh per the "Relationship to internal/acp" reasoning above.

### 2. Client (`internal/a2a/client.go`)

```go
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string // resolved via config.ResolveAPIKey before construction
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client

// Discover fetches and parses {baseURL}/.well-known/agent.json.
func (c *Client) Discover(ctx context.Context) (*AgentCard, error)

// Send makes a message/send call and returns the resulting Task.
// If the task isn't terminal yet, the caller polls GetTask.
func (c *Client) Send(ctx context.Context, msg Message) (*Task, error)

// Stream makes a message/stream call and returns a channel of Task
// snapshots, closed when the task reaches a terminal state or ctx is done.
func (c *Client) Stream(ctx context.Context, msg Message) (<-chan Task, error)

func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error)
func (c *Client) CancelTask(ctx context.Context, taskID string) error
```

`Send`/`Stream`/`GetTask`/`CancelTask` all marshal a `RPCRequest` with the corresponding method (`message/send`, `message/stream`, `tasks/get`, `tasks/cancel`), POST it, and unmarshal the `RPCResponse`. `Stream` additionally parses `text/event-stream` chunks (`internal/a2a/sse.go`), each carrying one `Task` snapshot as its JSON-RPC-wrapped SSE payload — same shape MCP's SSE transport already handles for a different protocol, so the parsing logic follows `internal/tools/mcp/sse_transport.go`'s existing pattern (event boundary detection, one JSON payload per event) rather than reinventing it.

Agent Card fetches are cached in-process, keyed by `baseURL`, for the lifetime of the rubichan process — repeated `a2a_call`s to the same agent within one session don't refetch the card.

### 3. Server (`internal/a2a/server.go`, `agentcard.go`)

```go
type Server struct {
	agent     *agent.Agent    // drives actual task execution
	store     TaskStore
	card      AgentCard
	requireAuth bool
	apiKey      string        // resolved via config.ResolveAPIKey
}

func NewServer(a *agent.Agent, store TaskStore, card AgentCard, requireAuth bool, apiKey string) *Server
func (s *Server) Handler() http.Handler // routes /.well-known/agent.json and the JSON-RPC endpoint
```

`BuildAgentCard(registry *tools.Registry, skills *skills.Registry, name, url string) AgentCard` (`agentcard.go`) walks the existing tool and skill registries and emits one `Skill` per tool/skill, so the card always reflects what this rubichan instance can actually do — no manually-maintained duplicate list.

Request handling: `message/send` creates a `Task` (`TaskSubmitted`) in the `TaskStore`, then runs it through `s.agent`'s normal turn-execution path in a goroutine, updating task state (`TaskWorking` → terminal) as the turn progresses; `message/send` itself returns once the task reaches a terminal state or the request's own context deadline, whichever comes first — an A2A task genuinely waiting past that (e.g. `INPUT_REQUIRED`) is returned in its non-terminal state, same as the spec allows. `message/stream` instead pushes each state transition as an SSE event immediately as it happens. A panic inside the driven turn is recovered at the goroutine boundary and turned into `TaskFailed` with the panic value as the task's `Error` — the listener itself never goes down.

Auth: when `requireAuth` is true (the default — see Data Flow below), every request except the `/.well-known/agent.json` card fetch itself must carry a matching `Authorization: Bearer <apiKey>` header, checked before the request reaches the JSON-RPC dispatch.

### 4. Task storage (`internal/a2a/taskstore.go`, `sqlitetaskstore.go`)

```go
type TaskStore interface {
	Create(ctx context.Context, t Task) error
	Get(ctx context.Context, id string) (Task, error)
	Update(ctx context.Context, t Task) error
}
```

`SQLiteTaskStore` implements this against rubichan's existing `modernc.org/sqlite` connection (same database file already used for conversation persistence, new `a2a_tasks` table, with a `role` column of `"server"` or `"client"` so both sides can share the table without their rows colliding). `internal/a2a/client.go` itself stays storage-agnostic — it has no `TaskStore` dependency. The client-side usage lives entirely in `internal/tools/a2a/call_tool.go`, which owns its own `SQLiteTaskStore` instance (role `"client"`) and calls `Create`/`Update` around its `Client.Send`/`GetTask` calls, so a crash mid-poll is recoverable rather than silently forgotten.

### 5. Tools (`internal/tools/a2a/`)

Three tools, registered like any other tool in the existing registry:

- **`a2a_discover(url_or_name)`** — resolves `url_or_name` against configured `[[a2a.agents]]` entries by name, or treats it as a raw URL; calls `Client.Discover`; returns the card's skills/description as tool output.
- **`a2a_call(agent, message, stream=false)`** — resolves `agent` the same way; calls `Client.Send` or `Client.Stream`; on a non-terminal `Send` result, polls `GetTask` (bounded backoff, same shape as `internal/provider`'s existing retry helpers) until terminal or a configurable timeout; persists the pending task ID via the client-side `TaskStore` before the first poll so a crash mid-wait is resumable; returns the task's final `Artifact`/`Message` text.
- **`a2a_dispatch(task_description, fanout=false)`** (Phase 3) — asks the `Registry` to score every known agent's `Skill.Tags`/`Skill.Description` against `task_description` (substring/tag overlap, not an LLM call); calls the single best match, or every match above a threshold if `fanout=true`; returns all results together for the calling agent turn to synthesize.

### 6. Registry (`internal/a2a/registry.go`, Phase 3)

```go
type Registry struct {
	agents map[string]AgentCard // keyed by configured name
}

func (r *Registry) Add(name string, card AgentCard)
func (r *Registry) Match(taskDescription string) []string // agent names, best first
```

Populated at startup from `[[a2a.agents]]` config entries (each discovered once via `Client.Discover`) plus any agent `a2a_discover` adds during a session.

### 7. Config (`internal/config/config.go`)

```go
type A2AConfig struct {
	Agents      []A2AAgentConfig `toml:"agents"`
	ServeAddr   string           `toml:"serve_addr"`   // default "127.0.0.1:9910"
	RequireAuth bool             `toml:"require_auth"` // default true
	APIKeySource string          `toml:"api_key_source"`
	APIKey      string           `toml:"api_key"`
}

type A2AAgentConfig struct {
	Name         string `toml:"name"`
	URL          string `toml:"url"`
	APIKeySource string `toml:"api_key_source"`
	APIKey       string `toml:"api_key"`
}
```

Structurally parallel to `MCPConfig`/`MCPServerConfig`. Credential resolution reuses `config.ResolveAPIKey` — the same `env`/`config`/`keyring` convention already used by every provider (and just fixed for Zai in #348), not a new scheme.

## Data Flow

**Phase 1 (client):** `a2a_discover` → GET `/.well-known/agent.json` → cached `AgentCard`. `a2a_call` → `message/send` (or `message/stream`) → if non-terminal, record task in the client-side `SQLiteTaskStore`, then poll `tasks/get` until terminal → return result text, mark task record resolved.

**Phase 2 (server):** inbound `message/send` → `TaskStore.Create` (`TaskSubmitted`) → drive `s.agent` turn → `TaskStore.Update` through `TaskWorking` to a terminal state → response carries the final (or still-non-terminal) `Task`. `message/stream` pushes the same transitions as SSE events instead of a single final response.

**Phase 3 (orchestration):** `a2a_dispatch` → `Registry.Match` (local, no network call) → one or more `a2a_call`-equivalent invocations against the matched agents → results returned together, uninterpreted — this design does not merge or reconcile them.

## Error Handling

Client-side failures (unreachable agent, malformed card, JSON-RPC error, auth rejected, poll timeout) return as ordinary Go errors from `internal/a2a/client.go`, surfaced by the `internal/tools/a2a` wrappers as normal tool-call errors — the same path any other failing tool already takes; no new error-reporting mechanism.

Server-side: a panic inside the driven `Agent` turn is recovered and recorded as `TaskFailed` with the panic value as `Task.Error`, never propagated past the goroutine boundary. Malformed inbound JSON-RPC requests get a standard JSON-RPC error response (`-32600`/`-32601`/`-32602`), not a raw HTTP 500. Auth failures return HTTP 401 before any JSON-RPC parsing happens.

## Testing

TDD throughout per CLAUDE.md, target >90% coverage on all new packages. Client and server tests run against `internal/testutil`'s existing in-memory HTTP transport (`mem://`, already used by provider tests) — no real network calls in the suite. `SQLiteTaskStore` tests use a temp-file SQLite database per test, matching the existing conversation-persistence test pattern. One integration test in `internal/a2a` starts a Phase 2 `Server` and drives it with a Phase 1 `Client` in the same process, over the in-memory transport, proving the two sides agree on the wire format without needing a real external A2A agent.

## References

- [A2A Protocol site](https://a2a-protocol.org)
- [A2A Protocol spec](https://github.com/a2aproject/A2A)
- [NousResearch/hermes-agent#514](https://github.com/NousResearch/hermes-agent/issues/514) — research source for this design
