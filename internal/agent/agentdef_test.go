package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentDefRegistryRegisterAndGet(t *testing.T) {
	reg := NewAgentDefRegistry()
	def := &AgentDef{
		Name:         "explorer",
		Description:  "Explore codebase",
		SystemPrompt: "You are an explorer.",
		Tools:        []string{"file", "search"},
		MaxTurns:     5,
	}
	err := reg.Register(def)
	require.NoError(t, err)

	got, ok := reg.Get("explorer")
	assert.True(t, ok)
	assert.Equal(t, "explorer", got.Name)
	assert.Equal(t, []string{"file", "search"}, got.Tools)
}

func TestAgentDefRegistryDuplicateError(t *testing.T) {
	reg := NewAgentDefRegistry()
	def := &AgentDef{Name: "explorer"}
	require.NoError(t, reg.Register(def))
	assert.Error(t, reg.Register(def))
}

func TestAgentDefRegistryAll(t *testing.T) {
	reg := NewAgentDefRegistry()
	_ = reg.Register(&AgentDef{Name: "b"})
	_ = reg.Register(&AgentDef{Name: "a"})

	all := reg.All()
	assert.Len(t, all, 2)
	assert.Equal(t, "a", all[0].Name) // sorted
	assert.Equal(t, "b", all[1].Name)
}

func TestAgentDefRegistryUnregister(t *testing.T) {
	reg := NewAgentDefRegistry()
	_ = reg.Register(&AgentDef{Name: "explorer"})
	assert.NoError(t, reg.Unregister("explorer"))
	_, ok := reg.Get("explorer")
	assert.False(t, ok)
}

func TestAgentDefRegistryUnregisterNotFound(t *testing.T) {
	reg := NewAgentDefRegistry()
	assert.Error(t, reg.Unregister("nonexistent"))
}

func TestAgentDefRegistryGetNotFound(t *testing.T) {
	reg := NewAgentDefRegistry()
	_, ok := reg.Get("nonexistent")
	assert.False(t, ok)
}
