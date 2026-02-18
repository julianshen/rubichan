package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillStateTransitions(t *testing.T) {
	tests := []struct {
		name string
		from SkillState
		to   SkillState
	}{
		{"Inactive to Activating", SkillStateInactive, SkillStateActivating},
		{"Activating to Active", SkillStateActivating, SkillStateActive},
		{"Activating to Error", SkillStateActivating, SkillStateError},
		{"Active to Inactive", SkillStateActive, SkillStateInactive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Skill{State: tt.from}
			err := s.TransitionTo(tt.to)
			require.NoError(t, err)
			assert.Equal(t, tt.to, s.State)
		})
	}
}

func TestSkillStateInvalidTransition(t *testing.T) {
	tests := []struct {
		name string
		from SkillState
		to   SkillState
	}{
		{"Active to Activating", SkillStateActive, SkillStateActivating},
		{"Inactive to Active", SkillStateInactive, SkillStateActive},
		{"Inactive to Error", SkillStateInactive, SkillStateError},
		{"Error to Active", SkillStateError, SkillStateActive},
		{"Error to Activating", SkillStateError, SkillStateActivating},
		{"Active to Error", SkillStateActive, SkillStateError},
		{"Active to Active", SkillStateActive, SkillStateActive},
		{"Inactive to Inactive", SkillStateInactive, SkillStateInactive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Skill{State: tt.from}
			err := s.TransitionTo(tt.to)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid state transition")
			// State should remain unchanged on error.
			assert.Equal(t, tt.from, s.State)
		})
	}
}

func TestHookPhaseString(t *testing.T) {
	tests := []struct {
		phase    HookPhase
		expected string
	}{
		{HookOnActivate, "OnActivate"},
		{HookOnDeactivate, "OnDeactivate"},
		{HookOnConversationStart, "OnConversationStart"},
		{HookOnBeforePromptBuild, "OnBeforePromptBuild"},
		{HookOnBeforeToolCall, "OnBeforeToolCall"},
		{HookOnAfterToolResult, "OnAfterToolResult"},
		{HookOnAfterResponse, "OnAfterResponse"},
		{HookOnBeforeWikiSection, "OnBeforeWikiSection"},
		{HookOnSecurityScanComplete, "OnSecurityScanComplete"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.phase.String())
		})
	}
}

func TestHookPhaseStringUnknown(t *testing.T) {
	unknown := HookPhase(999)
	assert.Equal(t, "HookPhase(999)", unknown.String())
}

func TestSkillStateString(t *testing.T) {
	tests := []struct {
		state    SkillState
		expected string
	}{
		{SkillStateInactive, "Inactive"},
		{SkillStateActivating, "Activating"},
		{SkillStateActive, "Active"},
		{SkillStateError, "Error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestSkillStateStringUnknown(t *testing.T) {
	unknown := SkillState(999)
	assert.Equal(t, "SkillState(999)", unknown.String())
}
