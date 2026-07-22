package agent

import (
	"context"
	"fmt"
	"strings"

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

// builtinContextStrategies returns the agent's built-in prompt
// contributors in canonical section order. They are prepended ahead of
// user-registered strategies at construction time so the section order is
// fixed regardless of option order. Each adapter gates itself (nil
// dependency, empty query) and reads agent fields lazily at call time, so
// option application order does not matter.
func (a *Agent) builtinContextStrategies() []agentsdk.ContextStrategy {
	return []agentsdk.ContextStrategy{
		scratchpadStrategy{agent: a},
		progressStrategy{agent: a},
		knowledgeStrategy{agent: a},
		memoriesStrategy{agent: a},
	}
}

// scratchpadStrategy contributes the user-editable scratchpad notes.
type scratchpadStrategy struct{ agent *Agent }

func (s scratchpadStrategy) ContributePromptSections(context.Context, agentsdk.PromptContext) []agentsdk.PromptSection {
	if s.agent.scratchpad == nil {
		return nil
	}
	return []agentsdk.PromptSection{{
		Title:   "Scratchpad",
		Content: s.agent.scratchpad.Render(),
		Reason:  "user-editable notes change across turns",
	}}
}

// progressStrategy contributes the auto-populated progress tracker,
// which survives compaction.
type progressStrategy struct{ agent *Agent }

func (s progressStrategy) ContributePromptSections(context.Context, agentsdk.PromptContext) []agentsdk.PromptSection {
	if s.agent.progress == nil {
		return nil
	}
	return []agentsdk.PromptSection{{
		Title:   "Progress",
		Content: s.agent.progress.Render(),
		Reason:  "accumulates tool results at runtime and changes each turn",
	}}
}

// knowledgeStrategy contributes knowledge-graph entities selected for the
// user's message and records their usage for injection metrics.
type knowledgeStrategy struct{ agent *Agent }

func (s knowledgeStrategy) ContributePromptSections(ctx context.Context, info agentsdk.PromptContext) []agentsdk.PromptSection {
	a := s.agent
	if a.knowledgeSelector == nil || info.UserMessage == "" {
		return nil
	}
	entities, err := a.knowledgeSelector.Select(ctx, info.UserMessage, info.TokenBudget)
	if err != nil || len(entities) == 0 {
		return nil
	}
	knowledge := renderKnowledgeSection(entities)
	if knowledge == "" {
		return nil
	}
	// Record that these entities were selected and injected into the prompt.
	// Errors are silently discarded to ensure metrics recording never blocks
	// prompt building.
	_ = a.knowledgeSelector.RecordUsage(ctx, entities)
	return []agentsdk.PromptSection{{
		Title:   "Project Knowledge",
		Content: knowledge,
		Reason:  "selected per-query from knowledge graph based on user message content",
	}}
}

// memoriesStrategy contributes cross-session memories relevant to the
// user's message.
type memoriesStrategy struct{ agent *Agent }

func (s memoriesStrategy) ContributePromptSections(_ context.Context, info agentsdk.PromptContext) []agentsdk.PromptSection {
	a := s.agent
	if len(a.allMemories) == 0 || info.UserMessage == "" {
		return nil
	}
	relevant := SelectRelevantMemories(a.allMemories, info.UserMessage, 5)
	if len(relevant) == 0 {
		return nil
	}
	var sb strings.Builder
	for _, m := range relevant {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", m.Tag, m.Content))
	}
	return []agentsdk.PromptSection{{
		Title:   "Relevant Memories",
		Content: sb.String(),
		Reason:  "selected per-query from cross-session memory store based on user message content",
	}}
}
