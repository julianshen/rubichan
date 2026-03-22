package tui

// ToolType classifies tool invocations for visual differentiation.
// Uses ASCII-safe icon characters instead of spec emojis for reliable
// terminal rendering across all emulators.
type ToolType int

const (
	ToolTypeDefault  ToolType = iota
	ToolTypeShell             // shell
	ToolTypeFile              // file
	ToolTypeSearch            // search
	ToolTypeProcess           // process
	ToolTypeSubagent          // task (subagent dispatch)
)

// ClassifyTool returns the ToolType for a given tool name.
// Names must match the canonical Tool.Name() values from internal/tools/.
func ClassifyTool(name string) ToolType {
	switch name {
	case "shell":
		return ToolTypeShell
	case "file":
		return ToolTypeFile
	case "search":
		return ToolTypeSearch
	case "process":
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
