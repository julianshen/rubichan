package mcpbackend

import (
	"context"
	"fmt"
	"time"

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

// NewMCPBackend creates a new MCP-backed skill backend from an existing transport.
func NewMCPBackend(serverName string, transport mcpclient.Transport) *MCPBackend {
	return &MCPBackend{
		serverName: serverName,
		transport:  transport,
	}
}

// NewMCPBackendFromConfig creates an MCP backend by constructing the appropriate
// transport from config fields. This is the factory used by the backendFactory
// in main.go to wire BackendMCP skills discovered from MCPServerConfig.
func NewMCPBackendFromConfig(ctx context.Context, transport, command string, args []string, sseURL string) (*MCPBackend, error) {
	var t mcpclient.Transport
	var err error

	switch transport {
	case "stdio":
		if command == "" {
			return nil, fmt.Errorf("mcp backend: stdio transport requires a command")
		}
		t, err = mcpclient.NewStdioTransport(command, args)
		if err != nil {
			return nil, fmt.Errorf("mcp backend: create stdio transport: %w", err)
		}
	case "sse":
		if sseURL == "" {
			return nil, fmt.Errorf("mcp backend: sse transport requires a url")
		}
		initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		t, err = mcpclient.NewSSETransport(initCtx, sseURL)
		if err != nil {
			return nil, fmt.Errorf("mcp backend: create sse transport: %w", err)
		}
	default:
		return nil, fmt.Errorf("mcp backend: unsupported transport %q", transport)
	}

	return &MCPBackend{
		serverName: "rubichan",
		transport:  t,
	}, nil
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
