package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/julianshen/rubichan/internal/tools"
)

// CapabilityBroker enforces per-call capability checks for skill tool
// execution. All skill backends route tool calls through a broker so
// that approval, sandboxing, and auditing apply uniformly regardless of
// backend type (Starlark, Go plugin, process, MCP).
type CapabilityBroker interface {
	// CheckExecution validates that the given tool call is permitted
	// under the skill's declared permissions. Returns nil if allowed,
	// or an error describing which capability was denied.
	CheckExecution(ctx context.Context, toolName string, input json.RawMessage) error
}

// DefaultCapabilityBroker checks all declared permissions via the
// skill's PermissionChecker before each tool execution.
type DefaultCapabilityBroker struct {
	skillName string
	checker   PermissionChecker
	perms     []Permission
}

// NewCapabilityBroker creates a broker that enforces the given
// permissions on every tool call for the named skill.
func NewCapabilityBroker(skillName string, checker PermissionChecker, perms []Permission) *DefaultCapabilityBroker {
	permsCopy := make([]Permission, len(perms))
	copy(permsCopy, perms)
	return &DefaultCapabilityBroker{
		skillName: skillName,
		checker:   checker,
		perms:     permsCopy,
	}
}

// CheckExecution validates that all declared permissions are still
// granted. This catches revocations that happen after activation.
func (b *DefaultCapabilityBroker) CheckExecution(_ context.Context, toolName string, _ json.RawMessage) error {
	for _, perm := range b.perms {
		if err := b.checker.CheckPermission(perm); err != nil {
			return fmt.Errorf("skill %q tool %q: capability %s denied: %w", b.skillName, toolName, perm, err)
		}
	}
	return nil
}

// BrokeredTool wraps a tools.Tool with a CapabilityBroker that checks
// permissions before each execution. This ensures external backends
// (process, MCP) that don't self-enforce permissions are still gated.
type BrokeredTool struct {
	inner  tools.Tool
	broker CapabilityBroker
}

// NewBrokeredTool wraps a tool with capability enforcement.
func NewBrokeredTool(inner tools.Tool, broker CapabilityBroker) *BrokeredTool {
	return &BrokeredTool{inner: inner, broker: broker}
}

// Name returns the wrapped tool's name.
func (bt *BrokeredTool) Name() string { return bt.inner.Name() }

// Description returns the wrapped tool's description.
func (bt *BrokeredTool) Description() string { return bt.inner.Description() }

// InputSchema returns the wrapped tool's input schema.
func (bt *BrokeredTool) InputSchema() json.RawMessage { return bt.inner.InputSchema() }

// Execute checks capabilities via the broker, then delegates to the
// wrapped tool. If the broker denies, the tool is not executed and an
// error result is returned.
func (bt *BrokeredTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	if err := bt.broker.CheckExecution(ctx, bt.inner.Name(), input); err != nil {
		return tools.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return bt.inner.Execute(ctx, input)
}

// Inner returns the wrapped tool, useful for testing.
func (bt *BrokeredTool) Inner() tools.Tool { return bt.inner }
