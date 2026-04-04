# Multi-Model Tool Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make rubichan's tool system work reliably across model families (Claude, GPT, Qwen, Llama, Gemma) by adding model-level capability detection, system-prompt tool fallback, model-specific tool filtering, and reduced tool-discovery friction.

**Architecture:** Three layers of improvement: (1) a model capabilities registry that knows what each model family can do, (2) a text-based tool fallback for models without native tool_use support, (3) smarter tool selection that reduces dependence on `tool_search` meta-discovery. Each layer is independently testable and deployable.

**Tech Stack:** Go, JSON schema, existing provider/tools/agent packages. No new dependencies.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/provider/capabilities.go` | `ModelCapabilities` struct + `DetectCapabilities(provider, model)` registry |
| `internal/provider/capabilities_test.go` | Tests for capability detection |
| `internal/tools/text_tool_prompt.go` | Renders tool definitions as system prompt text + parses XML tool calls from text |
| `internal/tools/text_tool_prompt_test.go` | Tests for text prompt rendering and XML parsing |
| `internal/tools/selector.go` | Modify: add model-aware tool filtering + system prompt hints |
| `internal/tools/selector_test.go` | Modify: add tests for new selection behaviors |
| `internal/tools/deferral.go` | Modify: expose tool summary for system prompt injection |
| `internal/tools/deferral_test.go` | Modify: test summary generation |
| `internal/agent/agent.go` | Modify: use capabilities to choose native vs text-based tools |
| `internal/agent/agent_test.go` | Modify: test branching logic |
| `pkg/agentsdk/types.go` | Modify: add `ModelCapabilities` to `CompletionRequest` |
| `cmd/rubichan/main.go` | Modify: pass model name to `DetectCapabilities` |

---

## Task 1: Model Capabilities Registry

**Files:**
- Create: `internal/provider/capabilities.go`
- Create: `internal/provider/capabilities_test.go`
- Modify: `cmd/rubichan/main.go:2106-2165` (replace `ModelCapabilities` and `detectModelCapabilities`)

This task moves `ModelCapabilities` from `cmd/rubichan/main.go` into `internal/provider/` with per-model granularity instead of per-provider.

- [ ] **Step 1: Write failing test for capability detection**

```go
// internal/provider/capabilities_test.go
package provider

import "testing"

func TestDetectCapabilities_Anthropic(t *testing.T) {
	caps := DetectCapabilities("anthropic", "claude-sonnet-4-20250514")
	if !caps.SupportsNativeToolUse {
		t.Error("Claude should support native tool use")
	}
	if !caps.SupportsStreaming {
		t.Error("Claude should support streaming")
	}
	if !caps.SupportsSystemPrompt {
		t.Error("Claude should support system prompts")
	}
}

func TestDetectCapabilities_OpenRouterQwen(t *testing.T) {
	caps := DetectCapabilities("openrouter", "qwen/qwen3.5-397b-a17b")
	if !caps.SupportsNativeToolUse {
		t.Error("Qwen 3.5 via OpenRouter should support native tool use")
	}
}

func TestDetectCapabilities_UnknownModel(t *testing.T) {
	caps := DetectCapabilities("openrouter", "unknown/mystery-model-7b")
	// Unknown models default to optimistic: tool use enabled,
	// but text fallback hint is set.
	if !caps.SupportsNativeToolUse {
		t.Error("unknown models should default to tool-use-capable (optimistic)")
	}
	if !caps.NeedsToolDiscoveryHint {
		t.Error("unknown models should get tool discovery hints")
	}
}

