package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSkillYAML is a test helper that creates a SKILL.yaml inside dir/<name>/.
func writeSkillYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte(content), 0o644))
}

// minimalManifestYAML returns a valid SKILL.yaml string for the given name.
func minimalManifestYAML(name string) string {
	return `name: ` + name + `
version: 1.0.0
description: "Skill ` + name + `"
types:
  - prompt
`
}

func TestDiscoverUserSkills(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkillYAML(t, userDir, "my-skill", minimalManifestYAML("my-skill"))
	writeSkillYAML(t, userDir, "another-skill", minimalManifestYAML("another-skill"))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 2)

	// Both should be from user source.
	byName := indexByName(skills)
	s1 := byName["my-skill"]
	require.NotNil(t, s1)
	assert.Equal(t, SourceUser, s1.Source)
	assert.Equal(t, filepath.Join(userDir, "my-skill"), s1.Dir)

	s2 := byName["another-skill"]
	require.NotNil(t, s2)
	assert.Equal(t, SourceUser, s2.Source)
}

func TestDiscoverProjectSkills(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkillYAML(t, projectDir, "proj-skill", minimalManifestYAML("proj-skill"))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)
	assert.Equal(t, "proj-skill", skills[0].Manifest.Name)
	assert.Equal(t, SourceProject, skills[0].Source)
	assert.Equal(t, filepath.Join(projectDir, "proj-skill"), skills[0].Dir)
}

func TestDiscoverExplicitSkills(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Place the skill in the user dir so it can be found.
	writeSkillYAML(t, userDir, "explicit-one", minimalManifestYAML("explicit-one"))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover([]string{"explicit-one"})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)
	assert.Equal(t, "explicit-one", skills[0].Manifest.Name)
	// Explicit flag overrides the source to SourceInline.
	assert.Equal(t, SourceInline, skills[0].Source)
}

func TestDiscoverDeduplication(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Same skill name in both user and project dirs.
	writeSkillYAML(t, userDir, "shared-skill", `name: shared-skill
version: 2.0.0
description: "User version"
types:
  - prompt
`)
	writeSkillYAML(t, projectDir, "shared-skill", `name: shared-skill
version: 1.0.0
description: "Project version"
types:
  - prompt
`)

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)

	// User skill should win over project skill.
	assert.Equal(t, "shared-skill", skills[0].Manifest.Name)
	assert.Equal(t, "2.0.0", skills[0].Manifest.Version)
	assert.Equal(t, SourceUser, skills[0].Source)
}

func TestDiscoverDeduplicationBuiltinWins(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Place a skill in user dir.
	writeSkillYAML(t, userDir, "builtin-skill", minimalManifestYAML("builtin-skill"))

	loader := NewLoader(userDir, projectDir)

	// Register a builtin with the same name.
	builtinManifest := &SkillManifest{
		Name:        "builtin-skill",
		Version:     "9.0.0",
		Description: "Builtin version",
		Types:       []SkillType{SkillTypePrompt},
	}
	loader.RegisterBuiltin(builtinManifest)

	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)

	// Builtin should win over user skill.
	assert.Equal(t, "builtin-skill", skills[0].Manifest.Name)
	assert.Equal(t, "9.0.0", skills[0].Manifest.Version)
	assert.Equal(t, SourceBuiltin, skills[0].Source)
}

func TestDiscoverMissingRequiredDep(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkillYAML(t, userDir, "dep-skill", `name: dep-skill
version: 1.0.0
description: "Has a required dep"
types:
  - prompt
dependencies:
  - name: nonexistent
    version: ">=1.0.0"
`)

	loader := NewLoader(userDir, projectDir)
	_, _, err := loader.Discover(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "dep-skill")
}

func TestDiscoverMissingOptionalDep(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkillYAML(t, userDir, "opt-dep-skill", `name: opt-dep-skill
version: 1.0.0
description: "Has an optional dep"
types:
  - prompt
dependencies:
  - name: missing-optional
    version: ">=1.0.0"
    optional: true
`)

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	assert.Equal(t, "opt-dep-skill", skills[0].Manifest.Name)

	// Should produce a warning, not an error.
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "missing-optional")
	assert.Contains(t, warnings[0], "opt-dep-skill")
}

