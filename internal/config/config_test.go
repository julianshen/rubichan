package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Provider.Model)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
	assert.Equal(t, "prompt", cfg.Agent.ApprovalMode)
	assert.Equal(t, 100000, cfg.Agent.ContextBudget)
	assert.Equal(t, 5, cfg.Worktree.MaxCount)
	assert.True(t, cfg.Worktree.AutoCleanup)
	assert.Empty(t, cfg.Worktree.BaseBranch)
	assert.Equal(t, "mcp", cfg.Browser.PreferredBackend)
}

func TestLoadFromFile(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	cfg, err := Load("/nonexistent/path/config.toml")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", cfg.Provider.Default)
	assert.Equal(t, 50, cfg.Agent.MaxTurns)
}

func TestLoadInvalidTOML(t *testing.T) {
	t.Parallel()

	tmpFile := filepath.Join(t.TempDir(), "bad.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte("[invalid toml..."), 0644))

	_, err := Load(tmpFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config file")
}

func TestConfigWithSkillsSection(t *testing.T) {
	t.Parallel()

	tomlContent := `
[skills]
registry_url = "https://custom.registry.dev"
user_dir = "/tmp/skills"
dirs = ["/tmp/pack-a", "/tmp/pack-b"]
activation_threshold = 25
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
	assert.Equal(t, []string{"/tmp/pack-a", "/tmp/pack-b"}, cfg.Skills.Dirs)
	assert.Equal(t, 25, cfg.Skills.ActivationThreshold)
	assert.Equal(t, 5, cfg.Skills.MaxLLMCallsPerTurn)
	assert.Equal(t, 15, cfg.Skills.MaxShellExecPerTurn)
	assert.Equal(t, 8, cfg.Skills.MaxNetFetchPerTurn)
	assert.Equal(t, []string{"code-review", "doc-gen"}, cfg.Skills.ApprovedSkills)
}

func TestConfigSkillsDefaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Equal(t, "https://registry.rubichan.dev", cfg.Skills.RegistryURL)
	assert.Nil(t, cfg.Skills.ApprovedSkills)
	assert.Equal(t, "", cfg.Skills.UserDir)
	assert.Nil(t, cfg.Skills.Dirs)
	assert.Equal(t, 1, cfg.Skills.ActivationThreshold)
	assert.Equal(t, 10, cfg.Skills.MaxLLMCallsPerTurn)
	assert.Equal(t, 20, cfg.Skills.MaxShellExecPerTurn)
	assert.Equal(t, 10, cfg.Skills.MaxNetFetchPerTurn)
}

func TestOllamaConfig(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestBrowserConfig(t *testing.T) {
	t.Parallel()

	tomlData := `
[browser]
preferred_backend = "native"
mcp_server = "playwright"
artifact_dir = ".rubichan/browser-artifacts"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "native", cfg.Browser.PreferredBackend)
	assert.Equal(t, "playwright", cfg.Browser.MCPServer)
	assert.Equal(t, ".rubichan/browser-artifacts", cfg.Browser.ArtifactDir)
}

func TestMCPServerConfigValidate(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	cfg := DefaultConfig()
	assert.Equal(t, "high", cfg.Security.FailOn)
	assert.False(t, cfg.Security.EnableLLMAnalysis)
	assert.Equal(t, 10, cfg.Security.MaxLLMCalls)
	assert.Nil(t, cfg.Security.ExcludePatterns)
}

func TestSecurityConfigFromTOML(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestSaveAndReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	cfg.Provider.Default = "ollama"
	cfg.Provider.Model = "llama3"
	cfg.Agent.MaxTurns = 25
	cfg.Skills.Dirs = []string{"/tmp/pack-a", "/tmp/pack-b"}

	err := Save(path, cfg)
	require.NoError(t, err)

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "ollama", loaded.Provider.Default)
	assert.Equal(t, "llama3", loaded.Provider.Model)
	assert.Equal(t, 25, loaded.Agent.MaxTurns)
	assert.Equal(t, []string{"/tmp/pack-a", "/tmp/pack-b"}, loaded.Skills.Dirs)
}

func TestTrustRulesDefaultEmpty(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Nil(t, cfg.Agent.TrustRules)
}

func TestTrustRulesFromTOML(t *testing.T) {
	t.Parallel()

	tomlContent := `
[agent]
max_turns = 50
approval_mode = "prompt"

[[agent.trust_rules]]
tool = "shell"
pattern = "^go test"
action = "allow"

[[agent.trust_rules]]
tool = "shell"
pattern = "^rm\\s"
action = "deny"

[[agent.trust_rules]]
tool = "file"
pattern = ".*"
action = "allow"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.Agent.TrustRules, 3)

	assert.Equal(t, "shell", cfg.Agent.TrustRules[0].Tool)
	assert.Equal(t, "^go test", cfg.Agent.TrustRules[0].Pattern)
	assert.Equal(t, "allow", cfg.Agent.TrustRules[0].Action)

	assert.Equal(t, "shell", cfg.Agent.TrustRules[1].Tool)
	assert.Equal(t, `^rm\s`, cfg.Agent.TrustRules[1].Pattern)
	assert.Equal(t, "deny", cfg.Agent.TrustRules[1].Action)

	assert.Equal(t, "file", cfg.Agent.TrustRules[2].Tool)
	assert.Equal(t, ".*", cfg.Agent.TrustRules[2].Pattern)
	assert.Equal(t, "allow", cfg.Agent.TrustRules[2].Action)
}

func TestSaveCreatesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "config.toml")

	err := Save(path, DefaultConfig())
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestConfigAgentDefinitions(t *testing.T) {
	t.Parallel()

	tomlData := `
[provider]
default = "anthropic"
model = "claude-3"

[agent]
max_turns = 10

[[agent.definitions]]
name = "explorer"
description = "Explore codebase"
system_prompt = "You are an explorer."
tools = ["file", "search"]
max_turns = 5
max_depth = 2
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.Agent.Definitions, 1)
	assert.Equal(t, "explorer", cfg.Agent.Definitions[0].Name)
	assert.Equal(t, "Explore codebase", cfg.Agent.Definitions[0].Description)
	assert.Equal(t, "You are an explorer.", cfg.Agent.Definitions[0].SystemPrompt)
	assert.Equal(t, []string{"file", "search"}, cfg.Agent.Definitions[0].Tools)
	assert.Equal(t, 5, cfg.Agent.Definitions[0].MaxTurns)
	assert.Equal(t, 2, cfg.Agent.Definitions[0].MaxDepth)
}

