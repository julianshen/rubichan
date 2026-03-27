package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify_ForceShellPrefix(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	result := c.Classify("!docker compose up")

	assert.Equal(t, ClassShellCommand, result.Classification)
	assert.Equal(t, "docker compose up", result.Command)
	assert.Equal(t, "!docker compose up", result.Raw)
}

func TestClassify_ForceLLMPrefix(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	result := c.Classify("?what does this Makefile do")

	assert.Equal(t, ClassLLMQuery, result.Classification)
	assert.Equal(t, "what does this Makefile do", result.Raw)
}

func TestClassify_SlashCommand(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	result := c.Classify("/model claude-sonnet-4-5")

	assert.Equal(t, ClassSlashCommand, result.Classification)
	assert.Equal(t, "model", result.Command)
	assert.Equal(t, []string{"claude-sonnet-4-5"}, result.Args)
}

func TestClassify_KnownExecutable(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{"ls": true, "git": true})

	result := c.Classify("ls -la")
	assert.Equal(t, ClassShellCommand, result.Classification)
	assert.Equal(t, "ls -la", result.Command)

	result = c.Classify("git status")
	assert.Equal(t, ClassShellCommand, result.Classification)
	assert.Equal(t, "git status", result.Command)
}

func TestClassify_BuiltinCommand(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	tests := []struct {
		input   string
		command string
		args    []string
	}{
		{"cd src/", "cd", []string{"src/"}},
		{"export FOO=bar", "export", []string{"FOO=bar"}},
		{"exit", "exit", nil},
		{"quit", "quit", nil},
	}

	for _, tt := range tests {
		result := c.Classify(tt.input)
		assert.Equal(t, ClassBuiltinCommand, result.Classification, "input: %s", tt.input)
		assert.Equal(t, tt.command, result.Command, "input: %s", tt.input)
		if tt.args != nil {
			assert.Equal(t, tt.args, result.Args, "input: %s", tt.input)
		}
	}
}

func TestClassify_NaturalLanguageQuestionWords(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	queries := []string{
		"what files changed?",
		"explain the auth flow",
		"how do I run tests",
		"why did that fail",
		"describe the architecture",
	}

	for _, q := range queries {
		result := c.Classify(q)
		assert.Equal(t, ClassLLMQuery, result.Classification, "input: %q", q)
	}
}

func TestClassify_ImperativeNaturalLanguage(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	queries := []string{
		"fix the failing test",
		"refactor the auth module",
		"add error handling to main.go",
		"create a new test file",
		"update the README",
	}

	for _, q := range queries {
		result := c.Classify(q)
		assert.Equal(t, ClassLLMQuery, result.Classification, "input: %q", q)
	}
}

func TestClassify_AmbiguousDefaultsToLLM(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	queries := []string{
		"run the tests",
		"deploy to staging",
		"check for errors",
	}

	for _, q := range queries {
		result := c.Classify(q)
		assert.Equal(t, ClassLLMQuery, result.Classification, "input: %q", q)
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{})

	result := c.Classify("")
	assert.Equal(t, ClassEmpty, result.Classification)

	result = c.Classify("   ")
	assert.Equal(t, ClassEmpty, result.Classification)
}

func TestScanPATH(t *testing.T) {
	t.Parallel()
	executables := ScanPATH()

	// These should exist on any Unix system
	assert.True(t, executables["ls"], "ls should be found in PATH")
	assert.True(t, executables["sh"], "sh should be found in PATH")
}

func TestClassify_EnvPrefixWithKnownExecutable(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{"go": true})

	result := c.Classify("GOFLAGS=-v go test ./...")

	assert.Equal(t, ClassShellCommand, result.Classification)
	assert.Equal(t, "GOFLAGS=-v go test ./...", result.Command)
}

func TestClassify_PipeChainWithKnownExecutable(t *testing.T) {
	t.Parallel()
	c := NewInputClassifier(map[string]bool{"ls": true, "cat": true})

	result := c.Classify("ls -la | grep test")
	assert.Equal(t, ClassShellCommand, result.Classification)

	result = c.Classify("cat file.go | head -20")
	assert.Equal(t, ClassShellCommand, result.Classification)
}
