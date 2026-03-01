package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
