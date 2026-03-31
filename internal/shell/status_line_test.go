package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- 5A: StatusLine Core ---

func TestStatusLineRender(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)
	sl.Update("cwd", "~/project")
	sl.Update("branch", "main")
	sl.Update("exitcode", "0")

	rendered := sl.Render()
	assert.Contains(t, rendered, "~/project")
	assert.Contains(t, rendered, "main")
	assert.Contains(t, rendered, "✓")
}

func TestStatusLineUpdate(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)
	sl.Update("branch", "main")
	assert.Contains(t, sl.Render(), "main")

	sl.Update("branch", "feature")
	rendered := sl.Render()
	assert.Contains(t, rendered, "feature")
	assert.NotContains(t, rendered, "main")
}

func TestStatusLineWidth(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(30)
	sl.Update("cwd", "/very/long/path/that/exceeds/width/limit/deeply/nested")

	rendered := sl.Render()
	// Should be truncated to fit width (accounting for ANSI codes, check visible content)
	visibleLen := visibleLength(rendered)
	assert.LessOrEqual(t, visibleLen, 30)
}

func TestStatusLineDisabled(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)
	sl.enabled = false

	sl.Update("cwd", "~/project")
	assert.Equal(t, "", sl.Render())
}

func TestStatusLineNilSafe(t *testing.T) {
	t.Parallel()

	// When statusLine is nil on ShellHost, no panic
	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{},
		Stdin:       strings.NewReader("\n"),
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "" },
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)
}

// --- 5B: Segment Formatting ---

func TestStatusSegmentCWD(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)
	sl.homeDir = "/home/user"

	sl.UpdateCWD("/home/user/project")
	assert.Contains(t, sl.Render(), "~/project")

	sl.UpdateCWD("/opt/other")
	assert.Contains(t, sl.Render(), "/opt/other")
}

func TestStatusSegmentBranch(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)

	sl.Update("branch", "main")
	assert.Contains(t, sl.Render(), "main")

	// Empty branch — omitted
	sl.Update("branch", "")
	rendered := sl.Render()
	assert.NotContains(t, rendered, "()")
}

func TestStatusSegmentExitCode(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)

	sl.UpdateExitCode(0)
	assert.Contains(t, sl.Render(), "✓")

	sl.UpdateExitCode(1)
	rendered := sl.Render()
	assert.Contains(t, rendered, "✗")
	assert.Contains(t, rendered, "1")
}

func TestStatusSegmentModel(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)

	sl.UpdateModel("claude-sonnet-4-5")
	assert.Contains(t, sl.Render(), "sonnet")

	sl.UpdateModel("claude-opus-4-6")
	assert.Contains(t, sl.Render(), "opus")

	sl.UpdateModel("gpt-4o")
	assert.Contains(t, sl.Render(), "gpt-4o")
}

// --- 5C: Prompt Integration ---

func TestPromptRendererWithStatusLine(t *testing.T) {
	t.Parallel()

	sl := NewStatusLine(80)
	sl.homeDir = "/home/user"
	sl.UpdateExitCode(0)

	pr := NewPromptRenderer("/home/user")
	pr.statusLine = sl

	prompt := pr.Render("/home/user/project", "main")
	// Should have status line before the ai$ prompt
	assert.Contains(t, prompt, "~/project")
	assert.Contains(t, prompt, "ai$ ")
	// Status line should come before prompt
	aiIdx := strings.Index(prompt, "ai$ ")
	assert.Greater(t, aiIdx, 0)
}

func TestPromptRendererWithoutStatusLine(t *testing.T) {
	t.Parallel()

	pr := NewPromptRenderer("/home/user")
	prompt := pr.Render("/home/user/project", "main")
	// Should be same as original format
	assert.Equal(t, "~/project (main) ai$ ", prompt)
}

func TestShellHostStatusLineIntegration(t *testing.T) {
	t.Parallel()

	var commands []string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		commands = append(commands, cmd)
		if cmd == "ls" {
			return "file.go", "", 0, nil
		}
		return "", "error", 1, nil
	}

	stdout := &bytes.Buffer{}

	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{"ls": true, "false": true},
		ShellExec:   exec,
		Stdin:       strings.NewReader("ls\nfalse\n"),
		Stdout:      stdout,
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "main" },
		StatusLine:  true,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	output := stdout.String()
	// Status line should be rendered (contains branch and exit code info)
	assert.Contains(t, output, "main")
	// After failure, should show failure indicator (strip ANSI to check)
	stripped := stripANSI(output)
	assert.Contains(t, stripped, "✗")
}

// visibleLength returns the length of a string without ANSI escape codes.
func visibleLength(s string) int {
	inEscape := false
	length := 0
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length++
	}
	return length
}
