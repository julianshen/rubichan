package mcpbackend

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPBackendLoadAndTools(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			// Initialize
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"test-server","version":"1.0"}}}`),
			// ListTools
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"read_file","description":"Read a file","inputSchema":{"type":"object"}}]}}`),
		},
	}

	backend := NewMCPBackend("test-server", mt)

	manifest := skills.SkillManifest{
		Name:        "mcp-test-server",
		Version:     "1.0.0",
		Description: "MCP test server",
		Types:       []skills.SkillType{skills.SkillTypeTool},
	}

	err := backend.Load(manifest, &noopChecker{})
	require.NoError(t, err)

	tools := backend.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, "mcp_test-server_read_file", tools[0].Name())
	assert.Equal(t, "Read a file", tools[0].Description())
}

func TestMCPBackendUnload(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`),
		},
	}

	backend := NewMCPBackend("test", mt)
	manifest := skills.SkillManifest{
		Name: "mcp-test", Version: "1.0.0", Description: "test",
		Types: []skills.SkillType{skills.SkillTypeTool},
	}

	require.NoError(t, backend.Load(manifest, &noopChecker{}))
	err := backend.Unload()
	assert.NoError(t, err)
	assert.Empty(t, backend.Tools())
}

func TestMCPBackendLoadInitializeError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			// Malformed Initialize response triggers error
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request"}}`),
		},
	}

	backend := NewMCPBackend("broken", mt)
	err := backend.Load(skills.SkillManifest{Name: "test"}, &noopChecker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialize MCP server")
}

func TestMCPBackendLoadListToolsError(t *testing.T) {
	mt := &mockTransport{
		responses: []json.RawMessage{
			// Initialize succeeds
			json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test","version":"1.0"}}}`),
			// ListTools returns error
			json.RawMessage(`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}`),
		},
	}

	backend := NewMCPBackend("broken", mt)
	err := backend.Load(skills.SkillManifest{Name: "test"}, &noopChecker{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list MCP tools")
}

func TestMCPBackendUnloadWithNilClient(t *testing.T) {
	backend := NewMCPBackend("test", nil)
	err := backend.Unload()
	assert.NoError(t, err)
	assert.Nil(t, backend.Tools())
}

func TestMCPBackendHooksReturnsNil(t *testing.T) {
	backend := NewMCPBackend("test", nil)
	assert.Nil(t, backend.Hooks())
}

// mockTransport for tests.
type mockTransport struct {
	responses []json.RawMessage
	idx       int
}

func (m *mockTransport) Send(_ context.Context, _ any) error { return nil }

func (m *mockTransport) Receive(_ context.Context, result any) error {
	if m.idx >= len(m.responses) {
		return nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return json.Unmarshal(resp, result)
}

func (m *mockTransport) Close() error { return nil }

func TestNewMCPBackendFromConfigUnsupportedTransport(t *testing.T) {
	_, err := NewMCPBackendFromConfig(context.Background(), "test-server", "websocket", "", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

func TestNewMCPBackendFromConfigStdioRequiresCommand(t *testing.T) {
	_, err := NewMCPBackendFromConfig(context.Background(), "test-server", "stdio", "", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a command")
}

func TestNewMCPBackendFromConfigSSERequiresURL(t *testing.T) {
	_, err := NewMCPBackendFromConfig(context.Background(), "test-server", "sse", "", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a url")
}

type noopChecker struct{}

func (n *noopChecker) CheckPermission(_ skills.Permission) error { return nil }
func (n *noopChecker) CheckRateLimit(_ string) error             { return nil }
func (n *noopChecker) ResetTurnLimits()                          {}
