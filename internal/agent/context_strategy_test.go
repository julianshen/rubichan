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
	kg "github.com/julianshen/rubichan/pkg/knowledgegraph"
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

// TestBuiltinDynamicSectionsRenderInOrder pins the built-in dynamic
// prompt sections — scratchpad, progress, project knowledge (with usage
// recording), relevant memories — and their relative order, followed by
// any registered strategy's sections. Written green before the built-ins
// moved onto the ContextStrategy seam; must stay green after.
func TestBuiltinDynamicSectionsRenderInOrder(t *testing.T) {
	selector := &prefetchRecordingSelector{results: []kg.ScoredEntity{
		{Entity: &kg.Entity{ID: "kg-1", Title: "Arch Choice", Body: "chose Go"}, Score: 1, EstimatedTokens: 5},
	}}
	custom := &recordingContextStrategy{sections: []agentsdk.PromptSection{
		{Title: "Custom Lore", Content: "dragons live here", Reason: "test"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg,
		WithKnowledgeGraph(selector),
		WithContextStrategies(custom),
	)
	a.scratchpad.Set("todo", "ship the seam")
	a.progress.Record(1, "build", "compiled core", "ok")
	a.allMemories = []MemoryEntry{{
		Tag:        "architecture",
		Content:    "the architecture is hexagonal",
		Normalized: "architecture the architecture is hexagonal",
	}}

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "architecture question")

	positions := make([]int, 0, 5)
	for _, title := range []string{"## Scratchpad", "## Progress", "## Project Knowledge", "## Relevant Memories", "## Custom Lore"} {
		idx := strings.Index(prompt, title)
		require.NotEqual(t, -1, idx, "section %q missing from prompt", title)
		positions = append(positions, idx)
	}
	assert.IsNonDecreasing(t, positions, "built-in sections must keep their order, custom strategies last")

	assert.Contains(t, prompt, "ship the seam")
	assert.Contains(t, prompt, "the architecture is hexagonal")

	selector.mu.Lock()
	defer selector.mu.Unlock()
	assert.NotEmpty(t, selector.recorded, "knowledge injection must record usage")
}

// fixedStaticSource contributes fixed construction-time sections.
type fixedStaticSource struct{ sections []agentsdk.StaticSection }

func (s fixedStaticSource) ContributeStaticSections() []agentsdk.StaticSection {
	return s.sections
}

// TestStaticPromptSourcesContributeSections pins the StaticPromptSource
// seam: a source registered via WithStaticPromptSources contributes
// cacheable sections to the system prompt at construction time, rendered
// after the built-in static sections; blank sections are skipped.
func TestStaticPromptSourcesContributeSections(t *testing.T) {
	source := fixedStaticSource{sections: []agentsdk.StaticSection{
		{Title: "House Rules", Content: "always rhyme"},
		{Title: "Blank Rules", Content: "  \n"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithStaticPromptSources(source))

	prompt := a.conversation.SystemPrompt()
	assert.Contains(t, prompt, "## House Rules\n\nalways rhyme",
		"contributed static section must render like other cacheable sections")
	assert.NotContains(t, prompt, "## Blank Rules",
		"blank static sections must be skipped")
	assert.Less(t, strings.Index(prompt, "## Identity"), strings.Index(prompt, "## House Rules"),
		"registered sources render after built-in static sections")
}

// TestContextStrategyEmptySectionsAreSkipped: strategies may return nothing
// (their gate not met); empty titles/contents must not litter the prompt.
func TestContextStrategyEmptySectionsAreSkipped(t *testing.T) {
	strategy := &recordingContextStrategy{sections: []agentsdk.PromptSection{
		{Title: "Empty", Content: "", Reason: "gate not met"},
		{Title: "Blank", Content: "  \n\t\n", Reason: "whitespace only"},
	}}

	reg := tools.NewRegistry()
	cfg := config.DefaultConfig()
	a := New(&mockProvider{}, reg, autoApprove, cfg, WithContextStrategies(strategy))

	prompt, _, _ := a.buildSystemPromptWithFragments(context.Background(), "q")

	assert.False(t, strings.Contains(prompt, "## Empty"),
		"sections with empty content must be skipped")
	assert.False(t, strings.Contains(prompt, "## Blank"),
		"sections with whitespace-only content must be skipped")
}
