package tui

import "strings"

// ToolType classifies tool invocations for visual differentiation.
type ToolType int

const (
	ToolTypeDefault  ToolType = iota
	ToolTypeShell             // shell
	ToolTypeFile              // file
	ToolTypeSearch            // search
	ToolTypeProcess           // process
	ToolTypeSubagent          // task (subagent dispatch)
	ToolTypeGit               // git_*
	ToolTypeNet               // http_*, browser_*
	ToolTypeLSP               // lsp_*
	ToolTypePlatform          // xcode_*
	ToolTypeMCP               // mcp-* or mcp_*
)

// ClassifyTool returns the ToolType for a given tool name.
func ClassifyTool(name string) ToolType {
	switch {
	case name == "shell":
		return ToolTypeShell
	case name == "file":
		return ToolTypeFile
	case name == "search":
		return ToolTypeSearch
	case name == "process":
		return ToolTypeProcess
	case name == "task", name == "list_tasks":
		return ToolTypeSubagent
	case strings.HasPrefix(name, "git_"):
		return ToolTypeGit
	case strings.HasPrefix(name, "http_"), strings.HasPrefix(name, "browser_"):
		return ToolTypeNet
	case strings.HasPrefix(name, "lsp_"):
		return ToolTypeLSP
	case strings.HasPrefix(name, "xcode_"), strings.HasPrefix(name, "swift_"),
		strings.HasPrefix(name, "sim_"), strings.HasPrefix(name, "codesign_"):
		return ToolTypePlatform
	case strings.HasPrefix(name, "mcp-"), strings.HasPrefix(name, "mcp_"):
		return ToolTypeMCP
	default:
		return ToolTypeDefault
	}
}

// Icon returns a short prefix icon for display in tool result headers.
func (tt ToolType) Icon() string {
	switch tt {
	case ToolTypeShell:
		return "❯ "
	case ToolTypeFile:
		return "◇ "
	case ToolTypeSearch:
		return "⊘ "
	case ToolTypeProcess:
		return "⊕ "
	case ToolTypeSubagent:
		return "◈ "
	case ToolTypeGit:
		return "⎇ "
	case ToolTypeNet:
		return "⇄ "
	case ToolTypeLSP:
		return "◉ "
	case ToolTypePlatform:
		return "⌘ "
	case ToolTypeMCP:
		return "⬡ "
	default:
		return "• "
	}
}
