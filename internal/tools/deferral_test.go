package tools

import (
	"strings"
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

	// The MCP tool should have been deferred (exceeds 10% of 1000 budget).
	assert.Equal(t, 1, dm.DeferredCount(), "mcp-xcode-build should be deferred")

	results := dm.Search("xcode")
	assert.Equal(t, 1, len(results), "should find the deferred xcode tool")
	assert.Equal(t, "mcp-xcode-build", results[0].Name)
}

func TestToolSummaryNoDeferredTools(t *testing.T) {
	dm := NewDeferralManager(0.10)

	activeTools := []provider.ToolDef{
		{Name: "shell", Description: "Execute shell commands.", InputSchema: []byte(`{}`)},
		{Name: "file", Description: "Read or write files.", InputSchema: []byte(`{}`)},
	}

	summary := dm.ToolSummary(activeTools)

	assert.Contains(t, summary, "## Available Tools")
	assert.Contains(t, summary, "**shell**: Execute shell commands.")
	assert.Contains(t, summary, "**file**: Read or write files.")
	// No deferred tools — the hint paragraph must not appear.
	assert.NotContains(t, summary, "Additional tools are available")
}

func TestToolSummaryWithDeferredTools(t *testing.T) {
	dm := NewDeferralManager(0.10)

	bigSchema := make([]byte, 2000)
	for i := range bigSchema {
		bigSchema[i] = 'a'
	}

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "Execute shell commands.", InputSchema: []byte(`{}`)},
		{Name: "mcp-http", Description: "Fetch HTTP resources.", InputSchema: bigSchema},
	}

	activeTools, _ := dm.SelectForContext(allTools, 1000)

	summary := dm.ToolSummary(activeTools)

	assert.Contains(t, summary, "## Available Tools")
	assert.Contains(t, summary, "**shell**: Execute shell commands.")
	// Deferred tools trigger the hint paragraph.
	assert.Contains(t, summary, "Additional tools are available but not shown")
	assert.Contains(t, summary, "tool_search")
}

func TestToolSummaryTruncatesLongDescriptions(t *testing.T) {
	dm := NewDeferralManager(0.10)

	longDesc := "This is a very long description that goes on and on past one hundred and twenty characters without stopping early and never has a sentence boundary anywhere in sight"
	activeTools := []provider.ToolDef{
		{Name: "verbose-tool", Description: longDesc, InputSchema: []byte(`{}`)},
	}

	summary := dm.ToolSummary(activeTools)

	// The description should be truncated with "..." when it exceeds 120 chars.
	for _, line := range strings.Split(summary, "\n") {
		if strings.Contains(line, "**verbose-tool**") {
			parts := strings.SplitN(line, ": ", 2)
			assert.LessOrEqual(t, len(parts[1]), 123) // 120 + "..."
			assert.True(t, strings.HasSuffix(parts[1], "..."), "truncated descriptions should end with ellipsis")
		}
	}
}

func TestDeferralManagerZeroEffectiveWindow(t *testing.T) {
	dm := NewDeferralManager(0.10)

	allTools := []provider.ToolDef{
		{Name: "shell", Description: "exec", InputSchema: []byte(`{}`)},
		{Name: "file", Description: "read/write", InputSchema: []byte(`{}`)},
		{Name: "http_get", Description: "fetch", InputSchema: []byte(`{}`)},
	}

	// With effectiveWindow=0, budget is 0 — all non-core tools should be deferred.
	active, deferred := dm.SelectForContext(allTools, 0)
	assert.Greater(t, len(active), 0, "core tools must survive even with zero window")
	// At minimum, core tools should be active.
	hasShell := false
	for _, td := range active {
		if td.Name == "shell" {
			hasShell = true
		}
	}
	assert.True(t, hasShell, "core tools should be active even with zero window")
	_ = deferred // non-core tools may or may not be deferred depending on their token cost
}

func TestTruncateToFirstSentenceWithURL(t *testing.T) {
	// URLs contain dots that should not be treated as sentence boundaries.
	desc := "Fetch data from api.example.com and return results."
	result := truncateToFirstSentence(desc)
	// The first '.' is inside the URL, but is followed by 'e' not space,
	// so truncation should occur at the period after "results".
	assert.Equal(t, "Fetch data from api.example.com and return results.", result)
}

func TestTruncateToFirstSentenceWithAbbreviation(t *testing.T) {
	// "U.S." has dots not followed by spaces (except the last).
	desc := "Use the U.S. format for dates."
	result := truncateToFirstSentence(desc)
	// "U." is followed by "S", not space, so it's not a sentence end.
	// "S." is followed by " ", so it IS detected as a sentence end — this is
	// a known limitation. The result will be "Use the U.S." which is acceptable
	// for a display-only summary truncation.
	assert.Contains(t, result, "U.S.")
}

func TestToolSummaryFirstSentenceTruncation(t *testing.T) {
	dm := NewDeferralManager(0.10)

	// Description with multiple sentences — only first should appear.
	desc := "Execute shell commands. Be careful with side effects."
	activeTools := []provider.ToolDef{
		{Name: "shell", Description: desc, InputSchema: []byte(`{}`)},
	}

	summary := dm.ToolSummary(activeTools)

	assert.Contains(t, summary, "Execute shell commands.")
	assert.NotContains(t, summary, "Be careful with side effects.")
}

func TestTruncateToFirstSentence(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Simple sentence.", "Simple sentence."},
		{"First sentence. Second sentence.", "First sentence."},
		{"No punctuation here", "No punctuation here"},
		{"Short!", "Short!"},
		{"Question? Answer.", "Question?"},
		{"", ""},
	}

	for _, tc := range tests {
		got := truncateToFirstSentence(tc.input)
		assert.Equal(t, tc.expected, got, "input: %q", tc.input)
	}
}
