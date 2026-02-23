package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Provider.Model)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
	assert.Equal(t, "prompt", cfg.Agent.ApprovalMode)
	assert.Equal(t, 100000, cfg.Agent.ContextBudget)
}

func TestLoadFromFile(t *testing.T) {
	tomlContent := `
[provider]
default = "openai"
model = "gpt-4o"

[provider.anthropic]
api_key_source = "keyring"

[agent]
max_turns = 30
approval_mode = "auto"
context_budget = 50000
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.Provider.Default)
	assert.Equal(t, "gpt-4o", cfg.Provider.Model)
	assert.Equal(t, "keyring", cfg.Provider.Anthropic.APIKeySource)
	assert.Equal(t, 30, cfg.Agent.MaxTurns)
	assert.Equal(t, "auto", cfg.Agent.ApprovalMode)
	assert.Equal(t, 50000, cfg.Agent.ContextBudget)
}

func TestLoadOpenAICompatibleProviders(t *testing.T) {
	tomlContent := `
[provider]
default = "openrouter"
model = "anthropic/claude-sonnet-4-5"

[[provider.openai_compatible]]
name = "openai"
base_url = "https://api.openai.com/v1"
api_key_source = "env"

[[provider.openai_compatible]]
name = "openrouter"
base_url = "https://openrouter.ai/api/v1"
api_key_source = "env"
extra_headers = { HTTP-Referer = "https://github.com/user/rubichan" }
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "openrouter", cfg.Provider.Default)
	require.Len(t, cfg.Provider.OpenAI, 2)
	assert.Equal(t, "openai", cfg.Provider.OpenAI[0].Name)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Provider.OpenAI[0].BaseURL)
	assert.Equal(t, "openrouter", cfg.Provider.OpenAI[1].Name)
	assert.Equal(t, "https://openrouter.ai/api/v1", cfg.Provider.OpenAI[1].BaseURL)
	assert.Equal(t, "https://github.com/user/rubichan", cfg.Provider.OpenAI[1].ExtraHeaders["HTTP-Referer"])
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
}

func TestLoadInvalidTOML(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bad.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("[invalid toml..."), 0644))

	_, err := Load(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config file")
}

func TestConfigWithSkillsSection(t *testing.T) {
	tomlContent := `
[skills]
registry_url = "https://custom.registry.dev"
user_dir = "/tmp/skills"
max_llm_calls_per_turn = 5
max_shell_exec_per_turn = 15
max_net_fetch_per_turn = 8
approved_skills = ["code-review", "doc-gen"]
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.registry.dev", cfg.Skills.RegistryURL)
	assert.Equal(t, "/tmp/skills", cfg.Skills.UserDir)
	assert.Equal(t, 5, cfg.Skills.MaxLLMCallsPerTurn)
	assert.Equal(t, 15, cfg.Skills.MaxShellExecPerTurn)
	assert.Equal(t, 8, cfg.Skills.MaxNetFetchPerTurn)
	assert.Equal(t, []string{"code-review", "doc-gen"}, cfg.Skills.ApprovedSkills)
}

func TestConfigSkillsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "https://registry.rubichan.dev", cfg.Skills.RegistryURL)
	assert.Nil(t, cfg.Skills.ApprovedSkills)
	assert.Equal(t, "", cfg.Skills.UserDir)
	assert.Equal(t, 10, cfg.Skills.MaxLLMCallsPerTurn)
	assert.Equal(t, 20, cfg.Skills.MaxShellExecPerTurn)
	assert.Equal(t, 10, cfg.Skills.MaxNetFetchPerTurn)
}

func TestOllamaConfig(t *testing.T) {
	tomlData := `
[provider]
default = "ollama"
model = "llama3"

[provider.ollama]
base_url = "http://localhost:11434"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.Provider.Default)
	assert.Equal(t, "http://localhost:11434", cfg.Provider.Ollama.BaseURL)
}

