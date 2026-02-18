package builtin

import (
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopChecker is a no-op PermissionChecker for testing.
type noopChecker struct{}

func (noopChecker) CheckPermission(_ skills.Permission) error { return nil }
func (noopChecker) CheckRateLimit(_ string) error             { return nil }
func (noopChecker) ResetTurnLimits()                          {}

func TestCoreToolsManifest(t *testing.T) {
	m := CoreToolsManifest()

	assert.Equal(t, "core-tools", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	require.Len(t, m.Types, 1)
	assert.Equal(t, skills.SkillTypeTool, m.Types[0])
	assert.Empty(t, m.Permissions, "built-in core-tools needs no declared permissions")
	assert.Empty(t, string(m.Implementation.Backend), "built-in skills should not set a backend")
}

func TestCoreToolsRegistersFileShell(t *testing.T) {
	backend := &CoreToolsBackend{WorkDir: t.TempDir()}
	m := CoreToolsManifest()

	err := backend.Load(m, noopChecker{})
	require.NoError(t, err)

	toolList := backend.Tools()
	require.Len(t, toolList, 2, "core-tools should expose exactly 2 tools")

	names := make(map[string]bool)
	for _, tool := range toolList {
		names[tool.Name()] = true
	}
	assert.True(t, names["file"], "core-tools must expose a 'file' tool")
	assert.True(t, names["shell"], "core-tools must expose a 'shell' tool")

	// Hooks should be empty.
	assert.Empty(t, backend.Hooks())

	// Unload should succeed.
	assert.NoError(t, backend.Unload())
}
