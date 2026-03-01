package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Feature 1: Progressive Disclosure (SkillIndex) ---

func TestSkillIndexFromManifest(t *testing.T) {
	m := &SkillManifest{
		Name:        "test-skill",
		Version:     "1.0.0",
		Description: "A test skill",
		Types:       []SkillType{SkillTypeTool, SkillTypePrompt},
		Triggers: TriggerConfig{
			Files:     []string{"*.go"},
			Keywords:  []string{"golang"},
			Languages: []string{"go"},
			Modes:     []string{"interactive"},
		},
	}

	idx := NewSkillIndex(m, SourceUser, "/path/to/skill")

	assert.Equal(t, "test-skill", idx.Name)
	assert.Equal(t, "A test skill", idx.Description)
	assert.Equal(t, []SkillType{SkillTypeTool, SkillTypePrompt}, idx.Types)
	assert.Equal(t, SourceUser, idx.Source)
	assert.Equal(t, "/path/to/skill", idx.Dir)
	assert.Equal(t, []string{"*.go"}, idx.Triggers.Files)
	assert.Equal(t, []string{"golang"}, idx.Triggers.Keywords)
}

func TestSkillIndexFromManifestPreservesAllFields(t *testing.T) {
	t.Run("all fields populated", func(t *testing.T) {
		m := &SkillManifest{
			Name:        "full-skill",
			Description: "Full description",
			Types:       []SkillType{SkillTypeWorkflow},
			Triggers: TriggerConfig{
				Files:     []string{"Makefile"},
				Keywords:  []string{"build"},
				Languages: []string{"python"},
				Modes:     []string{"headless"},
			},
		}
		idx := NewSkillIndex(m, SourceProject, "/project/skills/full")
		assert.Equal(t, "full-skill", idx.Name)
		assert.Equal(t, "Full description", idx.Description)
		assert.Equal(t, []SkillType{SkillTypeWorkflow}, idx.Types)
		assert.Equal(t, SourceProject, idx.Source)
		assert.Equal(t, "/project/skills/full", idx.Dir)
		assert.Equal(t, []string{"Makefile"}, idx.Triggers.Files)
	})

	t.Run("nil manifest produces minimal index", func(t *testing.T) {
		idx := NewSkillIndex(nil, SourceBuiltin, "/builtin")
		assert.Empty(t, idx.Name)
		assert.Empty(t, idx.Description)
		assert.Nil(t, idx.Types)
		assert.Equal(t, SourceBuiltin, idx.Source)
		assert.Equal(t, "/builtin", idx.Dir)
	})

	t.Run("zero-value manifest", func(t *testing.T) {
		m := &SkillManifest{}
		idx := NewSkillIndex(m, SourceMCP, "")
		assert.Empty(t, idx.Name)
		assert.Empty(t, idx.Description)
		assert.Empty(t, idx.Types)
		assert.Equal(t, SourceMCP, idx.Source)
	})
}

func TestSkillIndexTypesCopyIsolation(t *testing.T) {
	m := &SkillManifest{
		Name:        "isolation-test",
		Description: "Test",
		Types:       []SkillType{SkillTypeTool},
	}
	idx := NewSkillIndex(m, SourceUser, "")

	// Mutating the index's Types should not affect the manifest.
	idx.Types[0] = SkillTypePrompt
	assert.Equal(t, SkillTypeTool, m.Types[0])
}

func TestRuntimeGetSkillIndexes(t *testing.T) {
	rt, _, _ := newTestRuntime(t, []string{"idx-one", "idx-two"}, nil)

	m1 := testManifest("idx-one")
	m1.Description = "First skill"
	m2 := testManifest("idx-two")
	m2.Description = "Second skill"

	rt.loader.RegisterBuiltin(m1)
	rt.loader.RegisterBuiltin(m2)
	require.NoError(t, rt.Discover(nil))

	indexes := rt.GetSkillIndexes()
	require.Len(t, indexes, 2)

	byName := make(map[string]SkillIndex)
	for _, idx := range indexes {
		byName[idx.Name] = idx
	}

	assert.Equal(t, "First skill", byName["idx-one"].Description)
	assert.Equal(t, "Second skill", byName["idx-two"].Description)
	assert.Equal(t, SourceBuiltin, byName["idx-one"].Source)
}

func TestRuntimeGetSkillIndexesEmpty(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)
	indexes := rt.GetSkillIndexes()
	assert.Empty(t, indexes)
}

func TestRuntimeGetSkillIndexesReflectsSource(t *testing.T) {
	rt, _, _ := newTestRuntime(t, nil, nil)

	m1 := testManifest("src-builtin")
	rt.loader.RegisterBuiltin(m1)
	require.NoError(t, rt.Discover(nil))

	indexes := rt.GetSkillIndexes()
	require.Len(t, indexes, 1)
	assert.Equal(t, SourceBuiltin, indexes[0].Source)
}
