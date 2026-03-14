package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolsFlagEmpty(t *testing.T) {
	result := parseToolsFlag("")
	assert.Nil(t, result)
}

func TestParseToolsFlagWhitespace(t *testing.T) {
	result := parseToolsFlag("   ")
	assert.Nil(t, result)
}

func TestParseToolsFlagSingle(t *testing.T) {
	result := parseToolsFlag("file")
	assert.True(t, result["file"])
	assert.False(t, result["shell"])
}

func TestParseToolsFlagMultiple(t *testing.T) {
	result := parseToolsFlag("file,shell")
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
}

func TestParseToolsFlagWithSpaces(t *testing.T) {
	result := parseToolsFlag(" file , shell ")
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
}

func TestToolsConfigShouldEnableDefaultsToTrue(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
	}
	assert.True(t, tc.ShouldEnable("file"))
	assert.True(t, tc.ShouldEnable("shell"))
}

func TestToolsConfigShouldEnableWithCLIOverrides(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		CLIOverrides:      map[string]bool{"file": true},
	}
	assert.True(t, tc.ShouldEnable("file"))
	assert.False(t, tc.ShouldEnable("shell"))
}

func TestToolsConfigShouldEnableRespectsModelCapability(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: false},
	}
	assert.False(t, tc.ShouldEnable("file"))
}

func TestToolsConfigShouldEnableRespectsUserPrefs(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		UserPreferences: UserToolPrefs{
			Disabled: map[string]bool{"shell": true},
			Enabled:  map[string]bool{"file": true, "shell": true},
		},
	}
	assert.True(t, tc.ShouldEnable("file"))
	assert.False(t, tc.ShouldEnable("shell"))
	assert.False(t, tc.ShouldEnable("search"))
}

func TestToolsConfigShouldEnableRespectsAppleProjectContext(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		ProjectContext: ProjectContext{
			AppleProjectDetected: false,
			AppleSkillRequested:  false,
		},
	}
	assert.False(t, tc.ShouldEnable("xcode_build"))
	assert.False(t, tc.ShouldEnable("swift_test"))
	assert.False(t, tc.ShouldEnable("sim_boot"))
	assert.False(t, tc.ShouldEnable("codesign_verify"))
	assert.False(t, tc.ShouldEnable("xcrun"))
	assert.True(t, tc.ShouldEnable("file"))

	tc.ProjectContext.AppleSkillRequested = true
	assert.True(t, tc.ShouldEnable("xcode_build"))
	assert.True(t, tc.ShouldEnable("swift_test"))
	assert.True(t, tc.ShouldEnable("sim_boot"))
	assert.True(t, tc.ShouldEnable("codesign_verify"))
	assert.True(t, tc.ShouldEnable("xcrun"))
}

func TestToolsConfigShouldEnableRespectsFeatureFlags(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		FeatureFlags:      map[string]bool{"tools.shell": false},
	}
	assert.False(t, tc.ShouldEnable("shell"))
	assert.True(t, tc.ShouldEnable("file"))
}

func TestToolsConfigShouldEnableHeadlessDenyByDefault(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		HeadlessMode:      true,
	}
	// No CLIOverrides → all tools denied in headless mode.
	assert.False(t, tc.ShouldEnable("file"))
	assert.False(t, tc.ShouldEnable("shell"))
	assert.False(t, tc.ShouldEnable("search"))
}

func TestToolsConfigShouldEnableHeadlessWithExplicitAllowlist(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		HeadlessMode:      true,
		CLIOverrides:      map[string]bool{"file": true, "search": true},
	}
	assert.True(t, tc.ShouldEnable("file"))
	assert.True(t, tc.ShouldEnable("search"))
	assert.False(t, tc.ShouldEnable("shell"))
}

func TestParseToolsFlagAllProfile(t *testing.T) {
	// "all" is a special profile that expands to all standard tools.
	result := parseToolsFlag("all")
	assert.NotNil(t, result)
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
	assert.True(t, result["search"])
	assert.True(t, result["process"])
	assert.True(t, result["notes"])
}

func TestApplyHeadlessBootstrapProbePromptEnabled(t *testing.T) {
	prompt := "Build a backend and verify it"
	got := applyHeadlessBootstrapProbePrompt(prompt, true)
	assert.Contains(t, got, "Headless bootstrap requirement:")
	assert.Contains(t, got, `{"command":"pwd"}`)
	assert.Contains(t, got, "User task:")
	assert.True(t, strings.HasSuffix(got, prompt))
}

func TestApplyHeadlessBootstrapProbePromptDisabled(t *testing.T) {
	prompt := "Build a backend and verify it"
	got := applyHeadlessBootstrapProbePrompt(prompt, false)
	assert.Equal(t, prompt, got)
}

