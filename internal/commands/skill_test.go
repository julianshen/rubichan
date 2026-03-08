package commands

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock SkillLister ---

type mockSkillLister struct {
	skills          []SkillInfo
	activateErr     error
	activatedName   string
	deactivateErr   error
	deactivatedName string
}

func (m *mockSkillLister) ListSkills() []SkillInfo {
	return m.skills
}

func (m *mockSkillLister) ActivateSkill(name string) error {
	m.activatedName = name
	return m.activateErr
}

func (m *mockSkillLister) DeactivateSkill(name string) error {
	m.deactivatedName = name
	return m.deactivateErr
}

// --- Skill Command Tests ---

func TestSkillCommandName(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})
	assert.Equal(t, "skill", cmd.Name())
}

func TestSkillCommandDescription(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})
	assert.NotEmpty(t, cmd.Description())
}

func TestSkillCommandArguments(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})
	args := cmd.Arguments()
	require.Len(t, args, 1)
	assert.Equal(t, "subcommand", args[0].Name)
	assert.False(t, args[0].Required)
}

func TestSkillCommandListNoArgs(t *testing.T) {
	lister := &mockSkillLister{
		skills: []SkillInfo{
			{Name: "alpha", Description: "Alpha skill", Source: "builtin", State: "Active"},
			{Name: "beta", Description: "Beta skill", Source: "project", State: "Inactive"},
		},
	}
	cmd := NewSkillCommand(lister)

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "alpha")
	assert.Contains(t, result.Output, "beta")
	assert.Contains(t, result.Output, "Active")
	assert.Contains(t, result.Output, "Inactive")
}

func TestSkillCommandListSubcommand(t *testing.T) {
	lister := &mockSkillLister{
		skills: []SkillInfo{
			{Name: "alpha", Description: "Alpha skill", Source: "builtin", State: "Active"},
		},
	}
	cmd := NewSkillCommand(lister)

	result, err := cmd.Execute(context.Background(), []string{"list"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "alpha")
	assert.Contains(t, result.Output, "Active")
}

func TestSkillCommandListEmpty(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})

	result, err := cmd.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, result.Output, "No skills")
}

func TestSkillCommandActivate(t *testing.T) {
	lister := &mockSkillLister{}
	cmd := NewSkillCommand(lister)

	result, err := cmd.Execute(context.Background(), []string{"activate", "my-skill"})
	require.NoError(t, err)
	assert.Equal(t, "my-skill", lister.activatedName)
	assert.Contains(t, result.Output, "my-skill")
	assert.Contains(t, result.Output, "activated")
}

func TestSkillCommandActivateNoName(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})

	_, err := cmd.Execute(context.Background(), []string{"activate"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "skill name is required")
}

func TestSkillCommandActivateError(t *testing.T) {
	lister := &mockSkillLister{activateErr: fmt.Errorf("not found")}
	cmd := NewSkillCommand(lister)

	_, err := cmd.Execute(context.Background(), []string{"activate", "missing"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSkillCommandDeactivate(t *testing.T) {
	lister := &mockSkillLister{}
	cmd := NewSkillCommand(lister)

	result, err := cmd.Execute(context.Background(), []string{"deactivate", "my-skill"})
	require.NoError(t, err)
	assert.Equal(t, "my-skill", lister.deactivatedName)
	assert.Contains(t, result.Output, "my-skill")
	assert.Contains(t, result.Output, "deactivated")
}

func TestSkillCommandDeactivateNoName(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})

	_, err := cmd.Execute(context.Background(), []string{"deactivate"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "skill name is required")
}

func TestSkillCommandDeactivateError(t *testing.T) {
	lister := &mockSkillLister{deactivateErr: fmt.Errorf("not active")}
	cmd := NewSkillCommand(lister)

	_, err := cmd.Execute(context.Background(), []string{"deactivate", "inactive-skill"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestSkillCommandUnknownSubcommand(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{})

	_, err := cmd.Execute(context.Background(), []string{"bogus"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown subcommand")
}

func TestSkillCommandCompleteSubcommands(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{
		skills: []SkillInfo{
			{Name: "alpha"},
			{Name: "beta"},
		},
	})

	candidates := cmd.Complete(context.Background(), nil)
	require.Len(t, candidates, 3)
	assert.Equal(t, "list", candidates[0].Value)
	assert.Equal(t, "activate", candidates[1].Value)
	assert.Equal(t, "deactivate", candidates[2].Value)
}

func TestSkillCommandCompleteActivateShowsInactiveOnly(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{
		skills: []SkillInfo{
			{Name: "alpha", Description: "Alpha skill", State: "Active"},
			{Name: "beta", Description: "Beta skill", State: "Inactive"},
			{Name: "gamma", Description: "Gamma skill", State: "Inactive"},
		},
	})

	candidates := cmd.Complete(context.Background(), []string{"activate"})
	require.Len(t, candidates, 2)
	assert.Equal(t, "beta", candidates[0].Value)
	assert.Equal(t, "gamma", candidates[1].Value)
}

func TestSkillCommandCompleteDeactivateShowsActiveOnly(t *testing.T) {
	cmd := NewSkillCommand(&mockSkillLister{
		skills: []SkillInfo{
			{Name: "alpha", Description: "Alpha skill", State: "Active"},
			{Name: "beta", Description: "Beta skill", State: "Inactive"},
		},
	})

	candidates := cmd.Complete(context.Background(), []string{"deactivate"})
	require.Len(t, candidates, 1)
	assert.Equal(t, "alpha", candidates[0].Value)
}

func TestSkillCommandImplementsSlashCommand(t *testing.T) {
	var _ SlashCommand = NewSkillCommand(&mockSkillLister{})
}