func TestConfigAgentDefinitionsMultiple(t *testing.T) {
	t.Parallel()

	tomlData := `
[agent]
max_turns = 10

[[agent.definitions]]
name = "explorer"
tools = ["file"]

[[agent.definitions]]
name = "coder"
tools = ["file", "shell"]
model = "claude-sonnet-4-5"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.Len(t, cfg.Agent.Definitions, 2)
	assert.Equal(t, "explorer", cfg.Agent.Definitions[0].Name)
	assert.Equal(t, "coder", cfg.Agent.Definitions[1].Name)
	assert.Equal(t, "claude-sonnet-4-5", cfg.Agent.Definitions[1].Model)
}

func TestConfigAgentDefinitionsDefaultEmpty(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Nil(t, cfg.Agent.Definitions)
}

func TestConfigCacheSection(t *testing.T) {
	t.Parallel()

	tomlData := `
[provider]
default = "ollama"
model = "llama3"

[agent.cache]
ollama_keep_alive = "15m"
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "15m", cfg.Agent.Cache.OllamaKeepAlive)
}

func TestConfigPermissionsSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[permissions.tools]
allow = ["file", "code_search"]
deny = ["dangerous"]

[permissions.shell]
allow_commands = ["go test"]
deny_commands = ["rm -rf /"]

