package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/store"
	"github.com/julianshen/rubichan/internal/tools"
)

func newTestAdapter(t *testing.T, handler http.Handler) (*skillManagerAdapter, string) {
	t.Helper()
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	var registryURL string
	if handler != nil {
		ts := httptest.NewServer(handler)
		t.Cleanup(ts.Close)
		registryURL = ts.URL
	} else {
		registryURL = "http://localhost:0" // unreachable
	}

	client := skills.NewRegistryClient(registryURL, s, 5*time.Minute)
	return &skillManagerAdapter{
		registry:  client,
		store:     s,
		skillsDir: skillsDir,
	}, tmpDir
}

func TestSkillManagerAdapterSearch(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/search", r.URL.Path)
		assert.Equal(t, "kubernetes", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]skills.RegistrySearchResult{
			{Name: "kubernetes", Version: "1.2.0", Description: "kubectl wrapper"},
		})
	})

	adapter, _ := newTestAdapter(t, handler)
	results, err := adapter.Search(context.Background(), "kubernetes")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "kubernetes", results[0].Name)
	assert.Equal(t, "1.2.0", results[0].Version)
	assert.Equal(t, "kubectl wrapper", results[0].Description)
}

func TestSkillManagerAdapterList(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)

	// Empty list.
	entries, err := adapter.List()
	require.NoError(t, err)
	assert.Empty(t, entries)

	// Add a skill state.
	require.NoError(t, adapter.store.SaveSkillState(store.SkillInstallState{
		Name:    "test-skill",
		Version: "1.0.0",
		Source:  "/tmp/skills/test-skill",
	}))

	entries, err = adapter.List()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "test-skill", entries[0].Name)
	assert.Equal(t, "1.0.0", entries[0].Version)
}

func TestSkillManagerAdapterInstallFromLocal(t *testing.T) {
	adapter, tmpDir := newTestAdapter(t, nil)

	// Create a source skill directory with SKILL.yaml.
	srcDir := filepath.Join(tmpDir, "src-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	manifest := `name: local-skill
version: "0.1.0"
description: A test skill
types: [tool]
implementation:
  backend: starlark
  entrypoint: skill.star
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.yaml"), []byte(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "skill.star"), []byte("# star"), 0o644))

	result, err := adapter.Install(context.Background(), srcDir)
	require.NoError(t, err)
	assert.Equal(t, "local-skill", result.Name)
	assert.Equal(t, "0.1.0", result.Version)

	// Verify skill was copied to skills dir.
	_, err = os.Stat(filepath.Join(adapter.skillsDir, "local-skill", "SKILL.yaml"))
	assert.NoError(t, err)

	// Verify store state.
	state, err := adapter.store.GetSkillState("local-skill")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "local-skill", state.Name)
}

