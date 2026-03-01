package appledev

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/julianshen/rubichan/internal/skills"
)

func TestSystemPrompt_NotEmpty(t *testing.T) {
	p := SystemPrompt()
	assert.NotEmpty(t, p)
}

func TestSystemPrompt_ContainsKeyTopics(t *testing.T) {
	p := SystemPrompt()
	assert.Contains(t, p, "Xcode")
	assert.Contains(t, p, "Swift")
	assert.Contains(t, p, "SwiftUI")
	assert.Contains(t, p, "code signing")
}

func TestRegisterPrompt(t *testing.T) {
	loader := skills.NewLoader("", "")
	RegisterPrompt(loader)

	discovered, _, err := loader.Discover(nil)
	assert.NoError(t, err)
	assert.Len(t, discovered, 1)

	ds := discovered[0]
	assert.Equal(t, "apple-platform-guide", ds.Manifest.Name)
	assert.Equal(t, skills.SourceBuiltin, ds.Source)
	assert.Contains(t, ds.Manifest.Types, skills.SkillTypePrompt)
	assert.NotEmpty(t, ds.Manifest.Prompt.SystemPromptFile)
	assert.Contains(t, ds.Manifest.Triggers.Modes, "interactive")
}
