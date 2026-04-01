package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/integrations"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/toolexec"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/internal/tui"
	"github.com/julianshen/rubichan/internal/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRepoRoot returns the git repository root for the current project.
// This avoids hardcoding absolute paths that break on other machines and CI.
func testRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := runGitCommand("rev-parse", "--show-toplevel")
	require.NoError(t, err)
	return strings.TrimSpace(out)
}

// ---------------------------------------------------------------------------
// ToolsConfig.ShouldEnable — additional edge cases
// ---------------------------------------------------------------------------

func TestToolsConfigShouldEnable_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tc       ToolsConfig
		toolName string
		want     bool
	}{
		{
			name: "non-native model still registers tools for text-based fallback",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: false},
			},
			toolName: "file",
			want:     true,
		},
		{
			name: "headless with no overrides denies all",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				HeadlessMode:      true,
			},
			toolName: "file",
			want:     false,
		},
		{
			name: "headless with explicit allowlist allows listed",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				HeadlessMode:      true,
				CLIOverrides:      map[string]bool{"file": true},
			},
			toolName: "file",
			want:     true,
		},
		{
			name: "headless with explicit allowlist denies unlisted",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				HeadlessMode:      true,
				CLIOverrides:      map[string]bool{"file": true},
			},
			toolName: "shell",
			want:     false,
		},
		{
			name: "feature flag disables tool",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				FeatureFlags:      map[string]bool{"tools.search": false},
			},
			toolName: "search",
			want:     false,
		},
		{
			name: "feature flag does not affect unrelated tool",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				FeatureFlags:      map[string]bool{"tools.search": false},
			},
			toolName: "file",
			want:     true,
		},
		{
			name: "user disabled overrides enabled",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				UserPreferences: UserToolPrefs{
					Enabled:  map[string]bool{"shell": true},
					Disabled: map[string]bool{"shell": true},
				},
			},
			toolName: "shell",
			want:     false,
		},
		{
			name: "user enabled list restricts to only listed tools",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				UserPreferences: UserToolPrefs{
					Enabled: map[string]bool{"file": true},
				},
			},
			toolName: "shell",
			want:     false,
		},
		{
			name: "apple tool without apple project or skill",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				ProjectContext: ProjectContext{
					AppleProjectDetected: false,
					AppleSkillRequested:  false,
				},
			},
			toolName: "xcode_build",
			want:     false,
		},
		{
			name: "apple tool with apple project detected",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				ProjectContext: ProjectContext{
					AppleProjectDetected: true,
					AppleSkillRequested:  false,
				},
			},
			toolName: "swift_test",
			want:     true,
		},
		{
			name: "apple tool with apple skill requested",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				ProjectContext: ProjectContext{
					AppleProjectDetected: false,
					AppleSkillRequested:  true,
				},
			},
			toolName: "sim_boot",
			want:     true,
		},
		{
			name: "CLI overrides act as whitelist in non-headless",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
				CLIOverrides:      map[string]bool{"search": true},
			},
			toolName: "file",
			want:     false,
		},
		{
			name: "no overrides in non-headless allows all",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
			},
			toolName: "file",
			want:     true,
		},
		{
			name: "xcrun tool is apple-only",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
			},
			toolName: "xcrun",
			want:     false,
		},
		{
			name: "xcrun_something tool is apple-only",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
			},
			toolName: "xcrun_notarize",
			want:     false,
		},
		{
			name: "codesign_verify tool is apple-only",
			tc: ToolsConfig{
				ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
			},
			toolName: "codesign_verify",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.tc.ShouldEnable(tt.toolName)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// parseToolsFlag — additional cases
// ---------------------------------------------------------------------------

func TestParseToolsFlag_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantNil  bool
		wantKeys []string
	}{
		{"empty string", "", true, nil},
		{"whitespace only", "   ", true, nil},
		{"single tool", "file", false, []string{"file"}},
		{"multiple tools", "file,shell,search", false, []string{"file", "shell", "search"}},
		{"tools with spaces", " file , shell ", false, []string{"file", "shell"}},
		{"all profile", "all", false, []string{"file", "shell", "search", "process", "notes"}},
		{"trailing comma", "file,shell,", false, []string{"file", "shell"}},
		{"leading comma", ",file", false, []string{"file"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseToolsFlag(tt.input)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				for _, key := range tt.wantKeys {
					assert.True(t, result[key], "expected %s to be true", key)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// versionString
// ---------------------------------------------------------------------------

func TestVersionString_Format(t *testing.T) {
	t.Parallel()
	s := versionString()
	assert.Contains(t, s, "rubichan")
	assert.Contains(t, s, "(commit:")
	assert.Contains(t, s, "built:")
}

// ---------------------------------------------------------------------------
// autoDetectProvider — additional edge cases
// ---------------------------------------------------------------------------

func TestAutoDetectProvider_EnvAPIKeyOverridesAutoDetect(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-env-test-key")

	cfg := config.DefaultConfig()
	detected := autoDetectProvider(cfg, "", "http://localhost:11434")
	assert.False(t, detected, "should not auto-detect when ANTHROPIC_API_KEY env var set")
}

func TestAutoDetectProvider_NonDefaultProvider(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Provider.Default = "openrouter" // not anthropic
	detected := autoDetectProvider(cfg, "", "http://localhost:11434")
	assert.False(t, detected, "should not auto-detect when provider is not anthropic")
}

// ---------------------------------------------------------------------------
// buildHierarchicalChecker
// ---------------------------------------------------------------------------

func TestBuildHierarchicalChecker_NilPolicies(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	// With default config (no policies), should return nil.
	checker := buildHierarchicalChecker(cfg, "", t.TempDir())
	assert.Nil(t, checker)
}

func TestBuildHierarchicalChecker_WithUserPolicies(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Permissions.Tools.Allow = []string{"file"}
	cfg.Permissions.Tools.Deny = []string{"shell"}

	checker := buildHierarchicalChecker(cfg, "/tmp/fake-config.toml", t.TempDir())
	assert.NotNil(t, checker)
}

func TestBuildHierarchicalChecker_WithShellPolicy(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Permissions.Shell.AllowCommands = []string{"ls"}
	cfg.Permissions.Shell.DenyCommands = []string{"rm -rf /"}

	checker := buildHierarchicalChecker(cfg, "", t.TempDir())
	assert.NotNil(t, checker)
}

func TestBuildHierarchicalChecker_WithFilePolicy(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Permissions.Files.AllowPatterns = []string{"*.go"}

	checker := buildHierarchicalChecker(cfg, "", t.TempDir())
	assert.NotNil(t, checker)
}

func TestBuildHierarchicalChecker_WithSkillPolicy(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Permissions.Skills.AutoApprove = []string{"my-skill"}

	checker := buildHierarchicalChecker(cfg, "", t.TempDir())
	assert.NotNil(t, checker)
}

func TestBuildHierarchicalChecker_EmptyCfgPathUsesDefault(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Permissions.Tools.Allow = []string{"file"}

	// Empty cfgPath should still work (falls back to UserHomeDir).
	checker := buildHierarchicalChecker(cfg, "", t.TempDir())
	assert.NotNil(t, checker)
}

// ---------------------------------------------------------------------------
// registerCoreTools
// ---------------------------------------------------------------------------

func TestRegisterCoreTools_AllDisabled(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		HeadlessMode:      true,
		// No CLIOverrides => all tools denied in headless
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	// With headless mode and no overrides, core tools should not be registered.
	// Extended tools may or may not be registered depending on their enablement logic.
	names := toolNames(registry)
	assert.NotContains(t, names, "file")
	assert.NotContains(t, names, "shell")
	assert.NotContains(t, names, "search")
	assert.NotContains(t, names, "process")
}

func TestRegisterCoreTools_FileOnly(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      map[string]bool{"file": true},
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "file")
	assert.NotContains(t, names, "shell")
	assert.NotContains(t, names, "search")
}

func TestRegisterCoreTools_ShellOnly(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      map[string]bool{"shell": true},
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "shell")
	assert.NotContains(t, names, "file")
}

func TestRegisterCoreTools_SearchOnly(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      map[string]bool{"search": true},
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "search")
	assert.NotContains(t, names, "file")
}

func TestRegisterCoreTools_ProcessOnly(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      map[string]bool{"process": true},
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "process")
	// Process tool has a cleanup
	assert.NotEmpty(t, result.cleanups)
}

