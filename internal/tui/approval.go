package tui

import (
	"fmt"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/julianshen/rubichan/internal/persona"
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

// ApprovalPrompt shows an inline approval prompt for a tool call.
type ApprovalPrompt struct {
	tool   string
	args   string
	result ApprovalResult
	done   bool
	box    lipgloss.Style
}

// NewApprovalPrompt creates a new approval prompt for the given tool and args.
// The width parameter controls the box width.
func NewApprovalPrompt(tool, args string, width int) *ApprovalPrompt {
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	box := styleApprovalBorder.Width(boxWidth)

	return &ApprovalPrompt{
		tool: tool,
		args: args,
		box:  box,
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

// HandleKey processes a single keypress for the approval prompt.
// Returns true if the key was handled (approval decision made).
func (a *ApprovalPrompt) HandleKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "y", "Y":
		a.SetResult(ApprovalYes)
		return true
	case "n", "N":
		a.SetResult(ApprovalNo)
		return true
	case "a", "A":
		a.SetResult(ApprovalAlways)
		return true
	case "d", "D":
		a.SetResult(ApprovalDenyAlways)
		return true
	}
	return false
}

// View renders the approval prompt as a bordered box with tool info,
// risk level indicator, destructive warning, and options.
func (a *ApprovalPrompt) View() string {
	risk := classifyRisk(a.tool)
	var riskLabel string
	switch risk {
	case RiskHigh:
		riskLabel = riskHighStyle.Render("⚠ HIGH RISK")
	case RiskMedium:
		riskLabel = riskMediumStyle.Render("● MEDIUM")
	default:
		riskLabel = riskLowStyle.Render("○ LOW")
	}

	sanitizedTool := stripANSI(a.tool)
	sanitizedArgs := stripANSI(a.args)
	header := fmt.Sprintf("%s  %s", riskLabel, persona.ApprovalAsk(sanitizedTool))
	detail := styleSectionLabel.Render("  args: ") + sanitizedArgs

	var body string
	body = header + "\n" + detail
	if isDestructiveCommand(sanitizedArgs) {
		body += "\n" + warningStyle.Render("  ⚠ Destructive command detected")
	}

	// Render options with highlighted keys for clarity.
	options := fmt.Sprintf("\n  %s%s  %s%s  %s%s  %s%s",
		styleApprovalKey.Render("[y]"), styleApprovalLabel.Render("es"),
		styleApprovalKey.Render("[n]"), styleApprovalLabel.Render("o"),
		styleApprovalKey.Render("[a]"), styleApprovalLabel.Render("lways"),
		styleApprovalKey.Render("[d]"), styleApprovalLabel.Render("eny always"),
	)
	body += options

	return a.box.Render(body) + "\n"
}
