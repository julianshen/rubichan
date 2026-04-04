package tools

import (
	"encoding/json"
	"testing"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/stretchr/testify/assert"
)

func makeToolDef(name string) provider.ToolDef {
	return provider.ToolDef{
		Name:        name,
		Description: name + " tool",
		InputSchema: json.RawMessage(`{}`),
	}
}

func allTestTools() []provider.ToolDef {
	return []provider.ToolDef{
		makeToolDef("shell"),
		makeToolDef("file"),
		makeToolDef("process"),
		makeToolDef("tool_search"),
		makeToolDef("search"),
		makeToolDef("git_status"),
		makeToolDef("http_get"),
		makeToolDef("browser_open"),
		makeToolDef("xcode_build"),
		makeToolDef("xcode_discover"),
		makeToolDef("mcp-github"),
	}
}

func toolNames(defs []provider.ToolDef) []string {
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

func TestCategorize(t *testing.T) {
	assert.Equal(t, CategoryCore, Categorize("shell"))
	assert.Equal(t, CategoryCore, Categorize("file"))
	assert.Equal(t, CategoryCore, Categorize("process"))
	assert.Equal(t, CategoryFileSystem, Categorize("search"))
	assert.Equal(t, CategoryGit, Categorize("git_status"))
	assert.Equal(t, CategoryNet, Categorize("http_get"))
	assert.Equal(t, CategoryNet, Categorize("browser_open"))
	assert.Equal(t, CategoryPlatform, Categorize("xcode_build"))
	assert.Equal(t, CategoryPlatform, Categorize("xcode_discover"))
	assert.Equal(t, CategoryMCP, Categorize("mcp-github"))
	assert.Equal(t, CategorySkill, Categorize("custom_tool"))
}

func safeBaselineToolNames() []string {
	return []string{"shell", "file", "process", "tool_search"}
}

func TestSelectorCoreToolsAlwaysPresent(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "read the file at main.go"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "tool_search")
}

func TestSelectorFileToolsIncludedOnFileMentions(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "search for the function definition in the codebase"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	assert.Contains(t, names, "search")
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
}

func TestSelectorPlatformToolsIncludedOnPlatformMentions(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "build the xcode project"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	assert.Contains(t, names, "xcode_build")
	assert.Contains(t, names, "xcode_discover")
}

func TestSelectorSafeBaselineFallback(t *testing.T) {
	ts := NewToolSelector()
	// Generic message with no file/platform keywords
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello, how are you?"}}},
	}

	result := ts.Select(messages, allTestTools())

	assert.ElementsMatch(t, safeBaselineToolNames(), toolNames(result))
}

func TestSelectorRecentToolUsageIncluded(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "assistant", Content: []provider.ContentBlock{
			{Type: "tool_use", Name: "xcode_build", ID: "t1"},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Text: "build succeeded"},
		}},
		{Role: "user", Content: []provider.ContentBlock{
			{Type: "text", Text: "now run the tests"},
		}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	// xcode_build should be included because it was recently used
	assert.Contains(t, names, "xcode_build")
}

func TestSelectorEmptyMessagesReturnSafeBaseline(t *testing.T) {
	ts := NewToolSelector()

	result := ts.Select(nil, allTestTools())
	assert.ElementsMatch(t, safeBaselineToolNames(), toolNames(result))
}

func TestSelectorKeywordDetection(t *testing.T) {
	ts := NewToolSelector()

	// File keywords
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "check the .go files in the directory"}}},
	}
	result := ts.Select(messages, allTestTools())
	names := toolNames(result)
	assert.Contains(t, names, "search")

	// Platform keywords
	messages = []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "run the ios simulator"}}},
	}
	result = ts.Select(messages, allTestTools())
	names = toolNames(result)
	assert.Contains(t, names, "xcode_build")
}

func TestSelectorExplorationKeywordsActivateSearchAndGit(t *testing.T) {
	ts := NewToolSelector()

	explorationPrompts := []string{
		"Give me the brief",
		"Analyze this project",
		"list the features",
		"review the codes",
		"describe the architecture",
		"explain how this codebase works",
		"show me an overview of the project",
		"summarize the modules",
	}

	for _, prompt := range explorationPrompts {
		t.Run(prompt, func(t *testing.T) {
			messages := []provider.Message{
				{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: prompt}}},
			}

			result := ts.Select(messages, allTestTools())
			names := toolNames(result)

			assert.Contains(t, names, "search",
				"search tool should be included for exploration query: %q", prompt)
			assert.Contains(t, names, "git_status",
				"git tools should be included for exploration query: %q", prompt)
			assert.Contains(t, names, "shell", "core tools always present")
			assert.Contains(t, names, "file", "core tools always present")
		})
	}
}

