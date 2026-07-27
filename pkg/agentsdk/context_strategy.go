package agentsdk

import (
	"context"
	"strings"
)

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

// ContributeSections runs every strategy for one prompt build and returns
// the sections they produced, skipping any whose content is empty or
// whitespace-only.
//
// Each strategy is invoked behind its own recover boundary: strategies are
// third-party code running on the turn goroutine, and a panicking one must
// contribute nothing rather than abort the turn or starve its siblings.
//
// Shared by both agent loops. They differ in what they do with the result —
// one appends to a prompt builder, the other returns the slice — but the
// iteration and the recover boundary are the part worth having once.
func ContributeSections(ctx context.Context, strategies []ContextStrategy, info PromptContext, logger Logger) []PromptSection {
	var out []PromptSection
	for _, strategy := range strategies {
		for _, section := range sectionsRecovering(ctx, strategy, info, logger) {
			if strings.TrimSpace(section.Content) == "" {
				continue
			}
			out = append(out, section)
		}
	}
	return out
}

// sectionsRecovering invokes one strategy behind a recover boundary; on
// panic it contributes nothing for this prompt build.
func sectionsRecovering(ctx context.Context, strategy ContextStrategy, info PromptContext, logger Logger) (sections []PromptSection) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("context strategy ContributePromptSections panicked: %v", r)
		}
	}()
	return strategy.ContributePromptSections(ctx, info)
}
