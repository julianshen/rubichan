# Agent Definitions with Tool Filtering

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Formalize agent modes as first-class definitions with tool filtering, custom models, and custom prompts. Replace ad-hoc mode checks with a declarative agent registry.

**Architecture:** Port ccgo's `agent/agent.go:77-116` and `agent/filter.go:7-91`. An `AgentDefinition` struct describes an agent's capabilities. An `AgentRegistry` manages built-in and custom definitions. Tool filtering uses allow/deny/wildcard patterns.

**Tech Stack:** Go, existing `agentsdk.Tool` interface, TOML config.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `pkg/agentsdk/agent_def.go` | `AgentDefinition`, `AgentMode`, `ToolFilter` |
| `internal/agent/registry.go` | `AgentRegistry`, built-in definitions, custom discovery |
| `internal/agent/filter.go` | `FilterTools`, wildcard matching, deny set |

---

## Chunk 1: Agent Definition Types

### Task 1: Define AgentDefinition and AgentMode

**Files:**
- Create: `pkg/agentsdk/agent_def.go`

**Code:**

```go
package agentsdk

// AgentMode is a built-in agent type.
type AgentMode string

const (
	// AgentModeGeneralPurpose has all tools, inherits parent model.
	AgentModeGeneralPurpose AgentMode = "general-purpose"
	// AgentModeExplore is read-only + shell, uses fast model.
	AgentModeExplore AgentMode = "explore"
	// AgentModePlan is read-only planning, no CLAUDE.md.
	AgentModePlan AgentMode = "plan"
	// AgentModeVerification is background checking agent.
	AgentModeVerification AgentMode = "verification"
)

// AgentDefinition describes an agent's capabilities and constraints.
type AgentDefinition struct {
	// Name is the agent identifier (e.g., "explore", "custom-docs").
	Name string
	// Mode is the built-in type. Empty for custom agents.
	Mode AgentMode
	// Description explains the agent's purpose.
	Description string
	// Tools is the tool filter. ["*"] means all tools.
	Tools []string
	// DisallowedTools removes tools from the wildcard set.
	DisallowedTools []string
	// Model overrides the parent model. "inherit" uses parent's model.
	Model string
	// SystemPrompt is prepended to the conversation.
	SystemPrompt string
	// OmitCLAUDEMd excludes CLAUDE.md from context.
	OmitCLAUDEMd bool
	// MaxTurns caps the agent's execution. 0 means inherit.
	MaxTurns int
}

// IsCoordinator returns true if this agent only runs coordinator tools
// (subagent spawn, task stop, message send).
func (d *AgentDefinition) IsCoordinator() bool {
	if len(d.Tools) != 3 {
		return false
	}
	// Check for exact coordinator tool set
	coordTools := map[string]bool{"Agent": true, "TaskStop": true, "SendMessage": true}
	for _, t := range d.Tools {
		if !coordTools[t] {
			return false
		}
	}
	return true
}
```

**Test:**

```go
func TestAgentDefinitionIsCoordinator(t *testing.T) {
	coord := &AgentDefinition{Tools: []string{"Agent", "TaskStop", "SendMessage"}}
	require.True(t, coord.IsCoordinator())
	
	general := &AgentDefinition{Tools: []string{"*"}}
	require.False(t, general.IsCoordinator())
}
```

**Command:**
```bash
go test ./pkg/agentsdk/... -run TestAgentDefinitionIsCoordinator -v
```

**Expected:** PASS.

---

## Chunk 2: Tool Filtering

### Task 2: Implement tool filtering with wildcard support

**Files:**
- Create: `internal/agent/filter.go`

**Code:**

```go
package agent

import (
	"strings"
	
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// FilterTools applies an agent definition's tool filter to the full
// tool registry. Supports wildcard mode, explicit allow, and deny sets.
func FilterTools(
	allTools []tools.ToolDef,
	def *agentsdk.AgentDefinition,
	globallyDisallowed []string,
) []tools.ToolDef {
	// Build deny set
	deny := make(map[string]bool)
	for _, t := range globallyDisallowed {
		deny[t] = true
	}
	for _, t := range def.DisallowedTools {
		deny[t] = true
	}
	
	// Coordinator mode: only coordinator tools
	if def.IsCoordinator() {
		return filterByNames(allTools, def.Tools, deny)
	}
	
	// Wildcard mode: all tools except denied
	if len(def.Tools) == 1 && def.Tools[0] == "*" {
		return filterDenied(allTools, deny)
	}
	
	// Explicit allow mode
	return filterByNames(allTools, def.Tools, deny)
}

func filterDenied(all []tools.ToolDef, deny map[string]bool) []tools.ToolDef {
	var out []tools.ToolDef
	for _, t := range all {
		if !deny[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

func filterByNames(all []tools.ToolDef, allow []string, deny map[string]bool) []tools.ToolDef {
	allowSet := make(map[string]bool)
	for _, name := range allow {
		allowSet[name] = true
	}
	
	var out []tools.ToolDef
	for _, t := range all {
		if allowSet[t.Name] && !deny[t.Name] {
			out = append(out, t)
		}
	}
	return out
}
```

**Test:**

```go
func TestFilterToolsWildcard(t *testing.T) {
	all := []tools.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}
	
	def := &agentsdk.AgentDefinition{Tools: []string{"*"}, DisallowedTools: []string{"shell"}}
	filtered := FilterTools(all, def, nil)
	
	require.Len(t, filtered, 2)
	require.Equal(t, "read_file", filtered[0].Name)
	require.Equal(t, "write_file", filtered[1].Name)
}

func TestFilterToolsExplicitAllow(t *testing.T) {
	all := []tools.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}
	
	def := &agentsdk.AgentDefinition{Tools: []string{"read_file", "grep"}}
	filtered := FilterTools(all, def, nil)
	
	require.Len(t, filtered, 1)
	require.Equal(t, "read_file", filtered[0].Name)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestFilterTools -v
```

