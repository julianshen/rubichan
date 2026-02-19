package integrations

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GitCommit represents a git log entry.
type GitCommit struct {
	Hash    string
	Author  string
	Message string
}

// GitFileStatus represents a git status entry.
type GitFileStatus struct {
	Path   string
	Status string
}

// GitRunner executes git commands in a project directory.
type GitRunner struct {
	workDir string
}

// NewGitRunner creates a GitRunner for the given directory.
func NewGitRunner(workDir string) *GitRunner {
	return &GitRunner{workDir: workDir}
}

// Diff runs git diff with optional arguments.
func (g *GitRunner) Diff(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"diff"}, args...)
	return g.run(ctx, cmdArgs...)
}

// Log runs git log and parses the output into structured commits.
// Uses ASCII record separator (\x1e) as delimiter to avoid conflicts
// with pipe characters that may appear in commit subjects or author names.
func (g *GitRunner) Log(ctx context.Context, args ...string) ([]GitCommit, error) {
	const sep = "\x1e"
	cmdArgs := append([]string{"log", "--format=%H%x1e%an%x1e%s"}, args...)
	out, err := g.run(ctx, cmdArgs...)
	if err != nil {
		return nil, err
	}

	var commits []GitCommit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, sep, 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, GitCommit{
			Hash:    parts[0],
			Author:  parts[1],
			Message: parts[2],
		})
	}

	return commits, nil
}

// Status returns the git working tree status.
func (g *GitRunner) Status(ctx context.Context) ([]GitFileStatus, error) {
	out, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	var statuses []GitFileStatus
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])

		// Handle renames: "R  old.go -> new.go" — use the new path.
		if strings.HasPrefix(status, "R") {
			if idx := strings.Index(path, " -> "); idx >= 0 {
				path = path[idx+4:]
			}
		}

		statuses = append(statuses, GitFileStatus{
			Path:   path,
			Status: status,
		})
	}

	return statuses, nil
}

func (g *GitRunner) run(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("git: no subcommand provided")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.workDir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", args[0], string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}
