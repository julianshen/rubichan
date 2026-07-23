package agent

import (
	"context"
	"strings"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// skillPromptFragment is one skill-contributed system-prompt fragment.
// The JSON tags define the wire shape the before-prompt-build hook sees
// and returns (skill_name / prompt).
type skillPromptFragment struct {
	SkillName string `json:"skill_name"`
	Prompt    string `json:"prompt"`
}

// promptBuildContext is the payload handed to before-prompt-build hooks:
// the base system prompt, the current skill fragments, and a snapshot of
// the context budget so a hook can make budget-aware decisions.
type promptBuildContext struct {
	BaseSystemPrompt      string                `json:"base_system_prompt"`
	SkillPromptFragments  []skillPromptFragment `json:"skill_prompt_fragments"`
	ContextBudgetTotal    int                   `json:"context_budget_total"`
	ContextBudgetMaxOut   int                   `json:"context_budget_max_output_tokens"`
	ContextBudgetWindow   int                   `json:"context_budget_effective_window"`
	ContextBudgetSystem   int                   `json:"context_budget_system_prompt_tokens"`
	ContextBudgetSkills   int                   `json:"context_budget_skill_prompt_tokens"`
	ContextBudgetTools    int                   `json:"context_budget_tool_description_tokens"`
	ContextBudgetMessages int                   `json:"context_budget_conversation_tokens"`
}

// promptBuildMutation is the accumulated set of changes a before-prompt-build
// hook (or chain of hooks) requests against the base prompt and fragments.
type promptBuildMutation struct {
	ReplaceBaseSystemPrompt        string
	ReplaceBaseSystemPromptPresent bool
	AppendSystemPrompt             string
	ReplaceSkillFragments          []skillPromptFragment
	ReplaceSkillFragmentsPresent   bool
	AppendSkillFragments           []skillPromptFragment
}

// normalizeSkillFragments coerces a hook's fragment payload — which may
// arrive as native fragments or as decoded JSON ([]any of maps) — into a
// fragment slice. The bool reports whether the value was a recognized
// fragment shape at all (nil counts, as an explicit clear).
func normalizeSkillFragments(raw any) ([]skillPromptFragment, bool) {
	switch v := raw.(type) {
	case nil:
		return []skillPromptFragment{}, true
	case []skillPromptFragment:
		return append([]skillPromptFragment(nil), v...), true
	case []any:
		out := make([]skillPromptFragment, 0, len(v))
		for _, item := range v {
			fragMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := fragMap["skill_name"].(string)
			prompt, _ := fragMap["prompt"].(string)
			if name != "" && prompt != "" {
				out = append(out, skillPromptFragment{SkillName: name, Prompt: prompt})
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// mergePromptBuildMutation folds a hook's requested changes into dst. It is
// called once per hook data map so a chain of hooks accumulates mutations,
// with later hooks overriding earlier ones for the replace/append keys.
func mergePromptBuildMutation(dst *promptBuildMutation, data map[string]any) {
	if data == nil {
		return
	}
	if v, ok := data["replace_base_system_prompt"].(string); ok {
		dst.ReplaceBaseSystemPrompt = v
		dst.ReplaceBaseSystemPromptPresent = true
	}
	if v, ok := data["append_system_prompt"].(string); ok {
		dst.AppendSystemPrompt = v
	}
	if raw, ok := data["replace_skill_fragments"]; ok {
		if fragments, parsed := normalizeSkillFragments(raw); parsed {
			dst.ReplaceSkillFragments = fragments
			dst.ReplaceSkillFragmentsPresent = true
		}
	}
	if raw, ok := data["append_skill_fragments"]; ok {
		if fragments, parsed := normalizeSkillFragments(raw); parsed {
			dst.AppendSkillFragments = fragments
		}
	}
}

// renderSkillPromptText concatenates fragment prompts for telemetry —
// the same text the fragments render into the system prompt, used only to
// measure the skill-prompt token component.
func renderSkillPromptText(fragments []skillPromptFragment) string {
	var sb strings.Builder
	for _, f := range fragments {
		if f.Prompt == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(f.Prompt)
	}
	return sb.String()
}

// skillPromptContributor encapsulates the skill runtime's integration with
// system-prompt construction: budgeted fragment selection plus the
// HookOnBeforePromptBuild mutation protocol.
//
// This is deliberately NOT an agentsdk.ContextStrategy. A before-prompt-build
// hook can *replace the base system prompt wholesale* (replace_base_system_prompt),
// a whole-prompt transform; ContextStrategy.ContributePromptSections can only
// *append* sections, never rewrite the base. Forcing this behind the
// section-contribution seam would silently drop the base-replacement and
// fragment-replacement capabilities, so it stays at the prompt-build site
// as a cohesive, named unit instead.
type skillPromptContributor struct {
	runtime *skills.Runtime
	logger  agentsdk.Logger
}

// resolve applies the skill runtime's contributions to prompt construction:
// it selects budgeted fragments, dispatches the before-prompt-build hook
// with the base prompt / fragments / budget snapshot, and applies any
// requested mutations. It returns the (possibly replaced) base prompt and
// the final fragments. With no runtime configured it is a pass-through:
// the base prompt is returned unchanged with no fragments.
func (c *skillPromptContributor) resolve(ctx context.Context, basePrompt string, budget ContextBudget) (string, []skillPromptFragment) {
	if c.runtime == nil {
		return basePrompt, nil
	}

	fragments := make([]skillPromptFragment, 0)
	for _, f := range c.runtime.GetBudgetedPromptFragments() {
		if f.ResolvedPrompt == "" {
			continue
		}
		fragments = append(fragments, skillPromptFragment{
			SkillName: f.SkillName,
			Prompt:    f.ResolvedPrompt,
		})
	}

	hookEvent := skills.HookEvent{
		Phase: skills.HookOnBeforePromptBuild,
		Ctx:   ctx,
		Data: map[string]any{
			skills.HookDataPromptBuild: promptBuildContext{
				BaseSystemPrompt:      basePrompt,
				SkillPromptFragments:  fragments,
				ContextBudgetTotal:    budget.Total,
				ContextBudgetMaxOut:   budget.MaxOutputTokens,
				ContextBudgetWindow:   budget.EffectiveWindow(),
				ContextBudgetSystem:   budget.SystemPrompt,
				ContextBudgetSkills:   budget.SkillPrompts,
				ContextBudgetTools:    budget.ToolDescriptions,
				ContextBudgetMessages: budget.Conversation,
			},
		},
	}

	result, err := c.runtime.DispatchHook(hookEvent)
	if err != nil {
		c.logger.Warn("before-prompt-build hook failed: %v", err)
		return basePrompt, fragments
	}
	if result == nil {
		return basePrompt, fragments
	}

	mutation := promptBuildMutation{}
	mergePromptBuildMutation(&mutation, hookEvent.Data)
	mergePromptBuildMutation(&mutation, result.Modified)

	if mutation.ReplaceBaseSystemPromptPresent {
		basePrompt = mutation.ReplaceBaseSystemPrompt
	}
	if mutation.AppendSystemPrompt != "" {
		basePrompt = strings.TrimSpace(basePrompt + "\n\n" + mutation.AppendSystemPrompt)
	}
	if mutation.ReplaceSkillFragmentsPresent {
		fragments = mutation.ReplaceSkillFragments
	}
	if len(mutation.AppendSkillFragments) > 0 {
		fragments = append(fragments, mutation.AppendSkillFragments...)
	}

	return basePrompt, fragments
}
