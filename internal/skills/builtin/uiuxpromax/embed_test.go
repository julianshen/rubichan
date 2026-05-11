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
	assert.Equal(t, skills.SourceBundled, ds.Source)
	assert.Empty(t, ds.Dir)
	assert.Empty(t, ds.InstructionBody)
}

func TestRegisterIsIdempotent(t *testing.T) {
	cacheRoot := t.TempDir()
	loader := skills.NewLoader("", "")

	require.NoError(t, Register(loader, cacheRoot))
	require.NoError(t, Register(loader, cacheRoot))

	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, "ui-ux-pro-max", discovered[0].Manifest.Name)
}

func TestRegisterReturnsErrorForInvalidCacheRoot(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(cacheRoot, []byte("x"), 0o644))

	loader := skills.NewLoader("", "")
	// With lazy materialization, registration itself does not touch cacheRoot.
	err := Register(loader, cacheRoot)
	require.NoError(t, err)
}

func TestEmbedContentMaterialize(t *testing.T) {
	ec := &skills.EmbedContent{
		FS:     content,
		Prefix: embeddedSkillRoot,
	}

	cacheDir := t.TempDir()
	skillDir, err := ec.Materialize(cacheDir, "ui-ux-pro-max")
	require.NoError(t, err)

	assert.DirExists(t, skillDir)
	assert.FileExists(t, filepath.Join(skillDir, "SKILL.md"))
	assert.FileExists(t, filepath.Join(skillDir, "scripts", "search.py"))
	assert.FileExists(t, filepath.Join(skillDir, "data", "styles.csv"))
	assert.FileExists(t, filepath.Join(skillDir, "templates", "platforms", "codex.json"))
}
