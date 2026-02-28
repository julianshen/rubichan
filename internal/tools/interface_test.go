package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolResultDisplayContentFallback(t *testing.T) {
	// When DisplayContent is empty, consumers should fall back to Content.
	r := ToolResult{Content: "LLM-facing content", DisplayContent: ""}
	assert.Equal(t, "LLM-facing content", r.Display())

	// When DisplayContent is set, Display() returns it.
	r2 := ToolResult{Content: "compact", DisplayContent: "rich output"}
	assert.Equal(t, "rich output", r2.Display())
}