func TestDetectCapabilities_OllamaLocal(t *testing.T) {
	caps := DetectCapabilities("ollama", "llama3.2:3b")
	if !caps.SupportsNativeToolUse {
		t.Error("Ollama models should support native tool use (Ollama handles format)")
	}
	if !caps.NeedsToolDiscoveryHint {
		t.Error("small Ollama models should get tool discovery hints")
	}
	if caps.MaxToolCount != 8 {
		t.Errorf("small models should have reduced tool count, got %d", caps.MaxToolCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/... -run TestDetectCapabilities -v`
Expected: FAIL — `DetectCapabilities` undefined

- [ ] **Step 3: Implement ModelCapabilities and DetectCapabilities**

```go
// internal/provider/capabilities.go
package provider

import "strings"

// ModelCapabilities describes tool-related capabilities of the active model.
// Capabilities are detected per-model (not just per-provider) to handle
// heterogeneous model catalogs on OpenRouter/Ollama.
type ModelCapabilities struct {
	// SupportsNativeToolUse indicates the model can process tool definitions
	// sent via the API's tools[] parameter and return structured tool_use blocks.
	SupportsNativeToolUse bool

	// SupportsStreaming indicates the model supports token-level streaming.
	SupportsStreaming bool

	// SupportsSystemPrompt indicates the model accepts a system prompt.
	SupportsSystemPrompt bool

	// NeedsToolDiscoveryHint indicates the system prompt should include
	// explicit guidance about available tools and how to use tool_search.
	// Set true for smaller/open models that may not proactively discover tools.
	NeedsToolDiscoveryHint bool

	// MaxToolCount limits how many tools are sent to the model. 0 means unlimited.
	// Smaller models perform better with fewer tools (reduced confusion).
	MaxToolCount int

	// PreferBatchEdits indicates the model works better with a single batch-edit
	// tool (like apply_patch) rather than granular edit/write tools.
	PreferBatchEdits bool
}

// DetectCapabilities returns model-level capability flags for the given
// provider and model combination. It uses heuristics based on known model
// families, falling back to sensible defaults for unknown models.
func DetectCapabilities(providerName, modelID string) ModelCapabilities {
	// Normalize for matching.
	model := strings.ToLower(modelID)

	switch providerName {
	case "anthropic":
		return anthropicCapabilities(model)
	case "ollama":
		return ollamaCapabilities(model)
	default:
		// OpenAI-compatible providers (OpenRouter, etc.)
		return openAICompatCapabilities(model)
	}
}

func anthropicCapabilities(model string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse:  true,
		SupportsStreaming:      true,
		SupportsSystemPrompt:   true,
		NeedsToolDiscoveryHint: false,
	}
	// Haiku models benefit from fewer tools.
	if strings.Contains(model, "haiku") {
		caps.NeedsToolDiscoveryHint = true
		caps.MaxToolCount = 12
	}
	return caps
}

func ollamaCapabilities(model string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse:  true,
		SupportsStreaming:      true,
		SupportsSystemPrompt:   true,
		NeedsToolDiscoveryHint: true, // Most Ollama models benefit from hints.
	}
	// Estimate model size from name for tool count limit.
	if isSmallModel(model) {
		caps.MaxToolCount = 8
	} else {
		caps.MaxToolCount = 15
	}
	return caps
}

func openAICompatCapabilities(model string) ModelCapabilities {
	caps := ModelCapabilities{
		SupportsNativeToolUse:  true,
		SupportsStreaming:      true,
		SupportsSystemPrompt:   true,
		NeedsToolDiscoveryHint: false,
	}

	// GPT-4+ class models: full capability.
	if strings.Contains(model, "gpt-4") || strings.Contains(model, "gpt-5") {
		return caps
	}

	// Known capable open models on OpenRouter.
	switch {
	case strings.Contains(model, "claude"):
		return caps // Anthropic via OpenRouter
	case strings.Contains(model, "qwen"):
		caps.NeedsToolDiscoveryHint = true
		if isSmallModel(model) {
			caps.MaxToolCount = 8
		}
	case strings.Contains(model, "llama"),
		strings.Contains(model, "gemma"),
		strings.Contains(model, "mistral"),
		strings.Contains(model, "deepseek"):
		caps.NeedsToolDiscoveryHint = true
		if isSmallModel(model) {
			caps.MaxToolCount = 8
		}
	default:
		// Unknown model: optimistic, with hints.
		caps.NeedsToolDiscoveryHint = true
	}

	return caps
}

// isSmallModel guesses if a model is small (<= ~14B params) based on common
// naming conventions (e.g. "7b", "3b", "9b", "14b").
func isSmallModel(model string) bool {
	small := []string{"1b", "2b", "3b", "4b", "7b", "8b", "9b", "13b", "14b",
		"1.5b", "3.5b", "nano", "mini", "tiny", "small"}
	for _, s := range small {
		if strings.Contains(model, s) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/... -run TestDetectCapabilities -v`
Expected: PASS

- [ ] **Step 5: Write test for isSmallModel edge cases**

```go
func TestIsSmallModel(t *testing.T) {
	tests := []struct {
		model string
		small bool
	}{
		{"llama3.2:3b", true},
		{"qwen/qwen3.5-9b", true},
		{"qwen/qwen3.5-397b-a17b", false},
		{"claude-sonnet-4-20250514", false},
		{"nemotron-nano-9b-v2", true},
		{"deepseek-r1:14b", true},
		{"mistral-large-2411", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := isSmallModel(tt.model); got != tt.small {
				t.Errorf("isSmallModel(%q) = %v, want %v", tt.model, got, tt.small)
			}
		})
	}
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/provider/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/provider/capabilities.go internal/provider/capabilities_test.go
git commit -m "[BEHAVIORAL] Add per-model capability detection registry"
```

---

## Task 2: Wire ModelCapabilities Into CompletionRequest

**Files:**
- Modify: `pkg/agentsdk/types.go:8-16` (add Capabilities to CompletionRequest)
- Modify: `internal/agent/agent.go:1050-1057` (pass capabilities)
- Modify: `cmd/rubichan/main.go:2106-2165` (delegate to new DetectCapabilities)

This task threads the per-model capabilities through the request path so providers and tools can adapt.

- [ ] **Step 1: Write failing test for CompletionRequest.Capabilities**

```go
// pkg/agentsdk/types_test.go (add to existing file or create)
package agentsdk

import "testing"

func TestCompletionRequestHasCapabilities(t *testing.T) {
	req := CompletionRequest{
		Model: "test",
		Capabilities: ModelCapabilities{
			SupportsNativeToolUse: true,
			MaxToolCount:          10,
		},
	}
	if !req.Capabilities.SupportsNativeToolUse {
		t.Error("expected SupportsNativeToolUse to be true")
	}
	if req.Capabilities.MaxToolCount != 10 {
		t.Errorf("expected MaxToolCount 10, got %d", req.Capabilities.MaxToolCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/agentsdk/... -run TestCompletionRequestHasCapabilities -v`
Expected: FAIL — `ModelCapabilities` undefined in agentsdk

- [ ] **Step 3: Add ModelCapabilities to agentsdk and CompletionRequest**

In `pkg/agentsdk/types.go`, add `ModelCapabilities` struct (copy from `internal/provider/capabilities.go` — this is the canonical public version) and add field to `CompletionRequest`:

```go
// After existing types, add:

// ModelCapabilities describes tool-related capabilities of the active model.
type ModelCapabilities struct {
	SupportsNativeToolUse  bool
	SupportsStreaming      bool
	SupportsSystemPrompt   bool
	NeedsToolDiscoveryHint bool
	MaxToolCount           int
	PreferBatchEdits       bool
}

// In CompletionRequest, add after CacheBreakpoints:
//   Capabilities ModelCapabilities
```

Update `internal/provider/capabilities.go` to use the agentsdk type:

```go
// Replace local ModelCapabilities with:
type ModelCapabilities = agentsdk.ModelCapabilities
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/agentsdk/... -run TestCompletionRequestHasCapabilities -v`
Expected: PASS

- [ ] **Step 5: Update cmd/rubichan/main.go to use new DetectCapabilities**

Replace the existing `ModelCapabilities` struct and `detectModelCapabilities` function in `cmd/rubichan/main.go` with a call to the new `provider.DetectCapabilities`:

```go
// In main.go, replace:
//   ModelCapabilities: detectModelCapabilities(cfg),
// With:
//   ModelCapabilities: provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model),

// Delete the old ModelCapabilities struct and detectModelCapabilities function.
// Update ToolsConfig to use provider.ModelCapabilities.
```

- [ ] **Step 6: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add pkg/agentsdk/types.go pkg/agentsdk/types_test.go internal/provider/capabilities.go cmd/rubichan/main.go
git commit -m "[STRUCTURAL] Move ModelCapabilities to agentsdk, wire through CompletionRequest"
```

---

## Task 3: Tool Discovery Hint in System Prompt

**Files:**
- Modify: `internal/tools/deferral.go` (add `ToolSummary()` method)
- Modify: `internal/tools/deferral_test.go` (test summary generation)
- Modify: `internal/agent/agent.go:1032-1057` (inject hint when NeedsToolDiscoveryHint)

When `NeedsToolDiscoveryHint` is true, inject a brief tool inventory into the system prompt so the model knows what tools exist even before seeing the tools[] array. This also guides the model to use `tool_search` for deferred tools.

- [ ] **Step 1: Write failing test for DeferralManager.ToolSummary**

```go
// internal/tools/deferral_test.go (add to existing)
func TestDeferralManager_ToolSummary(t *testing.T) {
	dm := NewDeferralManager(0.10)

	tools := []provider.ToolDef{
		{Name: "shell", Description: "Execute shell commands"},
		{Name: "file", Description: "Read and write files"},
		{Name: "search", Description: "Search files"},
		{Name: "http_get", Description: "Make HTTP GET requests"},
		{Name: "xcode_build", Description: "Build Xcode projects"},
	}

	// Run selection with tiny window so some tools get deferred.
	active, _ := dm.SelectForContext(tools, 500)

	summary := dm.ToolSummary(active)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	// Summary should list active tools.
	if !strings.Contains(summary, "shell") {
		t.Error("summary should mention shell")
	}
	// If tools were deferred, summary should mention tool_search.
	if dm.DeferredCount() > 0 && !strings.Contains(summary, "tool_search") {
		t.Error("summary should mention tool_search when tools are deferred")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestDeferralManager_ToolSummary -v`
Expected: FAIL — `ToolSummary` undefined

- [ ] **Step 3: Implement ToolSummary**

```go
// internal/tools/deferral.go — add method:

// ToolSummary returns a human-readable summary of active and deferred tools
// suitable for injection into the system prompt. This helps models that
// struggle with tool discovery understand what's available.
func (dm *DeferralManager) ToolSummary(activeTools []provider.ToolDef) string {
	var b strings.Builder
	b.WriteString("## Available Tools\n\n")
	b.WriteString("You have these tools available:\n")
	for _, t := range activeTools {
		b.WriteString("- **")
		b.WriteString(t.Name)
		b.WriteString("**: ")
		// Truncate long descriptions to first sentence.
		desc := t.Description
		if idx := strings.Index(desc, ". "); idx > 0 && idx < 120 {
			desc = desc[:idx+1]
		} else if len(desc) > 120 {
			desc = desc[:120] + "..."
		}
		b.WriteString(desc)
		b.WriteString("\n")
	}
	if dm.DeferredCount() > 0 {
		b.WriteString("\nAdditional tools are available but not shown to save context. ")
		b.WriteString("Use the **tool_search** tool with a keyword query to discover them. ")
		b.WriteString("For example: tool_search({\"query\": \"http\"}) to find HTTP tools.\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/... -run TestDeferralManager_ToolSummary -v`
Expected: PASS

- [ ] **Step 5: Wire hint into agent loop**

In `internal/agent/agent.go`, after tool selection (around line 1037), add:

```go
// After: activeTools, _ := a.deferral.SelectForContext(allToolDefs, budget.EffectiveWindow())
// Add:
if a.capabilities.NeedsToolDiscoveryHint {
    toolHint := a.deferral.ToolSummary(activeTools)
    systemPrompt = systemPrompt + "\n\n" + toolHint
}
```

This requires threading `capabilities` into the agent. Add a field to the `Agent` struct:

```go
// In agent struct, add:
capabilities provider.ModelCapabilities
```

And an option function:

```go
func WithCapabilities(caps provider.ModelCapabilities) Option {
    return func(a *Agent) { a.capabilities = caps }
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/agent/... ./internal/tools/... -v -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tools/deferral.go internal/tools/deferral_test.go internal/agent/agent.go
git commit -m "[BEHAVIORAL] Add tool discovery hint in system prompt for small models"
```

---

## Task 4: MaxToolCount Enforcement

**Files:**
- Modify: `internal/tools/selector.go` (add tool count limit)
- Modify: `internal/tools/selector_test.go` (test limit enforcement)

When `MaxToolCount > 0`, limit the number of tools sent to the model. Core tools are always included; non-core tools are trimmed by priority.

- [ ] **Step 1: Write failing test for tool count limiting**

```go
// internal/tools/selector_test.go (add to existing)
func TestSelectForContext_MaxToolCount(t *testing.T) {
	allTools := []provider.ToolDef{
		{Name: "shell", Description: "Execute commands"},
		{Name: "file", Description: "File operations"},
		{Name: "process", Description: "Manage processes"},
		{Name: "tool_search", Description: "Search tools"},
		{Name: "search", Description: "Search files"},
		{Name: "http_get", Description: "HTTP GET"},
		{Name: "git_status", Description: "Git status"},
		{Name: "xcode_build", Description: "Xcode build"},
		{Name: "lsp_hover", Description: "LSP hover"},
		{Name: "db_query", Description: "Database query"},
	}

	// With MaxToolCount=6, should keep core tools + trim non-core.
	selected := ApplyMaxToolCount(allTools, 6)
	if len(selected) > 6 {
		t.Errorf("expected at most 6 tools, got %d", len(selected))
	}
	// Core tools must always be present.
	names := make(map[string]bool)
	for _, td := range selected {
		names[td.Name] = true
	}
	for _, core := range []string{"shell", "file", "process", "tool_search"} {
		if !names[core] {
			t.Errorf("core tool %q must always be included", core)
		}
	}
}

func TestApplyMaxToolCount_ZeroMeansUnlimited(t *testing.T) {
	tools := make([]provider.ToolDef, 20)
	for i := range tools {
		tools[i] = provider.ToolDef{Name: fmt.Sprintf("tool_%d", i)}
	}
	selected := ApplyMaxToolCount(tools, 0)
	if len(selected) != 20 {
		t.Errorf("MaxToolCount=0 should not limit, got %d", len(selected))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestApplyMaxToolCount -v`
Expected: FAIL — `ApplyMaxToolCount` undefined

- [ ] **Step 3: Implement ApplyMaxToolCount**

```go
// internal/tools/selector.go — add function:

// ApplyMaxToolCount limits the tool list to at most maxCount tools.
// Core tools and tool_search are always preserved. Non-core tools are
// trimmed from the end. If maxCount is 0, no limit is applied.
func ApplyMaxToolCount(tools []provider.ToolDef, maxCount int) []provider.ToolDef {
	if maxCount <= 0 || len(tools) <= maxCount {
		return tools
	}

	var core, nonCore []provider.ToolDef
	for _, td := range tools {
		if Categorize(td.Name) == CategoryCore || td.Name == "tool_search" {
			core = append(core, td)
		} else {
			nonCore = append(nonCore, td)
		}
	}

	// If core already exceeds limit, return core only (shouldn't happen in practice).
	if len(core) >= maxCount {
		return core
	}

	remaining := maxCount - len(core)
	if remaining > len(nonCore) {
		remaining = len(nonCore)
	}
	return append(core, nonCore[:remaining]...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/... -run TestApplyMaxToolCount -v`
Expected: PASS

- [ ] **Step 5: Wire into agent loop**

In `internal/agent/agent.go`, after the deferral selection:

```go
// After: activeTools, _ := a.deferral.SelectForContext(allToolDefs, budget.EffectiveWindow())
// Add:
if a.capabilities.MaxToolCount > 0 {
    activeTools = tools.ApplyMaxToolCount(activeTools, a.capabilities.MaxToolCount)
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/tools/... ./internal/agent/... -v -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tools/selector.go internal/tools/selector_test.go internal/agent/agent.go
git commit -m "[BEHAVIORAL] Enforce MaxToolCount for small models to reduce tool confusion"
```

---

## Task 5: Text-Based Tool Fallback (XML Format)

**Files:**
- Create: `internal/tools/text_tool_prompt.go`
- Create: `internal/tools/text_tool_prompt_test.go`

This is the fallback for models that don't support native tool_use. Tool definitions are rendered as text in the system prompt, and the model's text output is parsed for `<tool_use>` XML blocks. This task builds the rendering and parsing utilities only — wiring into the agent loop is Task 6.

- [ ] **Step 1: Write failing test for rendering tool definitions as text**

```go
// internal/tools/text_tool_prompt_test.go
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
)

func TestRenderToolsAsText(t *testing.T) {
	tools := []provider.ToolDef{
		{
			Name:        "shell",
			Description: "Execute a shell command.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The command to run"}},"required":["command"]}`),
		},
		{
			Name:        "file",
			Description: "Read or write a file.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path"]}`),
		},
	}

	text := RenderToolsAsText(tools)

	if !strings.Contains(text, "## Tools") {
		t.Error("should contain Tools header")
	}
	if !strings.Contains(text, "### shell") {
		t.Error("should contain shell tool")
	}
	if !strings.Contains(text, `"command"`) {
		t.Error("should include parameter names")
	}
	if !strings.Contains(text, "<tool_use>") {
		t.Error("should include usage instructions with XML format")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestRenderToolsAsText -v`
Expected: FAIL — `RenderToolsAsText` undefined

- [ ] **Step 3: Implement RenderToolsAsText**

```go
// internal/tools/text_tool_prompt.go
package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// RenderToolsAsText converts tool definitions into a text-based system prompt
// section with XML-formatted usage instructions. This is used as a fallback
// for models that don't support native tool_use API parameters.
func RenderToolsAsText(tools []provider.ToolDef) string {
	var b strings.Builder
	b.WriteString("## Tools\n\n")
	b.WriteString("You have access to the following tools. To call a tool, respond with a tool_use XML block:\n\n")
	b.WriteString("```\n<tool_use>\n<name>TOOL_NAME</name>\n<input>{\"param\": \"value\"}</input>\n</tool_use>\n```\n\n")
	b.WriteString("You may call multiple tools in a single response by including multiple <tool_use> blocks.\n\n")

	for _, td := range tools {
		b.WriteString("### ")
		b.WriteString(td.Name)
		b.WriteString("\n\n")
		b.WriteString(td.Description)
		b.WriteString("\n\n")

		// Render parameters from JSON Schema.
		if len(td.InputSchema) > 0 {
			b.WriteString("**Parameters:**\n")
			renderSchemaParams(&b, td.InputSchema)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderSchemaParams writes a human-readable parameter list from a JSON Schema.
func renderSchemaParams(b *strings.Builder, schema json.RawMessage) {
	var s struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		b.WriteString("(schema unavailable)\n")
		return
	}

	requiredSet := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		requiredSet[r] = true
	}

	for name, prop := range s.Properties {
		req := ""
		if requiredSet[name] {
			req = " **(required)**"
		}
		desc := prop.Description
		if desc == "" {
			desc = prop.Type
		}
		fmt.Fprintf(b, "- `%s` (%s)%s: %s\n", name, prop.Type, req, desc)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/... -run TestRenderToolsAsText -v`
Expected: PASS

- [ ] **Step 5: Write failing test for parsing XML tool calls from text**

```go
func TestParseTextToolCalls(t *testing.T) {
	text := `I'll create the file for you.

<tool_use>
<name>file</name>
<input>{"path": "hello.txt", "content": "Hello, world!"}</input>
</tool_use>

And then run the build:

<tool_use>
<name>shell</name>
<input>{"command": "npm run build"}</input>
</tool_use>`

	calls := ParseTextToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}

	if calls[0].Name != "file" {
		t.Errorf("first call name = %q, want file", calls[0].Name)
	}
	if calls[1].Name != "shell" {
		t.Errorf("second call name = %q, want shell", calls[1].Name)
	}

	// Verify JSON input is valid.
	var input map[string]string
	if err := json.Unmarshal(calls[0].Input, &input); err != nil {
		t.Fatalf("first call input is not valid JSON: %v", err)
	}
	if input["path"] != "hello.txt" {
		t.Errorf("path = %q, want hello.txt", input["path"])
	}
}

func TestParseTextToolCalls_NoToolCalls(t *testing.T) {
	text := "Just a normal response with no tool calls."
	calls := ParseTextToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(calls))
	}
}

func TestParseTextToolCalls_MalformedXML(t *testing.T) {
	text := `<tool_use>
<name>shell</name>
<input>not valid json</input>
</tool_use>`
	calls := ParseTextToolCalls(text)
	// Malformed input should still be captured (agent can report error).
	if len(calls) != 1 {
		t.Fatalf("expected 1 call even with malformed input, got %d", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("name = %q, want shell", calls[0].Name)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestParseTextToolCalls -v`
Expected: FAIL — `ParseTextToolCalls` undefined

- [ ] **Step 7: Implement ParseTextToolCalls**

```go
// internal/tools/text_tool_prompt.go — add:

import (
	"regexp"
	// ... existing imports
)

// TextToolCall represents a tool invocation parsed from text-based XML output.
type TextToolCall struct {
	Name  string
	Input json.RawMessage
}

// toolUsePattern matches <tool_use>...<name>...</name>...<input>...</input>...</tool_use>
// with DOTALL semantics ((?s) flag makes . match newlines).
var toolUsePattern = regexp.MustCompile(`(?s)<tool_use>\s*<name>\s*(\S+?)\s*</name>\s*<input>\s*(.*?)\s*</input>\s*</tool_use>`)

// ParseTextToolCalls extracts tool invocations from a model's text response
// using the XML-based format described in RenderToolsAsText.
func ParseTextToolCalls(text string) []TextToolCall {
	matches := toolUsePattern.FindAllStringSubmatch(text, -1)
	calls := make([]TextToolCall, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		name := strings.TrimSpace(m[1])
		rawInput := strings.TrimSpace(m[2])
		if rawInput == "" {
			rawInput = "{}"
		}
		calls = append(calls, TextToolCall{
			Name:  name,
			Input: json.RawMessage(rawInput),
		})
	}
	return calls
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/tools/... -run TestParseTextToolCalls -v`
Expected: PASS

- [ ] **Step 9: Run all tests**

Run: `go test ./internal/tools/... -v -count=1`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/tools/text_tool_prompt.go internal/tools/text_tool_prompt_test.go
git commit -m "[BEHAVIORAL] Add text-based tool prompt rendering and XML tool call parsing"
```

---

## Task 6: Wire Text-Based Fallback Into Agent Loop

**Files:**
- Modify: `internal/agent/agent.go` (branch on SupportsNativeToolUse)

When `SupportsNativeToolUse` is false, the agent should:
1. NOT send tools in `CompletionRequest.Tools` (send empty)
2. Instead, render tools as text in the system prompt via `RenderToolsAsText`
3. After receiving the model's text response, parse it for `<tool_use>` XML blocks
4. Convert parsed calls into the same `pendingTools` format used by native tool_use

- [ ] **Step 1: Write failing test for text-based tool call extraction in agent**

Create a test helper that simulates a text response with embedded tool_use XML:

```go
// internal/agent/agent_test.go (add to existing)
func TestExtractTextToolCalls(t *testing.T) {
	text := `Let me read that file.

<tool_use>
<name>file</name>
<input>{"path": "main.go", "operation": "read"}</input>
</tool_use>`

	calls := extractTextToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "file" {
		t.Errorf("name = %q, want file", calls[0].Name)
	}
	if calls[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -run TestExtractTextToolCalls -v`
Expected: FAIL — `extractTextToolCalls` undefined

- [ ] **Step 3: Implement extractTextToolCalls and agent branching**

In `internal/agent/agent.go`, add a helper:

```go
import "github.com/julianshen/rubichan/internal/tools"

// extractTextToolCalls parses XML tool_use blocks from text output and converts
// them to ToolUseBlock format with auto-generated IDs.
func extractTextToolCalls(text string) []provider.ToolUseBlock {
	parsed := tools.ParseTextToolCalls(text)
	blocks := make([]provider.ToolUseBlock, len(parsed))
	for i, tc := range parsed {
		blocks[i] = provider.ToolUseBlock{
			ID:    fmt.Sprintf("text_call_%d", i+1),
			Name:  tc.Name,
			Input: tc.Input,
		}
	}
	return blocks
}
```

In the turn execution method (around line 1050), add the branching:

```go
// Before building CompletionRequest:
useNativeTools := a.capabilities.SupportsNativeToolUse

var reqTools []provider.ToolDef
if useNativeTools {
    reqTools = activeTools
}
// If not native, tools are rendered into systemPrompt (already handled by Task 3 hint,
// but for !SupportsNativeToolUse we use full RenderToolsAsText instead of summary).
if !useNativeTools {
    toolPrompt := tools.RenderToolsAsText(activeTools)
    systemPrompt = systemPrompt + "\n\n" + toolPrompt
}

req := provider.CompletionRequest{
    // ...
    Tools: reqTools,  // Empty if !useNativeTools
    // ...
}
```

After stream parsing (around line 1155), add text-based tool extraction:

```go
// After finalizing text and tool from stream:
if !useNativeTools && len(pendingTools) == 0 && currentTextBuf != "" {
    // No native tool calls found — try XML parsing from text.
    textCalls := extractTextToolCalls(currentTextBuf)
    for _, tc := range textCalls {
        pendingTools = append(pendingTools, tc)
        blocks = append(blocks, provider.ContentBlock{
            Type:  "tool_use",
            ID:    tc.ID,
            Name:  tc.Name,
            Input: tc.Input,
        })
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/... -run TestExtractTextToolCalls -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "[BEHAVIORAL] Wire text-based tool fallback for models without native tool_use"
```

---

## Task 7: Wire Capabilities Through cmd/rubichan

**Files:**
- Modify: `cmd/rubichan/main.go` (pass capabilities to agent constructor)

This task threads the detected capabilities from `cmd/rubichan/main.go` through to the agent via `WithCapabilities()`.

- [ ] **Step 1: Update interactive mode agent construction**

In the interactive mode setup (around line 1200-1550), find where `agent.New()` is called and add:

```go
// Where agent options are assembled, add:
caps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)
opts = append(opts, agent.WithCapabilities(caps))
```

- [ ] **Step 2: Update headless mode agent construction**

In the headless mode setup (around line 1650-1750), do the same:

```go
caps := provider.DetectCapabilities(cfg.Provider.Default, cfg.Provider.Model)
opts = append(opts, agent.WithCapabilities(caps))
```

- [ ] **Step 3: Remove old detectModelCapabilities and ModelCapabilities from main.go**

Delete the old `ModelCapabilities` struct (lines 2107-2109), `detectModelCapabilities` function (lines 2143-2165), and update `ToolsConfig` to use `provider.ModelCapabilities`:

```go
type ToolsConfig struct {
    ModelCapabilities provider.ModelCapabilities
    // ... rest unchanged
}
```

- [ ] **Step 4: Run all tests and build**

Run: `go build ./cmd/rubichan && go test ./... 2>&1 | tail -20`
Expected: BUILD SUCCESS, PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/rubichan/main.go internal/agent/agent.go
git commit -m "[STRUCTURAL] Wire DetectCapabilities through interactive and headless agent paths"
```

---

## Task 8: Integration Test — Qwen-Style Model Simulation

**Files:**
- Create: `internal/agent/text_tool_integration_test.go`

Test the full cycle: system prompt has tool definitions → model responds with XML → agent extracts and executes tools.

- [ ] **Step 1: Write integration test with mock provider**

```go
// internal/agent/text_tool_integration_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
)

// mockTextToolProvider simulates a model that returns text with XML tool calls
// instead of native tool_use blocks.
type mockTextToolProvider struct {
	responses []string
	callIdx   int
}

func (m *mockTextToolProvider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 10)
	go func() {
		defer close(ch)
		if m.callIdx < len(m.responses) {
			ch <- provider.StreamEvent{Type: "text_delta", Text: m.responses[m.callIdx]}
			m.callIdx++
		}
		ch <- provider.StreamEvent{Type: "stop"}
	}()
	return ch, nil
}

func TestTextBasedToolFallback_ExtractsXMLToolCalls(t *testing.T) {
	mockProvider := &mockTextToolProvider{
		responses: []string{
			"I'll check the file.\n\n<tool_use>\n<name>file</name>\n<input>{\"path\": \"test.txt\", \"operation\": \"read\"}</input>\n</tool_use>",
		},
	}

	calls := extractTextToolCalls(mockProvider.responses[0])
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "file" {
		t.Errorf("name = %q, want file", calls[0].Name)
	}

	var input map[string]string
	if err := json.Unmarshal(calls[0].Input, &input); err != nil {
		t.Fatalf("invalid input JSON: %v", err)
	}
	if input["path"] != "test.txt" {
		t.Errorf("path = %q, want test.txt", input["path"])
	}
}

func TestTextBasedToolFallback_NoToolCalls(t *testing.T) {
	text := "Here's the answer: 42. No tools needed."
	calls := extractTextToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/agent/... -run TestTextBasedToolFallback -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/text_tool_integration_test.go
git commit -m "[BEHAVIORAL] Add integration tests for text-based tool fallback"
```

---

## Task 9: Improve Hallucination-Resistant Aliases

**Files:**
- Modify: `internal/tools/registry.go` (expand aliases for common hallucinated tool names)
- Modify: `internal/tools/registry_test.go` (test new aliases)

The Qwen 3.5 test showed the model called `tool_shell` and `write_file`. Add more hallucination-resistant aliases based on observed model behavior.

- [ ] **Step 1: Write failing test for new aliases**

```go
// internal/tools/registry_test.go (add to existing)
func TestHallucinationAliases(t *testing.T) {
	reg := NewRegistry()
	// Register canonical tools.
	reg.Register(newStubTool("shell"))
	reg.Register(newStubTool("file"))
	reg.Register(newStubTool("search"))
	reg.RegisterDefaultAliases()

	// Qwen-style hallucinations.
	tests := []struct {
		alias    string
		expected string
	}{
		{"tool_shell", "shell"},
		{"tool_file", "file"},
		{"tool_search", "search"},
		{"run_shell", "shell"},
		{"execute_command", "shell"},
		{"write_file", "file"},
		{"read_file", "file"},
		{"file_write", "file"},
		{"file_read", "file"},
		{"create_file", "file"},
		{"edit_file", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			tool, ok := reg.Get(tt.alias)
			if !ok {
				t.Fatalf("alias %q not resolved", tt.alias)
			}
			if tool.Name() != tt.expected {
				t.Errorf("alias %q resolved to %q, want %q", tt.alias, tool.Name(), tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/... -run TestHallucinationAliases -v`
Expected: FAIL for new aliases not yet registered

- [ ] **Step 3: Expand RegisterDefaultAliases**

In `internal/tools/registry.go`, add the new aliases to `RegisterDefaultAliases()`:

```go
// Add these aliases in RegisterDefaultAliases():
// tool_* prefix pattern (common in Qwen, Llama):
{"tool_shell", "shell"},
{"tool_file", "file"},
{"tool_search", "search"},
{"tool_process", "process"},
// execute variants:
{"execute_command", "shell"},
{"run_command", "shell"},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/... -run TestHallucinationAliases -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./internal/tools/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "[BEHAVIORAL] Expand hallucination-resistant aliases for Qwen/Llama tool names"
```

---

## Task 10: Final Integration — Build + Verify

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

Run: `go test ./... -count=1 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

- [ ] **Step 3: Check formatting**

Run: `gofmt -l .`
Expected: No output (all formatted)

- [ ] **Step 4: Build binary**

Run: `go build -o rubichan_test ./cmd/rubichan`
Expected: BUILD SUCCESS

- [ ] **Step 5: Manual smoke test with Qwen**

Run rubichan with Qwen 3.5 and verify tools are discoverable:

```bash
./rubichan_test --headless --model "qwen/qwen3.5-397b-a17b" \
  --auto-approve --approve-cwd --max-turns 10 --timeout 5m \
  --tools "shell,file,search,tool_search" \
  --prompt "Create a file called hello.txt with the content 'Hello from Qwen'"
```

Expected: File created successfully (model should find tools via system prompt hint).

- [ ] **Step 6: Commit any final fixes**

```bash
git add -A
git commit -m "[BEHAVIORAL] Final integration fixes for multi-model tool compatibility"
```
