package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectInfo holds detected information about a project.
type ProjectInfo struct {
	Languages []string
	BuildCmds []string
	TestCmds  []string
	LintCmds  []string
}

// initCommand implements the /init slash command that generates AGENTS.md or CLAUDE.md.
type initCommand struct {
	workDir string
}

// NewInitCommand creates a command that generates an AGENTS.md or CLAUDE.md file
// for the current project based on detected project structure.
func NewInitCommand(workDir string) SlashCommand {
	return &initCommand{workDir: workDir}
}

func (c *initCommand) Name() string        { return "init" }
func (c *initCommand) Description() string { return "Generate AGENTS.md or CLAUDE.md for the project" }

func (c *initCommand) Arguments() []ArgumentDef {
	return []ArgumentDef{
		{
			Name:        "format",
			Description: "Format to generate: agents (default) or claude",
			Required:    false,
			Static:      []string{"agents", "claude", "agent"},
		},
	}
}

func (c *initCommand) Complete(_ context.Context, _ []string) []Candidate {
	return nil
}

func (c *initCommand) Execute(_ context.Context, args []string) (Result, error) {
	format := "agents"
	if len(args) > 0 {
		format = strings.ToLower(args[0])
	}

	var filename string
	switch format {
	case "agents":
		filename = "AGENTS.md"
	case "claude":
		filename = "CLAUDE.md"
	case "agent":
		filename = "AGENT.md"
	default:
		return Result{}, fmt.Errorf("unknown format %q: use 'agents' or 'claude'", format)
	}

	target := filepath.Join(c.workDir, filename)
	if _, err := os.Stat(target); err == nil {
		return Result{}, fmt.Errorf("%s already exists; remove it first or edit manually", filename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("checking for existing %s: %w", filename, err)
	}

	info := DetectProjectInfo(c.workDir)
	content := GenerateContent(filename, info)

	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing %s: %w", filename, err)
	}

	return Result{Output: fmt.Sprintf("Generated %s in project root.", filename)}, nil
}

// DetectProjectInfo scans the working directory for project markers.
func DetectProjectInfo(dir string) ProjectInfo {
	var info ProjectInfo

	// Go
	if fileExists(filepath.Join(dir, "go.mod")) {
		info.Languages = append(info.Languages, "Go")
		info.BuildCmds = append(info.BuildCmds, "go build ./...")
		info.TestCmds = append(info.TestCmds, "go test ./...")
		info.LintCmds = append(info.LintCmds, "golangci-lint run ./...")
	}

	// Node/JS/TS
	if fileExists(filepath.Join(dir, "package.json")) {
		info.Languages = append(info.Languages, "JavaScript/TypeScript")
		pm := detectNodePM(dir)
		scripts := readPackageScripts(filepath.Join(dir, "package.json"))
		if _, ok := scripts["build"]; ok {
			info.BuildCmds = append(info.BuildCmds, pm+" run build")
		}
		if _, ok := scripts["test"]; ok {
			info.TestCmds = append(info.TestCmds, pm+" test")
		}
		if _, ok := scripts["lint"]; ok {
			info.LintCmds = append(info.LintCmds, pm+" run lint")
		}
	}

	// Python
	if fileExists(filepath.Join(dir, "pyproject.toml")) || fileExists(filepath.Join(dir, "setup.py")) || fileExists(filepath.Join(dir, "requirements.txt")) {
		info.Languages = append(info.Languages, "Python")
		info.TestCmds = append(info.TestCmds, "pytest")
		info.LintCmds = append(info.LintCmds, "ruff check .")
	}

	// Rust
	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		info.Languages = append(info.Languages, "Rust")
		info.BuildCmds = append(info.BuildCmds, "cargo build")
		info.TestCmds = append(info.TestCmds, "cargo test")
		info.LintCmds = append(info.LintCmds, "cargo clippy")
	}

	return info
}

// GenerateContent builds the markdown content for the given filename.
func GenerateContent(filename string, info ProjectInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", filename))

	// Project overview
	b.WriteString("## Project Overview\n\n")
	if len(info.Languages) > 0 {
		b.WriteString(fmt.Sprintf("This is a %s project.\n\n", strings.Join(info.Languages, " / ")))
	} else {
		b.WriteString("<!-- Describe what this project does and its purpose. -->\n\n")
	}

	// Build commands
	if len(info.BuildCmds) > 0 || len(info.TestCmds) > 0 || len(info.LintCmds) > 0 {
		b.WriteString("## Build & Test Commands\n\n```bash\n")
		for _, cmd := range info.BuildCmds {
			b.WriteString(cmd + "\n")
		}
		for _, cmd := range info.TestCmds {
			b.WriteString(cmd + "\n")
		}
		for _, cmd := range info.LintCmds {
			b.WriteString(cmd + "\n")
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("## Build & Test Commands\n\n")
		b.WriteString("<!-- Add your build, test, and lint commands here. -->\n\n")
		b.WriteString("```bash\n# build\n# test\n# lint\n```\n\n")
	}

	// Code style
	b.WriteString("## Code Style & Conventions\n\n")
	b.WriteString("<!-- Describe coding conventions, formatting rules, and patterns used in this project. -->\n\n")

	// Architecture
	b.WriteString("## Architecture\n\n")
	b.WriteString("<!-- Describe the high-level architecture, key directories, and how components interact. -->\n\n")

	// Development workflow
	b.WriteString("## Development Workflow\n\n")
	b.WriteString("<!-- Describe the development workflow: branching strategy, PR process, CI/CD, etc. -->\n\n")

	return b.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func detectNodePM(dir string) string {
	if fileExists(filepath.Join(dir, "bun.lockb")) || fileExists(filepath.Join(dir, "bun.lock")) {
		return "bun"
	}
	if fileExists(filepath.Join(dir, "pnpm-lock.yaml")) {
		return "pnpm"
	}
	if fileExists(filepath.Join(dir, "yarn.lock")) {
		return "yarn"
	}
	return "npm"
}

func readPackageScripts(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", filepath.Base(path), err)
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse %s: %v\n", filepath.Base(path), err)
		return nil
	}
	return pkg.Scripts
}
