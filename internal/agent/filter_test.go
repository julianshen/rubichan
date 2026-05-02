package agent

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

func TestFilterToolsWildcard(t *testing.T) {
	all := []provider.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}

	def := &agentsdk.AgentDefinition{Tools: []string{"*"}, DisallowedTools: []string{"shell"}}
	filtered := FilterTools(all, def, nil)

	require.Len(t, filtered, 2)
	require.Equal(t, "read_file", filtered[0].Name)
	require.Equal(t, "write_file", filtered[1].Name)
}

func TestFilterToolsExplicitAllow(t *testing.T) {
	all := []provider.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}

	def := &agentsdk.AgentDefinition{Tools: []string{"read_file", "grep"}}
	filtered := FilterTools(all, def, nil)

	require.Len(t, filtered, 1)
	require.Equal(t, "read_file", filtered[0].Name)
}

func TestFilterToolsGloballyDisallowed(t *testing.T) {
	all := []provider.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}

	def := &agentsdk.AgentDefinition{Tools: []string{"*"}}
	filtered := FilterTools(all, def, []string{"shell"})

	require.Len(t, filtered, 2)
	require.NotContains(t, toolNames(filtered), "shell")
}

func TestFilterToolsNilDefinition(t *testing.T) {
	all := []provider.ToolDef{
		{Name: "read_file"},
		{Name: "write_file"},
	}

	filtered := FilterTools(all, nil, nil)
	require.Equal(t, all, filtered)
}

func TestFilterToolsCoordinator(t *testing.T) {
	all := []provider.ToolDef{
		{Name: "Agent"},
		{Name: "TaskStop"},
		{Name: "SendMessage"},
		{Name: "shell"},
	}

	def := &agentsdk.AgentDefinition{Tools: []string{"Agent", "TaskStop", "SendMessage"}}
	filtered := FilterTools(all, def, nil)

	require.Len(t, filtered, 3)
	require.NotContains(t, toolNamesFromDefs(filtered), "shell")
}

func toolNamesFromDefs(tools []provider.ToolDef) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