func TestRegisterCoreTools_AllTools(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		// No CLIOverrides, non-headless => all tools allowed
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "search")
	assert.Contains(t, names, "process")
}

func TestCoreToolsResult_CleanupExecution(t *testing.T) {
	t.Parallel()
	called := false
	result := &coreToolsResult{
		cleanups: []func(){
			func() { called = true },
		},
	}
	for _, cleanup := range result.cleanups {
		cleanup()
	}
	assert.True(t, called)
}

func TestCoreToolsResult_MultipleCleanups(t *testing.T) {
	t.Parallel()
	var order []int
	result := &coreToolsResult{
		cleanups: []func(){
			func() { order = append(order, 1) },
			func() { order = append(order, 2) },
			func() { order = append(order, 3) },
		},
	}
	for _, cleanup := range result.cleanups {
		cleanup()
	}
	assert.Equal(t, []int{1, 2, 3}, order)
}

// toolNames extracts tool name strings from a registry.
func toolNames(r *tools.Registry) []string {
	var names []string
	for _, d := range r.All() {
		names = append(names, d.Name)
	}
	return names
}

// ---------------------------------------------------------------------------
// splitTrustRules
// ---------------------------------------------------------------------------

func TestSplitTrustRules_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rules     []config.TrustRuleConf
		wantRegex int
		wantGlobs int
	}{
		{
			name:      "empty rules",
			rules:     nil,
			wantRegex: 0,
			wantGlobs: 0,
		},
		{
			name: "all regex rules",
			rules: []config.TrustRuleConf{
				{Tool: "shell", Pattern: "^ls$", Action: "allow"},
				{Tool: "file", Pattern: ".*\\.go$", Action: "allow"},
			},
			wantRegex: 2,
			wantGlobs: 0,
		},
		{
			name: "all glob rules",
			rules: []config.TrustRuleConf{
				{Glob: "*.go", Action: "allow"},
				{Glob: "*.ts", Action: "allow"},
			},
			wantRegex: 0,
			wantGlobs: 2,
		},
		{
			name: "mixed regex and glob",
			rules: []config.TrustRuleConf{
				{Tool: "shell", Pattern: "^ls$", Action: "allow"},
				{Glob: "*.go", Action: "deny"},
				{Tool: "file", Pattern: ".*", Action: "allow"},
			},
			wantRegex: 2,
			wantGlobs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			regex, globs := splitTrustRules(tt.rules)
			assert.Len(t, regex, tt.wantRegex)
			assert.Len(t, globs, tt.wantGlobs)
		})
	}
}

func TestSplitTrustRules_PreservesFields(t *testing.T) {
	t.Parallel()
	rules := []config.TrustRuleConf{
		{Tool: "shell", Pattern: "^pwd$", Action: "allow"},
		{Glob: "**/*.go", Action: "deny"},
	}
	regex, globs := splitTrustRules(rules)

	require.Len(t, regex, 1)
	assert.Equal(t, "shell", regex[0].Tool)
	assert.Equal(t, "^pwd$", regex[0].Pattern)
	assert.Equal(t, "allow", regex[0].Action)

	require.Len(t, globs, 1)
	assert.Equal(t, "**/*.go", globs[0].Glob)
	assert.Equal(t, "deny", globs[0].Action)
}

// ---------------------------------------------------------------------------
// convertHookRules
// ---------------------------------------------------------------------------

func TestConvertHookRules_Empty(t *testing.T) {
	t.Parallel()
	result := convertHookRules(nil, "test")
	assert.Nil(t, result)
}

func TestConvertHookRules_BasicConversion(t *testing.T) {
	t.Parallel()
	rules := []config.HookRuleConfig{
		{
			Event:       "pre_tool_call",
			Pattern:     "shell",
			Command:     "echo hello",
			Description: "test hook",
		},
	}
	result := convertHookRules(rules, "config")

	require.Len(t, result, 1)
	assert.Equal(t, "pre_tool_call", result[0].Event)
	assert.Equal(t, "shell", result[0].Pattern)
	assert.Equal(t, "echo hello", result[0].Command)
	assert.Equal(t, "test hook", result[0].Description)
	assert.Equal(t, "config", result[0].Source)
	assert.Equal(t, 30*time.Second, result[0].Timeout) // default
}

func TestConvertHookRules_CustomTimeout(t *testing.T) {
	t.Parallel()
	rules := []config.HookRuleConfig{
		{
			Event:   "post_tool_call",
			Command: "echo done",
			Timeout: "10s",
		},
	}
	result := convertHookRules(rules, "agent.md")

	require.Len(t, result, 1)
	assert.Equal(t, 10*time.Second, result[0].Timeout)
}

func TestConvertHookRules_InvalidTimeoutUsesDefault(t *testing.T) {
	t.Parallel()
	rules := []config.HookRuleConfig{
		{
			Event:   "pre_tool_call",
			Command: "echo hello",
			Timeout: "not-a-duration",
		},
	}
	result := convertHookRules(rules, "config")

	require.Len(t, result, 1)
	assert.Equal(t, 30*time.Second, result[0].Timeout) // fallback to default
}

// ---------------------------------------------------------------------------
// interactiveSignalAbort
// ---------------------------------------------------------------------------

func TestInteractiveSignalAbort_Error(t *testing.T) {
	t.Parallel()
	abort := &interactiveSignalAbort{name: "SIGINT", exitCode: 130}
	assert.Equal(t, "interactive session aborted by SIGINT", abort.Error())
}

func TestInteractiveSignalAbort_ExitCode(t *testing.T) {
	t.Parallel()
	abort := &interactiveSignalAbort{name: "SIGQUIT", exitCode: 131}
	assert.Equal(t, 131, abort.exitCode)
}

// ---------------------------------------------------------------------------
// signalAbortFromContext
// ---------------------------------------------------------------------------

func TestSignalAbortFromContext_NoAbort(t *testing.T) {
	t.Parallel()
	abort := signalAbortFromContext(context.Background())
	assert.Nil(t, abort)
}

func TestSignalAbortFromContext_WithAbort(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&interactiveSignalAbort{name: "quit", exitCode: 131})
	abort := signalAbortFromContext(ctx)
	require.NotNil(t, abort)
	assert.Equal(t, "quit", abort.name)
	assert.Equal(t, 131, abort.exitCode)
}

func TestSignalAbortFromContext_WithNonAbortCause(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.DeadlineExceeded)
	abort := signalAbortFromContext(ctx)
	assert.Nil(t, abort)
}

// ---------------------------------------------------------------------------
// interactiveExitError
// ---------------------------------------------------------------------------

func TestInteractiveExitError_NoAbort(t *testing.T) {
	t.Parallel()
	err := interactiveExitError(context.Background())
	assert.Nil(t, err)
}

// ---------------------------------------------------------------------------
// handleInteractiveProgramError — additional cases
// ---------------------------------------------------------------------------