**Expected:** Tests fail — filter not yet wired.

---

## Chunk 3: Agent Registry

### Task 3: Implement AgentRegistry with built-in definitions

**Files:**
- Create: `internal/agent/registry.go`

**Code:**

```go
package agent

import (
	"sync"
	
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// AgentRegistry manages built-in and custom agent definitions.
type AgentRegistry struct {
	mu       sync.RWMutex
	agents   map[string]*agentsdk.AgentDefinition
	builtIns map[string]*agentsdk.AgentDefinition
}

// NewAgentRegistry creates a registry with built-in definitions.
func NewAgentRegistry() *AgentRegistry {
	r := &AgentRegistry{
		agents:   make(map[string]*agentsdk.AgentDefinition),
		builtIns: make(map[string]*agentsdk.AgentDefinition),
	}
	r.registerBuiltIns()
	return r
}

func (r *AgentRegistry) registerBuiltIns() {
	r.builtIns["general-purpose"] = &agentsdk.AgentDefinition{
		Name:        "general-purpose",
		Mode:        agentsdk.AgentModeGeneralPurpose,
		Description: "All-purpose agent with full tool access",
		Tools:       []string{"*"},
		Model:       "inherit",
	}
	
	r.builtIns["explore"] = &agentsdk.AgentDefinition{
		Name:        "explore",
		Mode:        agentsdk.AgentModeExplore,
		Description: "Fast read-only exploration agent",
		Tools:       []string{"read_file", "grep", "glob", "list_dir", "shell"},
		Model:       "haiku",
		OmitCLAUDEMd: true,
	}
	
	r.builtIns["plan"] = &agentsdk.AgentDefinition{
		Name:        "plan",
		Mode:        agentsdk.AgentModePlan,
		Description: "Planning agent with read-only tools",
		Tools:       []string{"read_file", "grep", "glob", "list_dir"},
		Model:       "inherit",
		OmitCLAUDEMd: true,
	}
}

// Get returns an agent definition by name. Checks custom agents first,
// then built-ins.
func (r *AgentRegistry) Get(name string) (*agentsdk.AgentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if def, ok := r.agents[name]; ok {
		return def, true
	}
	def, ok := r.builtIns[name]
	return def, ok
}

// Register adds a custom agent definition.
func (r *AgentRegistry) Register(def *agentsdk.AgentDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	if def.Name == "" {
		return fmt.Errorf("agent definition name is required")
	}
	
	r.agents[def.Name] = def
	return nil
}
```

**Test:**

```go
func TestAgentRegistry(t *testing.T) {
	r := NewAgentRegistry()
	
	// Built-in exists
	def, ok := r.Get("explore")
	require.True(t, ok)
	require.Equal(t, agentsdk.AgentModeExplore, def.Mode)
	
	// Custom registration
	custom := &agentsdk.AgentDefinition{Name: "custom", Tools: []string{"read_file"}}
	require.NoError(t, r.Register(custom))
	
	got, ok := r.Get("custom")
	require.True(t, ok)
	require.Equal(t, "read_file", got.Tools[0])
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgentRegistry -v
```

**Expected:** PASS.

---

## Chunk 4: Integration

### Task 4: Wire AgentRegistry into Agent

**Files:**
- Modify: `internal/agent/agent.go`

**Code:**

```go
type Agent struct {
	// ... existing fields ...
	
	// agentDef is the active agent definition. Defaults to general-purpose.
	agentDef *agentsdk.AgentDefinition
	// agentRegistry holds built-in and custom agent definitions.
	agentRegistry *AgentRegistry
}

// WithAgentDefinition sets the active agent definition.
func WithAgentDefinition(def *agentsdk.AgentDefinition) AgentOption {
	return func(a *Agent) {
		a.agentDef = def
	}
}

func New(provider provider.Provider, registry *tools.Registry, autoApprove ApprovalFunc, cfg *config.AgentConfig, opts ...AgentOption) *Agent {
	// ... existing init ...
	a := &Agent{
		// ... existing fields ...
		agentDef:      &agentsdk.AgentDefinition{Name: "general-purpose", Tools: []string{"*"}},
		agentRegistry: NewAgentRegistry(),
	}
	// ... apply opts ...
}

// SelectTools returns tools filtered by the active agent definition.
func (a *Agent) SelectTools(all []tools.ToolDef) []tools.ToolDef {
	return FilterTools(all, a.agentDef, nil)
}
```

**Command:**
```bash
go test ./internal/agent/... -run TestAgent -v
```

**Expected:** Existing tests pass.

---

## Validation Commands

```bash
go test ./pkg/agentsdk/...
go test ./internal/agent/...
go test -cover ./internal/agent/...
golangci-lint run ./internal/agent/...
gofmt -l .
```

---

## PR Description

**Title:** `[STRUCTURAL] Agent definitions with tool filtering`

**Body:**
- Formalize agent modes as `AgentDefinition` structs with tool filtering
- Built-in agents: general-purpose, explore, plan, verification
- Tool filtering: wildcard (`["*"]`), explicit allow, deny sets
- `AgentRegistry` manages built-in and custom definitions
- Ports ccgo's `agent/agent.go:77-116` and `agent/filter.go:7-91` patterns

**Commit prefix:** `[STRUCTURAL]`
