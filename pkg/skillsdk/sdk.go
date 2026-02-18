// Package skillsdk provides the public SDK for Go skill plugin authors.
//
// This is the stable interface contract that external projects import to build
// skills for Rubichan. It defines the types and interfaces that the Go plugin
// backend expects when loading .so files via NewSkill() skillsdk.SkillPlugin.
//
// This package is intentionally self-contained and MUST NOT import anything
// from internal/. All types here mirror the Starlark SDK built-ins so that
// Go plugin authors have the same capabilities as Starlark skill authors.
package skillsdk

// Manifest describes a skill's metadata. It is returned by SkillPlugin.Manifest()
// and used by the runtime to register and display the skill.
type Manifest struct {
	// Name is the unique identifier for the skill (e.g., "my-linter").
	Name string

	// Version is the semantic version of the skill (e.g., "1.0.0").
	Version string

	// Description is a short human-readable summary of what the skill does.
	Description string

	// Author is the name or handle of the skill author.
	Author string

	// License is the SPDX license identifier (e.g., "MIT", "Apache-2.0").
	License string
}

// SkillPlugin is the interface that Go plugin .so files must implement.
// The runtime discovers plugins by calling the exported NewSkill() function,
// which must return a value satisfying this interface.
type SkillPlugin interface {
	// Manifest returns the skill's metadata.
	Manifest() Manifest

	// Activate is called when the skill is being loaded into the runtime.
	// The Context provides access to all SDK operations (file I/O, shell, git, etc.).
	Activate(ctx Context) error

	// Deactivate is called when the skill is being unloaded from the runtime.
	Deactivate(ctx Context) error
}

// Context provides SDK operations to skill plugins. Each method corresponds
// to a built-in available in the Starlark SDK, giving Go plugin authors the
// same capabilities.
type Context interface {
	// ReadFile reads the contents of a file at the given path.
	ReadFile(path string) (string, error)

	// WriteFile writes content to a file at the given path, creating or
	// overwriting as needed.
	WriteFile(path, content string) error

	// ListDir lists the entries in a directory.
	ListDir(path string) ([]FileInfo, error)

	// SearchFiles searches for files matching a glob pattern.
	SearchFiles(pattern string) ([]string, error)

	// Exec runs an external command and returns the result.
	Exec(command string, args ...string) (ExecResult, error)

	// Complete sends a prompt to the LLM and returns the response text.
	Complete(prompt string) (string, error)

	// Fetch retrieves the contents of a URL as a string.
	Fetch(url string) (string, error)

	// GitDiff runs git diff with the given arguments and returns the output.
	GitDiff(args ...string) (string, error)

	// GitLog runs git log with the given arguments and returns parsed commits.
	GitLog(args ...string) ([]GitCommit, error)

	// GitStatus returns the current git working tree status.
	GitStatus() ([]GitFileStatus, error)

	// GetEnv returns the value of an environment variable.
	GetEnv(key string) string

	// ProjectRoot returns the absolute path to the project root directory.
	ProjectRoot() string

	// InvokeSkill calls another skill by name, passing input data and
	// receiving output data.
	InvokeSkill(name string, input map[string]any) (map[string]any, error)
}

// ExecResult holds the output of a command execution.
type ExecResult struct {
	// Stdout is the standard output of the command.
	Stdout string

	// Stderr is the standard error output of the command.
	Stderr string

	// ExitCode is the process exit code (0 typically means success).
	ExitCode int
}

// FileInfo describes a file or directory entry.
type FileInfo struct {
	// Name is the base name of the file or directory.
	Name string

	// IsDir is true if the entry is a directory.
	IsDir bool

	// Size is the file size in bytes (0 for directories).
	Size int64
}

// GitCommit represents a single git commit.
type GitCommit struct {
	// Hash is the full commit SHA.
	Hash string

	// Author is the commit author name.
	Author string

	// Message is the commit message (first line).
	Message string

	// Date is the commit date as an ISO 8601 string.
	Date string
}

// GitFileStatus represents the status of a single file in the git working tree.
type GitFileStatus struct {
	// Path is the file path relative to the repository root.
	Path string

	// Status is the git status code (e.g., "M" for modified, "A" for added).
	Status string
}
