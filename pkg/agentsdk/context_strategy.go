package agentsdk

import "context"

// PromptContext carries the per-turn inputs the agent loop offers to
// context strategies at prompt-build time.
type PromptContext struct {
	// UserMessage is the user message that started the current loop.
	UserMessage string
	// TokenBudget is the token budget available for contributed sections
	// (the loop's skill-prompt budget share).
	TokenBudget int
}

// PromptSection is one system-prompt section contributed by a strategy.
// Contributed sections render after the cache boundary as uncached dynamic
// sections — they are assumed to vary per turn. Reason documents why the
// section cannot be cached; like the internal prompt builder's uncached
// sections, it exists for review and grep, not runtime behavior.
type PromptSection struct {
	Title   string
	Content string
	Reason  string
}

// ContextStrategy is pluggable context-window content: called
// synchronously at prompt-build time to contribute sections to the system
// prompt. Sections whose content is empty or whitespace-only are skipped,
// so a strategy whose gate is not met simply returns nothing.
type ContextStrategy interface {
	ContributePromptSections(ctx context.Context, info PromptContext) []PromptSection
}

// StaticSection is one cacheable system-prompt section contributed at
// agent construction time. Unlike PromptSection there is no cache Reason
// — static sections sit before the cache boundary by definition.
type StaticSection struct {
	Title   string
	Content string
}

// StaticPromptSource contributes construction-time system-prompt
// sections: assembled exactly once when the agent is built, rendered in
// registration order after the built-in static sections (base system,
// identity, soul, project guidelines, extra prompts), and cacheable —
// identical across every turn of the session. Sections whose content is
// empty or whitespace-only are skipped.
type StaticPromptSource interface {
	ContributeStaticSections() []StaticSection
}
