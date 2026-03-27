package shell

import (
	"os"
	"path/filepath"
	"strings"
)

// InputClassification represents how the shell interprets user input.
type InputClassification int

const (
	ClassEmpty          InputClassification = iota // No input
	ClassShellCommand                              // Execute directly via shell
	ClassBuiltinCommand                            // Handle in-process (cd, export, exit)
	ClassLLMQuery                                  // Send to the LLM agent
	ClassSlashCommand                              // Delegate to command registry
)

// ClassifiedInput is the result of classifying a user input line.
type ClassifiedInput struct {
	Classification InputClassification
	Raw            string   // Original input
	Command        string   // Parsed command (for shell/builtin/slash)
	Args           []string // Parsed arguments (for builtin/slash)
}

// InputClassifier determines whether input is a shell command or LLM query.
type InputClassifier struct {
	knownExecutables map[string]bool
}

// NewInputClassifier creates a classifier with the given set of known executables.
func NewInputClassifier(knownExecutables map[string]bool) *InputClassifier {
	return &InputClassifier{knownExecutables: knownExecutables}
}

var builtins = map[string]bool{
	"cd":     true,
	"export": true,
	"exit":   true,
	"quit":   true,
}

var questionWords = map[string]bool{
	"what":     true,
	"why":      true,
	"how":      true,
	"explain":  true,
	"describe": true,
	"where":    true,
	"when":     true,
	"which":    true,
	"who":      true,
}

var imperativeVerbs = map[string]bool{
	"fix":      true,
	"refactor": true,
	"add":      true,
	"create":   true,
	"update":   true,
	"find":     true,
	"remove":   true,
	"delete":   true,
	"rename":   true,
	"move":     true,
	"change":   true,
	"implement": true,
	"write":    true,
	"debug":    true,
	"optimize": true,
	"review":   true,
	"check":    true,
	"run":      true,
	"deploy":   true,
	"show":     true,
	"list":     true,
}

// Classify determines the classification of the given input line.
func (c *InputClassifier) Classify(input string) ClassifiedInput {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ClassifiedInput{Classification: ClassEmpty, Raw: input}
	}

	// Force shell prefix: !
	if strings.HasPrefix(trimmed, "!") {
		cmd := strings.TrimSpace(trimmed[1:])
		return ClassifiedInput{
			Classification: ClassShellCommand,
			Raw:            input,
			Command:        cmd,
		}
	}

	// Force LLM prefix: ?
	if strings.HasPrefix(trimmed, "?") {
		query := strings.TrimSpace(trimmed[1:])
		return ClassifiedInput{
			Classification: ClassLLMQuery,
			Raw:            query,
		}
	}

	// Slash command
	if strings.HasPrefix(trimmed, "/") {
		parts := strings.Fields(trimmed[1:])
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}
		return ClassifiedInput{
			Classification: ClassSlashCommand,
			Raw:            input,
			Command:        parts[0],
			Args:           args,
		}
	}

	// Extract the first "word" to check against builtins and executables.
	firstWord := extractExecutable(trimmed)

	// Built-in commands
	if builtins[firstWord] {
		parts := strings.Fields(trimmed)
		var args []string
		if len(parts) > 1 {
			args = parts[1:]
		}
		return ClassifiedInput{
			Classification: ClassBuiltinCommand,
			Raw:            input,
			Command:        parts[0],
			Args:           args,
		}
	}

	// Known executable (direct or after env prefix, or in pipe chain)
	if c.knownExecutables[firstWord] {
		return ClassifiedInput{
			Classification: ClassShellCommand,
			Raw:            input,
			Command:        trimmed,
		}
	}

	// Natural language heuristics
	lowerFirst := strings.ToLower(firstWord)
	if questionWords[lowerFirst] || imperativeVerbs[lowerFirst] {
		return ClassifiedInput{
			Classification: ClassLLMQuery,
			Raw:            input,
		}
	}

	// Default: LLM query
	return ClassifiedInput{
		Classification: ClassLLMQuery,
		Raw:            input,
	}
}

// extractExecutable finds the actual executable name from an input line,
// handling env var prefixes (VAR=val cmd) and pipe chains.
func extractExecutable(input string) string {
	// Handle pipe chains — use the first segment
	if idx := strings.Index(input, "|"); idx > 0 {
		input = strings.TrimSpace(input[:idx])
	}

	fields := strings.Fields(input)
	for _, f := range fields {
		// Skip environment variable assignments (KEY=VALUE)
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
			continue
		}
		return f
	}
	return ""
}

// ScanPATH scans $PATH directories and returns a set of executable names found.
func ScanPATH() map[string]bool {
	result := make(map[string]bool)
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return result
	}

	for _, dir := range filepath.SplitList(pathEnv) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			// Check if executable
			if info.Mode()&0o111 != 0 {
				result[entry.Name()] = true
			}
		}
	}
	return result
}
