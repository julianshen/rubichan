package toolexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schemaTestTool struct {
	name   string
	schema json.RawMessage
}

func (t *schemaTestTool) Name() string                 { return t.name }
func (t *schemaTestTool) Description() string          { return "test tool" }
func (t *schemaTestTool) InputSchema() json.RawMessage { return t.schema }
func (t *schemaTestTool) Execute(_ context.Context, input json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

type schemaTestLookup struct {
	tools map[string]tools.Tool
}

func (l *schemaTestLookup) Get(name string) (tools.Tool, bool) {
	t, ok := l.tools[name]
	return t, ok
}

func TestSchemaValidationMiddleware_ValidInput(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{
		"echo": &schemaTestTool{
			name:   "echo",
			schema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`),
		},
	}}

	called := false
	base := func(_ context.Context, tc ToolCall) Result {
		called = true
		return Result{Content: "ok"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	result := handler(context.Background(), ToolCall{
		Name:  "echo",
		Input: json.RawMessage(`{"text":"hello"}`),
	})

	assert.True(t, called)
	assert.False(t, result.IsError)
}

func TestSchemaValidationMiddleware_MissingRequired(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{
		"echo": &schemaTestTool{
			name:   "echo",
			schema: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`),
		},
	}}

	called := false
	base := func(_ context.Context, _ ToolCall) Result {
		called = true
		return Result{Content: "ok"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	result := handler(context.Background(), ToolCall{
		Name:  "echo",
		Input: json.RawMessage(`{}`),
	})

	assert.False(t, called, "base handler should not be called on validation failure")
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "missing required field")
}

func TestSchemaValidationMiddleware_InvalidJSON(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{
		"echo": &schemaTestTool{
			name:   "echo",
			schema: json.RawMessage(`{"type":"object"}`),
		},
	}}

	base := func(_ context.Context, _ ToolCall) Result {
		return Result{Content: "ok"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	result := handler(context.Background(), ToolCall{
		Name:  "echo",
		Input: json.RawMessage(`not json`),
	})

	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "JSON object")
}

func TestSchemaValidationMiddleware_UnknownTool(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{}}

	called := false
	base := func(_ context.Context, _ ToolCall) Result {
		called = true
		return Result{Content: "unknown tool"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	_ = handler(context.Background(), ToolCall{
		Name:  "missing",
		Input: json.RawMessage(`{}`),
	})

	assert.True(t, called, "should pass through to next handler for unknown tools")
}

func TestSchemaValidationMiddleware_EmptySchema(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{
		"noschema": &schemaTestTool{name: "noschema", schema: nil},
	}}

	called := false
	base := func(_ context.Context, _ ToolCall) Result {
		called = true
		return Result{Content: "ok"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	_ = handler(context.Background(), ToolCall{
		Name:  "noschema",
		Input: json.RawMessage(`{}`),
	})

	require.True(t, called, "should pass through when no schema is defined")
}

func TestSchemaValidationMiddleware_EmptyInput(t *testing.T) {
	t.Parallel()

	lookup := &schemaTestLookup{tools: map[string]tools.Tool{
		"noreq": &schemaTestTool{
			name:   "noreq",
			schema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		},
	}}

	called := false
	base := func(_ context.Context, _ ToolCall) Result {
		called = true
		return Result{Content: "ok"}
	}

	mw := SchemaValidationMiddleware(lookup)
	handler := mw(base)
	result := handler(context.Background(), ToolCall{
		Name:  "noreq",
		Input: nil,
	})

	assert.True(t, called)
	assert.False(t, result.IsError)
}
