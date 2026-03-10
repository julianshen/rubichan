package tools

import "strings"

// ToolCategory classifies tools into groups for progressive disclosure.
type ToolCategory int

const (
	CategoryCore       ToolCategory = iota // Always sent: shell, file
	CategoryFileSystem                     // File search: search
	CategoryPlatform                       // Platform-specific: xcode_*
	CategoryMCP                            // MCP-provided tools
	CategorySkill                          // Skill-provided tools
)

// Categorize assigns a ToolCategory to a tool based on its name.
func Categorize(name string) ToolCategory {
	switch {
	case name == "shell" || name == "file" || name == "process":
		return CategoryCore
	case name == "search" || name == "db_query" || strings.HasPrefix(name, "git_"):
		return CategoryFileSystem
	case strings.HasPrefix(name, "http_") || strings.HasPrefix(name, "browser_"):
		return CategoryMCP
	case strings.HasPrefix(name, "xcode_"):
		return CategoryPlatform
	case strings.HasPrefix(name, "mcp-"):
		return CategoryMCP
	case strings.HasPrefix(name, "mcp_"):
		return CategoryMCP
	default:
		return CategorySkill
	}
}
