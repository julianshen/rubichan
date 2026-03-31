package shell

import (
	"os"
	"path/filepath"
	"strings"
)

// Completion is a single completion candidate.
type Completion struct {
	Text        string // The completion text to insert
	Display     string // What to show in the menu (may include description)
	Description string // Optional short description
}

// Completer provides tab-completion candidates for shell input.
type Completer struct {
	executables   map[string]bool
	workDir       *string // pointer for live tracking
	slashCommands func() []string
	gitBranches   func(dir string) []string
}

// NewCompleter creates a completer.
func NewCompleter(
	executables map[string]bool,
	workDir *string,
	slashCommands func() []string,
	gitBranches func(dir string) []string,
) *Completer {
	return &Completer{
		executables:   executables,
		workDir:       workDir,
		slashCommands: slashCommands,
		gitBranches:   gitBranches,
	}
}

// gitBranchSubcommands are git subcommands that take a branch name argument.
var gitBranchSubcommands = map[string]bool{
	"checkout": true,
	"switch":   true,
	"branch":   true,
	"merge":    true,
	"rebase":   true,
}

// Complete returns completion candidates for the given input and cursor position.
func (c *Completer) Complete(input string, pos int) []Completion {
	// Work with text up to cursor
	text := input
	if pos < len(input) {
		text = input[:pos]
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	// Slash command completion
	if strings.HasPrefix(trimmed, "/") {
		return c.completeSlashCommand(trimmed[1:])
	}

	fields := strings.Fields(trimmed)

	// Git branch completion
	if len(fields) >= 3 && fields[0] == "git" && gitBranchSubcommands[fields[1]] {
		prefix := fields[len(fields)-1]
		// Only if the cursor is after the subcommand
		return c.completeGitBranch(prefix)
	}

	// First word — complete commands (executables + builtins)
	if len(fields) == 1 && !strings.HasSuffix(text, " ") {
		return c.completeCommand(fields[0])
	}

	// Subsequent words — complete file paths
	if len(fields) >= 1 {
		prefix := ""
		if strings.HasSuffix(text, " ") {
			// After a space — complete all files
			prefix = ""
		} else if len(fields) >= 2 {
			prefix = fields[len(fields)-1]
		}

		dirsOnly := len(fields) >= 1 && fields[0] == "cd"
		return c.completeFilePath(prefix, dirsOnly)
	}

	return nil
}

func (c *Completer) completeCommand(prefix string) []Completion {
	var results []Completion
	lower := strings.ToLower(prefix)

	// Executables
	for name := range c.executables {
		if strings.HasPrefix(strings.ToLower(name), lower) {
			results = append(results, Completion{Text: name})
		}
	}

	// Builtins
	for name := range builtins {
		if strings.HasPrefix(name, lower) {
			results = append(results, Completion{Text: name})
		}
	}

	return results
}

func (c *Completer) completeSlashCommand(prefix string) []Completion {
	if c.slashCommands == nil {
		return nil
	}

	var results []Completion
	lower := strings.ToLower(prefix)
	for _, name := range c.slashCommands() {
		if strings.HasPrefix(strings.ToLower(name), lower) {
			results = append(results, Completion{Text: name})
		}
	}
	return results
}

func (c *Completer) completeGitBranch(prefix string) []Completion {
	if c.gitBranches == nil || c.workDir == nil {
		return nil
	}

	var results []Completion
	lower := strings.ToLower(prefix)
	for _, branch := range c.gitBranches(*c.workDir) {
		if strings.HasPrefix(strings.ToLower(branch), lower) {
			results = append(results, Completion{Text: branch})
		}
	}
	return results
}

func (c *Completer) completeFilePath(prefix string, dirsOnly bool) []Completion {
	if c.workDir == nil {
		return nil
	}

	// Determine the directory to list and the prefix to filter
	dir := *c.workDir
	filePrefix := prefix

	if strings.Contains(prefix, "/") {
		parts := strings.SplitN(prefix, "/", -1)
		subdir := strings.Join(parts[:len(parts)-1], "/")
		filePrefix = parts[len(parts)-1]

		if filepath.IsAbs(subdir) {
			dir = subdir
		} else {
			dir = filepath.Join(*c.workDir, subdir)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var results []Completion
	lower := strings.ToLower(filePrefix)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && filePrefix == "" {
			continue // skip hidden files unless prefix starts with .
		}

		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}

		if dirsOnly && !entry.IsDir() {
			continue
		}

		display := name
		if entry.IsDir() {
			display += "/"
		}

		results = append(results, Completion{Text: display})
	}

	return results
}
