package mcpbackend

import (
	"context"
	"fmt"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	mcpclient "github.com/julianshen/rubichan/internal/tools/mcp"
)

// MCPBackend implements skills.SkillBackend for MCP-discovered skills.
// It connects to an MCP server, discovers its tools, and wraps them as
// tools.Tool instances that can be registered in the tool registry.
type MCPBackend struct {
	serverName string
	transport  mcpclient.Transport
	client     *mcpclient.Client
	tools      []tools.Tool
}

// compile-time check: MCPBackend implements skills.SkillBackend.
var _ skills.SkillBackend = (*MCPBackend)(nil)

// NewMCPBackend creates a new MCP-backed skill backend.
func NewMCPBackend(serverName string, transport mcpclient.Transport) *MCPBackend {
	return &MCPBackend{
		serverName: serverName,
		transport:  transport,
	}
}

// Load connects to the MCP server and discovers its tools.
func (b *MCPBackend) Load(_ skills.SkillManifest, _ skills.PermissionChecker) error {
	b.client = mcpclient.NewClient(b.serverName, b.transport)

	ctx := context.Background()
	if err := b.client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize MCP server %q: %w", b.serverName, err)
	}

	mcpTools, err := b.client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list MCP tools from %q: %w", b.serverName, err)
	}

	b.tools = make([]tools.Tool, len(mcpTools))
	for i, mt := range mcpTools {
		b.tools[i] = mcpclient.WrapTool(b.serverName, b.client, mt)
	}

	return nil
}

// Tools returns the wrapped MCP tools.
func (b *MCPBackend) Tools() []tools.Tool {
	return b.tools
}

// Hooks returns no hooks — MCP skills don't register hooks.
func (b *MCPBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	return nil
}

// Unload disconnects from the MCP server.
func (b *MCPBackend) Unload() error {
	b.tools = nil
	if b.client != nil {
		return b.client.Close()
	}
	return nil
}
