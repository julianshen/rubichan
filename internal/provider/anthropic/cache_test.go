package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyWithCacheBreakpoints(t *testing.T) {
	p := New("https://api.anthropic.com", "test-key")
	req := provider.CompletionRequest{
		Model:            "claude-sonnet-4-5",
		System:           "You are helpful. ## Rules\nBe nice.",
		Messages:         []provider.Message{provider.NewUserMessage("hello")},
		MaxTokens:        1024,
		CacheBreakpoints: []int{15}, // breakpoint after "You are helpful"
	}

	body, err := p.buildRequestBody(req)
	assert.NoError(t, err)
	assert.Contains(t, string(body), "cache_control")
}

func TestBuildCachedSystemBlocksSingleBreakpoint(t *testing.T) {
	system := "You are helpful. ## Rules\nBe nice."
	// Breakpoint at byte offset 16 splits after "You are helpful."
	blocks := buildCachedSystemBlocks(system, []int{16})

	require.Len(t, blocks, 2)

	// First block: system[0:16] = "You are helpful.", has cache_control
	assert.Equal(t, "text", blocks[0].Type)
	assert.Equal(t, "You are helpful.", blocks[0].Text)
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "ephemeral", blocks[0].CacheControl.Type)

	// Second block: system[16:] = " ## Rules\nBe nice.", no cache_control
	assert.Equal(t, "text", blocks[1].Type)
	assert.Equal(t, " ## Rules\nBe nice.", blocks[1].Text)
	assert.Nil(t, blocks[1].CacheControl)
}

func TestBuildCachedSystemBlocksMultipleBreakpoints(t *testing.T) {
	system := "AAABBBCCC"
	blocks := buildCachedSystemBlocks(system, []int{3, 6})

	require.Len(t, blocks, 3)

	assert.Equal(t, "AAA", blocks[0].Text)
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "ephemeral", blocks[0].CacheControl.Type)

	assert.Equal(t, "BBB", blocks[1].Text)
	require.NotNil(t, blocks[1].CacheControl)
	assert.Equal(t, "ephemeral", blocks[1].CacheControl.Type)

	assert.Equal(t, "CCC", blocks[2].Text)
	assert.Nil(t, blocks[2].CacheControl)
}

func TestBuildCachedSystemBlocksBreakpointBeyondLength(t *testing.T) {
	system := "short"
	blocks := buildCachedSystemBlocks(system, []int{100})

	// Breakpoint clamped to string length: one block with cache_control, no trailing block
	require.Len(t, blocks, 1)
	assert.Equal(t, "short", blocks[0].Text)
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "ephemeral", blocks[0].CacheControl.Type)
}

func TestBuildCachedSystemBlocksBreakpointAtEnd(t *testing.T) {
	system := "exact"
	blocks := buildCachedSystemBlocks(system, []int{5})

	// Breakpoint at exact end: one block with cache_control, no trailing block
	require.Len(t, blocks, 1)
	assert.Equal(t, "exact", blocks[0].Text)
	require.NotNil(t, blocks[0].CacheControl)
}

func TestBuildCachedSystemBlocksSkipInvalidBreakpoint(t *testing.T) {
	system := "AAABBB"
	// Breakpoint at 0 is <= prev (0), should be skipped
	blocks := buildCachedSystemBlocks(system, []int{0, 3})

	require.Len(t, blocks, 2)
	assert.Equal(t, "AAA", blocks[0].Text)
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "BBB", blocks[1].Text)
	assert.Nil(t, blocks[1].CacheControl)
}

func TestBuildRequestBodyBackwardCompatibleWithoutBreakpoints(t *testing.T) {
	p := New("https://api.anthropic.com", "test-key")
	req := provider.CompletionRequest{
		Model:     "claude-sonnet-4-5",
		System:    "You are helpful.",
		Messages:  []provider.Message{provider.NewUserMessage("hello")},
		MaxTokens: 1024,
		// CacheBreakpoints is nil — backward compatible
	}

	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	// Parse the JSON and verify system is a plain string, not an array
	var parsed map[string]any
	err = json.Unmarshal(body, &parsed)
	require.NoError(t, err)

	systemVal, ok := parsed["system"]
	require.True(t, ok, "system field must be present")
	_, isString := systemVal.(string)
	assert.True(t, isString, "system must serialize as a plain string when no breakpoints")
	assert.Equal(t, "You are helpful.", systemVal)
}

func TestBuildRequestBodyEmptyBreakpointsBackwardCompatible(t *testing.T) {
	p := New("https://api.anthropic.com", "test-key")
	req := provider.CompletionRequest{
		Model:            "claude-sonnet-4-5",
		System:           "You are helpful.",
		Messages:         []provider.Message{provider.NewUserMessage("hello")},
		MaxTokens:        1024,
		CacheBreakpoints: []int{}, // empty slice, should still serialize as string
	}

	body, err := p.buildRequestBody(req)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(body, &parsed)
	require.NoError(t, err)

	systemVal := parsed["system"]
	_, isString := systemVal.(string)
	assert.True(t, isString, "system must serialize as a plain string when breakpoints slice is empty")
}
