package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillWatcherNew(t *testing.T) {
	rt := NewRuntime(nil, nil, nil, nil, nil, nil)
	w, err := NewSkillWatcher(rt)
	require.NoError(t, err)
	require.NotNil(t, w)
	w.Stop()
}

func TestSkillWatcherIsSkillFile(t *testing.T) {
	rt := NewRuntime(nil, nil, nil, nil, nil, nil)
	w, err := NewSkillWatcher(rt)
	require.NoError(t, err)
	defer w.Stop()

	assert.True(t, w.isSkillFile("/path/to/SKILL.yaml"))
	assert.True(t, w.isSkillFile("/path/to/SKILL.md"))
	assert.True(t, w.isSkillFile("/path/to/skill.md"))
	assert.True(t, w.isSkillFile("/home/user/.kilo/skills/my-skill/SKILL.yaml"))
	assert.False(t, w.isSkillFile("/path/to/random.txt"))
	assert.False(t, w.isSkillFile("/path/to/main.go"))
}

func TestSkillWatcherStartStop(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	rt := NewRuntime(loader, nil, nil, nil, nil, nil)

	w, err := NewSkillWatcher(rt)
	require.NoError(t, err)

	require.NoError(t, w.Start())
	// Give it a moment to set up watches.
	time.Sleep(100 * time.Millisecond)
	w.Stop()
}

func TestSkillWatcherReloadOnChange(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	rt := NewRuntime(loader, nil, nil, nil, nil, nil)

	// Start watcher with short debounce for testing.
	w, err := NewSkillWatcher(rt)
	require.NoError(t, err)
	w.debounce = 200 * time.Millisecond
	require.NoError(t, w.Start())
	defer w.Stop()

	// Create a new skill file in an already-watched directory.
	newSkillDir := filepath.Join(userDir, "new-skill")
	require.NoError(t, os.MkdirAll(newSkillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(newSkillDir, "SKILL.yaml"),
		[]byte("name: new-skill\nversion: 1.0.0\ndescription: A new skill\ntypes:\n  - prompt\n"),
		0o644,
	))

	// Trigger reload via the event loop by writing a file in a watched dir.
	testFile := filepath.Join(userDir, "trigger.md")
	require.NoError(t, os.WriteFile(testFile, []byte("trigger"), 0o644))

	// Wait for debounce + reload with retry.
	require.Eventually(t, func() bool {
		rt.mu.RLock()
		_, ok := rt.skills["new-skill"]
		rt.mu.RUnlock()
		return ok
	}, 2*time.Second, 100*time.Millisecond, "new skill should be discovered after reload")
}

func TestSkillWatcherAutoAddWatch(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	loader := NewLoader(userDir, projectDir)
	rt := NewRuntime(loader, nil, nil, nil, nil, nil)

	w, err := NewSkillWatcher(rt)
	require.NoError(t, err)
	w.debounce = 100 * time.Millisecond
	require.NoError(t, w.Start())
	defer w.Stop()

	// Create a new skill directory that matches isSkillDir pattern.
	newSkillDir := filepath.Join(userDir, ".kilo", "skills", "auto-skill")
	require.NoError(t, os.MkdirAll(newSkillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(newSkillDir, "SKILL.yaml"),
		[]byte("name: auto-skill\nversion: 1.0.0\ndescription: Auto discovered\ntypes:\n  - prompt\n"),
		0o644,
	))

	// Trigger reload by touching a file in the watched userDir.
	testFile := filepath.Join(userDir, "trigger.md")
	require.NoError(t, os.WriteFile(testFile, []byte("trigger"), 0o644))

	// Wait for debounce + reload with retry.
	require.Eventually(t, func() bool {
		rt.mu.RLock()
		_, ok := rt.skills["auto-skill"]
		rt.mu.RUnlock()
		return ok
	}, 2*time.Second, 100*time.Millisecond, "skill in auto-added watch dir should be discovered")
}