func TestMCPServersConfig(t *testing.T) {
	tomlData := `
[[mcp.servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

[[mcp.servers]]
name = "web-search"
transport = "sse"
url = "http://localhost:3001/sse"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.MCP.Servers, 2)

	assert.Equal(t, "filesystem", cfg.MCP.Servers[0].Name)
	assert.Equal(t, "stdio", cfg.MCP.Servers[0].Transport)
	assert.Equal(t, "npx", cfg.MCP.Servers[0].Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}, cfg.MCP.Servers[0].Args)

	assert.Equal(t, "web-search", cfg.MCP.Servers[1].Name)
	assert.Equal(t, "sse", cfg.MCP.Servers[1].Transport)
	assert.Equal(t, "http://localhost:3001/sse", cfg.MCP.Servers[1].URL)
}

func TestMCPServerConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MCPServerConfig
		wantErr string
	}{
		{
			name:    "missing name",
			cfg:     MCPServerConfig{Transport: "stdio", Command: "echo"},
			wantErr: "name is required",
		},
		{
			name:    "missing transport",
			cfg:     MCPServerConfig{Name: "test"},
			wantErr: "transport is required",
		},
		{
			name:    "unknown transport",
			cfg:     MCPServerConfig{Name: "test", Transport: "websocket"},
			wantErr: "unknown transport",
		},
		{
			name:    "stdio missing command",
			cfg:     MCPServerConfig{Name: "test", Transport: "stdio"},
			wantErr: "command is required",
		},
		{
			name:    "sse missing url",
			cfg:     MCPServerConfig{Name: "test", Transport: "sse"},
			wantErr: "url is required",
		},
		{
			name: "valid stdio",
			cfg:  MCPServerConfig{Name: "test", Transport: "stdio", Command: "echo"},
		},
		{
			name: "valid sse",
			cfg:  MCPServerConfig{Name: "test", Transport: "sse", URL: "http://localhost:3001/sse"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadMCPServerValidationError(t *testing.T) {
	tomlData := `
[[mcp.servers]]
name = "broken"
transport = "stdio"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	_, err := Load(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestSecurityConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "high", cfg.Security.FailOn)
	assert.False(t, cfg.Security.EnableLLMAnalysis)
	assert.Equal(t, 10, cfg.Security.MaxLLMCalls)
	assert.Nil(t, cfg.Security.ExcludePatterns)
}

func TestSecurityConfigFromTOML(t *testing.T) {
	tomlContent := `
[security]
fail_on = "critical"
enable_llm_analysis = true
max_llm_calls = 5
exclude_patterns = ["vendor/**", "testdata/**"]
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "critical", cfg.Security.FailOn)
	assert.True(t, cfg.Security.EnableLLMAnalysis)
	assert.Equal(t, 5, cfg.Security.MaxLLMCalls)
	assert.Equal(t, []string{"vendor/**", "testdata/**"}, cfg.Security.ExcludePatterns)
}

func TestConfigSkillsApproved(t *testing.T) {
	tomlContent := `
[skills]
approved_skills = ["lint-fixer", "test-gen", "security-scan"]
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.Skills.ApprovedSkills, 3)
	assert.Equal(t, "lint-fixer", cfg.Skills.ApprovedSkills[0])
	assert.Equal(t, "test-gen", cfg.Skills.ApprovedSkills[1])
	assert.Equal(t, "security-scan", cfg.Skills.ApprovedSkills[2])
	// Defaults should still be set for fields not specified in TOML
	assert.Equal(t, "https://registry.rubichan.dev", cfg.Skills.RegistryURL)
	assert.Equal(t, 10, cfg.Skills.MaxLLMCallsPerTurn)
	assert.Equal(t, 20, cfg.Skills.MaxShellExecPerTurn)
	assert.Equal(t, 10, cfg.Skills.MaxNetFetchPerTurn)
}
