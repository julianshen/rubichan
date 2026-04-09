# ACP Adoption Migration Guide

## Overview

Rubichan now uses the Agent Client Protocol (ACP) as its standardized backbone for agent-mode communication. This document explains the changes and migration strategy.

## What Changed

### Before (Direct Agent Core Calls)
- Three modes (Interactive, Headless, Wiki) called agent core directly via function calls
- Mode-specific request/response handling within cmd/rubichan/main.go
- No standardization with external editor integrations

### After (ACP-Based)
- All modes communicate with agent core via ACP (JSON-RPC 2.0 standardized)
- Common protocol for tools, skills, security, and mode-specific operations
- Enables future IDE integrations (VS Code, JetBrains, etc.) via ACP client libraries
- Mode clients in `internal/modes/{interactive,headless,wiki}/acp_client.go`

## Architecture Changes

### New Components
- `internal/acp/` — ACP protocol implementation (server, types, handlers)
  - `types.go` — JSON-RPC 2.0 message types + Rubichan extensions
  - `server.go` — ACP server with method routing
  - `stdio.go` — stdin/stdout JSON-RPC transport
  - `capabilities.go` — capability registry
  - `skill_protocol.go` — skill system extension
  - `security_protocol.go` — security verdict extension

- `internal/modes/{interactive,headless,wiki}/acp_client.go` — mode-specific ACP clients
  - `interactive/acp_client.go` — TUI client with prompt/tool execution
  - `headless/acp_client.go` — CI/CD client with code review/security scan
  - `wiki/acp_client.go` — batch client with documentation generation

### Modified Components
- `internal/agent/agent.go` — ACP server initialization via `WithACP()` option
- `internal/agent/acp_handlers.go` — ACP method implementations
- `cmd/rubichan/main.go` — mode entrypoints now wire agents with `WithACP()`
- `test/e2e/acp_integration_test.go` — end-to-end integration tests

## Backward Compatibility

### Full Compatibility
- Existing CLI behavior unchanged (same flags, same output formats)
- Tool system unchanged (tools still implement Tool interface)
- Skill system unchanged (skills still use SKILL.yaml manifests)
- Security engine unchanged (same vulnerability checks)
- All existing code paths continue to work as before

### Breaking Changes
- None at the public API level
- Internal agent core API replaced by ACP (private implementation detail)
- ACP server is opt-in via `WithACP()` agent option

## For Developers

### Using ACP in a New Mode

1. Create `internal/modes/mymode/acp_client.go` with a client struct
2. Implement mode-specific methods that construct ACP requests
3. Create tests in `internal/modes/mymode/test/acp_client_test.go`
4. In mode entrypoint (cmd/rubichan/mymode.go):
   - Create agent with `agent.WithACP()` option
   - Create mode-specific ACP client
   - Route operations through the client to the agent's ACP server

### Extending ACP

1. Define new types in a protocol extension file (e.g., `internal/acp/myfeature_protocol.go`)
2. Create a register function to wire methods into the registry
3. Implement the interface in the agent core (e.g., `internal/agent/acp_handlers.go`)
4. Add tests in `internal/acp/test/`

Example (adding a new ACP method):

```go
// internal/acp/myfeature_protocol.go
type MyFeatureRequest struct {
    Input string `json:"input"`
}

type MyFeatureResponse struct {
    Output string `json:"output"`
}

func RegisterMyFeature(registry *CapabilityRegistry) error {
    return registry.RegisterMethod("myfeature/execute", MyFeatureHandler)
}

func MyFeatureHandler(ctx context.Context, params json.RawMessage, agent *agent.Agent) (*Response, error) {
    var req MyFeatureRequest
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }
    // Process request with agent
    return &Response{Result: MyFeatureResponse{Output: "..."}}, nil
}
```

## Performance Characteristics

- **Local (stdio)**: Minimal overhead (JSON serialization only)
- **Latency**: Request/response round-trip adds <1ms per operation
- **Throughput**: Synchronous request/response model
- **Scalability**: Each mode instance gets its own agent with isolated state
- **Concurrency**: Thread-safe via sync.Mutex on ID generation in clients

## Testing

All ACP functionality is covered by:
- Protocol tests in `internal/acp/test/` (92%+ coverage)
- Mode adapter tests in `internal/modes/*/test/`
- Integration tests in `test/e2e/acp_integration_test.go`

Run all tests:
```bash
go test ./...
```

Run with coverage:
```bash
go test -cover ./...
```

Run only e2e tests:
```bash
go test ./test/e2e/...
```

## Migration Checklist for Custom Integrations

If you have built custom integrations on top of Rubichan:

- [ ] Verify your integration still works with the existing CLI
- [ ] If you were calling agent functions directly, consider using ACP clients
- [ ] Test with the new ACP transport if building external tools
- [ ] Check `docs/ACP_ARCHITECTURE.md` for protocol details
- [ ] Review error handling (ACP uses standard JSON-RPC error codes)

## Future Enhancements

- Remote HTTP/WebSocket transport for cloud deployment
- IDE integrations (VS Code, JetBrains) via ACP clients
- Async/streaming operations for long-running tasks
- MCP (Model Context Protocol) client integration
- gRPC transport option for high-throughput scenarios

## Questions?

See `docs/ACP_ARCHITECTURE.md` for technical details, or review the ACP specification in the code comments.
