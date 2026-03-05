package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
	assert.True(t, tc.ShouldEnable("file"))

	tc.ProjectContext.AppleSkillRequested = true
	assert.True(t, tc.ShouldEnable("xcode_build"))
}

func TestToolsConfigShouldEnableRespectsFeatureFlags(t *testing.T) {
	tc := ToolsConfig{
		ModelCapabilities: ModelCapabilities{SupportsToolUse: true},
		FeatureFlags:      map[string]bool{"tools.shell": false},
	}
	assert.False(t, tc.ShouldEnable("shell"))
	assert.True(t, tc.ShouldEnable("file"))
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

	rt, closer, err := createSkillRuntime(context.Background(), nil, nil, nil, "interactive")
	assert.Error(t, err)
	assert.Nil(t, rt)
	assert.Nil(t, closer)
}
