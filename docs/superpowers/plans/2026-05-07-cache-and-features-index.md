# Implementation Plans Index

All plans created from ccgo/Claude Code research for cache mechanisms and additional features.

---

## Cache & Performance

| # | Plan | File | Priority | Description |
|---|------|------|----------|-------------|
| C1 | Prompt Cache Break Detection | `2026-05-07-prompt-cache-break-detection.md` | HIGH | Two-phase tracking (pre/post call) with diagnosis when cache read tokens drop |
| C2 | Tool Schema Cache | `2026-05-07-tool-schema-cache.md` | HIGH | Session-scoped cache preventing tool re-rendering, preserving prompt cache |
| C3 | Streaming Stall Detection | `2026-05-07-streaming-stall-detection.md` | MEDIUM | Per-stream watchdog monitoring SSE event gaps, warning at 30s |

## Context Management

| # | Plan | File | Priority | Description |
|---|------|------|----------|-------------|
| C4 | Cached Microcompact | `2026-05-07-cached-microcompact.md` | HIGH | Anthropic cache_edits API to remove tool results without cache invalidation |
| C5 | Forked Agent Pattern | `2026-05-07-forked-agent-pattern.md` | HIGH | Subagents sharing parent's prompt cache via identical cache-key params |
| C6 | Message ID Tags | `2026-05-07-message-id-tags.md` | LOW | [id:xxxxx] injection for cross-referencing messages |

## Ecosystem Integration

| # | Plan | File | Priority | Description |
|---|------|------|----------|-------------|
| C7 | MCP Support | `2026-05-07-mcp-support.md` | MEDIUM | Model Context Protocol client (stdio/SSE/HTTP/WS transports) |
| C8 | LSP Integration | `2026-05-07-lsp-integration.md` | MEDIUM | Language Server Protocol client for diagnostics |

---

## Dependency Graph

```
C1: Prompt Cache Break Detection (no deps)
C2: Tool Schema Cache (no deps)
  ↓
C3: Streaming Stall Detection (no deps)

C4: Cached Microcompact (depends on Anthropic provider cache_control support — already exists)
C5: Forked Agent Pattern (no deps)
C6: Message ID Tags (no deps)

C7: MCP Support (no deps)
C8: LSP Integration (no deps)
```

## Recommended Execution Order

1. **C2** (tool schema cache) — simple, high impact on cache stability
2. **C1** (cache break detection) — builds on existing cache tracking
3. **C3** (streaming stall detection) — reliability improvement
4. **C5** (forked agent) — enables efficient summarization
5. **C4** (cached microcompact) — advanced, builds on cache_edits API
6. **C6** (message ID tags) — optional, low priority
7. **C7** (MCP support) — ecosystem expansion
8. **C8** (LSP integration) — ecosystem expansion

## Already Ported (from previous work)

- File Read Cache ✅
- Token Budget Tracker ✅
- Session Memory ✅
- Auto-Dream ✅
- Agent Summaries ✅
- CollapseStore ✅
- Classifier Improvements ✅
- Tmux Display ✅
- Mailbox/Coordinator ✅
- Prompt Cache API (system blocks with cache_control) ✅
- Non-streaming fallback ✅
- Streaming idle timeout (45s warn, 90s kill) ✅

## Out of Scope

- Voice Mode (requires WebSocket STT infrastructure)
- Magic Docs (requires markdown doc automation)
- Advisor Tool (requires server-side routing)
- VCR Recording (testing utility)
- Plugin System (requires package management)
- Remote Sessions (requires WebSocket bridging)
- Skill Search (requires semantic search infrastructure)
- Prompt Suggestion (requires speculative generation)
