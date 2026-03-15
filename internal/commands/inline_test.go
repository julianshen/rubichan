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
	assert.Equal(t, `/skill activate brainstorming`, result.Command)
	assert.Equal(t, []string{"/skill", "activate", "brainstorming"}, result.Args)
	assert.Equal(t, "activate", result.Action)
	assert.Equal(t, "brainstorming", result.Name)
}

func TestRewriteInlineSkillDirectiveDeactivate(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brainstorming","action":"deactivate"})`)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, `/skill deactivate brainstorming`, result.Command)
	assert.Equal(t, []string{"/skill", "deactivate", "brainstorming"}, result.Args)
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

func TestRewriteInlineSkillDirectiveRejectsEmptyPayload(t *testing.T) {
	_, ok, err := RewriteInlineSkillDirective(`__skill()`)

	require.Error(t, err)
	assert.True(t, ok)
	assert.Contains(t, err.Error(), "payload is required")
}

func TestRewriteInlineSkillDirectiveRejectsInvalidJSON(t *testing.T) {
	_, ok, err := RewriteInlineSkillDirective(`__skill(not-json)`)

	require.Error(t, err)
	assert.True(t, ok)
	assert.Contains(t, err.Error(), "parse skill directive")
}

func TestRewriteInlineSkillDirectiveRejectsUnsupportedAction(t *testing.T) {
	_, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brainstorming","action":"fly"})`)

	require.Error(t, err)
	assert.True(t, ok)
	assert.Contains(t, err.Error(), "unsupported skill directive action")
}

func TestRewriteInlineSkillDirectiveNormalizesActionCase(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brainstorming","action":"ACTIVATE"})`)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "activate", result.Action)
	assert.Equal(t, []string{"/skill", "activate", "brainstorming"}, result.Args)
}

func TestRewriteInlineSkillDirectiveCommandMatchesArgsEscaping(t *testing.T) {
	result, ok, err := RewriteInlineSkillDirective(`__skill({"name":"brain\"storm\\ing"})`)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"/skill", "activate", `brain"storm\ing`}, result.Args)
	assert.Equal(t, `/skill activate "brain\"storm\\ing"`, result.Command)
}