func TestIsAppleOnlyTool(t *testing.T) {
	assert.True(t, isAppleOnlyTool("xcode_build"))
	assert.True(t, isAppleOnlyTool("swift_test"))
	assert.True(t, isAppleOnlyTool("sim_boot"))
	assert.True(t, isAppleOnlyTool("codesign_verify"))
	assert.True(t, isAppleOnlyTool("xcrun"))
	assert.False(t, isAppleOnlyTool("file"))
}

func TestDetectModelCapabilities(t *testing.T) {
	assert.False(t, detectModelCapabilities(nil).SupportsToolUse)

	cfg := config.DefaultConfig()
	assert.True(t, detectModelCapabilities(cfg).SupportsToolUse)

	cfg.Provider.Default = "ollama"
	assert.True(t, detectModelCapabilities(cfg).SupportsToolUse)

	cfg.Provider.Default = "openrouter"
	cfg.Provider.OpenAI = []config.OpenAICompatibleConfig{
		{Name: "openrouter"},
	}
	assert.True(t, detectModelCapabilities(cfg).SupportsToolUse)

	cfg.Provider.Default = "unknown-provider"
	cfg.Provider.OpenAI = nil
	assert.False(t, detectModelCapabilities(cfg).SupportsToolUse)
}

func TestParseSkillsFlagEmpty(t *testing.T) {
	result := parseSkillsFlag("")
	assert.Nil(t, result)
}

func TestParseSkillsFlagWhitespace(t *testing.T) {
	result := parseSkillsFlag("   ")
	assert.Nil(t, result)
}

func TestParseSkillsFlagSingle(t *testing.T) {
	result := parseSkillsFlag("my-skill")
	assert.Equal(t, []string{"my-skill"}, result)
}

func TestParseSkillsFlagMultiple(t *testing.T) {
	result := parseSkillsFlag("skill-a,skill-b")
	assert.Equal(t, []string{"skill-a", "skill-b"}, result)
}

func TestParseSkillsFlagWithSpaces(t *testing.T) {
	result := parseSkillsFlag(" skill-a , skill-b ")
	assert.Equal(t, []string{"skill-a", "skill-b"}, result)
}

func TestCreateSkillRuntimeNilConfig(t *testing.T) {
	// When config is nil, createSkillRuntime returns an error.
	oldFlag := skillsFlag
	skillsFlag = ""
	defer func() { skillsFlag = oldFlag }()

	rt, closer, err := createSkillRuntime(context.Background(), nil, nil, nil, "interactive", t.TempDir())
	assert.Error(t, err)
	assert.Nil(t, rt)
	assert.Nil(t, closer)
}

func TestCreateSkillRuntimeInteractiveActivatesFrontendDesignForFrontendApp(t *testing.T) {
	oldFlag := skillsFlag
	skillsFlag = ""
	defer func() { skillsFlag = oldFlag }()

	oldHome := os.Getenv("HOME")
	tempHome := t.TempDir()
	require.NoError(t, os.Setenv("HOME", tempHome))
	defer func() {
		_ = os.Setenv("HOME", oldHome)
	}()

	registry := tools.NewRegistry()
	cfg := config.DefaultConfig()
	workDir := filepath.Join("..", "..", "examples", "app-generation-smoke")

	rt, closer, err := createSkillRuntime(context.Background(), registry, nil, cfg, "interactive", workDir)
	require.NoError(t, err)
	require.NotNil(t, rt)
	if closer != nil {
		defer closer.Close()
	}

	summaries := rt.GetAllSkillSummaries()
	var found bool
	for _, summary := range summaries {
		if summary.Name == "frontend-design" {
			found = true
			assert.Equal(t, "Active", summary.State.String())
			break
		}
	}
	assert.True(t, found, "expected frontend-design to be discovered")
}

func TestEmitSkillDiscoveryWarnings(t *testing.T) {
	userDir := t.TempDir()
	skillDir := filepath.Join(userDir, "opt-dep-skill")
	assert.NoError(t, os.MkdirAll(skillDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte(`name: opt-dep-skill
version: 1.0.0
description: "skill with optional dependency"
types:
  - tool
implementation:
  backend: starlark
  entrypoint: skill.star
dependencies:
  - name: missing-optional
    optional: true
`), 0o644))

	loader := skills.NewLoader(userDir, "")
	rt := skills.NewRuntime(loader, nil, nil, nil, nil, nil)
	assert.NoError(t, rt.Discover(nil))

	var buf bytes.Buffer
	emitSkillDiscoveryWarnings(&buf, rt)
	assert.Contains(t, buf.String(), "warning: skill \"opt-dep-skill\": optional dependency \"missing-optional\" not found")
}