[permissions.files]
allow_patterns = ["*.go"]
deny_patterns = [".env"]
`), 0644)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"file", "code_search"}, cfg.Permissions.Tools.Allow)
	assert.Equal(t, []string{"dangerous"}, cfg.Permissions.Tools.Deny)
	assert.Equal(t, []string{"go test"}, cfg.Permissions.Shell.AllowCommands)
	assert.Equal(t, []string{".env"}, cfg.Permissions.Files.DenyPatterns)
}

func TestWorktreeConfigFromTOML(t *testing.T) {
	t.Parallel()

	tomlData := `
[worktree]
max_count = 10
base_branch = "develop"
auto_cleanup = false
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlData), 0o644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, 10, cfg.Worktree.MaxCount)
	assert.Equal(t, "develop", cfg.Worktree.BaseBranch)
	assert.False(t, cfg.Worktree.AutoCleanup)
}

func TestConfigHooksSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[hooks]
trust_project_hooks = true

[[hooks.rules]]
event = "post_edit"
pattern = "*.go"
command = "gofmt -w {file}"
timeout = "60s"

[[hooks.rules]]
event = "pre_shell"
command = "echo {command}"
`), 0644)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.True(t, cfg.Hooks.TrustProjectHooks)
	require.Len(t, cfg.Hooks.Rules, 2)
	assert.Equal(t, "post_edit", cfg.Hooks.Rules[0].Event)
	assert.Equal(t, "*.go", cfg.Hooks.Rules[0].Pattern)
	assert.Equal(t, "gofmt -w {file}", cfg.Hooks.Rules[0].Command)
	assert.Equal(t, "60s", cfg.Hooks.Rules[0].Timeout)
}

func TestConfigLSPSection(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[lsp]
enabled = false
auto_install = false
`), 0644)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.False(t, cfg.LSP.IsEnabled())
	assert.False(t, cfg.LSP.IsAutoInstall())
}

func TestConfigLSPDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"
`), 0644)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.True(t, cfg.LSP.IsEnabled())
	assert.True(t, cfg.LSP.IsAutoInstall())
}

func TestConfigSubagentSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[provider]
default = "anthropic"

[agent]
max_subagents = 5
max_requests_per_minute = 120
`), 0644)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Agent.MaxSubagents)
	assert.Equal(t, 120, cfg.Agent.MaxRequestsPerMinute)
}

func TestSandboxConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.False(t, cfg.Sandbox.IsEnabled())
	assert.True(t, cfg.Sandbox.IsAllowUnsandboxedCommands())
	assert.Nil(t, cfg.Sandbox.ExcludedCommands)
	assert.Nil(t, cfg.Sandbox.Network.AllowedDomains)
	assert.Equal(t, 0, cfg.Sandbox.Network.ProxyPort)
	assert.Nil(t, cfg.Sandbox.Filesystem.AllowWrite)
	assert.Nil(t, cfg.Sandbox.Filesystem.DenyRead)
}

