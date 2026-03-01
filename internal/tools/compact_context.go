package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// CompactResult mirrors agent.CompactResult to avoid circular import.
type CompactResult struct {
	BeforeTokens   int
	AfterTokens    int
	BeforeMsgCount int
	AfterMsgCount  int
	StrategiesRun  []string
}

// Compactor can force a context compaction.
type Compactor interface {
	ForceCompact(ctx context.Context) CompactResult
}

// CompactContextTool allows the LLM to proactively compress its context.
type CompactContextTool struct {
	compactor Compactor
}

// NewCompactContextTool creates a new CompactContextTool.
func NewCompactContextTool(c Compactor) *CompactContextTool {
	return &CompactContextTool{compactor: c}
}

func (t *CompactContextTool) Name() string { return "compact_context" }

func (t *CompactContextTool) Description() string {
	return "Compress conversation context to free space. Call before large operations when context usage is high."
}

func (t *CompactContextTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *CompactContextTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	result := t.compactor.ForceCompact(ctx)
	summary := fmt.Sprintf(
		"Compacted context from %d to %d tokens (%d messages -> %d). Strategies: %v",
		result.BeforeTokens, result.AfterTokens,
		result.BeforeMsgCount, result.AfterMsgCount,
		result.StrategiesRun,
	)
	return ToolResult{Content: summary}, nil
}
