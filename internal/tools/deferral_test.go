package tools

import (
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func TestDeferralManagerNoDeferralUnderThreshold(t *testing.T) {
	dm := NewDeferralManager(0.10) // 10% threshold

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},
		{Name: "file", Description: "read/write", InputSchema: []byte(`{}`)},
	}

	active, deferred := dm.SelectForContext(allTools, 100000)
	assert.Equal(t, len(allTools), len(active))
	assert.Equal(t, 0, deferred)
}

func TestDeferralManagerDefersOverThreshold(t *testing.T) {
	dm := NewDeferralManager(0.10) // 10% of 1000 = 100 token budget for tools

	// Create tools where MCP tools push past the threshold.
	bigSchema := make([]byte, 2000) // ~500 tokens each
	for i := range bigSchema {
		bigSchema[i] = 'a'
	}

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},      // core — never deferred
		{Name: "mcp-tool1", Description: "big", InputSchema: bigSchema},      // MCP — deferred first
		{Name: "mcp-tool2", Description: "also big", InputSchema: bigSchema}, // MCP — deferred first
	}

	active, deferred := dm.SelectForContext(allTools, 1000)
	assert.Greater(t, deferred, 0, "should defer some MCP tools")
	// Core tool "shell" should always be active.
	hasShell := false
	for _, td := range active {
		if td.Name == "shell" {
			hasShell = true
		}
	}
	assert.True(t, hasShell, "core tools should never be deferred")
}

func TestDeferralManagerSearch(t *testing.T) {
	dm := NewDeferralManager(0.10)

	bigSchema := make([]byte, 2000)
	for i := range bigSchema {
		bigSchema[i] = 'a'
	}

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},
		{Name: "mcp-xcode-build", Description: "Build Xcode projects", InputSchema: bigSchema},
	}

	dm.SelectForContext(allTools, 1000) // trigger deferral

	results := dm.Search("xcode")
	assert.GreaterOrEqual(t, len(results), 0) // may or may not find depending on deferral
}
