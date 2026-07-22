package agent

import (
	"context"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// WithContextStrategies registers strategies that contribute system-prompt
// sections at prompt-build time. Contributed sections render after the
// built-in dynamic sections (scratchpad, progress, knowledge, memories)
// and before skill prompt fragments. Nil strategies are ignored. See
// agentsdk.ContextStrategy.
func WithContextStrategies(strategies ...agentsdk.ContextStrategy) AgentOption {
	return func(a *Agent) {
		for _, s := range strategies {
			if s != nil {
				a.contextStrategies = append(a.contextStrategies, s)
			}
		}
	}
}

// contributeStrategySections invokes every registered context strategy and
// adds its non-empty sections to the prompt builder as uncached dynamic
// sections. Panics are recovered per strategy — this is a public seam
// running on the turn goroutine, where an unrecovered panic would abort
// the user turn and starve sibling strategies.
func (a *Agent) contributeStrategySections(ctx context.Context, pb *PromptBuilder, info agentsdk.PromptContext) {
	for _, strategy := range a.contextStrategies {
		for _, section := range a.strategySectionsRecovering(ctx, strategy, info) {
			if section.Content == "" {
				continue
			}
			pb.AddDynamicSection_UNCACHED(section.Title, section.Content, section.Reason)
		}
	}
}

// strategySectionsRecovering invokes one strategy behind a recover
// boundary; on panic the strategy contributes nothing this turn.
func (a *Agent) strategySectionsRecovering(ctx context.Context, strategy agentsdk.ContextStrategy, info agentsdk.PromptContext) (sections []agentsdk.PromptSection) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Warn("context strategy ContributePromptSections panicked: %v", r)
		}
	}()
	return strategy.ContributePromptSections(ctx, info)
}
