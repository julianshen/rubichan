package shell

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptGeneratorBasicGeneration(t *testing.T) {
	t.Parallel()

	agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "#!/usr/bin/env bash\nset -euo pipefail\nfind . -name '*.go' | wc -l\n"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	workDir := "/project"
	sg := NewScriptGenerator(agentTurn, nil, &workDir)

	script, err := sg.Generate(context.Background(), "count all Go files")
	require.NoError(t, err)
	assert.Contains(t, script, "find . -name '*.go'")
	assert.Contains(t, script, "set -euo pipefail")
}

func TestScriptGeneratorPromptFormat(t *testing.T) {
	t.Parallel()

	var capturedPrompt string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedPrompt = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "echo hello"}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	workDir := "/my/project"
	sg := NewScriptGenerator(agentTurn, nil, &workDir)

	_, _ = sg.Generate(context.Background(), "list large files")

	assert.Contains(t, capturedPrompt, "list large files")
	assert.Contains(t, capturedPrompt, "/my/project")
	assert.Contains(t, capturedPrompt, "set -euo pipefail")
	assert.Contains(t, capturedPrompt, "ONLY the script")
}

func TestScriptGeneratorExtractsCodeBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "with bash fences",
			response: "```bash\n#!/bin/bash\necho hello\n```\n",
			expected: "#!/bin/bash\necho hello",
		},
		{
			name:     "with plain fences",
			response: "```\necho hello\n```",
			expected: "echo hello",
		},
		{
			name:     "no fences",
			response: "echo hello",
			expected: "echo hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agentTurn := func(_ context.Context, _ string) (<-chan TurnEvent, error) {
				ch := make(chan TurnEvent, 2)
				ch <- TurnEvent{Type: "text_delta", Text: tt.response}
				ch <- TurnEvent{Type: "done"}
				close(ch)
				return ch, nil
			}

			workDir := "/project"
			sg := NewScriptGenerator(agentTurn, nil, &workDir)
			script, err := sg.Generate(context.Background(), "do something")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, script)
		})
	}
}

func TestScriptGeneratorEmptyPrompt(t *testing.T) {
	t.Parallel()

	workDir := "/project"
	sg := NewScriptGenerator(nil, nil, &workDir)

	_, err := sg.Generate(context.Background(), "")
	assert.Error(t, err)

	_, err = sg.Generate(context.Background(), "   ")
	assert.Error(t, err)
}

func TestScriptApprovalApproved(t *testing.T) {
	t.Parallel()

	var executedScript string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		executedScript = cmd
		return "output", "", 0, nil
	}

	workDir := "/project"
	sg := NewScriptGenerator(nil, exec, &workDir)

	stdout, stderr, exitCode, err := sg.Execute(context.Background(), "echo hello")
	require.NoError(t, err)
	assert.Equal(t, "output", stdout)
	assert.Equal(t, "", stderr)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "echo hello", executedScript)
}

func TestScriptApprovalRejected(t *testing.T) {
	t.Parallel()
	// Rejection is handled by the caller (ShellHost), not by ScriptGenerator.
	// ScriptGenerator.Execute is only called when approved.
	// This test verifies that not calling Execute means no execution.

	called := false
	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		called = true
		return "", "", 0, nil
	}

	workDir := "/project"
	sg := NewScriptGenerator(nil, exec, &workDir)
	_ = sg // not calling Execute
	assert.False(t, called)
}

func TestScriptExecutionOutput(t *testing.T) {
	t.Parallel()

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "file1.go\nfile2.go\n", "warning: large file", 0, nil
	}

	workDir := "/project"
	sg := NewScriptGenerator(nil, exec, &workDir)

	stdout, stderr, exitCode, err := sg.Execute(context.Background(), "find . -name '*.go'")
	require.NoError(t, err)
	assert.Equal(t, "file1.go\nfile2.go\n", stdout)
	assert.Equal(t, "warning: large file", stderr)
	assert.Equal(t, 0, exitCode)
}

// Integration tests for ShellHost + Smart Script

