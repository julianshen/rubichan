package agentsdk

import (
	"context"
	"strings"
)

// WithContextStrategies registers strategies that contribute system-prompt
// sections at prompt-build time. Contributed sections render after the base
// system prompt, in registration order, and are recomputed every loop
// iteration so they can vary per turn. Nil strategies are ignored. See
// ContextStrategy.
//
// This is the SDK loop's counterpart to internal/agent.WithContextStrategies:
// it lets an out-of-module embedder attach dynamic prompt content to the
// portable core without depending on any internal/ package.
func WithContextStrategies(strategies ...ContextStrategy) Option {
	return func(a *Agent) {
		for _, s := range strategies {
			if s != nil {
				a.contextStrategies = append(a.contextStrategies, s)
			}
		}
	}
}

// effectiveSystemPrompt returns the base system prompt with every registered
// strategy's non-blank sections appended. It is called once per loop
// iteration; the conversation's stored system prompt is never mutated.
func (a *Agent) effectiveSystemPrompt(ctx context.Context, userMessage string) string {
	base := a.conversation.SystemPrompt()
	if len(a.contextStrategies) == 0 {
		return base
	}
	sections := a.contributeStrategySections(ctx, PromptContext{
		UserMessage: userMessage,
		TokenBudget: a.config.ContextBudget,
	})
	if len(sections) == 0 {
		return base
	}

	parts := make([]string, 0, len(sections)+1)
	if base != "" {
		parts = append(parts, base)
	}
	for _, s := range sections {
		parts = append(parts, "## "+s.Title+"\n\n"+s.Content)
	}
	return strings.Join(parts, "\n\n")
}

// contributeStrategySections invokes every registered strategy and collects
// its non-empty sections. Sections whose content is empty or whitespace-only
// are skipped, so a strategy whose gate is not met contributes nothing.
func (a *Agent) contributeStrategySections(ctx context.Context, info PromptContext) []PromptSection {
	var out []PromptSection
	for _, strategy := range a.contextStrategies {
		for _, section := range a.strategySectionsRecovering(ctx, strategy, info) {
			if strings.TrimSpace(section.Content) == "" {
				continue
			}
			out = append(out, section)
		}
	}
	return out
}

// strategySectionsRecovering invokes one strategy behind a recover boundary;
// on panic the strategy contributes nothing this turn. This is a public seam
// running on the turn goroutine, where an unrecovered panic would abort the
// user's turn and starve sibling strategies.
func (a *Agent) strategySectionsRecovering(ctx context.Context, strategy ContextStrategy, info PromptContext) (sections []PromptSection) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Warn("context strategy ContributePromptSections panicked: %v", r)
		}
	}()
	return strategy.ContributePromptSections(ctx, info)
}
