package uiuxpromax

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterPopulatesLoader(t *testing.T) {
	loader := skills.NewLoader("", "")

	err := Register(loader, t.TempDir())
	require.NoError(t, err)

	discovered, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, discovered, 1)

	ds := discovered[0]
	assert.Equal(t, "ui-ux-pro-max", ds.Manifest.Name)
	assert.Equal(t, skills.SourceBuiltin, ds.Source)
	assert.NotEmpty(t, ds.InstructionBody)
	assert.DirExists(t, ds.Dir)
	assert.FileExists(t, filepath.Join(ds.Dir, "SKILL.md"))
	assert.FileExists(t, filepath.Join(ds.Dir, "scripts", "search.py"))
	assert.FileExists(t, filepath.Join(ds.Dir, "data", "styles.csv"))
	assert.FileExists(t, filepath.Join(ds.Dir, "templates", "platforms", "codex.json"))
}

func TestRegisterIsIdempotentForMaterializedCache(t *testing.T) {
	cacheRoot := t.TempDir()
	loader := skills.NewLoader("", "")

	require.NoError(t, Register(loader, cacheRoot))

	scriptPath := filepath.Join(cacheRoot, "builtin-skills", "ui-ux-pro-max", "scripts", "search.py")
	require.NoError(t, os.WriteFile(scriptPath, []byte("# sentinel\n"), 0o644))

	require.NoError(t, Register(loader, cacheRoot))

	data, err := os.ReadFile(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, "# sentinel\n", string(data))
	assert.FileExists(t, filepath.Join(cacheRoot, "builtin-skills", "ui-ux-pro-max", ".version"))
}

func TestRegisterReturnsErrorForInvalidCacheRoot(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(cacheRoot, []byte("x"), 0o644))

	loader := skills.NewLoader("", "")
	err := Register(loader, cacheRoot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create builtin skill directory")
}
