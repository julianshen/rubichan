package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ansiEscapePattern matches ANSI escape sequences: CSI (e.g. \x1b[31m),
// OSC with BEL or ST terminators (e.g. \x1b]8;;url\x1b\\), and other
// single-character Esc sequences (e.g. \x1b7).
var ansiEscapePattern = regexp.MustCompile(
	`\x1b\[[0-9;?]*[a-zA-Z~]` + // CSI sequences
		`|\x1b\][\x20-\x7e]*(?:\x07|\x1b\\)` + // OSC sequences (BEL or ST terminated, printable params)
		`|\x1b[^[\]0-9]?`, // Other Esc sequences (single char after ESC)
)

// stripANSI removes ANSI escape sequences from a string to prevent
// terminal injection via untrusted LLM-provided tool names or arguments.
func stripANSI(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

// ApprovalResult represents the user's choice on a tool approval prompt.
type ApprovalResult int

const (
	ApprovalPending ApprovalResult = iota
	ApprovalYes
	ApprovalNo
	ApprovalAlways
	ApprovalDenyAlways
)

// RiskLevel classifies tool risk for visual indication.
type RiskLevel int

const (
	RiskLow    RiskLevel = iota // file read, search
	RiskMedium                  // file write, patch
	RiskHigh                    // shell, process
)

// classifyRisk returns the risk level based on tool name.
func classifyRisk(tool string) RiskLevel {
	t := strings.ToLower(tool)
	switch {
	case strings.Contains(t, "shell") || strings.Contains(t, "bash") || strings.Contains(t, "exec"):
		return RiskHigh
	case strings.Contains(t, "write") || strings.Contains(t, "patch") || strings.Contains(t, "edit"):
		return RiskMedium
	default:
		return RiskLow
	}
}

// isDestructiveCommand checks if tool args contain destructive patterns.
func isDestructiveCommand(args string) bool {
	lower := strings.ToLower(args)
	patterns := []string{
		"rm -rf", "rm -r",
		"git reset --hard",
		"git push --force", "git push -f",
		"drop table", "drop database",
		"truncate table",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// Risk and warning styles use the centralized pink theme.
var (
	riskHighStyle   = styleRiskHigh
	riskMediumStyle = styleRiskMedium
	riskLowStyle    = styleRiskLow
	warningStyle    = styleDestructiveWarning
)

// OptionsForRisk returns the default set of approval options based on risk
// level and whether the command is destructive. Destructive or high-risk
// commands omit "always" to prevent accidental blanket approval.
func OptionsForRisk(tool, args string) []ApprovalResult {
	risk := classifyRisk(tool)
	destructive := isDestructiveCommand(args)

	if destructive {
		// Destructive commands: only yes/no, no blanket approval.
		return []ApprovalResult{ApprovalYes, ApprovalNo}
	}

	switch risk {
	case RiskHigh:
		// High risk: allow yes/no/always but no deny-always (user might
		// want to allow specific shell commands after reviewing).
		return []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways}
	case RiskMedium:
		return []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways}
	default:
		// Low risk: all options available.
		return []ApprovalResult{ApprovalYes, ApprovalNo, ApprovalAlways, ApprovalDenyAlways}
	}
}

// ApprovalPrompt shows an inline approval prompt for a tool call.
// It displays the tool name, arguments, risk level, and only the
// allowed approval options — like Claude Code's permission prompt.
type ApprovalPrompt struct {
	tool    string
	args    string
	options []ApprovalResult
	result  ApprovalResult
	done    bool
	box     lipgloss.Style
}

// NewApprovalPrompt creates a new approval prompt for the given tool and args.
// The width parameter controls the box width. The options parameter specifies
// which approval choices to display. If options is nil, defaults are chosen
// based on risk level.
func NewApprovalPrompt(tool, args string, width int, options []ApprovalResult) *ApprovalPrompt {
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	box := styleApprovalBorder.Width(boxWidth)

	if len(options) == 0 {
		options = OptionsForRisk(tool, args)
	}

	return &ApprovalPrompt{
		tool:    tool,
		args:    args,
		options: options,
		box:     box,
	}
}

// Done returns true if the user has made a decision.
func (a *ApprovalPrompt) Done() bool { return a.done }

// Result returns the user's approval decision.
func (a *ApprovalPrompt) Result() ApprovalResult { return a.result }

// SetResult sets the approval result and marks the prompt as done.
func (a *ApprovalPrompt) SetResult(r ApprovalResult) {
	a.result = r
	a.done = true
}

// hasOption returns true if the given option is in the allowed set.
func (a *ApprovalPrompt) hasOption(opt ApprovalResult) bool {
	for _, o := range a.options {
		if o == opt {
			return true
		}
	}
	return false
}

// HandleKey processes a single keypress for the approval prompt.
// Returns true if the key was handled (approval decision made).
// Only accepts keys for options that are currently displayed.
func (a *ApprovalPrompt) HandleKey(msg tea.KeyMsg) bool {
	var target ApprovalResult
	switch msg.String() {
	case "y", "Y":
		target = ApprovalYes
	case "n", "N":
		target = ApprovalNo
	case "a", "A":
		target = ApprovalAlways
	case "d", "D":
		target = ApprovalDenyAlways
	default:
		return false
	}

	if !a.hasOption(target) {
		return false
	}

	a.SetResult(target)
	return true
}

// toolDisplayName returns a human-friendly display name for a tool.
func toolDisplayName(tool string) string {
	names := map[string]string{
		"shell":      "Bash",
		"bash":       "Bash",
		"exec":       "Execute",
		"file_read":  "Read file",
		"read_file":  "Read file",
		"read":       "Read file",
		"file_write": "Write file",
		"write_file": "Write file",
		"write":      "Write file",
		"edit":       "Edit file",
		"patch":      "Patch file",
		"search":     "Search",
		"glob":       "Glob",
		"grep":       "Grep",
	}
	if name, ok := names[strings.ToLower(tool)]; ok {
		return name
	}
	return tool
}

// optionLabel returns the rendered label for an approval option.
func optionLabel(opt ApprovalResult) string {
	switch opt {
	case ApprovalYes:
		return styleApprovalKey.Render("[Y]") + styleApprovalLabel.Render("es")
	case ApprovalNo:
		return styleApprovalKey.Render("[N]") + styleApprovalLabel.Render("o")
	case ApprovalAlways:
		return styleApprovalKey.Render("[A]") + styleApprovalLabel.Render("lways allow")
	case ApprovalDenyAlways:
		return styleApprovalKey.Render("[D]") + styleApprovalLabel.Render("eny always")
	default:
		return ""
	}
}

// View renders the approval prompt as a bordered box with tool info,
// risk level indicator, destructive warning, and only the allowed options.
func (a *ApprovalPrompt) View() string {
	risk := classifyRisk(a.tool)
	sanitizedTool := stripANSI(a.tool)
	sanitizedArgs := stripANSI(a.args)
	displayName := toolDisplayName(sanitizedTool)

	// Tool name with risk icon.
	var icon string
	switch risk {
	case RiskHigh:
		icon = riskHighStyle.Render("⚠")
	case RiskMedium:
		icon = riskMediumStyle.Render("●")
	default:
		icon = riskLowStyle.Render("●")
	}

	header := fmt.Sprintf("  %s %s", icon, styleApprovalKey.Render(displayName))

	// Args on next line, indented.
	detail := styleSectionLabel.Render("    " + sanitizedArgs)

	body := header + "\n" + detail

	// Destructive warning.
	if isDestructiveCommand(sanitizedArgs) {
		body += "\n" + warningStyle.Render("  ⚠ Destructive command detected")
	}

	// Render only the allowed options.
	var optParts []string
	for _, opt := range a.options {
		if label := optionLabel(opt); label != "" {
			optParts = append(optParts, label)
		}
	}
	if len(optParts) > 0 {
		body += "\n\n  " + strings.Join(optParts, "  ")
	}

	return a.box.Render(body) + "\n"
}
