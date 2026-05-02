package hooks

import "strings"

// PromptHook applies a find/replace transformation to a prompt string.
// Used for pre_prompt and post_response hooks that modify the system
// prompt or model output without requiring external commands.
type PromptHook struct {
	Find    string
	Replace string
}

// Transform applies the find/replace to the input text.
func (h PromptHook) Transform(text string) string {
	return strings.ReplaceAll(text, h.Find, h.Replace)
}

// PromptHookChain applies multiple prompt hooks in sequence.
//
// Warning: each hook creates a new string via strings.ReplaceAll, so
// chaining many hooks on long prompts is O(n*m). Use sparingly on
// hot paths (e.g., limit to 3 hooks per prompt build).
type PromptHookChain []PromptHook

// Transform applies all hooks in order.
func (c PromptHookChain) Transform(text string) string {
	for _, h := range c {
		text = h.Transform(text)
	}
	return text
}
