package hooks

import "strings"

// PromptHook modifies prompts via find/replace without external commands.
// Used for pre_prompt and post_response lifecycle events.
type PromptHook struct {
	Find    string
	Replace string
}

func (h PromptHook) Transform(text string) string {
	return strings.ReplaceAll(text, h.Find, h.Replace)
}

// PromptHookChain runs multiple hooks in order.
//
// Warning: O(n*m) — each hook allocates a new string. Limit to 3 hooks
// on hot paths (per prompt build) to avoid GC pressure.
type PromptHookChain []PromptHook

func (c PromptHookChain) Transform(text string) string {
	for _, h := range c {
		text = h.Transform(text)
	}
	return text
}