func TestDiscoverEmptyDirs(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Empty(t, skills)
}

func TestDiscoverNonexistentDirs(t *testing.T) {
	loader := NewLoader("/nonexistent/user/dir", "/nonexistent/project/dir")
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Empty(t, skills)
}

func TestDiscoverInvalidManifest(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	skillDir := filepath.Join(userDir, "bad-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte("{{{invalid yaml"), 0o644))

	loader := NewLoader(userDir, projectDir)
	_, _, err := loader.Discover(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad-skill")
}

func TestDiscoverExplicitNotFound(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	_, _, err := loader.Discover([]string{"nonexistent-skill"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-skill")
}

func TestDiscoverSkipsNonSkillEntries(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Create a regular file (not a directory) in the user dir -- should be skipped.
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "not-a-dir.txt"), []byte("hello"), 0o644))

	// Create a subdirectory without SKILL.yaml -- should be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(userDir, "no-manifest"), 0o755))

	// Create a valid skill alongside the noise.
	writeSkillYAML(t, userDir, "real-skill", minimalManifestYAML("real-skill"))

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 1)
	assert.Equal(t, "real-skill", skills[0].Manifest.Name)
}

func TestDiscoverSatisfiedDependency(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	writeSkillYAML(t, userDir, "base-skill", minimalManifestYAML("base-skill"))
	writeSkillYAML(t, userDir, "dependent-skill", `name: dependent-skill
version: 1.0.0
description: "Depends on base-skill"
types:
  - prompt
dependencies:
  - name: base-skill
    version: ">=1.0.0"
`)

	loader := NewLoader(userDir, projectDir)
	skills, warnings, err := loader.Discover(nil)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, skills, 2)
}

func TestDiscoverMCPServers(t *testing.T) {
	mcpServers := []config.MCPServerConfig{
		{Name: "filesystem", Transport: "stdio", Command: "echo", Args: []string{"test"}},
		{Name: "web-search", Transport: "sse", URL: "http://localhost:3001/sse"},
	}

	loader := NewLoader(t.TempDir(), t.TempDir())
	loader.AddMCPServers(mcpServers)

	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)
	require.Len(t, discovered, 2)

	byName := indexByName(discovered)

	// Verify stdio transport config is preserved.
	fs := byName["mcp-filesystem"]
	require.NotNil(t, fs)
	assert.Equal(t, SourceMCP, fs.Source)
	assert.Equal(t, BackendMCP, fs.Manifest.Implementation.Backend)
	assert.Equal(t, "stdio", fs.Manifest.Implementation.MCPTransport)
	assert.Equal(t, "echo", fs.Manifest.Implementation.MCPCommand)
	assert.Equal(t, []string{"test"}, fs.Manifest.Implementation.MCPArgs)

	// Verify SSE transport config is preserved.
	ws := byName["mcp-web-search"]
	require.NotNil(t, ws)
	assert.Equal(t, SourceMCP, ws.Source)
	assert.Equal(t, BackendMCP, ws.Manifest.Implementation.Backend)
	assert.Equal(t, "sse", ws.Manifest.Implementation.MCPTransport)
	assert.Equal(t, "http://localhost:3001/sse", ws.Manifest.Implementation.MCPURL)
}

func TestDiscoverMCPNameCollision(t *testing.T) {
	userDir := t.TempDir()

	// Place a user skill with the same name as an MCP server would generate.
	writeSkillYAML(t, userDir, "mcp-filesystem", minimalManifestYAML("mcp-filesystem"))

	mcpServers := []config.MCPServerConfig{
		{Name: "filesystem", Transport: "stdio", Command: "echo"},
	}

	loader := NewLoader(userDir, t.TempDir())
	loader.AddMCPServers(mcpServers)

	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)
	require.Len(t, discovered, 1)

	// User skill should win — MCP auto-discovery skips if name already exists.
	assert.Equal(t, SourceUser, discovered[0].Source)
}

// indexByName builds a map of DiscoveredSkill by manifest name for test convenience.
func indexByName(skills []DiscoveredSkill) map[string]*DiscoveredSkill {
	m := make(map[string]*DiscoveredSkill, len(skills))
	for i := range skills {
		m[skills[i].Manifest.Name] = &skills[i]
	}
	return m
}
