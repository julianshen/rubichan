package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlineContentMaterialize(t *testing.T) {
	tmpDir := t.TempDir()
	ic := &InlineContent{
		Files: map[string]string{
			"SKILL.yaml": "name: test-skill\nversion: 1.0.0",
			"SKILL.md":   "# Test Skill\n\nThis is a test.",
		},
	}

	skillDir, err := ic.Materialize(tmpDir, "test-skill")
	require.NoError(t, err)
	assert.DirExists(t, skillDir)

	manifestPath := filepath.Join(skillDir, "SKILL.yaml")
	assert.FileExists(t, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, ic.Files["SKILL.yaml"], string(manifestData))

	mdPath := filepath.Join(skillDir, "SKILL.md")
	assert.FileExists(t, mdPath)
	mdData, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.Equal(t, ic.Files["SKILL.md"], string(mdData))
}

func TestEmbedContentMaterialize(t *testing.T) {
	tmpDir := t.TempDir()
	ec := &EmbedContent{
		FS:     os.DirFS("testdata/embed-skill"),
		Prefix: ".",
	}

	skillDir, err := ec.Materialize(tmpDir, "embed-skill")
	require.NoError(t, err)
	assert.DirExists(t, skillDir)

	manifestPath := filepath.Join(skillDir, "SKILL.yaml")
	assert.FileExists(t, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Contains(t, string(manifestData), "name: embed-skill")

	mdPath := filepath.Join(skillDir, "SKILL.md")
	assert.FileExists(t, mdPath)
	mdData, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.Contains(t, string(mdData), "Embed Skill")
}

func TestEmbedContentMaterializeWithPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	ec := &EmbedContent{
		FS:     os.DirFS("testdata"),
		Prefix: "embed-skill",
	}

	skillDir, err := ec.Materialize(tmpDir, "embed-skill")
	require.NoError(t, err)
	assert.DirExists(t, skillDir)

	manifestPath := filepath.Join(skillDir, "SKILL.yaml")
	assert.FileExists(t, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Contains(t, string(manifestData), "name: embed-skill")

	mdPath := filepath.Join(skillDir, "SKILL.md")
	assert.FileExists(t, mdPath)
	mdData, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.Contains(t, string(mdData), "Embed Skill")
}

func TestFileMapContentMaterialize(t *testing.T) {
	tmpDir := t.TempDir()
	fmc := &FileMapContent{
		Files: map[string][]byte{
			"SKILL.yaml":  []byte("name: filemap-skill\nversion: 3.0.0"),
			"SKILL.md":    []byte("# FileMap Skill\n\nFileMap content."),
			"lib/util.go": []byte("package util\n\nfunc Helper() {}"),
		},
	}

	skillDir, err := fmc.Materialize(tmpDir, "filemap-skill")
	require.NoError(t, err)
	assert.DirExists(t, skillDir)

	manifestPath := filepath.Join(skillDir, "SKILL.yaml")
	assert.FileExists(t, manifestPath)
	manifestData, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, fmc.Files["SKILL.yaml"], manifestData)

	mdPath := filepath.Join(skillDir, "SKILL.md")
	assert.FileExists(t, mdPath)
	mdData, err := os.ReadFile(mdPath)
	require.NoError(t, err)
	assert.Equal(t, fmc.Files["SKILL.md"], mdData)

	utilPath := filepath.Join(skillDir, "lib", "util.go")
	assert.FileExists(t, utilPath)
	utilData, err := os.ReadFile(utilPath)
	require.NoError(t, err)
	assert.Equal(t, fmc.Files["lib/util.go"], utilData)
}

func TestLoaderRegisterBundled(t *testing.T) {
	loader := NewLoader("", "")
	bs := BundledSkill{
		Name:        "test-bundled",
		Description: "A test bundled skill",
		Version:     "1.0.0",
		Content: &InlineContent{
			Files: map[string]string{
				"SKILL.yaml": "name: test-bundled\nversion: 1.0.0",
			},
		},
	}

	loader.RegisterBundled(bs)
	assert.Len(t, loader.bundled, 1)
	assert.Contains(t, loader.bundled, "test-bundled")
}

func TestLoaderDiscoverBundled(t *testing.T) {
	loader := NewLoader("", "")
	bs := BundledSkill{
		Name:        "test-bundled",
		Description: "A test bundled skill",
		Version:     "1.0.0",
		Content: &InlineContent{
			Files: map[string]string{
				"SKILL.yaml": "name: test-bundled\nversion: 1.0.0",
			},
		},
	}

	loader.RegisterBundled(bs)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Len(t, skills, 1)
	assert.Equal(t, "test-bundled", skills[0].Manifest.Name)
	assert.Equal(t, SourceBundled, skills[0].Source)
}
