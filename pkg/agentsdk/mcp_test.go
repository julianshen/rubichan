package agentsdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServerConfig(t *testing.T) {
	cfg := MCPServerConfig{
		Name:      "filesystem",
		Transport: MCPTransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem"},
	}
	require.Equal(t, "filesystem", cfg.Name)
	require.Equal(t, MCPTransportStdio, cfg.Transport)
}

func TestMCPToolDef(t *testing.T) {
	tool := MCPToolDef{
		Name:        "read_file",
		Description: "Read a file",
		ServerName:  "filesystem",
	}
	require.Equal(t, "filesystem", tool.ServerName)
	require.Equal(t, "read_file", tool.Name)
}

func TestMCPTransportTypeValid(t *testing.T) {
	assert.True(t, MCPTransportStdio.Valid())
	assert.True(t, MCPTransportSSE.Valid())
	assert.True(t, MCPTransportHTTP.Valid())
	assert.True(t, MCPTransportWS.Valid())
	assert.False(t, MCPTransportType("grpc").Valid())
	assert.False(t, MCPTransportType("").Valid())
}

func TestMCPServerConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MCPServerConfig
		wantErr string
	}{
		{
			name: "valid stdio",
			cfg: MCPServerConfig{
				Name:      "fs",
				Transport: MCPTransportStdio,
				Command:   "npx",
			},
		},
		{
			name: "valid sse",
			cfg: MCPServerConfig{
				Name:      "remote",
				Transport: MCPTransportSSE,
				URL:       "http://localhost:3000/sse",
			},
		},
		{
			name:    "missing name",
			cfg:     MCPServerConfig{Transport: MCPTransportStdio, Command: "npx"},
			wantErr: "name is required",
		},
		{
			name:    "unknown transport",
			cfg:     MCPServerConfig{Name: "x", Transport: MCPTransportType("grpc")},
			wantErr: "unknown transport",
		},
		{
			name:    "stdio missing command",
			cfg:     MCPServerConfig{Name: "fs", Transport: MCPTransportStdio},
			wantErr: "command is required",
		},
		{
			name:    "sse missing url",
			cfg:     MCPServerConfig{Name: "remote", Transport: MCPTransportSSE},
			wantErr: "url is required",
		},
		{
			name:    "http missing url",
			cfg:     MCPServerConfig{Name: "remote", Transport: MCPTransportHTTP},
			wantErr: "url is required",
		},
		{
			name:    "websocket missing url",
			cfg:     MCPServerConfig{Name: "remote", Transport: MCPTransportWS},
			wantErr: "url is required",
		},
		{
			name: "valid websocket",
			cfg: MCPServerConfig{
				Name:      "remote",
				Transport: MCPTransportWS,
				URL:       "ws://localhost:3000",
			},
		},
		{
			name: "oauth valid",
			cfg: MCPServerConfig{
				Name:      "remote",
				Transport: MCPTransportSSE,
				URL:       "http://localhost:3000",
				OAuth: &MCPOAuthConfig{
					ClientID: "client",
					TokenURL: "http://auth/token",
				},
			},
		},
		{
			name:    "oauth missing client_id",
			cfg:     MCPServerConfig{Name: "remote", Transport: MCPTransportSSE, URL: "http://localhost:3000", OAuth: &MCPOAuthConfig{}},
			wantErr: "oauth client_id is required",
		},
		{
			name:    "oauth missing token_url",
			cfg:     MCPServerConfig{Name: "remote", Transport: MCPTransportSSE, URL: "http://localhost:3000", OAuth: &MCPOAuthConfig{ClientID: "client"}},
			wantErr: "oauth token_url is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMCPServerConfigJSONRoundTrip(t *testing.T) {
	cfg := MCPServerConfig{
		Name:      "test",
		Transport: MCPTransportSSE,
		URL:       "http://localhost:3000",
		Env:       map[string]string{"KEY": "val"},
		OAuth: &MCPOAuthConfig{
			ClientID: "client",
			TokenURL: "http://auth/token",
		},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded MCPServerConfig
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, cfg.Name, decoded.Name)
	assert.Equal(t, cfg.Transport, decoded.Transport)
	assert.Equal(t, cfg.URL, decoded.URL)
	assert.Equal(t, cfg.Env, decoded.Env)
	require.NotNil(t, decoded.OAuth)
	assert.Equal(t, cfg.OAuth.ClientID, decoded.OAuth.ClientID)
	assert.Equal(t, cfg.OAuth.TokenURL, decoded.OAuth.TokenURL)
}

func TestMCPToolDefJSONRoundTrip(t *testing.T) {
	tool := MCPToolDef{
		Name:        "read",
		Description: "Read file",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		ServerName:  "fs",
	}

	data, err := json.Marshal(tool)
	require.NoError(t, err)

	var decoded MCPToolDef
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, tool.Name, decoded.Name)
	assert.Equal(t, tool.ServerName, decoded.ServerName)
	assert.Equal(t, string(tool.InputSchema), string(decoded.InputSchema))
}
