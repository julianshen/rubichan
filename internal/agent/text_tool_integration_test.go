package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTextFallback_RenderAndParse_RoundTrip verifies that tools rendered via
// RenderToolsAsText produce a format whose XML blocks ParseTextToolCalls can
// understand, and that extractTextToolCalls wraps those results correctly.
func TestTextFallback_RenderAndParse_RoundTrip(t *testing.T) {
	toolDefs := []provider.ToolDef{
		{
			Name:        "shell",
			Description: "Execute commands",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		},
		{
			Name:        "file",
			Description: "Read/write files",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		},
	}

	rendered := tools.RenderToolsAsText(toolDefs)

	assert.Contains(t, rendered, "shell", "rendered text must mention shell tool")
	assert.Contains(t, rendered, "file", "rendered text must mention file tool")
	assert.Contains(t, rendered, "<tool_use>", "rendered text must include example XML block")

	modelResponse := `I'll run the build.

<tool_use>
<name>shell</name>
<input>{"command": "npm run build"}</input>
</tool_use>`

	calls := tools.ParseTextToolCalls(modelResponse)
	require.Len(t, calls, 1)
	assert.Equal(t, "shell", calls[0].Name)

	blocks := extractTextToolCalls(modelResponse)
	require.Len(t, blocks, 1)
	assert.True(t, strings.HasPrefix(blocks[0].ID, "text_call_"), "ID should start with text_call_, got %q", blocks[0].ID)
	assert.Equal(t, "shell", blocks[0].Name)
}

// TestTextFallback_NoToolCallsInResponse verifies that plain text without any
// XML tool-call blocks produces an empty result.
func TestTextFallback_NoToolCallsInResponse(t *testing.T) {
	text := "Here's the answer: the code looks correct, no changes needed."
	calls := extractTextToolCalls(text)
	assert.Empty(t, calls)
}

// TestTextFallback_MultipleToolCalls verifies that multiple sequential
// <tool_use> blocks are all parsed and assigned sequential IDs.
func TestTextFallback_MultipleToolCalls(t *testing.T) {
	text := `<tool_use>
<name>file</name>
<input>{"path": "src/main.ts", "content": "console.log('hello')"}</input>
</tool_use>

<tool_use>
<name>shell</name>
<input>{"command": "npm run build"}</input>
</tool_use>`

	blocks := extractTextToolCalls(text)
	require.Len(t, blocks, 2)
	assert.Equal(t, "file", blocks[0].Name)
	assert.Equal(t, "shell", blocks[1].Name)
	assert.Equal(t, "text_call_1", blocks[0].ID)
	assert.Equal(t, "text_call_2", blocks[1].ID)
}
