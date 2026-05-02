package agent

import (
	"testing"

	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestAgentRegistry_BuiltIns(t *testing.T) {
	r := NewAgentRegistry()

	// Built-in exists
	def, ok := r.Get("explore")
	require.True(t, ok)
	require.Equal(t, agentsdk.AgentModeExplore, def.Mode)
	require.Equal(t, []string{"read_file", "grep", "glob", "list_dir", "shell"}, def.Tools)

	// General-purpose exists
	def, ok = r.Get("general-purpose")
	require.True(t, ok)
	require.Equal(t, agentsdk.AgentModeGeneralPurpose, def.Mode)

	// Plan exists
	def, ok = r.Get("plan")
	require.True(t, ok)
	require.Equal(t, agentsdk.AgentModePlan, def.Mode)
}

func TestAgentRegistry_CustomRegistration(t *testing.T) {
	r := NewAgentRegistry()

	custom := &agentsdk.AgentDefinition{Name: "custom", Tools: []string{"read_file"}}
	require.NoError(t, r.Register(custom))

	got, ok := r.Get("custom")
	require.True(t, ok)
	require.Equal(t, "read_file", got.Tools[0])
}

func TestAgentRegistry_RegisterEmptyName(t *testing.T) {
	r := NewAgentRegistry()
	custom := &agentsdk.AgentDefinition{Tools: []string{"read_file"}}
	require.Error(t, r.Register(custom))
}

func TestAgentRegistry_CustomOverridesBuiltIn(t *testing.T) {
	r := NewAgentRegistry()

	// Override built-in "explore"
	custom := &agentsdk.AgentDefinition{Name: "explore", Tools: []string{"read_file"}}
	require.NoError(t, r.Register(custom))

	got, ok := r.Get("explore")
	require.True(t, ok)
	require.Equal(t, []string{"read_file"}, got.Tools)
}

func TestAgentRegistry_Names(t *testing.T) {
	r := NewAgentRegistry()
	names := r.Names()
	require.GreaterOrEqual(t, len(names), 3) // at least 3 built-ins

	// Register custom
	require.NoError(t, r.Register(&agentsdk.AgentDefinition{Name: "custom"}))
	names = r.Names()
	require.Contains(t, names, "custom")
}