func TestSelectorExplorationKeywordsDoNotActivateNetOrPlatform(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "analyze this project"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	assert.NotContains(t, names, "http_get",
		"net tools should not be activated by exploration keywords alone")
	assert.NotContains(t, names, "xcode_build",
		"platform tools should not be activated by exploration keywords alone")
	assert.NotContains(t, names, "mcp-github",
		"MCP tools should not be activated by exploration keywords alone")
}

func TestSelectorGenericPromptsDoNotExposeSensitiveCategories(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "please help me with this task"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	assert.ElementsMatch(t, safeBaselineToolNames(), names)
	assert.NotContains(t, names, "http_get")
	assert.NotContains(t, names, "browser_open")
	assert.NotContains(t, names, "xcode_build")
	assert.NotContains(t, names, "xcode_discover")
	assert.NotContains(t, names, "mcp-github")
	assert.NotContains(t, names, "git_status")
}

// Tests for ApplyMaxToolCount

func TestApplyMaxToolCountZeroOrNegative(t *testing.T) {
	tools := allTestTools()

	// maxCount <= 0 means no limit — return unchanged
	assert.Equal(t, tools, ApplyMaxToolCount(tools, 0))
	assert.Equal(t, tools, ApplyMaxToolCount(tools, -1))
}

func TestApplyMaxToolCountWithinLimit(t *testing.T) {
	tools := allTestTools()

	// maxCount >= len(tools) — return unchanged
	assert.Equal(t, tools, ApplyMaxToolCount(tools, len(tools)))
	assert.Equal(t, tools, ApplyMaxToolCount(tools, len(tools)+5))
}

func TestApplyMaxToolCountPreservesCoreAndToolSearch(t *testing.T) {
	// allTestTools has 11 tools: shell, file, process, tool_search (core+search), then 7 non-core
	// Limit to exactly 4 (the core set) — must include shell, file, process, tool_search
	all := allTestTools()
	result := ApplyMaxToolCount(all, 4)
	names := toolNames(result)

	assert.Len(t, result, 4)
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "process")
	assert.Contains(t, names, "tool_search")
}

func TestApplyMaxToolCountTrimsNonCoreFromEnd(t *testing.T) {
	// allTestTools: shell, file, process, tool_search, search, git_status, http_get, browser_open, xcode_build, xcode_discover, mcp-github
	// Limit to 6 — keeps core 4 + first 2 non-core
	all := allTestTools()
	result := ApplyMaxToolCount(all, 6)
	names := toolNames(result)

	assert.Len(t, result, 6)
	// Core always present
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "process")
	assert.Contains(t, names, "tool_search")
	// First 2 non-core (search, git_status) kept; rest trimmed
	assert.Contains(t, names, "search")
	assert.Contains(t, names, "git_status")
	assert.NotContains(t, names, "http_get")
	assert.NotContains(t, names, "xcode_build")
	assert.NotContains(t, names, "mcp-github")
}

func TestApplyMaxToolCountMaxLessThanCoreCount(t *testing.T) {
	// If maxCount < number of core tools, all core tools are still returned (never drop core).
	all := allTestTools()
	result := ApplyMaxToolCount(all, 2)
	names := toolNames(result)

	// Core tools are always preserved even if they exceed maxCount
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "file")
	assert.Contains(t, names, "process")
	assert.Contains(t, names, "tool_search")
	// No non-core tools
	assert.NotContains(t, names, "search")
	assert.NotContains(t, names, "git_status")
}

func TestSelectReturnsSorted(t *testing.T) {
	ts := NewToolSelector()
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "read the file at main.go"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	// Verify the result is sorted alphabetically.
	for i := 1; i < len(names); i++ {
		assert.True(t, names[i-1] <= names[i],
			"expected sorted order but %q > %q at position %d", names[i-1], names[i], i)
	}
}

func TestSelectSafeBaselineReturnsSorted(t *testing.T) {
	ts := NewToolSelector()
	// Generic message triggers safe baseline fallback.
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello"}}},
	}

	result := ts.Select(messages, allTestTools())
	names := toolNames(result)

	for i := 1; i < len(names); i++ {
		assert.True(t, names[i-1] <= names[i],
			"expected sorted order but %q > %q at position %d", names[i-1], names[i], i)
	}
}
