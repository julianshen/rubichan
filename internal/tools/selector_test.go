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
		makeToolDef("search"),
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
	assert.Equal(t, CategoryFileSystem, Categorize("search"))
	assert.Equal(t, CategoryPlatform, Categorize("xcode_build"))
	assert.Equal(t, CategoryPlatform, Categorize("xcode_discover"))
	assert.Equal(t, CategoryMCP, Categorize("mcp-github"))
	assert.Equal(t, CategorySkill, Categorize("custom_tool"))
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

func TestSelectorAllToolsFallback(t *testing.T) {
	ts := NewToolSelector()
	// Generic message with no file/platform keywords
	messages := []provider.Message{
		{Role: "user", Content: []provider.ContentBlock{{Type: "text", Text: "hello, how are you?"}}},
	}

	result := ts.Select(messages, allTestTools())

	// Should fall back to all tools
	assert.ElementsMatch(t, toolNames(result), toolNames(allTestTools()))
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

func TestSelectorEmptyMessagesReturnsAll(t *testing.T) {
	ts := NewToolSelector()

	result := ts.Select(nil, allTestTools())
	assert.ElementsMatch(t, toolNames(result), toolNames(allTestTools()))
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
