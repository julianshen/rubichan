package codereview

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/skills"
)

// discoverSkill is a test helper that registers and discovers the single
// built-in skill, returning it. It fails the test if registration or
// discovery produces an error or unexpected count.
func discoverSkill(t *testing.T) skills.DiscoveredSkill {
	t.Helper()
	loader := skills.NewLoader("", "")
	Register(loader)

	discovered, _, err := loader.Discover(nil)
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	return discovered[0]
}

func TestRegisterPopulatesLoader(t *testing.T) {
	ds := discoverSkill(t)

	assert.Equal(t, "review-guide", ds.Manifest.Name)
	assert.Equal(t, skills.SourceBuiltin, ds.Source)
	assert.NotEmpty(t, ds.Manifest.Description)
	assert.Equal(t, []skills.SkillType{skills.SkillTypePrompt}, ds.Manifest.Types)
	assert.NotEmpty(t, ds.Manifest.Prompt.SystemPromptFile)
	assert.Greater(t, len(ds.Manifest.Prompt.SystemPromptFile), 100)
}

func TestReviewGuideIncludesAuthoringPatterns(t *testing.T) {
	ds := discoverSkill(t)
	content := ds.Manifest.Prompt.SystemPromptFile

	for _, section := range []string{
		"## Thinking Phase",
		"## Anti-Patterns",
		"## Calibration",
		"## Approaches",
		"## Verification",
	} {
		assert.Contains(t, content, section)
	}
}

func TestReviewGuideIncludesTriggers(t *testing.T) {
	ds := discoverSkill(t)

	assert.NotEmpty(t, ds.Manifest.Triggers.Keywords)
	assert.NotEmpty(t, ds.Manifest.Triggers.Modes)
}
