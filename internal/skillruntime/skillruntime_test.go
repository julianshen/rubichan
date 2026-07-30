package skillruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/skillruntime"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOptions(t *testing.T) skillruntime.Options {
	t.Helper()
	return skillruntime.Options{
		Registry:  tools.NewRegistry(),
		Config:    &config.Config{},
		Mode:      "interactive",
		WorkDir:   t.TempDir(),
		ConfigDir: t.TempDir(),
	}
}

func TestNewRequiresConfig(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	opts.Config = nil

	rt, closer, err := skillruntime.New(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")
	assert.Nil(t, rt)
	assert.Nil(t, closer, "a failed construction must not hand back a closer to nothing")
}

// TestNewCreatesAPersistentStore pins that approvals outlive the process. An
// in-memory store would make every session re-ask for permissions a user
// already granted.
func TestNewCreatesAPersistentStore(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	rt, closer, err := skillruntime.New(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, rt)
	require.NotNil(t, closer)
	defer closer.Close()

	_, err = os.Stat(filepath.Join(opts.ConfigDir, "skills.db"))
	assert.NoError(t, err, "skill approvals are persisted next to the config")
}

// TestNewCreatesTheConfigDirectory covers a first run, where nothing under
// the config path exists yet.
func TestNewCreatesTheConfigDirectory(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	opts.ConfigDir = filepath.Join(t.TempDir(), "nested", "rubichan")

	_, closer, err := skillruntime.New(context.Background(), opts)
	require.NoError(t, err)
	defer closer.Close()

	info, err := os.Stat(opts.ConfigDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewReportsAnUncreatableConfigDirectory(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	opts := testOptions(t)
	opts.ConfigDir = filepath.Join(blocker, "rubichan")

	_, _, err := skillruntime.New(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating config directory")
}

// TestNewReportsAnUnopenableStore covers a config directory whose skills.db
// is not a database — a directory of that name, say, left by a bad upgrade.
func TestNewReportsAnUnopenableStore(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	require.NoError(t, os.MkdirAll(filepath.Join(opts.ConfigDir, "skills.db"), 0o755))

	_, _, err := skillruntime.New(context.Background(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating skill store")
}

// TestNewHonoursTheConfiguredUserDir checks the override actually displaces
// the default rather than being added alongside it: a user who points at a
// shared skill directory should not silently also load ~/.config's.
func TestNewHonoursTheConfiguredUserDir(t *testing.T) {
	t.Parallel()

	defaultDir := t.TempDir()
	customDir := t.TempDir()
	writeSkill(t, defaultDir, "from-default")
	writeSkill(t, customDir, "from-custom")

	opts := testOptions(t)
	opts.ConfigDir = defaultDir
	opts.Config.Skills.UserDir = customDir

	rt, closer, err := skillruntime.New(context.Background(), opts)
	require.NoError(t, err)
	defer closer.Close()

	names := discoveredNames(rt)
	assert.Contains(t, names, "from-custom")
	assert.NotContains(t, names, "from-default")
}

// TestNewDiscoversProjectSkills covers the .rubichan/skills directory, which
// is how a repository ships skills to whoever checks it out.
func TestNewDiscoversProjectSkills(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	writeSkill(t, filepath.Join(opts.WorkDir, ".rubichan", "skills"), "project-skill")

	rt, closer, err := skillruntime.New(context.Background(), opts)
	require.NoError(t, err)
	defer closer.Close()

	assert.Contains(t, discoveredNames(rt), "project-skill")
}

// TestNewRegistersBuiltinSkills guards the built-ins against being lost in
// wiring: they are registered by New itself, not discovered from disk.
func TestNewRegistersBuiltinSkills(t *testing.T) {
	t.Parallel()

	rt, closer, err := skillruntime.New(context.Background(), testOptions(t))
	require.NoError(t, err)
	defer closer.Close()

	assert.NotEmpty(t, discoveredNames(rt), "the built-in prompt skills should always be present")
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	manifest := "name: " + name + "\nversion: 0.0.1\ndescription: test skill\ntype: prompt\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\n"+manifest+"---\n\nbody\n"), 0o600))
}

// discoveredNames lists every skill the runtime saw, activated or not.
// Activation depends on triggers; discovery is what these tests are about.
func discoveredNames(rt *skills.Runtime) []string {
	reports := rt.GetActivationReports()
	names := make([]string, 0, len(reports))
	for _, r := range reports {
		if r.Skill.Manifest != nil {
			names = append(names, r.Skill.Manifest.Name)
		}
	}
	return names
}
