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
	assert.Contains(t, prompt, "ガンバ")
	assert.Contains(t, prompt, "kaomoji")
	assert.Contains(t, prompt, "coding assistant")
}

func TestWelcomeMessage(t *testing.T) {
	msg := WelcomeMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
	assert.Contains(t, msg, "(>_<)")
}

func TestGoodbyeMessage(t *testing.T) {
	msg := GoodbyeMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
}

func TestThinkingMessage(t *testing.T) {
	msg := ThinkingMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "Ruby")
}

func TestErrorMessageIncludesError(t *testing.T) {
	msg := ErrorMessage("file not found")
	assert.Contains(t, msg, "Pigi")
	assert.Contains(t, msg, "file not found")
}

func TestSuccessMessage(t *testing.T) {
	msg := SuccessMessage()
	assert.NotEmpty(t, msg)
	assert.Contains(t, msg, "ガンバ")
}

func TestStatusPrefix(t *testing.T) {
	prefix := StatusPrefix()
	assert.NotEmpty(t, prefix)
	assert.Contains(t, prefix, "Ruby")
}

func TestApprovalAskIncludesTool(t *testing.T) {
	msg := ApprovalAsk("shell_exec")
	assert.Contains(t, msg, "Ruby")
	assert.Contains(t, msg, "shell_exec")
}
