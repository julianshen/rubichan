package agentsdk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccumulatorTextCommitsOnFinish(t *testing.T) {
	acc := NewStreamAccumulator()
	isTool := acc.AddText("hello ")
	assert.False(t, isTool)
	acc.AddText("world")
	acc.Finish()

	blocks := acc.Blocks()
	require.Len(t, blocks, 1)
	assert.Equal(t, "text", blocks[0].Type)
	assert.Equal(t, "hello world", blocks[0].Text)
	assert.Empty(t, acc.PendingTools())
}

func TestAccumulatorToolInputViaTextDeltas(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.AddText("thinking out loud... ")
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})

	// Text deltas during tool accumulation carry input JSON.
	isTool := acc.AddText(`{"command":`)
	assert.True(t, isTool)
	acc.AddText(`"ls"}`)
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "t1", tools[0].ID)
	assert.JSONEq(t, `{"command":"ls"}`, string(tools[0].Input))

	// Blocks: text first, then tool_use.
	blocks := acc.Blocks()
	require.Len(t, blocks, 2)
	assert.Equal(t, "text", blocks[0].Type)
	assert.Equal(t, "tool_use", blocks[1].Type)
	assert.Equal(t, "shell", blocks[1].Name)
}

func TestAccumulatorToolInputPrePopulated(t *testing.T) {
	acc := NewStreamAccumulator()
	input := json.RawMessage(`{"path":"main.go"}`)
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "file", Input: input})
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 1)
	assert.JSONEq(t, `{"path":"main.go"}`, string(tools[0].Input))
}

func TestAccumulatorStartToolCopiesInput(t *testing.T) {
	acc := NewStreamAccumulator()
	input := json.RawMessage(`{"a":1}`)
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "x", Input: input})
	input[1] = 'z' // mutate the caller's slice after StartTool
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 1)
	assert.JSONEq(t, `{"a":1}`, string(tools[0].Input))
}

func TestAccumulatorMultipleToolsFinalizeOnNextStart(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "a", Input: json.RawMessage(`{}`)})
	acc.StartTool(ToolUseBlock{ID: "t2", Name: "b", Input: json.RawMessage(`{}`)})
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 2)
	assert.Equal(t, "t1", tools[0].ID)
	assert.Equal(t, "t2", tools[1].ID)
}

func TestAccumulatorAddToolInput(t *testing.T) {
	acc := NewStreamAccumulator()
	// Outside a tool, AddToolInput is a no-op.
	assert.False(t, acc.AddToolInput(`{"x":1}`))

	acc.StartTool(ToolUseBlock{ID: "t1", Name: "a"})
	assert.True(t, acc.AddToolInput(`{"x":`))
	assert.True(t, acc.AddToolInput(`1}`))
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 1)
	assert.JSONEq(t, `{"x":1}`, string(tools[0].Input))
}

func TestAccumulatorKeepTextPredicate(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.KeepText = func(s string) bool { return strings.TrimSpace(s) != "" }
	acc.AddText("   \n\t  ")
	acc.Finish()
	assert.Empty(t, acc.Blocks(), "whitespace-only text dropped by predicate")

	acc2 := NewStreamAccumulator()
	// Default: any non-empty string is kept, including whitespace.
	acc2.AddText("   ")
	acc2.Finish()
	require.Len(t, acc2.Blocks(), 1)
}

func TestAccumulatorRejectedTextStaysBuffered(t *testing.T) {
	// Historical loop behavior: text the predicate rejects is not cleared —
	// it prefixes text that streams after an intervening tool call.
	acc := NewStreamAccumulator()
	acc.KeepText = func(s string) bool { return strings.TrimSpace(s) != "" }

	acc.AddText("  \n")
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "a", Input: json.RawMessage(`{}`)})
	acc.FinalizeTool()
	acc.AddText("real text")
	acc.Finish()

	blocks := acc.Blocks()
	require.Len(t, blocks, 2)
	assert.Equal(t, "tool_use", blocks[0].Type)
	assert.Equal(t, "text", blocks[1].Type)
	assert.Equal(t, "  \nreal text", blocks[1].Text)
}

