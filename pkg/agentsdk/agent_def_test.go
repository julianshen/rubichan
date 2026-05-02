package agentsdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentDefinitionIsCoordinator(t *testing.T) {
	coord := &AgentDefinition{Tools: []string{"Agent", "TaskStop", "SendMessage"}}
	require.True(t, coord.IsCoordinator())

	general := &AgentDefinition{Tools: []string{"*"}}
	require.False(t, general.IsCoordinator())

	partial := &AgentDefinition{Tools: []string{"Agent", "TaskStop"}}
	require.False(t, partial.IsCoordinator())
}

func TestAgentModeValues(t *testing.T) {
	require.Equal(t, AgentMode("general-purpose"), AgentModeGeneralPurpose)
	require.Equal(t, AgentMode("explore"), AgentModeExplore)
	require.Equal(t, AgentMode("plan"), AgentModePlan)
	require.Equal(t, AgentMode("verification"), AgentModeVerification)
}
