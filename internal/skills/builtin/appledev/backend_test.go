package appledev

import (
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools/xcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verify interface compliance.
var _ skills.SkillBackend = (*Backend)(nil)

func TestManifest(t *testing.T) {
	m := Manifest()
	assert.Equal(t, "apple-dev", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Contains(t, m.Types, skills.SkillTypeTool)
	assert.Contains(t, m.Types, skills.SkillTypePrompt)
}

func TestBackend_LoadDarwin(t *testing.T) {
	pc := &xcode.MockPlatformChecker{Darwin: true, XcodeBinPath: "/dev"}
	b := &Backend{WorkDir: "/tmp", Platform: pc}
	require.NoError(t, b.Load(skills.SkillManifest{}, nil))

	toolNames := make([]string, len(b.Tools()))
	for i, tool := range b.Tools() {
		toolNames[i] = tool.Name()
	}

	// Cross-platform tools (5)
	assert.Contains(t, toolNames, "xcode_discover")
	assert.Contains(t, toolNames, "swift_build")
	assert.Contains(t, toolNames, "swift_test")
	assert.Contains(t, toolNames, "swift_resolve")
	assert.Contains(t, toolNames, "swift_add_dep")

	// Darwin-only tools (13)
	assert.Contains(t, toolNames, "xcode_build")
	assert.Contains(t, toolNames, "xcode_test")
	assert.Contains(t, toolNames, "sim_list")
	assert.Contains(t, toolNames, "codesign_info")
	assert.Contains(t, toolNames, "xcrun")

	assert.Equal(t, 18, len(b.Tools())) // 5 cross-platform + 13 darwin
}

func TestBackend_LoadNonDarwin(t *testing.T) {
	pc := &xcode.MockPlatformChecker{Darwin: false}
	b := &Backend{WorkDir: "/tmp", Platform: pc}
	require.NoError(t, b.Load(skills.SkillManifest{}, nil))

	toolNames := make([]string, len(b.Tools()))
	for i, tool := range b.Tools() {
		toolNames[i] = tool.Name()
	}

	// Only cross-platform tools
	assert.Contains(t, toolNames, "xcode_discover")
	assert.Contains(t, toolNames, "swift_build")
	assert.Equal(t, 5, len(b.Tools()))

	// No darwin-only tools
	assert.NotContains(t, toolNames, "xcode_build")
	assert.NotContains(t, toolNames, "sim_list")
}

func TestBackend_HooksNil(t *testing.T) {
	b := &Backend{}
	assert.Nil(t, b.Hooks())
}

func TestBackend_UnloadNoError(t *testing.T) {
	b := &Backend{}
	assert.NoError(t, b.Unload())
}
