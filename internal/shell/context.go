package shell

import (
	"fmt"
	"strings"
)

// ContextTracker manages the sliding window of last shell command + output
// for injection into LLM context.
type ContextTracker struct {
	command       string
	output        string
	exitCode      int
	hasRecord     bool
	maxOutputSize int // Maximum output size in bytes before truncation
}

// NewContextTracker creates a context tracker with the given maximum output size.
func NewContextTracker(maxOutputSize int) *ContextTracker {
	if maxOutputSize < 0 {
		maxOutputSize = 0
	}
	return &ContextTracker{maxOutputSize: maxOutputSize}
}

// Record stores a shell command, its output, and exit code.
func (ct *ContextTracker) Record(command string, output string, exitCode int) {
	ct.command = command
	ct.exitCode = exitCode
	ct.hasRecord = true
	ct.output = truncateWithNotice(output, ct.maxOutputSize)
}

// LastCommand returns the last recorded command.
func (ct *ContextTracker) LastCommand() string {
	return ct.command
}

// LastOutput returns the last recorded output (possibly truncated).
func (ct *ContextTracker) LastOutput() string {
	return ct.output
}

// LastExitCode returns the last recorded exit code.
func (ct *ContextTracker) LastExitCode() int {
	return ct.exitCode
}

// ContextMessage returns a formatted context block for LLM injection.
// Returns empty string if no command has been recorded.
func (ct *ContextTracker) ContextMessage() string {
	if !ct.hasRecord {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "The user just ran `%s` (exit code %d)", ct.command, ct.exitCode)
	if ct.output != "" {
		fmt.Fprintf(&b, " with output:\n```\n%s\n```", ct.output)
	}
	return b.String()
}

// Clear resets the context tracker.
func (ct *ContextTracker) Clear() {
	ct.command = ""
	ct.output = ""
	ct.exitCode = 0
	ct.hasRecord = false
}
