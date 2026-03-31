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

	if r.statusLine != nil {
		r.statusLine.UpdateCWD(workDir)
		if gitBranch != "" {
			r.statusLine.Update(SegmentBranch, gitBranch)
		}
		sl := r.statusLine.Render()
		if sl != "" {
			b.WriteString(sl)
			b.WriteString("\n")
		}
		b.WriteString("ai$ ")
		return b.String()
	}

	b.WriteString(shortenHome(workDir, r.homeDir))

	if gitBranch != "" {
		b.WriteString(" (")
		b.WriteString(gitBranch)
		b.WriteString(")")
	}

	b.WriteString(" ai$ ")
	return b.String()
}
