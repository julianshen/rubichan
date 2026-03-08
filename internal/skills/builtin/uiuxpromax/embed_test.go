package uiuxpromax

import (
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
