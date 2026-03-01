package appledev

import (
	_ "embed"

	"github.com/julianshen/rubichan/internal/skills"
)

//go:embed system.md
var systemPrompt string

// SystemPrompt returns the Apple platform system prompt for injection into the agent.
func SystemPrompt() string {
	return systemPrompt
}

// RegisterPrompt registers the Apple platform system prompt as a built-in
// prompt skill. It auto-activates in interactive mode via trigger evaluation.
func RegisterPrompt(loader *skills.Loader) {
	m := &skills.SkillManifest{
		Name:        "apple-platform-guide",
		Version:     "1.0.0",
		Description: "Apple platform development expertise — Swift, Xcode, iOS/macOS best practices",
		Types:       []skills.SkillType{skills.SkillTypePrompt},
		Prompt: skills.PromptConfig{
			SystemPromptFile: systemPrompt,
		},
		Triggers: skills.TriggerConfig{
			Modes: []string{"interactive"},
		},
	}
	loader.RegisterBuiltin(m)
}
