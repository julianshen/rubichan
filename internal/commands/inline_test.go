package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteInlineSkillDirectiveActivate(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brainstorming"})`)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `/skill activate "brainstorming"`, result.Command)
	assert.Equal(t, "activate", result.Action)
	assert.Equal(t, "brainstorming", result.Name)
}

func TestRewriteInlineSkillDirectiveDeactivate(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brainstorming","action":"deactivate"})`)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `/skill deactivate "brainstorming"`, result.Command)
	assert.Equal(t, "deactivate", result.Action)
	assert.Equal(t, "brainstorming", result.Name)
}

func TestRewriteInlineSkillDirectiveRejectsMissingName(t *testing.T) {
	_, ok, err := RewriteInlineSkillDirective(`__skill({"action":"activate"})`)

	require.Error(t, err)
	assert.True(t, ok)
	assert.Contains(t, err.Error(), "name is required")
}

func TestRewriteInlineSkillDirectiveIgnoresPlainText(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective("hello")

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, InlineSkillDirectiveResult{}, result)
}
