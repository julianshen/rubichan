package appledev

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
