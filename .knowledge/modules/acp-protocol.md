---
id: mod-acp-protocol
kind: module
title: ACP Protocol
tags: [protocol, communication, agent]
source: manual
confidence: 0.9
---

The Agent Client Protocol (ACP) is the standardized backbone for agent-mode communication in Rubichan.

Key Components:
- Server: internal/acp/server.go with stdio transport
- Types: internal/acp/types.go with JSON-RPC 2.0
- Dispatcher: internal/acp/dispatcher.go for request-response correlation
