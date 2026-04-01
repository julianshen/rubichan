package tools

import (
	"sort"
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
			if containsFileKeywords(recentText) || containsExplorationKeywords(recentText) || recentToolNames[tool.Name] {
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

		case cat == CategoryGit:
			if containsExplorationKeywords(recentText) || recentToolNames[tool.Name] || containsToolNameKeyword(recentText, tool.Name) {
				selected = append(selected, tool)
				nonCoreMatched = true
			}

		case cat == CategoryNet || cat == CategoryMCP || cat == CategorySkill:
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

	// Sort deterministically by name for prompt cache stability.
	sortToolDefs(selected)
	return selected
}

// isAlwaysIncluded reports whether a tool should always be exposed regardless
// of context filtering or budget trimming.
func isAlwaysIncluded(name string) bool {
	return Categorize(name) == CategoryCore || name == "tool_search"
}

func selectSafeBaseline(allTools []provider.ToolDef) []provider.ToolDef {
	var selected []provider.ToolDef
	for _, tool := range allTools {
		if isAlwaysIncluded(tool.Name) {
			selected = append(selected, tool)
		}
	}
	// Sort deterministically by name for prompt cache stability.
	sortToolDefs(selected)
	return selected
}

// sortToolDefs sorts tool definitions alphabetically by name for deterministic
// ordering, which improves prompt cache hit rates across turns.
func sortToolDefs(defs []provider.ToolDef) {
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
}

// ApplyMaxToolCount trims tools to fit within maxCount, always preserving core
// tools (CategoryCore) and tool_search. Non-core tools are trimmed from the end
// of the slice. Returns tools unchanged when maxCount <= 0 or len(tools) <= maxCount.
func ApplyMaxToolCount(tools []provider.ToolDef, maxCount int) []provider.ToolDef {
	if maxCount <= 0 || len(tools) <= maxCount {
		return tools
	}

	var core, nonCore []provider.ToolDef
	for _, t := range tools {
		if isAlwaysIncluded(t.Name) {
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

// explorationKeywords indicate the user wants to understand or explore the
// project. These activate both FileSystem (search) and Git tools so the LLM
// can proactively investigate the codebase instead of responding with text only.
var explorationKeywords = []string{
	"analyze", "analyse", "review", "brief", "overview", "explain",
	"describe", "summarize", "summary", "understand", "explore",
	"structure", "architecture", "codebase", "project", "feature",
	"features", "module", "modules", "component", "components",
	"how does", "how do", "what does", "what do", "what is",
	"walk me through", "tell me about", "show me",
	"audit", "inspect", "examine", "assessment",
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

func containsExplorationKeywords(text string) bool {
	for _, kw := range explorationKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
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