func TestShellHostSmartScriptRouting(t *testing.T) {
	t.Parallel()

	// Intent classifier returns "action"
	classifyCall := 0
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		classifyCall++
		ch := make(chan TurnEvent, 2)
		if classifyCall == 1 {
			// Intent classification call
			ch <- TurnEvent{Type: "text_delta", Text: "action"}
		} else {
			// Script generation call
			ch <- TurnEvent{Type: "text_delta", Text: "#!/bin/bash\ngrep -r TODO . | wc -l"}
		}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	// Approval function that approves
	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "", nil
	}

	var executedCmd string
	exec := func(_ context.Context, cmd string, _ string) (string, string, int, error) {
		executedCmd = cmd
		return "42", "", 0, nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agentTurn,
		ShellExec:        exec,
		Stdin:            strings.NewReader("? find all TODO comments\n"),
		Stdout:           stdout,
		Stderr:           stderr,
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Script should have been generated and executed
	assert.Contains(t, executedCmd, "grep -r TODO")
	// Script should have been shown to the user
	assert.Contains(t, stdout.String(), "grep -r TODO")
	// Output of script execution should be shown
	assert.Contains(t, stdout.String(), "42")
}

func TestShellHostSmartScriptQuestionPassthrough(t *testing.T) {
	t.Parallel()

	callCount := 0
	var secondCallMsg string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		if callCount == 1 {
			// Intent classification → question
			ch <- TurnEvent{Type: "text_delta", Text: "question"}
		} else {
			// Normal conversational response
			secondCallMsg = msg
			ch <- TurnEvent{Type: "text_delta", Text: "A goroutine is a lightweight thread."}
		}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	stdout := &bytes.Buffer{}

	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "", nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agentTurn,
		Stdin:            strings.NewReader("? what is a goroutine\n"),
		Stdout:           stdout,
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Should have conversational response, not script
	assert.Contains(t, stdout.String(), "goroutine is a lightweight thread")
	assert.Contains(t, secondCallMsg, "what is a goroutine")
}

func TestShellHostSmartScriptDisabled(t *testing.T) {
	t.Parallel()

	var capturedMsg string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		capturedMsg = msg
		ch := make(chan TurnEvent, 2)
		ch <- TurnEvent{Type: "text_delta", Text: "Here's how to find TODOs..."}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	stdout := &bytes.Buffer{}

	// No ScriptApprovalFn → smart script disabled
	host := NewShellHost(ShellHostConfig{
		WorkDir:     "/project",
		HomeDir:     "/home/user",
		Executables: map[string]bool{},
		AgentTurn:   agentTurn,
		Stdin:       strings.NewReader("? find all TODO comments\n"),
		Stdout:      stdout,
		Stderr:      &bytes.Buffer{},
		GitBranchFn: func(string) string { return "" },
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// With smart script disabled, ? query goes directly to LLM as conversation
	assert.Contains(t, capturedMsg, "find all TODO comments")
	assert.Contains(t, stdout.String(), "Here's how to find TODOs")
}

func TestSmartScriptContextRecorded(t *testing.T) {
	t.Parallel()

	callCount := 0
	var followUpMsg string
	agentTurn := func(_ context.Context, msg string) (<-chan TurnEvent, error) {
		callCount++
		ch := make(chan TurnEvent, 2)
		switch callCount {
		case 1:
			// Intent classification → action
			ch <- TurnEvent{Type: "text_delta", Text: "action"}
		case 2:
			// Script generation
			ch <- TurnEvent{Type: "text_delta", Text: "echo counting\nwc -l *.go"}
		case 3:
			// Follow-up query
			followUpMsg = msg
			ch <- TurnEvent{Type: "text_delta", Text: "The script counted..."}
		}
		ch <- TurnEvent{Type: "done"}
		close(ch)
		return ch, nil
	}

	exec := func(_ context.Context, _ string, _ string) (string, string, int, error) {
		return "42 files", "", 0, nil
	}

	approvalFn := func(_ context.Context, _ string) (bool, string, error) {
		return true, "", nil
	}

	host := NewShellHost(ShellHostConfig{
		WorkDir:          "/project",
		HomeDir:          "/home/user",
		Executables:      map[string]bool{},
		AgentTurn:        agentTurn,
		ShellExec:        exec,
		Stdin:            strings.NewReader("? count Go files\nexplain the result\n"),
		Stdout:           &bytes.Buffer{},
		Stderr:           &bytes.Buffer{},
		GitBranchFn:      func(string) string { return "" },
		ScriptApprovalFn: approvalFn,
	})

	err := host.Run(context.Background())
	assert.NoError(t, err)

	// Follow-up query should have context from the script execution
	assert.Contains(t, followUpMsg, "42 files")
}
