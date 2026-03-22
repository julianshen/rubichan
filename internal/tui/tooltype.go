package tui

// ToolType classifies tool invocations for visual differentiation.
// Uses ASCII-safe icon characters instead of spec emojis for reliable
// terminal rendering across all emulators.
type ToolType int

const (
	ToolTypeDefault  ToolType = iota
	ToolTypeShell             // shell, bash, exec
	ToolTypeFile              // file_read, file_write, patch, edit
	ToolTypeSearch            // grep, code_search, glob, find
	ToolTypeProcess           // process, spawn
	ToolTypeSubagent          // task (subagent dispatch)
)

// ClassifyTool returns the ToolType for a given tool name.
func ClassifyTool(name string) ToolType {
	switch name {
	case "shell", "bash", "exec":
		return ToolTypeShell
	case "file_read", "file_write", "patch", "edit", "write":
		return ToolTypeFile
	case "grep", "code_search", "glob", "find":
		return ToolTypeSearch
	case "process", "spawn":
		return ToolTypeProcess
	case "task":
		return ToolTypeSubagent
	default:
		return ToolTypeDefault
	}
}

// Icon returns a short prefix icon for display in tool result headers.
// Uses ASCII-safe characters for terminal compatibility.
func (tt ToolType) Icon() string {
	switch tt {
	case ToolTypeShell:
		return "$ "
	case ToolTypeFile:
		return "~ "
	case ToolTypeSearch:
		return "? "
	case ToolTypeProcess:
		return "* "
	case ToolTypeSubagent:
		return "> "
	default:
		return ""
	}
}
