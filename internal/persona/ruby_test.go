package persona

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemPromptContainsIdentity(t *testing.T) {
	prompt := SystemPrompt()
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "## Identity")
	assert.Contains(t, prompt, "## Soul")
	assert.Contains(t, prompt, "Ruby")
	assert.Contains(t, prompt, "Pigi")
	assert.Contains(t, prompt, "ガンバ")
	assert.Contains(t, prompt, "kaomoji")
	assert.Contains(t, prompt, "coding assistant")
	assert.Contains(t, prompt, "Never reveal internal reasoning")
	assert.Contains(t, prompt, "assistantanalysis")
	assert.Contains(t, prompt, "to=functions")
}

func TestBaseSystemPrompt(t *testing.T) {
	prompt := BaseSystemPrompt()
	assert.Contains(t, prompt, "coding assistant")
	assert.NotContains(t, prompt, "Ruby Kurosawa")
}

func TestIdentityPrompt(t *testing.T) {
	prompt := IdentityPrompt()
	assert.Contains(t, prompt, "Ruby Kurosawa")
	assert.Contains(t, prompt, "Pigi")
}

func TestSoulPrompt(t *testing.T) {
	prompt := SoulPrompt()
	assert.Contains(t, prompt, "Core Principles")
	assert.Contains(t, prompt, "Boundaries")
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
	// All Ruby thinking messages contain a kaomoji (parenthesized expression).
	assert.Contains(t, msg, "(")
	assert.Contains(t, msg, ")")
}

func TestThinkingMessageRotates(t *testing.T) {
	// Calling ThinkingMessage multiple times should produce different messages.
	seen := make(map[string]bool)
	for i := 0; i < len(rubyThinkingMessages)+1; i++ {
		msg := ThinkingMessage()
		seen[msg] = true
	}
	// Should have seen at least 2 distinct messages.
	assert.GreaterOrEqual(t, len(seen), 2, "ThinkingMessage should rotate through multiple messages")
}

func TestThinkingMessageAllContainKaomoji(t *testing.T) {
	for i, msg := range rubyThinkingMessages {
		assert.Contains(t, msg, "(", "message %d should contain kaomoji: %s", i, msg)
		assert.Contains(t, msg, ")", "message %d should contain kaomoji: %s", i, msg)
	}
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

// --- Persona interface tests ---

func TestRubyPersonaImplementsInterface(t *testing.T) {
	var _ Persona = (*RubyPersona)(nil)
}

func TestActivePersonaDefaultsToRuby(t *testing.T) {
	p := Active()
	assert.IsType(t, &RubyPersona{}, p)
}

func TestSetActivePersona(t *testing.T) {
	original := Active()
	defer SetActive(original) // restore

	custom := &mockPersona{}
	SetActive(custom)
	assert.Equal(t, custom, Active())

	// Package-level functions should delegate to the custom persona.
	assert.Equal(t, "mock thinking", ThinkingMessage())
	assert.Equal(t, "mock welcome", WelcomeMessage())
	assert.Equal(t, "mock goodbye\n", GoodbyeMessage())
	assert.Equal(t, "mock error: oops", ErrorMessage("oops"))
	assert.Equal(t, "mock success", SuccessMessage())
	assert.Equal(t, "mock status", StatusPrefix())
	assert.Equal(t, "mock approval: shell", ApprovalAsk("shell"))
}

// mockPersona is a test persona that returns predictable strings.
type mockPersona struct{}

func (m *mockPersona) ThinkingMessage() string        { return "mock thinking" }
func (m *mockPersona) WelcomeMessage() string         { return "mock welcome" }
func (m *mockPersona) GoodbyeMessage() string         { return "mock goodbye\n" }
func (m *mockPersona) ErrorMessage(err string) string { return "mock error: " + err }
func (m *mockPersona) SuccessMessage() string         { return "mock success" }
func (m *mockPersona) StatusPrefix() string           { return "mock status" }
func (m *mockPersona) ApprovalAsk(tool string) string { return fmt.Sprintf("mock approval: %s", tool) }
