package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// recordingContextStrategy contributes fixed sections and records the
// PromptContext it was offered.
type recordingContextStrategy struct {
	sections []agentsdk.PromptSection
	infos    []agentsdk.PromptContext
}

func (s *recordingContextStrategy) ContributePromptSections(_ context.Context, info agentsdk.PromptContext) []agentsdk.PromptSection {
	s.infos = append(s.infos, info)
	return s.sections
}

// TestContextStrategiesContributePromptSections pins the ContextStrategy
// seam's prompt-build moment: a strategy registered via
// WithContextStrategies is offered the turn's user message and contributes
// sections that render as uncached dynamic sections of the system prompt.
func TestContextStrategiesContributePromptSections(t *testing.T) {
	strategy := &recordingContextStrategy{sections: []agentsdk.PromptSection{
		{Title: "Custom Lore", Content: "dragons live here", Reason: "test fixture varies per run"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithContextStrategies(strategy))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "tell me about the map")

	assert.Contains(t, prompt, "## Custom Lore\n\ndragons live here",
		"contributed section must render like other dynamic sections")
	require.Len(t, strategy.infos, 1)
	assert.Equal(t, "tell me about the map", strategy.infos[0].UserMessage)
}

// TestContextStrategyEmptySectionsAreSkipped: strategies may return nothing
// (their gate not met); empty titles/contents must not litter the prompt.
func TestContextStrategyEmptySectionsAreSkipped(t *testing.T) {
	strategy := &recordingContextStrategy{sections: []agentsdk.PromptSection{
		{Title: "Empty", Content: "", Reason: "gate not met"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithContextStrategies(strategy))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "q")

	assert.False(t, strings.Contains(prompt, "## Empty"),
		"sections with empty content must be skipped")
}