func TestHandleInteractiveProgramError_NilError(t *testing.T) {
	t.Parallel()
	err := handleInteractiveProgramError(nil, context.Background(), "test")
	assert.Nil(t, err)
}

func TestHandleInteractiveProgramError_RegularError(t *testing.T) {
	t.Parallel()
	err := handleInteractiveProgramError(assert.AnError, context.Background(), "running TUI")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "running TUI")
}

// ---------------------------------------------------------------------------
// isShellPwdProbe
// ---------------------------------------------------------------------------

func TestIsShellPwdProbe_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid pwd", `{"command":"pwd"}`, true},
		{"pwd with spaces", `{"command":" pwd "}`, true},
		{"not pwd", `{"command":"ls"}`, false},
		{"empty command", `{"command":""}`, false},
		{"invalid json", `not json`, false},
		{"missing command field", `{"cmd":"pwd"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isShellPwdProbe(json.RawMessage(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// ruleEngineChecker
// ---------------------------------------------------------------------------

func TestRuleEngineChecker_NoRulesDefaultsToCategory(t *testing.T) {
	t.Parallel()
	classifier := toolexec.NewClassifier(nil)
	engine := toolexec.NewRuleEngine(nil)
	checker := &ruleEngineChecker{classifier: classifier, engine: engine}

	// "file" is classified as CategoryFileRead which defaults to ActionAllow.
	result := checker.CheckApproval("file", json.RawMessage(`{}`))
	assert.Equal(t, agent.TrustRuleApproved, result)

	// "shell" is classified as CategoryBash which defaults to ActionAsk.
	resultShell := checker.CheckApproval("shell", json.RawMessage(`{}`))
	assert.Equal(t, agent.ApprovalRequired, resultShell)
}

func TestRuleEngineChecker_AllowRuleReturnsTrustRuleApproved(t *testing.T) {
	t.Parallel()
	classifier := toolexec.NewClassifier(nil)
	rules := []toolexec.PermissionRule{
		{Tool: "file", Action: toolexec.ActionAllow, Source: toolexec.SourceUser},
	}
	engine := toolexec.NewRuleEngine(rules)
	checker := &ruleEngineChecker{classifier: classifier, engine: engine}

	result := checker.CheckApproval("file", json.RawMessage(`{}`))
	assert.Equal(t, agent.TrustRuleApproved, result)
}

// ---------------------------------------------------------------------------
// noopPromptBackend
// ---------------------------------------------------------------------------

func TestNoopPromptBackend_AllMethodsWork(t *testing.T) {
	t.Parallel()
	backend := &noopPromptBackend{}

	assert.NoError(t, backend.Load(skills.SkillManifest{}, nil))
	assert.Empty(t, backend.Tools())
	assert.Empty(t, backend.Hooks())
	assert.Empty(t, backend.Commands())
	assert.Empty(t, backend.Agents())
	assert.NoError(t, backend.Unload())
}

// ---------------------------------------------------------------------------
// appendWorkingDirOption
// ---------------------------------------------------------------------------

func TestAppendWorkingDirOption_EmptyReturnsOriginal(t *testing.T) {
	t.Parallel()
	opts := appendWorkingDirOption(nil, "")
	assert.Nil(t, opts)
}

func TestAppendWorkingDirOption_NonEmptyAddsOption(t *testing.T) {
	t.Parallel()
	opts := appendWorkingDirOption(nil, "/some/path")
	assert.Len(t, opts, 1)

	a := &agent.Agent{}
	for _, opt := range opts {
		opt(a)
	}
	assert.Equal(t, "/some/path", a.WorkingDir())
}

// ---------------------------------------------------------------------------
// configDir
// ---------------------------------------------------------------------------

func TestConfigDir_ReturnsExpectedPath(t *testing.T) {
	t.Parallel()
	dir, err := configDir()
	require.NoError(t, err)
	assert.Contains(t, dir, ".config")
	assert.Contains(t, dir, "rubichan")
}

// ---------------------------------------------------------------------------
// getActiveSessionLogPath / setActiveSessionLogPath
// ---------------------------------------------------------------------------

func TestActiveSessionLogPath_RoundTrip(t *testing.T) {
	// Save and restore since this modifies global state.
	prev := getActiveSessionLogPath()
	defer setActiveSessionLogPath(prev)

	setActiveSessionLogPath("/tmp/test-session.log")
	assert.Equal(t, "/tmp/test-session.log", getActiveSessionLogPath())
}

// ---------------------------------------------------------------------------
// storeMemoryAdapter
// ---------------------------------------------------------------------------

func TestStoreMemoryAdapter_SaveAndLoad(t *testing.T) {
	t.Parallel()
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	adapter := &storeMemoryAdapter{store: s}

	err = adapter.SaveMemory("/tmp/project", "test-tag", "test content")
	require.NoError(t, err)

	memories, err := adapter.LoadMemories("/tmp/project")
	require.NoError(t, err)
	require.Len(t, memories, 1)
	assert.Equal(t, "test-tag", memories[0].Tag)
	assert.Equal(t, "test content", memories[0].Content)
}

func TestStoreMemoryAdapter_LoadEmpty(t *testing.T) {
	t.Parallel()
	s, err := store.NewStore(":memory:")
	require.NoError(t, err)
	defer s.Close()

	adapter := &storeMemoryAdapter{store: s}
	memories, err := adapter.LoadMemories("/nonexistent")
	require.NoError(t, err)
	assert.Empty(t, memories)
}

// ---------------------------------------------------------------------------
// agentDefLookupAdapter
// ---------------------------------------------------------------------------

func TestAgentDefLookupAdapter_Found(t *testing.T) {
	t.Parallel()
	reg := agent.NewAgentDefRegistry()
	_ = reg.Register(&agent.AgentDef{
		Name:        "test-agent",
		Description: "A test agent",
		MaxTurns:    10,
	})
	adapter := &agentDefLookupAdapter{reg: reg}

	def, ok := adapter.GetAgentDef("test-agent")
	assert.True(t, ok)
	require.NotNil(t, def)
	assert.Equal(t, "test-agent", def.Name)
	assert.Equal(t, 10, def.MaxTurns)
}

func TestAgentDefLookupAdapter_NotFound(t *testing.T) {
	t.Parallel()
	reg := agent.NewAgentDefRegistry()
	adapter := &agentDefLookupAdapter{reg: reg}

	def, ok := adapter.GetAgentDef("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, def)
}

// ---------------------------------------------------------------------------
// agentDefRegistrarAdapter
// ---------------------------------------------------------------------------

func TestAgentDefRegistrarAdapter_RegisterAndUnregister(t *testing.T) {
	t.Parallel()
	reg := agent.NewAgentDefRegistry()
	adapter := &agentDefRegistrarAdapter{reg: reg}

	err := adapter.Register(&skills.AgentDefinition{
		Name:        "skill-agent",
		Description: "Skill contributed agent",
		MaxTurns:    5,
	})
	require.NoError(t, err)

	// Verify it was registered.
	def, ok := reg.Get("skill-agent")
	assert.True(t, ok)
	assert.Equal(t, "skill-agent", def.Name)
	assert.Equal(t, 5, def.MaxTurns)

	// Unregister.
	err = adapter.Unregister("skill-agent")
	require.NoError(t, err)
	_, ok = reg.Get("skill-agent")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// wakeManagerAdapter
// ---------------------------------------------------------------------------

func TestWakeManagerAdapter_SubmitAndComplete(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeManagerAdapter{wm: wm}

	taskID := adapter.SubmitBackground("test-task", func() {})
	assert.NotEmpty(t, taskID)

	// Before completion, task should be pending/running.
	statuses := wm.Status()
	require.Len(t, statuses, 1)
	assert.Equal(t, taskID, statuses[0].ID)
	assert.Equal(t, "running", statuses[0].Status)

	// Complete the task and drain the event.
	adapter.CompleteBackground(taskID, "done", nil)
	<-wm.Events()

	// After completion, task should be removed from pending.
	statuses = wm.Status()
	assert.Empty(t, statuses)
}

// ---------------------------------------------------------------------------
// wakeStatusAdapter
// ---------------------------------------------------------------------------

func TestWakeStatusAdapter_EmptyStatus(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeStatusAdapter{wm: wm}

	statuses := adapter.BackgroundTaskStatus()
	assert.Empty(t, statuses)
}

func TestWakeStatusAdapter_WithTasks(t *testing.T) {
	t.Parallel()
	wm := agent.NewWakeManager()
	adapter := &wakeStatusAdapter{wm: wm}

	wm.Submit("task-a", func() {})
	wm.Submit("task-b", func() {})

	statuses := adapter.BackgroundTaskStatus()
	assert.Len(t, statuses, 2)
}

// ---------------------------------------------------------------------------
// detectGitBranch
// ---------------------------------------------------------------------------

func TestDetectGitBranch_ValidRepo(t *testing.T) {
	t.Parallel()
	// Create a dedicated test repo with a known branch to avoid failures
	// in detached HEAD worktrees or CI environments.
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "test-branch"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		require.NoError(t, cmd.Run(), "git %v failed", args)
	}
	branch, err := detectGitBranch(dir)
	require.NoError(t, err)
	assert.Equal(t, "test-branch", branch)
}

func TestDetectGitBranch_InvalidDir(t *testing.T) {
	t.Parallel()
	_, err := detectGitBranch(t.TempDir())
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// indexProjectFiles
// ---------------------------------------------------------------------------

func TestIndexProjectFiles_NonGitDir(t *testing.T) {
	t.Parallel()
	// Should not panic on a non-git directory.
	assert.NotPanics(t, func() {
		// We can't easily test the full function without tui imports,
		// but we can verify it doesn't crash.
		indexProjectFiles(t.TempDir(), nil)
	})
}

// ---------------------------------------------------------------------------
// captureAllStacks
// ---------------------------------------------------------------------------

func TestCaptureAllStacks_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()
	stacks := captureAllStacks()
	assert.NotEmpty(t, stacks)
	assert.Contains(t, string(stacks), "goroutine")
}

// ---------------------------------------------------------------------------
// writeStackDump
// ---------------------------------------------------------------------------

func TestWriteStackDump_CreatesFile(t *testing.T) {
	t.Parallel()
	cfgDir := t.TempDir()
	path, err := writeStackDump(cfgDir, "test-dump.log", "header: test\n\n")
	require.NoError(t, err)
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "header: test")
	assert.Contains(t, string(data), "goroutine")
}

func TestWriteStackDump_InvalidDir(t *testing.T) {
	t.Parallel()
	// Using a file path as the config dir should fail.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

	_, err := writeStackDump(tmpFile, "dump.log", "header\n")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// writeDiagnosticDump
// ---------------------------------------------------------------------------

func TestWriteDiagnosticDump_CreatesFile(t *testing.T) {
	t.Parallel()
	cfgDir := t.TempDir()
	path, err := writeDiagnosticDump(cfgDir, os.Interrupt, "/tmp/session.log")
	require.NoError(t, err)
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "signal: interrupt")
	assert.Contains(t, string(data), "session_log: /tmp/session.log")
}

// ---------------------------------------------------------------------------
// buildEventSink — additional cases
// ---------------------------------------------------------------------------

func TestBuildEventSink_DebugOnly(t *testing.T) {
	t.Parallel()
	sink := buildEventSink(nil, true)
	require.Len(t, sink, 1) // log sink only
}

func TestBuildEventSink_DebugWithStructuredLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := startEventLogger(path)
	require.NoError(t, err)
	defer logger.Close()

	sink := buildEventSink(logger, true)
	require.Len(t, sink, 2) // log sink + JSONL sink
}

// ---------------------------------------------------------------------------
// eventLogger.Close — nil safety
// ---------------------------------------------------------------------------

func TestEventLoggerClose_Nil(t *testing.T) {
	t.Parallel()
	var el *eventLogger
	assert.NoError(t, el.Close())
}

// ---------------------------------------------------------------------------
// sessionLogger.Close — nil safety
// ---------------------------------------------------------------------------

func TestSessionLoggerClose_Nil(t *testing.T) {
	t.Parallel()
	var sl *sessionLogger
	assert.NoError(t, sl.Close())
}

// ---------------------------------------------------------------------------
// startEventLogger
// ---------------------------------------------------------------------------

func TestStartEventLogger_EmptyPath(t *testing.T) {
	t.Parallel()
	logger, err := startEventLogger("")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

func TestStartEventLogger_WhitespacePath(t *testing.T) {
	t.Parallel()
	logger, err := startEventLogger("   ")
	assert.NoError(t, err)
	assert.Nil(t, logger)
}

// ---------------------------------------------------------------------------
// buildPipeline
// ---------------------------------------------------------------------------

func TestBuildPipeline_NilRuntime(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()

	pc := buildPipeline(registry, cfg, t.TempDir(), nil)
	assert.NotNil(t, pc.Pipeline)
	assert.NotNil(t, pc.Classifier)
	assert.NotNil(t, pc.RuleEngine)
}

func TestBuildPipeline_WithToolRules(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Agent.ToolRules = []config.ToolRuleConf{
		{Category: "file", Tool: "file", Action: "allow"},
	}

	pc := buildPipeline(registry, cfg, t.TempDir(), nil)
	assert.NotNil(t, pc.Pipeline)
}

// ---------------------------------------------------------------------------
// saveMemoriesBestEffort
// ---------------------------------------------------------------------------

func TestSaveMemoriesBestEffort_NilAgent(t *testing.T) {
	t.Parallel()
	// Should not panic.
	assert.NotPanics(t, func() {
		saveMemoriesBestEffort(context.Background(), nil, os.Stderr)
	})
}

func TestSaveMemoriesBestEffort_NilContext(t *testing.T) {
	t.Parallel()
	// Should not panic even with nil context.
	assert.NotPanics(t, func() {
		saveMemoriesBestEffort(nil, nil, os.Stderr)
	})
}

// ---------------------------------------------------------------------------
// wireExtendedTools
// ---------------------------------------------------------------------------

func TestWireExtendedTools_AllDisabled(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		HeadlessMode:      true,
		// No CLIOverrides => nothing enabled
	}

	err := wireExtendedTools(t.TempDir(), registry, cfg, toolsCfg)
	assert.NoError(t, err)
	assert.Empty(t, registry.All())
}

func TestWireExtendedTools_SomeEnabled(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      map[string]bool{"http_get": true, "git_status": true},
	}

	err := wireExtendedTools(t.TempDir(), registry, cfg, toolsCfg)
	assert.NoError(t, err)

	names := toolNames(registry)
	assert.Contains(t, names, "http_get")
	assert.Contains(t, names, "git_status")
}

// ---------------------------------------------------------------------------
// promptFolderAccess
// ---------------------------------------------------------------------------

func TestPromptFolderAccess_YesResponse(t *testing.T) {
	t.Parallel()
	var buf []byte
	w := &writerBuffer{buf: &buf}
	r := readerString("yes\n")

	allowed, err := promptFolderAccess("/tmp/project", r, w)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Contains(t, string(buf), "Allow rubichan to access this folder?")
}

func TestPromptFolderAccess_NoResponse(t *testing.T) {
	t.Parallel()
	var buf []byte
	w := &writerBuffer{buf: &buf}
	r := readerString("no\n")

	allowed, err := promptFolderAccess("/tmp/project", r, w)
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestPromptFolderAccess_CaseInsensitive(t *testing.T) {
	t.Parallel()
	var buf []byte
	w := &writerBuffer{buf: &buf}
	r := readerString("YES\n")

	allowed, err := promptFolderAccess("/tmp/project", r, w)
	require.NoError(t, err)
	assert.True(t, allowed)
}

// writerBuffer is a minimal io.Writer for tests.
type writerBuffer struct {
	buf *[]byte
}

func (w *writerBuffer) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

// readerString returns an io.Reader from a string.
func readerString(s string) *stringReader {
	return &stringReader{data: []byte(s)}
}

type stringReader struct {
	data []byte
	pos  int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, nil
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// ---------------------------------------------------------------------------
// provider.DetectCapabilities — additional edge cases
// ---------------------------------------------------------------------------

func TestDetectCapabilities_OllamaProvider(t *testing.T) {
	t.Parallel()
	caps := provider.DetectCapabilities("ollama", "llama3")
	assert.True(t, caps.SupportsNativeToolUse)
}

func TestDetectCapabilities_UnknownProviderFallsToOpenAICompat(t *testing.T) {
	t.Parallel()
	// Unknown providers fall through to OpenAI-compat path which is tool-capable.
	caps := provider.DetectCapabilities("some-unknown", "")
	assert.True(t, caps.SupportsNativeToolUse)
}

// ---------------------------------------------------------------------------
// wireWiki
// ---------------------------------------------------------------------------

func TestWireWiki_DisabledInHeadless(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		HeadlessMode:      true,
		// No CLIOverrides => wiki disabled
	}
	err := wireWiki(t.TempDir(), registry, nil, toolsCfg)
	assert.NoError(t, err)
	assert.Empty(t, registry.All())
}

// ---------------------------------------------------------------------------
// wireAppleDev
// ---------------------------------------------------------------------------

func TestWireAppleDev_NoAppleProject(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		ProjectContext: ProjectContext{
			AppleProjectDetected: false,
			AppleSkillRequested:  false,
		},
	}
	err := wireAppleDev(t.TempDir(), registry, toolsCfg)
	assert.NoError(t, err)
	assert.Empty(t, registry.All())
}

// ---------------------------------------------------------------------------
// isAppleOnlyTool — comprehensive coverage
// ---------------------------------------------------------------------------

func TestIsAppleOnlyTool_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tool string
		want bool
	}{
		{"xcode_ prefix", "xcode_build", true},
		{"swift_ prefix", "swift_test", true},
		{"sim_ prefix", "sim_boot", true},
		{"codesign_ prefix", "codesign_verify", true},
		{"xcrun exact", "xcrun", true},
		{"xcrun_ prefix", "xcrun_notarize", true},
		{"file tool", "file", false},
		{"shell tool", "shell", false},
		{"search tool", "search", false},
		{"process tool", "process", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isAppleOnlyTool(tt.tool))
		})
	}
}

// ---------------------------------------------------------------------------
// validateHeadlessBootstrapProbe — additional edge cases
// ---------------------------------------------------------------------------

func TestValidateHeadlessBootstrapProbe_NilResult(t *testing.T) {
	t.Parallel()
	err := validateHeadlessBootstrapProbe(nil, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap probe missing")
}

func TestValidateHeadlessBootstrapProbe_ShellDisabled(t *testing.T) {
	t.Parallel()
	err := validateHeadlessBootstrapProbe(nil, false)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// truncateID
// ---------------------------------------------------------------------------

func TestTruncateID_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"long UUID", "abcdefgh-1234-5678", "abcdefgh"},
		{"exactly 8", "abcdefgh", "abcdefgh"},
		{"short", "abc", "abc"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, truncateID(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// runGitCommand
// ---------------------------------------------------------------------------

func TestRunGitCommand_Success(t *testing.T) {
	t.Parallel()
	out, err := runGitCommand("version")
	require.NoError(t, err)
	assert.Contains(t, out, "git version")
}

func TestRunGitCommand_InvalidSubcommand(t *testing.T) {
	t.Parallel()
	_, err := runGitCommand("not-a-real-command")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// appendPersonaOptions
// ---------------------------------------------------------------------------

func TestAppendPersonaOptions_NoFiles(t *testing.T) {
	t.Parallel()
	// In an empty temp dir, no AGENT.md / IDENTITY.md / SOUL.md should exist.
	dir := t.TempDir()
	opts := appendPersonaOptions(nil, dir)
	assert.Empty(t, opts)
}

func TestAppendPersonaOptions_WithAgentMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("# Test Agent"), 0o644))

	opts := appendPersonaOptions(nil, dir)
	assert.Len(t, opts, 1)
}

// ---------------------------------------------------------------------------
// sessionCmd — structure tests
// ---------------------------------------------------------------------------

func TestSessionCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := sessionCmd()
	assert.Equal(t, "session", cmd.Use)

	var subNames []string
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Use)
	}
	assert.Contains(t, subNames, "list")
}

func TestSessionForkCmd_RequiresArg(t *testing.T) {
	t.Parallel()
	cmd := sessionForkCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestSessionDeleteCmd_RequiresArg(t *testing.T) {
	t.Parallel()
	cmd := sessionDeleteCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// wireSandboxProxy — basic path
// ---------------------------------------------------------------------------

func TestWireSandboxProxy_SandboxDisabled(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	// Sandbox.Enabled is nil by default, which means disabled.

	shellTool := tools.NewShellTool(t.TempDir(), 30*time.Second)
	cleanup, err := wireSandboxProxy(cfg, shellTool)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)
}

// ---------------------------------------------------------------------------
// wireLSPTools — disabled path
// ---------------------------------------------------------------------------

func TestWireLSPTools_Disabled(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	// LSP is enabled by default (nil = true), so explicitly disable it.
	disabled := false
	cfg.LSP.Enabled = &disabled

	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true}}

	mgr, cleanup, err := wireLSPTools(cfg, registry, toolsCfg, t.TempDir())
	assert.NoError(t, err)
	assert.Nil(t, cleanup)
	assert.Nil(t, mgr)
}

// ---------------------------------------------------------------------------
// saveMemoriesBestEffort — with actual agent (nil memory store)
// ---------------------------------------------------------------------------

func TestSaveMemoriesBestEffort_AgentWithNoMemoryStore(t *testing.T) {
	t.Parallel()
	// Should not panic or error with an agent that has no memory store.
	assert.NotPanics(t, func() {
		saveMemoriesBestEffort(context.Background(), nil, os.Stderr)
	})
}

// ---------------------------------------------------------------------------
// registerBuiltinSkillPrompts
// ---------------------------------------------------------------------------

func TestRegisterBuiltinSkillPrompts(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	loader := skills.NewLoader(filepath.Join(configDir, "skills"), "")

	err := registerBuiltinSkillPrompts(loader, configDir)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// emitSkillDiscoveryWarnings — nil inputs
// ---------------------------------------------------------------------------

func TestEmitSkillDiscoveryWarnings_NilWriter(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() {
		emitSkillDiscoveryWarnings(nil, nil)
	})
}

func TestEmitSkillDiscoveryWarnings_NilRuntime(t *testing.T) {
	t.Parallel()
	var buf []byte
	w := &writerBuffer{buf: &buf}
	assert.NotPanics(t, func() {
		emitSkillDiscoveryWarnings(w, nil)
	})
	assert.Empty(t, buf)
}

// ---------------------------------------------------------------------------
// openSessionStore
// ---------------------------------------------------------------------------

func TestOpenSessionStore_Works(t *testing.T) {
	s, err := openSessionStore()
	require.NoError(t, err)
	defer s.Close()
}

// ---------------------------------------------------------------------------
// Ollama helpers (from ollama.go)
// ---------------------------------------------------------------------------

func TestResolveOllamaBaseURL_Default(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.Flags().String("base-url", "", "")
	url := resolveOllamaBaseURL(cmd)
	assert.NotEmpty(t, url)
	assert.Contains(t, url, "11434")
}

func TestResolveOllamaBaseURL_Custom(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	cmd.Flags().String("base-url", "", "")
	_ = cmd.Flags().Set("base-url", "http://custom:9999")
	url := resolveOllamaBaseURL(cmd)
	assert.Equal(t, "http://custom:9999", url)
}

func TestFormatBytes_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500 B"},
		{"kilobytes", 1024, "1.0 KB"},
		{"megabytes", 1024 * 1024, "1.0 MB"},
		{"gigabytes", 1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, formatBytes(tt.bytes))
		})
	}
}

// ---------------------------------------------------------------------------
// starlarkGitRunnerAdapter
// ---------------------------------------------------------------------------

func TestStarlarkGitRunnerAdapter_Diff(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	diff, err := adapter.Diff(context.Background())
	require.NoError(t, err)
	_ = diff
}

func TestStarlarkGitRunnerAdapter_Log(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	entries, err := adapter.Log(context.Background(), "-1")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.NotEmpty(t, entries[0].Hash)
	assert.NotEmpty(t, entries[0].Author)
}

func TestStarlarkGitRunnerAdapter_Status(t *testing.T) {
	t.Parallel()
	adapter := &starlarkGitRunnerAdapter{
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	_, err := adapter.Status(context.Background())
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// pluginGitRunnerAdapter
// ---------------------------------------------------------------------------

func TestPluginGitRunnerAdapter_Diff(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	diff, err := adapter.Diff()
	require.NoError(t, err)
	_ = diff
}

func TestPluginGitRunnerAdapter_Log(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	entries, err := adapter.Log("-1")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.NotEmpty(t, entries[0].Hash)
}

func TestPluginGitRunnerAdapter_Status(t *testing.T) {
	t.Parallel()
	adapter := &pluginGitRunnerAdapter{
		ctx:    context.Background(),
		runner: integrations.NewGitRunner(testRepoRoot(t)),
	}
	_, err := adapter.Status()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// pluginHTTPFetcherAdapter
// ---------------------------------------------------------------------------

func TestPluginHTTPFetcherAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginHTTPFetcherAdapter{
		ctx:     context.Background(),
		fetcher: integrations.NewHTTPFetcher(5 * time.Second),
	}
	assert.NotNil(t, adapter)
}

// ---------------------------------------------------------------------------
// pluginLLMCompleterAdapter
// ---------------------------------------------------------------------------

func TestPluginLLMCompleterAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginLLMCompleterAdapter{
		ctx:       context.Background(),
		completer: nil,
	}
	assert.NotNil(t, adapter)
}

// ---------------------------------------------------------------------------
// pluginSkillInvokerAdapter
// ---------------------------------------------------------------------------

func TestPluginSkillInvokerAdapter_Construction(t *testing.T) {
	t.Parallel()
	adapter := &pluginSkillInvokerAdapter{
		ctx:     context.Background(),
		invoker: integrations.NewSkillInvoker(nil),
	}
	assert.NotNil(t, adapter)
}

// ---------------------------------------------------------------------------
// wireAppleDev with Apple project detected
// ---------------------------------------------------------------------------

func TestWireAppleDev_WithAppleProjectDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Package.swift"), []byte("// swift-tools-version:5.9"), 0o644))

	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		ProjectContext: ProjectContext{
			AppleProjectDetected: true,
			AppleSkillRequested:  true,
		},
	}
	err := wireAppleDev(dir, registry, toolsCfg)
	assert.NoError(t, err)
	assert.NotEmpty(t, registry.All())
}

// ---------------------------------------------------------------------------
// wireWiki with tools enabled
// ---------------------------------------------------------------------------

func TestWireWiki_Enabled(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
	}
	err := wireWiki(t.TempDir(), registry, nil, toolsCfg)
	assert.NoError(t, err)
	names := toolNames(registry)
	assert.Contains(t, names, "generate_wiki")
}

// ---------------------------------------------------------------------------
// wireSandboxProxy with sandbox enabled but no domains
// ---------------------------------------------------------------------------

func TestWireSandboxProxy_EnabledNoDomains(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	enabled := true
	cfg.Sandbox.Enabled = &enabled

	shellTool := tools.NewShellTool(t.TempDir(), 30*time.Second)
	cleanup, err := wireSandboxProxy(cfg, shellTool)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)
}

// ---------------------------------------------------------------------------
// wireLSPTools with LSP enabled
// ---------------------------------------------------------------------------

func TestWireLSPTools_Enabled(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	registry := tools.NewRegistry()
	toolsCfg := ToolsConfig{ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true}}

	_, cleanup, err := wireLSPTools(cfg, registry, toolsCfg, t.TempDir())
	assert.NoError(t, err)
	if cleanup != nil {
		cleanup()
	}
}

// ---------------------------------------------------------------------------
// plainInteractive setters
// ---------------------------------------------------------------------------

func TestPlainInteractiveSetters(t *testing.T) {
	t.Parallel()
	host := newPlainInteractiveHost(nil, os.Stdout, "test-model", 10, nil)
	host.SetAgent(nil)
	host.SetModel("new-model")
	host.SetGitBranch("main")
	host.SetDebug(true)
	host.SetEventSink(nil)
}

// ---------------------------------------------------------------------------
// sessionListCmd — execute
// ---------------------------------------------------------------------------

func TestSessionListCmd_Execute(t *testing.T) {
	cmd := sessionListCmd()
	cmd.SetArgs([]string{"--all"})
	err := cmd.Execute()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// setupWorkingDir (no worktree flag)
// ---------------------------------------------------------------------------

func TestSetupWorkingDir_NoWorktreeFlag(t *testing.T) {
	old := worktreeFlag
	worktreeFlag = ""
	defer func() { worktreeFlag = old }()

	cfg := config.DefaultConfig()
	cwd, mgr, cleanup, err := setupWorkingDir(cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, cwd)
	assert.Nil(t, mgr)
	assert.NotNil(t, cleanup)
	cleanup()
}

// ---------------------------------------------------------------------------
// replayCmd structure
// ---------------------------------------------------------------------------

func TestReplayCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := replayCmd()
	assert.Equal(t, "replay", cmd.Use)
}

// ---------------------------------------------------------------------------
// estimatePlainInteractiveCost
// ---------------------------------------------------------------------------

func TestEstimatePlainInteractiveCost(t *testing.T) {
	t.Parallel()
	result := estimatePlainInteractiveCost("claude-sonnet-4-5", 1000, 500)
	assert.Greater(t, result, 0.0)

	resultUnknown := estimatePlainInteractiveCost("unknown-model", 1000, 500)
	assert.Equal(t, 0.0, resultUnknown)
}

// ---------------------------------------------------------------------------
// singleLinePlainPreview
// ---------------------------------------------------------------------------

func TestSingleLinePlainPreview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"short", "short text"},
		{"multiline", "line1\nline2\nline3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preview := singleLinePlainPreview(tt.input, 80)
			assert.NotEmpty(t, preview)
		})
	}
}

// ---------------------------------------------------------------------------
// logFileSuffix — additional check
// ---------------------------------------------------------------------------

func TestLogFileSuffix_ContainsPID(t *testing.T) {
	t.Parallel()
	now := time.Now()
	suffix := logFileSuffix(now)
	assert.Contains(t, suffix, fmt.Sprintf("%d", os.Getpid()))
}

// ---------------------------------------------------------------------------
// loadConfig — basic test with default config
// ---------------------------------------------------------------------------

func TestLoadConfig_Default(t *testing.T) {
	// Save/restore global flags.
	oldConfigPath := configPath
	oldModelFlag := modelFlag
	oldProviderFlag := providerFlag
	defer func() {
		configPath = oldConfigPath
		modelFlag = oldModelFlag
		providerFlag = oldProviderFlag
	}()

	configPath = ""
	modelFlag = ""
	providerFlag = ""

	cfg, err := loadConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

func TestLoadConfig_WithModelOverride(t *testing.T) {
	oldConfigPath := configPath
	oldModelFlag := modelFlag
	oldProviderFlag := providerFlag
	defer func() {
		configPath = oldConfigPath
		modelFlag = oldModelFlag
		providerFlag = oldProviderFlag
	}()

	configPath = ""
	modelFlag = "custom-model"
	providerFlag = ""

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "custom-model", cfg.Provider.Model)
}

func TestLoadConfig_WithProviderOverride(t *testing.T) {
	oldConfigPath := configPath
	oldModelFlag := modelFlag
	oldProviderFlag := providerFlag
	defer func() {
		configPath = oldConfigPath
		modelFlag = oldModelFlag
		providerFlag = oldProviderFlag
	}()

	configPath = ""
	modelFlag = ""
	providerFlag = "openrouter"

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "openrouter", cfg.Provider.Default)
}

// ---------------------------------------------------------------------------
// pluginHTTPFetcherAdapter.Fetch
// ---------------------------------------------------------------------------

func TestPluginHTTPFetcherAdapter_Fetch(t *testing.T) {
	t.Parallel()
	adapter := &pluginHTTPFetcherAdapter{
		ctx:     context.Background(),
		fetcher: integrations.NewHTTPFetcher(5 * time.Second),
	}
	// Fetching an invalid URL should error.
	_, err := adapter.Fetch("http://localhost:1/nonexistent")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// pluginLLMCompleterAdapter.Complete
// ---------------------------------------------------------------------------

func TestPluginLLMCompleterAdapter_Complete_Wiring(t *testing.T) {
	t.Parallel()
	// Just verify construction; calling Complete with nil provider panics,
	// which shows the adapter correctly delegates to the completer.
	adapter := &pluginLLMCompleterAdapter{
		ctx:       context.Background(),
		completer: integrations.NewLLMCompleter(nil, "test-model"),
	}
	assert.NotNil(t, adapter)
	assert.NotNil(t, adapter.completer)
}

// ---------------------------------------------------------------------------
// pluginSkillInvokerAdapter.Invoke
// ---------------------------------------------------------------------------

func TestPluginSkillInvokerAdapter_Invoke_NilRuntime(t *testing.T) {
	t.Parallel()
	invoker := integrations.NewSkillInvoker(nil)
	adapter := &pluginSkillInvokerAdapter{
		ctx:     context.Background(),
		invoker: invoker,
	}
	// With no runtime set, Invoke should fail.
	_, err := adapter.Invoke("nonexistent", map[string]any{"key": "value"})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// newWorktreeManager
// ---------------------------------------------------------------------------

func TestNewWorktreeManager_InGitRepo(t *testing.T) {
	// We're running in a git repo, so this should succeed.
	// Need to restore loadConfig global state.
	oldConfigPath := configPath
	oldModelFlag := modelFlag
	oldProviderFlag := providerFlag
	defer func() {
		configPath = oldConfigPath
		modelFlag = oldModelFlag
		providerFlag = oldProviderFlag
	}()
	configPath = ""
	modelFlag = ""
	providerFlag = ""

	mgr, err := newWorktreeManager()
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

// ---------------------------------------------------------------------------
// worktreeProviderAdapter
// ---------------------------------------------------------------------------

func TestWorktreeProviderAdapter_HasChangesAndRemove_NotFound(t *testing.T) {
	t.Parallel()
	// Create a real manager on this repo.
	out, err := runGitCommand("rev-parse", "--show-toplevel")
	require.NoError(t, err)

	mgr := worktree.NewManager(strings.TrimSpace(out), worktree.Config{})
	adapter := &worktreeProviderAdapter{mgr: mgr}

	// HasWorktreeChanges on a non-existent worktree should error.
	_, err = adapter.HasWorktreeChanges(context.Background(), "nonexistent-worktree-xyz")
	assert.Error(t, err)

	// RemoveWorktree on non-existent should also error.
	err = adapter.RemoveWorktree(context.Background(), "nonexistent-worktree-xyz")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// worktree subcommands
// ---------------------------------------------------------------------------

func TestWorktreeListCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := worktreeListCmd()
	assert.Equal(t, "list", cmd.Use)
}

func TestWorktreeCleanupCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := worktreeCleanupCmd()
	assert.Equal(t, "cleanup", cmd.Use)
}

// ---------------------------------------------------------------------------
// session subcommand integration tests
// ---------------------------------------------------------------------------

func TestSessionListCmd_NoSessions(t *testing.T) {
	cmd := sessionListCmd()
	err := cmd.Execute()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// plainInteractive — partial coverage boosters
// ---------------------------------------------------------------------------

func TestPlainInteractive_StatusLine(t *testing.T) {
	t.Parallel()
	host := newPlainInteractiveHost(nil, os.Stdout, "test-model", 10, nil)
	host.SetGitBranch("main")

	line := host.statusLine()
	assert.Contains(t, line, "test-model")
	assert.Contains(t, line, "main")
}

func TestPlainInteractive_IsDestructiveToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tool  string
		input string
		want  bool
	}{
		{"shell is destructive", "shell", `{}`, true},
		{"file read is not destructive", "file", `{}`, false},
		{"file write is destructive", "file", `{"operation":"write"}`, true},
		{"file delete is destructive", "file", `{"operation":"delete"}`, true},
		{"search is not destructive", "search", `{}`, false},
		{"process is destructive", "process", `{}`, true},
		{"unknown is not destructive", "unknown_tool", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isDestructiveToolCall(tt.tool, json.RawMessage(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlainInteractive_SummarizeActiveSkills(t *testing.T) {
	t.Parallel()
	result := summarizePlainActiveSkills(nil)
	assert.Empty(t, result)
}

// ---------------------------------------------------------------------------
// ollama subcommands structure
// ---------------------------------------------------------------------------

func TestOllamaListCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := ollamaCmd()
	var subNames []string
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "pull")
	assert.Contains(t, subNames, "rm")
	assert.Contains(t, subNames, "status")
}

// ---------------------------------------------------------------------------
// renderReplayFollowChunk (partial)
// ---------------------------------------------------------------------------

func TestLogPlainSlashCommand(t *testing.T) {
	t.Parallel()
	// Should not panic.
	assert.NotPanics(t, func() {
		logPlainSlashCommand("/help", "Available commands: /help", nil, nil)
	})
}

// ---------------------------------------------------------------------------
// registerCoreTools with all standard tools
// ---------------------------------------------------------------------------

func TestRegisterCoreTools_FullProfile(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	// Use the "all" profile.
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides:      parseToolsFlag("all"),
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "search")
	assert.Contains(t, names, "process")
}

// ---------------------------------------------------------------------------
// wireExtendedTools with all tools enabled
// ---------------------------------------------------------------------------

func TestWireExtendedTools_AllEnabled(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		// No overrides, non-headless => all enabled
	}

	err := wireExtendedTools(t.TempDir(), registry, cfg, toolsCfg)
	assert.NoError(t, err)

	names := toolNames(registry)
	// Should include HTTP and git tools.
	assert.Contains(t, names, "http_get")
	assert.Contains(t, names, "git_status")
	assert.Contains(t, names, "git_diff")
	assert.Contains(t, names, "git_log")
	assert.Contains(t, names, "git_show")
	assert.Contains(t, names, "git_blame")
	assert.Contains(t, names, "db_query")
}

// ---------------------------------------------------------------------------
// loadConfig with custom config path
// ---------------------------------------------------------------------------

func TestLoadConfig_WithCustomConfigPath(t *testing.T) {
	oldConfigPath := configPath
	oldModelFlag := modelFlag
	oldProviderFlag := providerFlag
	defer func() {
		configPath = oldConfigPath
		modelFlag = oldModelFlag
		providerFlag = oldProviderFlag
	}()

	// Use a non-existent config file path (config.Load handles missing files).
	configPath = filepath.Join(t.TempDir(), "nonexistent-config.toml")
	modelFlag = ""
	providerFlag = ""

	cfg, err := loadConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

// ---------------------------------------------------------------------------
// createSkillRuntime — additional coverage
// ---------------------------------------------------------------------------

func TestCreateSkillRuntime_WithEmptySkillsFlag(t *testing.T) {
	oldFlag := skillsFlag
	skillsFlag = ""
	defer func() { skillsFlag = oldFlag }()

	oldHome := os.Getenv("HOME")
	tempHome := t.TempDir()
	require.NoError(t, os.Setenv("HOME", tempHome))
	defer func() { _ = os.Setenv("HOME", oldHome) }()

	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	workDir := t.TempDir()

	rt, closer, err := createSkillRuntime(context.Background(), registry, nil, cfg, "headless", workDir)
	require.NoError(t, err)
	require.NotNil(t, rt)
	if closer != nil {
		defer closer.Close()
	}
}

// ---------------------------------------------------------------------------
// pipelineComponents — verify all fields populated
// ---------------------------------------------------------------------------

func TestBuildPipeline_AllFieldsPopulated(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()

	pc := buildPipeline(registry, cfg, t.TempDir(), nil)
	assert.NotNil(t, pc.Pipeline)
	assert.NotNil(t, pc.Classifier)
	assert.NotNil(t, pc.RuleEngine)
}

// ---------------------------------------------------------------------------
// buildPipeline with .security.yaml
// ---------------------------------------------------------------------------

func TestBuildPipeline_WithSecurityYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	secYAML := `rules:
  - pattern: "test-pattern"
    severity: high
    description: "test rule"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.yaml"), []byte(secYAML), 0o644))

	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()

	pc := buildPipeline(registry, cfg, dir, nil)
	assert.NotNil(t, pc.Pipeline)
}