func TestSandboxConfigFromTOML(t *testing.T) {
	t.Parallel()

	tomlContent := `
[sandbox]
enabled = true
allow_unsandboxed_commands = false
excluded_commands = ["git", "docker"]

[sandbox.network]
allowed_domains = ["api.github.com", "registry.npmjs.org"]
proxy_port = 8080

[sandbox.filesystem]
allow_write = ["/tmp", "~/projects"]
deny_read = ["/etc/shadow", "/root"]
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	assert.True(t, cfg.Sandbox.IsEnabled())
	assert.False(t, cfg.Sandbox.IsAllowUnsandboxedCommands())
	assert.Equal(t, []string{"git", "docker"}, cfg.Sandbox.ExcludedCommands)
	assert.Equal(t, []string{"api.github.com", "registry.npmjs.org"}, cfg.Sandbox.Network.AllowedDomains)
	assert.Equal(t, 8080, cfg.Sandbox.Network.ProxyPort)
	assert.Equal(t, []string{"/tmp", "~/projects"}, cfg.Sandbox.Filesystem.AllowWrite)
	assert.Equal(t, []string{"/etc/shadow", "/root"}, cfg.Sandbox.Filesystem.DenyRead)
}

func TestLoadUnreadableFile(t *testing.T) {
	// Create a file that exists but cannot be read (not "not found").
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[provider]\ndefault = \"anthropic\"\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o000))
	defer os.Chmod(path, 0o644)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestSaveToUnwritablePath(t *testing.T) {
	// Try to save to a path where the parent directory cannot be created.
	// On macOS/Linux, /dev/null/impossible will fail for MkdirAll.
	err := Save("/dev/null/impossible/config.toml", DefaultConfig())
	require.Error(t, err)
}

func TestLoadWithInvalidSandboxConfig(t *testing.T) {
	t.Parallel()

	tomlContent := `
[sandbox]
excluded_commands = ["/usr/bin/git"]
`
	tmpFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tomlContent), 0644))

	_, err := Load(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox config")
}

func TestSandboxConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     SandboxConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: SandboxConfig{
				ExcludedCommands: []string{"git", "docker"},
				Network: SandboxNetworkConfig{
					AllowedDomains: []string{"api.github.com"},
				},
				Filesystem: SandboxFilesystemConfig{
					AllowWrite: []string{"/tmp", "~/projects", "./local"},
					DenyRead:   []string{"/etc/shadow"},
				},
			},
		},
		{
			name: "empty config is valid",
			cfg:  SandboxConfig{},
		},
		{
			name: "excluded command with path",
			cfg: SandboxConfig{
				ExcludedCommands: []string{"/usr/bin/git"},
			},
			wantErr: "excluded_commands[0]: must not contain '/' or spaces",
		},
		{
			name: "excluded command with spaces",
			cfg: SandboxConfig{
				ExcludedCommands: []string{"git commit"},
			},
			wantErr: "excluded_commands[0]: must not contain '/' or spaces",
		},
		{
			name: "domain with scheme",
			cfg: SandboxConfig{
				Network: SandboxNetworkConfig{
					AllowedDomains: []string{"https://api.github.com"},
				},
			},
			wantErr: "allowed_domains[0]: must not contain scheme (://) or port (:port)",
		},
		{
			name: "domain with port",
			cfg: SandboxConfig{
				Network: SandboxNetworkConfig{
					AllowedDomains: []string{"api.github.com:443"},
				},
			},
			wantErr: "allowed_domains[0]: must not contain scheme (://) or port (:port)",
		},
		{
			name: "filesystem path without valid prefix",
			cfg: SandboxConfig{
				Filesystem: SandboxFilesystemConfig{
					AllowWrite: []string{"relative/path"},
				},
			},
			wantErr: "allow_write[0]: must start with '/', '~/', or './'",
		},
		{
			name: "deny_read path without valid prefix",
			cfg: SandboxConfig{
				Filesystem: SandboxFilesystemConfig{
					DenyRead: []string{"no-prefix"},
				},
			},
			wantErr: "deny_read[0]: must start with '/', '~/', or './'",
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

func TestProviderConfig_SummaryModel(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")
	content := `
[provider]
default = "anthropic"
model = "claude-opus-4-6"
summary_model = "claude-haiku-4-5-20251001"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-6", cfg.Provider.Model)
	assert.Equal(t, "claude-haiku-4-5-20251001", cfg.Provider.SummaryModel)
}

func TestProviderConfig_SummaryModel_EmptyByDefault(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	assert.Equal(t, "", cfg.Provider.SummaryModel, "SummaryModel should default to empty (caller falls back to Model)")
}
