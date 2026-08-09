package a2a

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTool satisfies agentsdk.Tool with the minimum needed for
// BuildAgentCard, which only reads Name() and Description().
type fakeTool struct{ name, desc string }

func (f fakeTool) Name() string                 { return f.name }
func (f fakeTool) Description() string          { return f.desc }
func (f fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f fakeTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{}, nil
}

func TestBuildAgentCard(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeTool{name: "file", desc: "read/write files"}))
	require.NoError(t, reg.Register(fakeTool{name: "shell", desc: "run shell commands"}))

	card := BuildAgentCard(reg, "rubichan", "a coding agent", "https://example.com")

	assert.Equal(t, "rubichan", card.Name)
	assert.Equal(t, "https://example.com", card.URL)
	assert.True(t, card.Capabilities.Streaming)
	assert.Len(t, card.Skills, 2)
}