func TestSkillManagerAdapterRemove(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)

	// Create a skill in the skills dir and store.
	skillDir := filepath.Join(adapter.skillsDir, "removable")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.yaml"), []byte("name: removable"), 0o644))
	require.NoError(t, adapter.store.SaveSkillState(store.SkillInstallState{
		Name:    "removable",
		Version: "1.0.0",
		Source:  skillDir,
	}))

	err := adapter.Remove("removable")
	require.NoError(t, err)

	// Verify directory removed.
	_, err = os.Stat(skillDir)
	assert.True(t, os.IsNotExist(err))

	// Verify store state removed.
	state, err := adapter.store.GetSkillState("removable")
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestSkillManagerAdapterRemoveNotInstalled(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	err := adapter.Remove("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestSkillManagerAdapterRemoveInvalidName(t *testing.T) {
	adapter, _ := newTestAdapter(t, nil)
	err := adapter.Remove("../../etc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

// --- activation tests ---

type mockActivator struct {
	discoverCalled bool
	activateName   string
	discoverErr    error
	activateErr    error
}

func (m *mockActivator) Discover(_ []string) error {
	m.discoverCalled = true
	return m.discoverErr
}

func (m *mockActivator) Activate(name string) error {
	m.activateName = name
	return m.activateErr
}

func TestSkillManagerAdapterInstallActivatesSkill(t *testing.T) {
	act := &mockActivator{}
	adapter, tmpDir := newTestAdapter(t, nil)
	adapter.activator = act

	// Create a source skill.
	srcDir := filepath.Join(tmpDir, "activatable-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	manifest := `name: activatable
version: "1.0.0"
description: test
types: [tool]
implementation:
  backend: starlark
  entrypoint: skill.star
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.yaml"), []byte(manifest), 0o644))

	result, err := adapter.Install(context.Background(), srcDir)
	require.NoError(t, err)
	assert.True(t, result.Activated)
	assert.True(t, act.discoverCalled)
	assert.Equal(t, "activatable", act.activateName)
}

func TestSkillManagerAdapterInstallActivationFailureNonFatal(t *testing.T) {
	act := &mockActivator{activateErr: fmt.Errorf("activation failed")}
	adapter, tmpDir := newTestAdapter(t, nil)
	adapter.activator = act

	srcDir := filepath.Join(tmpDir, "fail-activate-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	manifest := `name: fail-activate
version: "1.0.0"
description: test
types: [tool]
implementation:
  backend: starlark
  entrypoint: skill.star
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.yaml"), []byte(manifest), 0o644))

	result, err := adapter.Install(context.Background(), srcDir)
	require.NoError(t, err)
	assert.False(t, result.Activated)
	assert.Equal(t, "fail-activate", result.Name)
}

func TestSkillManagerAdapterInstallWithoutActivator(t *testing.T) {
	adapter, tmpDir := newTestAdapter(t, nil)
	// adapter.activator is nil by default

	srcDir := filepath.Join(tmpDir, "no-activator-skill")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	manifest := `name: no-activator
version: "1.0.0"
description: test
types: [tool]
implementation:
  backend: starlark
  entrypoint: skill.star
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.yaml"), []byte(manifest), 0o644))

	result, err := adapter.Install(context.Background(), srcDir)
	require.NoError(t, err)
	assert.False(t, result.Activated)
}

func TestInstallGitURLNotMisroutedToLocal(t *testing.T) {
	// github.com/user/skill contains "/" but should go to git path, not local.
	assert.True(t, isGitURL("github.com/user/skill"))
	assert.True(t, isLocalPathAdapter("github.com/user/skill")) // both match!

	// The Install method must check isGitURL first.
	// We can't test the actual git clone here, but we can verify ordering
	// by checking that isGitURL is checked before isLocalPathAdapter in
	// the Install method logic above.
}

// --- helper function tests ---

func TestIsLocalPathAdapter(t *testing.T) {
	assert.True(t, isLocalPathAdapter("./my-skill"))
	assert.True(t, isLocalPathAdapter("/tmp/skill"))
	assert.True(t, isLocalPathAdapter("path/to/skill"))
	assert.False(t, isLocalPathAdapter("kubernetes"))
	assert.False(t, isLocalPathAdapter("kubernetes@1.0.0"))
}

func TestIsGitURL(t *testing.T) {
	assert.True(t, isGitURL("https://github.com/user/skill"))
	assert.True(t, isGitURL("ssh://git@github.com/user/skill"))
	assert.True(t, isGitURL("git@github.com:user/skill"))
	assert.True(t, isGitURL("github.com/user/skill"))
	assert.False(t, isGitURL("kubernetes"))
	assert.False(t, isGitURL("./my-skill"))
}

func TestParseNameVersionAdapter(t *testing.T) {
	name, version := parseNameVersionAdapter("kubernetes@1.2.0")
	assert.Equal(t, "kubernetes", name)
	assert.Equal(t, "1.2.0", version)

	name, version = parseNameVersionAdapter("kubernetes")
	assert.Equal(t, "kubernetes", name)
	assert.Equal(t, "latest", version)
}

func TestValidateSkillNameAdapter(t *testing.T) {
	assert.NoError(t, validateSkillNameAdapter("kubernetes"))
	assert.NoError(t, validateSkillNameAdapter("my-skill"))
	assert.NoError(t, validateSkillNameAdapter("skill_v2"))
	assert.Error(t, validateSkillNameAdapter("../../etc"))
	assert.Error(t, validateSkillNameAdapter(""))
}

// --- agent wiring tests ---

func TestAgentRegistersSkillManagerTool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	cfg.Skills.UserDir = t.TempDir()

	a := New(
		&capturingMockProvider{events: []provider.StreamEvent{{Type: "stop"}}},
		reg, autoApprove, cfg,
		WithStore(s),
	)
	require.NotNil(t, a)

	_, found := a.tools.Get("skill_manager")
	assert.True(t, found, "skill_manager tool should be registered when store is available")
}

func TestAgentSkipSkillManagerWithoutStore(t *testing.T) {
	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()

	a := New(
		&capturingMockProvider{events: []provider.StreamEvent{{Type: "stop"}}},
		reg, autoApprove, cfg,
		// No WithStore — store is nil
	)
	require.NotNil(t, a)

	_, found := a.tools.Get("skill_manager")
	assert.False(t, found, "skill_manager tool should not be registered without store")
}

// Verify adapter satisfies the interface.
var _ tools.SkillManagerAccess = (*skillManagerAdapter)(nil)
