package a2a

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTool satisfies agentsdk.Tool with the minimum needed for
// BuildAgentCard, which only reads Name() and Description().
type fakeTool struct{ name, desc string }

func (f fakeTool) Name() string                 { return f.name }
func (f fakeTool) Description() string          { return f.desc }
func (f fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{}`) }
func (f fakeTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{}, nil
}

func TestBuildAgentCard(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeTool{name: "file", desc: "read/write files"}))
	require.NoError(t, reg.Register(fakeTool{name: "shell", desc: "run shell commands"}))

	activeSkills := []*skills.Skill{
		{Manifest: &skills.SkillManifest{
			Name:        "commit-helper",
			Description: "drafts commit messages",
			Tags:        []string{"git", "workflow"},
		}},
	}

	card := BuildAgentCard(reg, activeSkills, "rubichan", "a coding agent", "https://example.com")

	assert.Equal(t, "rubichan", card.Name)
	assert.Equal(t, "https://example.com", card.URL)
	assert.True(t, card.Capabilities.Streaming)
	// One entry per tool plus one per active skill.
	require.Len(t, card.Skills, 3)

	toolSkill, found := findSkill(card.Skills, "file")
	require.True(t, found, "tool-derived skill entry must be present")
	assert.Equal(t, "read/write files", toolSkill.Description)
	assert.Empty(t, toolSkill.Tags, "tool-derived entries carry no tags")

	skillEntry, found := findSkill(card.Skills, "commit-helper")
	require.True(t, found, "skill-derived skill entry must be present")
	assert.Equal(t, "drafts commit messages", skillEntry.Description)
	assert.Equal(t, []string{"git", "workflow"}, skillEntry.Tags)
}

// TestBuildAgentCard_NilActiveSkills proves a nil skill list (no skill
// runtime, or discovery found nothing) degrades to tool-only entries rather
// than panicking — the common case when a2a serve has no skills configured.
func TestBuildAgentCard_NilActiveSkills(t *testing.T) {
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeTool{name: "file", desc: "read/write files"}))

	card := BuildAgentCard(reg, nil, "rubichan", "a coding agent", "https://example.com")

	assert.Len(t, card.Skills, 1)
}

func findSkill(list []Skill, id string) (Skill, bool) {
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return Skill{}, false
}
