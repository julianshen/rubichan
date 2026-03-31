package shell

import (
	"strings"
)

// PromptRenderer generates PS1-style shell prompts.
type PromptRenderer struct {
	homeDir    string
	statusLine *StatusLine
}

// NewPromptRenderer creates a prompt renderer that shortens paths under homeDir.
func NewPromptRenderer(homeDir string) *PromptRenderer {
	return &PromptRenderer{homeDir: homeDir}
}

// Render generates a prompt string with the given working directory and git branch.
// If branch is empty, the git branch indicator is omitted.
func (r *PromptRenderer) Render(workDir string, gitBranch string) string {
	var b strings.Builder

	// Render status line if available
	if r.statusLine != nil {
		sl := r.statusLine.Render()
		if sl != "" {
			b.WriteString(sl)
			b.WriteString("\n")
		}
	}

	// Shorten home directory to ~
	display := workDir
	if r.homeDir != "" && strings.HasPrefix(workDir, r.homeDir) {
		rest := workDir[len(r.homeDir):]
		if rest == "" {
			display = "~"
		} else if rest[0] == '/' {
			display = "~" + rest
		}
	}

	// When status line is active, use a shorter prompt (status has the detail)
	if r.statusLine != nil {
		b.WriteString("ai$ ")
		return b.String()
	}

	b.WriteString(display)

	if gitBranch != "" {
		b.WriteString(" (")
		b.WriteString(gitBranch)
		b.WriteString(")")
	}

	b.WriteString(" ai$ ")
	return b.String()
}