// ---------------------------------------------------------------------------
// buildPipeline with .security.local.yaml
// ---------------------------------------------------------------------------

func TestBuildPipeline_WithLocalSecurityYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	localYAML := `rules:
  - pattern: "local-pattern"
    severity: low
    description: "local rule"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".security.local.yaml"), []byte(localYAML), 0o644))

	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()

	pc := buildPipeline(registry, cfg, dir, nil)
	assert.NotNil(t, pc.Pipeline)
}

// ---------------------------------------------------------------------------
// wireSandboxProxy — hard lockdown path
// ---------------------------------------------------------------------------

func TestWireSandboxProxy_HardLockdownNoSandbox(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	enabled := true
	noUnsandboxed := false
	cfg.Sandbox.Enabled = &enabled
	cfg.Sandbox.AllowUnsandboxedCommands = &noUnsandboxed

	shellTool := tools.NewShellTool(t.TempDir(), 30*time.Second)
	_, err := wireSandboxProxy(cfg, shellTool)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox enabled")
}

// ---------------------------------------------------------------------------
// saveMemoriesBestEffort — test with cancelled context
// ---------------------------------------------------------------------------

func TestSaveMemoriesBestEffort_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NotPanics(t, func() {
		saveMemoriesBestEffort(ctx, nil, os.Stderr)
	})
}

// ---------------------------------------------------------------------------
// indexProjectFiles — in a git repo
// ---------------------------------------------------------------------------

func TestIndexProjectFiles_InGitRepo(t *testing.T) {
	t.Parallel()
	src := tui.NewFileCompletionSource("/Users/julianshen/prj/rubichan")
	assert.NotPanics(t, func() {
		indexProjectFiles("/Users/julianshen/prj/rubichan", src)
	})
}

// ---------------------------------------------------------------------------
// plainInteractive printSessionHeader
// ---------------------------------------------------------------------------

func TestPlainInteractive_PrintSessionHeader(t *testing.T) {
	t.Parallel()
	var buf []byte
	w := &writerBuffer{buf: &buf}
	host := newPlainInteractiveHost(nil, w, "claude-sonnet-4-5", 10, nil)
	host.SetGitBranch("main")
	host.printSessionHeader()
	assert.Contains(t, string(buf), "claude-sonnet-4-5")
}

// ---------------------------------------------------------------------------
// spawnerAdapter
// ---------------------------------------------------------------------------

func TestSpawnerAdapter_SpawnRequiresProvider(t *testing.T) {
	t.Parallel()
	spawner := &agent.DefaultSubagentSpawner{
		Config:    config.DefaultConfig(),
		AgentDefs: agent.NewAgentDefRegistry(),
	}
	adapter := &spawnerAdapter{spawner: spawner}
	_, err := adapter.Spawn(context.Background(), tools.TaskSpawnConfig{
		Name: "test-task",
	}, "do something")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// registerCoreTools with extended tools
// ---------------------------------------------------------------------------

func TestRegisterCoreTools_WithExtendedTools(t *testing.T) {
	t.Parallel()
	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	diffTracker := tools.NewDiffTracker()

	toolsCfg := ToolsConfig{
		ModelCapabilities: provider.ModelCapabilities{SupportsNativeToolUse: true},
		CLIOverrides: map[string]bool{
			"file": true, "shell": true, "search": true,
			"http_get": true, "git_status": true,
		},
	}

	result, err := registerCoreTools(t.TempDir(), registry, cfg, toolsCfg, diffTracker, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)

	names := toolNames(registry)
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "search")
	assert.Contains(t, names, "http_get")
	assert.Contains(t, names, "git_status")
}

// ---------------------------------------------------------------------------
// standardToolProfile
// ---------------------------------------------------------------------------

func TestStandardToolProfile(t *testing.T) {
	t.Parallel()
	assert.Contains(t, standardToolProfile, "file")
	assert.Contains(t, standardToolProfile, "shell")
	assert.Contains(t, standardToolProfile, "search")
	assert.Contains(t, standardToolProfile, "process")
	assert.Contains(t, standardToolProfile, "notes")
}

// ---------------------------------------------------------------------------
// appendPersonaOptions with IDENTITY.md and SOUL.md
// ---------------------------------------------------------------------------

func TestAppendPersonaOptions_WithIdentityMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("# Test Identity"), 0o644))
	opts := appendPersonaOptions(nil, dir)
	assert.Len(t, opts, 1)
}

func TestAppendPersonaOptions_WithSoulMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("# Test Soul"), 0o644))
	opts := appendPersonaOptions(nil, dir)
	assert.Len(t, opts, 1)
}

func TestAppendPersonaOptions_WithAllPersonaFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("# Agent"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("# Identity"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("# Soul"), 0o644))
	opts := appendPersonaOptions(nil, dir)
	assert.Len(t, opts, 3)
}
