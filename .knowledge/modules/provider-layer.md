---
id: mod-provider-layer
kind: module
title: Provider Layer
tags: [llm, provider, api]
source: manual
confidence: 0.9
---

LLM abstraction over Anthropic, OpenAI, Ollama. Custom HTTP+SSE, no vendor SDKs (ADR-006).

Providers:
- Anthropic (Claude)
- OpenAI (GPT)
- Ollama (local models)
- OpenRouter (aggregator)