func TestAccumulatorFinalizeToolMidStream(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "a", Input: json.RawMessage(`{}`)})
	acc.FinalizeTool() // content_block_stop

	// Tool is committed before the stream ends.
	require.Len(t, acc.PendingTools(), 1)
	assert.False(t, acc.HasPartialTool())

	// Subsequent text is normal text, not tool input.
	isTool := acc.AddText("after")
	assert.False(t, isTool)
}

func TestAccumulatorOnToolFinalizedHook(t *testing.T) {
	acc := NewStreamAccumulator()
	var hookTool ToolUseBlock
	var pendingAtHook int
	acc.OnToolFinalized = func(tc ToolUseBlock) {
		hookTool = tc
		// Invariant: the tool is already in PendingTools when the hook fires.
		pendingAtHook = len(acc.PendingTools())
	}
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})
	acc.AddText(`{"command":"ls"}`)
	acc.Finish()

	assert.Equal(t, "t1", hookTool.ID)
	assert.JSONEq(t, `{"command":"ls"}`, string(hookTool.Input))
	assert.Equal(t, 1, pendingAtHook)
}

func TestAccumulatorDropInvalidPartialTool(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})
	acc.AddText(`{"command": "truncat`) // stream cut mid-JSON

	assert.True(t, acc.HasPartialTool())
	dropped := acc.DropInvalidPartialTool()
	assert.True(t, dropped)
	acc.Finish()

	assert.Empty(t, acc.PendingTools())
	assert.Empty(t, acc.Blocks())
}

func TestAccumulatorDropInvalidPartialToolKeepsValid(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})
	acc.AddText(`{"command":"ls"}`)

	dropped := acc.DropInvalidPartialTool()
	assert.False(t, dropped, "valid JSON input must not be dropped")
	acc.Finish()
	assert.Len(t, acc.PendingTools(), 1)
}

func TestAccumulatorDropInvalidPartialToolNoInput(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell", Input: json.RawMessage(`{}`)})

	// No streamed input buffer — nothing to validate, tool kept.
	dropped := acc.DropInvalidPartialTool()
	assert.False(t, dropped)
	acc.Finish()
	assert.Len(t, acc.PendingTools(), 1)
}

func TestAccumulatorCurrentText(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.AddText("partial answer")
	assert.Equal(t, "partial answer", acc.CurrentText())
	acc.Finish()
	assert.Equal(t, "", acc.CurrentText(), "buffer cleared after finish")
}

func TestAccumulatorReset(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.AddText("some text")
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})
	acc.AddText(`{"comm`)
	acc.Reset()

	acc.Finish()
	assert.Empty(t, acc.Blocks())
	assert.Empty(t, acc.PendingTools())
	assert.False(t, acc.HasPartialTool())
	assert.Equal(t, "", acc.CurrentText())
}

func TestAccumulatorHasPartialTool(t *testing.T) {
	acc := NewStreamAccumulator()
	assert.False(t, acc.HasPartialTool())
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "shell"})
	assert.True(t, acc.HasPartialTool())
	acc.Finish()
	assert.False(t, acc.HasPartialTool())
}

func TestAccumulatorEmptyToolInputBufLeavesInputUntouched(t *testing.T) {
	// When no input deltas arrive, the Input provided at StartTool survives
	// (matches both loops: toolInputBuf overrides only when non-empty).
	acc := NewStreamAccumulator()
	acc.StartTool(ToolUseBlock{ID: "t1", Name: "a", Input: json.RawMessage(`{"k":"v"}`)})
	acc.Finish()

	tools := acc.PendingTools()
	require.Len(t, tools, 1)
	assert.JSONEq(t, `{"k":"v"}`, string(tools[0].Input))
}
