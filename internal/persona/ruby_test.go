package persona

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemPromptContainsIdentity(t *testing.T) {
	prompt := SystemPrompt()
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Ruby")
	assert.Contains(t, prompt, "Pigi")
	assert.Contains(t, prompt, "Ganbaruby")
	assert.Contains(t, prompt, "kaomoji")
	assert.Contains(t, prompt, "coding assistant")
}
