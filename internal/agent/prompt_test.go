package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptBuilderEmptyBuild(t *testing.T) {
	pb := NewPromptBuilder()
	prompt, breakpoints := pb.Build()
	assert.Equal(t, "", prompt)
	assert.Empty(t, breakpoints)
}

func TestPromptBuilderOrdersCacheableFirst(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "dynamic", Content: "changes every turn", Cacheable: false})
	pb.AddSection(PromptSection{Name: "static", Content: "never changes", Cacheable: true})

	prompt, _ := pb.Build()
	staticIdx := strings.Index(prompt, "never changes")
	dynamicIdx := strings.Index(prompt, "changes every turn")
	assert.Less(t, staticIdx, dynamicIdx, "cacheable sections should come first")
}

func TestPromptBuilderBreakpointAfterCacheable(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "base", Content: "base instructions", Cacheable: true})
	pb.AddSection(PromptSection{Name: "rules", Content: "project rules", Cacheable: true})
	pb.AddSection(PromptSection{Name: "notes", Content: "scratchpad", Cacheable: false})

	_, breakpoints := pb.Build()
	assert.Len(t, breakpoints, 1, "one breakpoint after last cacheable section")
	assert.Greater(t, breakpoints[0], 0)
}

func TestPromptBuilderAllCacheableNoBreakpoint(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "a", Content: "aaa", Cacheable: true})
	pb.AddSection(PromptSection{Name: "b", Content: "bbb", Cacheable: true})

	_, breakpoints := pb.Build()
	assert.Empty(t, breakpoints, "no breakpoint when all sections cacheable")
}

func TestPromptBuilderAllDynamicNoBreakpoint(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddSection(PromptSection{Name: "a", Content: "aaa", Cacheable: false})
	pb.AddSection(PromptSection{Name: "b", Content: "bbb", Cacheable: false})

	_, breakpoints := pb.Build()
	assert.Empty(t, breakpoints, "no breakpoint when all sections dynamic")
}

func TestPromptBuilder_TypedAPI_CacheableFirst(t *testing.T) {
	pb := NewPromptBuilder()
	pb.AddDynamicSection_UNCACHED("user-ctx", "session: abc123", "contains session-specific data")
	pb.AddCacheableSection("instructions", "You are a helpful assistant.")
	pb.AddCacheableSection("tools", "Available tools: ...")

	prompt, breakpoints := pb.Build()

	instrIdx := strings.Index(prompt, "You are a helpful assistant")
	toolsIdx := strings.Index(prompt, "Available tools")
	sessionIdx := strings.Index(prompt, "session: abc123")

	require.NotEqual(t, -1, instrIdx)
	require.NotEqual(t, -1, toolsIdx)
	require.NotEqual(t, -1, sessionIdx)
	assert.True(t, instrIdx < sessionIdx, "cacheable content must precede dynamic content")
	assert.True(t, toolsIdx < sessionIdx, "cacheable content must precede dynamic content")

	require.Len(t, breakpoints, 1, "expect exactly one breakpoint between cacheable and dynamic")
	assert.True(t, breakpoints[0] < sessionIdx)
	assert.True(t, breakpoints[0] > toolsIdx)
}

func TestPromptBuilder_CacheStability(t *testing.T) {
	// The breakpoint byte offset must be stable across dynamic content changes.
	makePrompt := func(sessionData string) (string, []int) {
		pb := NewPromptBuilder()
		pb.AddCacheableSection("system", "static instructions")
		pb.AddDynamicSection_UNCACHED("session", sessionData, "per-session data")
		return pb.Build()
	}

	_, bp1 := makePrompt("session: aaa")
	_, bp2 := makePrompt("session: bbb bbb bbb bbb bbb")

	require.Len(t, bp1, 1)
	require.Len(t, bp2, 1)
	assert.Equal(t, bp1[0], bp2[0], "breakpoint byte offset must be stable across dynamic content changes")
}
