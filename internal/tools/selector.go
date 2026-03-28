package tools

import (
	"strings"

	"github.com/julianshen/rubichan/internal/provider"
)

// ToolSelector selects relevant tools for a given conversation context.
type ToolSelector struct {
	lookback int // number of recent messages to check for recency
}

// NewToolSelector creates a ToolSelector with default settings.
func NewToolSelector() *ToolSelector {
	return &ToolSelector{lookback: 5}
}

// Select returns the subset of tools relevant to the current conversation.
// Core tools are always included. Other tools are included based on keyword
// heuristics and recent tool usage. When nothing else matches, it falls back
// to a safe baseline so deferred tools remain discoverable via tool_search.
func (ts *ToolSelector) Select(messages []provider.Message, allTools []provider.ToolDef) []provider.ToolDef {
	if len(messages) == 0 {
		return selectSafeBaseline(allTools)
	}

	// Collect recent text for keyword analysis.
	recentText := ts.collectRecentText(messages)
	recentToolNames := ts.collectRecentToolNames(messages)

	var selected []provider.ToolDef
	nonCoreMatched := false

	for _, tool := range allTools {
		cat := Categorize(tool.Name)

		switch {
		case cat == CategoryCore || tool.Name == "tool_search":
			selected = append(selected, tool)

		case cat == CategoryFileSystem:
			if containsFileKeywords(recentText) || recentToolNames[tool.Name] {
				selected = append(selected, tool)
				nonCoreMatched = true
			}

		case cat == CategoryPlatform:
			if containsPlatformKeywords(recentText) || recentToolNames[tool.Name] {
				selected = append(selected, tool)
				nonCoreMatched = true
			}

		case cat == CategoryLSP:
			if containsLSPKeywords(recentText) || recentToolNames[tool.Name] {
				selected = append(selected, tool)
				nonCoreMatched = true
			}

		case cat == CategoryGit || cat == CategoryNet || cat == CategoryMCP || cat == CategorySkill:
			if recentToolNames[tool.Name] || containsToolNameKeyword(recentText, tool.Name) {
				selected = append(selected, tool)
				nonCoreMatched = true
			}
		}
	}

	// Fallback: if no non-core tools matched, expose only the safe baseline.
	if !nonCoreMatched {
		return selectSafeBaseline(allTools)
	}

	return selected
}

func selectSafeBaseline(allTools []provider.ToolDef) []provider.ToolDef {
	var selected []provider.ToolDef
	for _, tool := range allTools {
		if Categorize(tool.Name) == CategoryCore || tool.Name == "tool_search" {
			selected = append(selected, tool)
		}
	}
	return selected
}

// ApplyMaxToolCount trims tools to fit within maxCount, always preserving core
// tools (CategoryCore) and tool_search. Non-core tools are trimmed from the end
// of the slice. Returns tools unchanged when maxCount <= 0 or len(tools) <= maxCount.
func ApplyMaxToolCount(tools []provider.ToolDef, maxCount int) []provider.ToolDef {
	if maxCount <= 0 || len(tools) <= maxCount {
		return tools
	}

	// Separate core/tool_search from non-core, preserving original order.
	var core, nonCore []provider.ToolDef
	for _, t := range tools {
		if Categorize(t.Name) == CategoryCore || t.Name == "tool_search" {
			core = append(core, t)
		} else {
			nonCore = append(nonCore, t)
		}
	}

	// Core tools are always kept; trim non-core from the end to fit budget.
	nonCoreSlots := maxCount - len(core)
	if nonCoreSlots < 0 {
		nonCoreSlots = 0
	}
	if nonCoreSlots < len(nonCore) {
		nonCore = nonCore[:nonCoreSlots]
	}

	return append(core, nonCore...)
}

// collectRecentText extracts text from the last few messages for keyword matching.
func (ts *ToolSelector) collectRecentText(messages []provider.Message) string {
	var sb strings.Builder
	start := len(messages) - ts.lookback
	if start < 0 {
		start = 0
	}
	for _, msg := range messages[start:] {
		for _, block := range msg.Content {
			if block.Text != "" {
				sb.WriteString(block.Text)
				sb.WriteString(" ")
			}
		}
	}
	return strings.ToLower(sb.String())
}

// collectRecentToolNames returns tool names used in recent messages.
func (ts *ToolSelector) collectRecentToolNames(messages []provider.Message) map[string]bool {
	names := make(map[string]bool)
	start := len(messages) - ts.lookback
	if start < 0 {
		start = 0
	}
	for _, msg := range messages[start:] {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.Name != "" {
				names[block.Name] = true
			}
		}
	}
	return names
}

var fileKeywords = []string{
	"file", "path", "directory", "folder", "read", "write", "search",
	"find", "grep", "code", "source", ".go", ".py", ".js", ".ts",
	".rs", ".java", ".c", ".cpp", ".h", ".md", ".txt", ".json",
	".yaml", ".toml", ".xml",
}

var platformKeywords = []string{
	"xcode", "swift", "ios", "macos", "apple", "simulator",
	"build", "codesign", "xcrun", "spm", "package.swift",
	"xcodebuild", ".xcodeproj", ".xcworkspace",
}

func containsFileKeywords(text string) bool {
	for _, kw := range fileKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func containsPlatformKeywords(text string) bool {
	for _, kw := range platformKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

var lspKeywords = []string{
	"lsp", "language server", "definition", "references", "hover",
	"diagnostics", "completions", "rename", "code action", "symbol",
	"call hierarchy", "type signature", "go to definition", "find references",
	"compiler error", "compile error",
}

func containsLSPKeywords(text string) bool {
	for _, kw := range lspKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func containsToolNameKeyword(text, toolName string) bool {
	return strings.Contains(text, strings.ToLower(toolName))
}
