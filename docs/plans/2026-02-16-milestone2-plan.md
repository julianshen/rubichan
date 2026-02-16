# Milestone 2: Headless Mode + Output + Code Review — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a non-interactive headless mode to rubichan that accepts prompts via CLI flags/stdin, runs the agent loop, and outputs JSON or Markdown — including a code-review mode that extracts git diffs and produces structured reviews.

**Architecture:** The headless runner reuses the existing `agent.Agent` and its `Turn()` method. A new `internal/runner` package handles input resolution and event collection. A new `internal/output` package provides formatter implementations. A new `internal/pipeline` package handles mode-specific prompt construction (code review). The CLI routes to headless vs TUI based on the `--headless` flag.

**Tech Stack:** Go 1.26, Cobra (CLI), existing agent/provider/tools packages. No new dependencies.

---

## Tasks

### Task 1: Output types and Formatter interface

**Files:**
- Create: `internal/output/formatter.go`
- Test: `internal/output/formatter_test.go`

**Step 1: Write the test**

```go
// internal/output/formatter_test.go
package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunResultToolCallLogJSON(t *testing.T) {
	r := RunResult{
		Prompt:    "hello",
		Response:  "world",
		TurnCount: 1,
		Duration:  2 * time.Second,
		Mode:      "generic",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"op":"read"}`), Result: "ok", IsError: false},
		},
	}

	assert.Equal(t, "hello", r.Prompt)
	assert.Equal(t, "world", r.Response)
	assert.Len(t, r.ToolCalls, 1)
	assert.Equal(t, "file", r.ToolCalls[0].Name)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/output/ -run TestRunResultToolCallLogJSON -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/output/formatter.go
package output

import (
	"encoding/json"
	"time"
)

// RunResult holds the collected output from a headless agent run.
type RunResult struct {
	Prompt    string        `json:"prompt"`
	Response  string        `json:"response"`
	ToolCalls []ToolCallLog `json:"tool_calls,omitempty"`
	TurnCount int           `json:"turn_count"`
	Duration  time.Duration `json:"duration_ms"`
	Mode      string        `json:"mode"`
	Error     string        `json:"error,omitempty"`
}

// ToolCallLog records a single tool invocation during a run.
type ToolCallLog struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Result  string          `json:"result"`
	IsError bool            `json:"is_error,omitempty"`
}

// Formatter formats a RunResult into output bytes.
type Formatter interface {
	Format(result *RunResult) ([]byte, error)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/output/ -run TestRunResultToolCallLogJSON -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/output/formatter.go internal/output/formatter_test.go
git commit -m "feat(output): add RunResult types and Formatter interface"
```

---

### Task 2: JSON formatter

**Files:**
- Create: `internal/output/json.go`
- Test: `internal/output/json_test.go`

**Step 1: Write the test**

```go
// internal/output/json_test.go
package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFormatterBasic(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:    "say hello",
		Response:  "Hello!",
		TurnCount: 1,
		Duration:  500 * time.Millisecond,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "say hello", decoded["prompt"])
	assert.Equal(t, "Hello!", decoded["response"])
	assert.Equal(t, "generic", decoded["mode"])
	assert.Equal(t, float64(1), decoded["turn_count"])
}

func TestJSONFormatterWithToolCalls(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:   "read file",
		Response: "contents",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"path":"main.go"}`), Result: "package main"},
		},
		TurnCount: 2,
		Duration:  time.Second,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	calls, ok := decoded["tool_calls"].([]any)
	require.True(t, ok)
	assert.Len(t, calls, 1)
}

func TestJSONFormatterWithError(t *testing.T) {
	f := NewJSONFormatter()
	result := &RunResult{
		Prompt:    "fail",
		Response:  "",
		TurnCount: 0,
		Duration:  0,
		Mode:      "generic",
		Error:     "something went wrong",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(out, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "something went wrong", decoded["error"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/output/ -run TestJSONFormatter -v`
Expected: FAIL — `NewJSONFormatter` undefined

**Step 3: Write minimal implementation**

```go
// internal/output/json.go
package output

import "encoding/json"

// JSONFormatter outputs RunResult as JSON.
type JSONFormatter struct{}

// NewJSONFormatter creates a new JSONFormatter.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// Format marshals the RunResult as indented JSON.
func (f *JSONFormatter) Format(result *RunResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/output/ -run TestJSONFormatter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/output/json.go internal/output/json_test.go
git commit -m "feat(output): add JSON formatter"
```

---

### Task 3: Markdown formatter

**Files:**
- Create: `internal/output/markdown.go`
- Test: `internal/output/markdown_test.go`

**Step 1: Write the test**

```go
// internal/output/markdown_test.go
package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownFormatterBasic(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "say hello",
		Response:  "Hello there!",
		TurnCount: 1,
		Duration:  2 * time.Second,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Hello there!")
	assert.Contains(t, s, "1 turn")
}

func TestMarkdownFormatterWithToolCalls(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:   "read a file",
		Response: "The file contains code.",
		ToolCalls: []ToolCallLog{
			{Name: "file", Input: json.RawMessage(`{"op":"read"}`), Result: "package main", IsError: false},
			{Name: "shell", Input: json.RawMessage(`{"command":"ls"}`), Result: "main.go", IsError: false},
		},
		TurnCount: 3,
		Duration:  5 * time.Second,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Tool Calls")
	assert.Contains(t, s, "file")
	assert.Contains(t, s, "shell")
	assert.Contains(t, s, "3 turns")
}

func TestMarkdownFormatterWithError(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "fail",
		Response:  "",
		TurnCount: 0,
		Duration:  0,
		Mode:      "generic",
		Error:     "timeout exceeded",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.Contains(t, s, "Error")
	assert.Contains(t, s, "timeout exceeded")
}

func TestMarkdownFormatterNoToolCallsSection(t *testing.T) {
	f := NewMarkdownFormatter()
	result := &RunResult{
		Prompt:    "hello",
		Response:  "Hi!",
		TurnCount: 1,
		Duration:  time.Second,
		Mode:      "generic",
	}

	out, err := f.Format(result)
	require.NoError(t, err)

	s := string(out)
	assert.False(t, strings.Contains(s, "Tool Calls"))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/output/ -run TestMarkdownFormatter -v`
Expected: FAIL — `NewMarkdownFormatter` undefined

**Step 3: Write minimal implementation**

```go
// internal/output/markdown.go
package output

import (
	"fmt"
	"strings"
)

// MarkdownFormatter outputs RunResult as human-readable Markdown.
type MarkdownFormatter struct{}

// NewMarkdownFormatter creates a new MarkdownFormatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

// Format renders the RunResult as Markdown.
func (f *MarkdownFormatter) Format(result *RunResult) ([]byte, error) {
	var b strings.Builder

	if result.Error != "" {
		b.WriteString("## Error\n\n")
		b.WriteString(result.Error)
		b.WriteString("\n")
		return []byte(b.String()), nil
	}

	b.WriteString(result.Response)
	b.WriteString("\n")

	if len(result.ToolCalls) > 0 {
		b.WriteString("\n## Tool Calls\n\n")
		for i, tc := range result.ToolCalls {
			status := "ok"
			if tc.IsError {
				status = "error"
			}
			b.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, tc.Name, status))
		}
	}

	b.WriteString(fmt.Sprintf("\n---\n*Completed in %d turn(s), %s*\n",
		result.TurnCount, result.Duration.Round(100*time.Millisecond)))

	return []byte(b.String()), nil
}
```

Note: You will need to add `"time"` to the import block.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/output/ -run TestMarkdownFormatter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/output/markdown.go internal/output/markdown_test.go
git commit -m "feat(output): add Markdown formatter"
```

---

### Task 4: Input resolver

**Files:**
- Create: `internal/runner/input.go`
- Test: `internal/runner/input_test.go`

**Step 1: Write the test**

```go
// internal/runner/input_test.go
package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInputFromPromptFlag(t *testing.T) {
	text, err := ResolveInput("hello world", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", text)
}

func TestResolveInputFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	err := os.WriteFile(path, []byte("prompt from file"), 0644)
	require.NoError(t, err)

	text, err := ResolveInput("", path, nil)
	require.NoError(t, err)
	assert.Equal(t, "prompt from file", text)
}

func TestResolveInputFromStdin(t *testing.T) {
	reader := strings.NewReader("piped input")
	text, err := ResolveInput("", "", reader)
	require.NoError(t, err)
	assert.Equal(t, "piped input", text)
}

func TestResolveInputPromptTakesPrecedence(t *testing.T) {
	reader := strings.NewReader("stdin")
	text, err := ResolveInput("flag wins", "", reader)
	require.NoError(t, err)
	assert.Equal(t, "flag wins", text)
}

func TestResolveInputFileTakesPrecedenceOverStdin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	err := os.WriteFile(path, []byte("file wins"), 0644)
	require.NoError(t, err)

	reader := strings.NewReader("stdin")
	text, err := ResolveInput("", path, reader)
	require.NoError(t, err)
	assert.Equal(t, "file wins", text)
}

func TestResolveInputNoInput(t *testing.T) {
	_, err := ResolveInput("", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input")
}

func TestResolveInputFileMissing(t *testing.T) {
	_, err := ResolveInput("", "/nonexistent/path.txt", nil)
	require.Error(t, err)
}

func TestResolveInputEmptyPrompt(t *testing.T) {
	_, err := ResolveInput("   ", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestResolveInput -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/runner/input.go
package runner

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ResolveInput determines the user prompt from the available sources.
// Priority: promptFlag > filePath > stdinReader.
// stdinReader may be nil if stdin is a TTY (no pipe).
func ResolveInput(promptFlag, filePath string, stdinReader io.Reader) (string, error) {
	if text := strings.TrimSpace(promptFlag); text != "" {
		return text, nil
	}

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("reading prompt file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	if stdinReader != nil {
		data, err := io.ReadAll(stdinReader)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text, nil
		}
	}

	return "", fmt.Errorf("no input provided: use --prompt, --file, or pipe to stdin")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/ -run TestResolveInput -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runner/input.go internal/runner/input_test.go
git commit -m "feat(runner): add input resolver for prompt/file/stdin"
```

---

### Task 5: Headless runner

**Files:**
- Create: `internal/runner/headless.go`
- Test: `internal/runner/headless_test.go`

This is the core piece. It takes a prompt, runs the agent loop, collects events, and returns a `RunResult`.

**Step 1: Write the test**

```go
// internal/runner/headless_test.go
package runner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/agent"
)

// mockTurnFunc simulates Agent.Turn by returning a channel of canned events.
type mockTurnFunc func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error)

func makeEventCh(events ...agent.TurnEvent) <-chan agent.TurnEvent {
	ch := make(chan agent.TurnEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestHeadlessRunnerBasic(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "text_delta", Text: "Hello "},
			agent.TurnEvent{Type: "text_delta", Text: "World"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "say hello", "generic")
	require.NoError(t, err)

	assert.Equal(t, "say hello", result.Prompt)
	assert.Equal(t, "Hello World", result.Response)
	assert.Equal(t, "generic", result.Mode)
	assert.Empty(t, result.ToolCalls)
	assert.Empty(t, result.Error)
}

func TestHeadlessRunnerWithToolCalls(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "tool_call", ToolCall: &agent.ToolCallEvent{
				ID: "t1", Name: "file", Input: []byte(`{"op":"read"}`),
			}},
			agent.TurnEvent{Type: "tool_result", ToolResult: &agent.ToolResultEvent{
				ID: "t1", Name: "file", Content: "package main", IsError: false,
			}},
			agent.TurnEvent{Type: "text_delta", Text: "Done"},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "read file", "generic")
	require.NoError(t, err)

	assert.Equal(t, "Done", result.Response)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "file", result.ToolCalls[0].Name)
	assert.Equal(t, "package main", result.ToolCalls[0].Result)
}

func TestHeadlessRunnerError(t *testing.T) {
	turnFn := func(_ context.Context, msg string) (<-chan agent.TurnEvent, error) {
		return makeEventCh(
			agent.TurnEvent{Type: "error", Error: assert.AnError},
			agent.TurnEvent{Type: "done"},
		), nil
	}

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(context.Background(), "fail", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
}

func TestHeadlessRunnerTimeout(t *testing.T) {
	turnFn := func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error) {
		ch := make(chan agent.TurnEvent)
		go func() {
			defer close(ch)
			<-ctx.Done()
			ch <- agent.TurnEvent{Type: "error", Error: ctx.Err()}
			ch <- agent.TurnEvent{Type: "done"}
		}()
		return ch, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := NewHeadlessRunner(turnFn)
	result, err := r.Run(ctx, "slow", "generic")
	require.NoError(t, err)

	assert.NotEmpty(t, result.Error)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestHeadlessRunner -v`
Expected: FAIL — `NewHeadlessRunner` undefined

**Step 3: Write minimal implementation**

```go
// internal/runner/headless.go
package runner

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/julianshen/rubichan/internal/agent"
	"github.com/julianshen/rubichan/internal/output"
)

// TurnFunc matches the signature of agent.Agent.Turn.
type TurnFunc func(ctx context.Context, msg string) (<-chan agent.TurnEvent, error)

// HeadlessRunner executes a single agent turn and collects the result.
type HeadlessRunner struct {
	turn TurnFunc
}

// NewHeadlessRunner creates a new HeadlessRunner with the given turn function.
func NewHeadlessRunner(turn TurnFunc) *HeadlessRunner {
	return &HeadlessRunner{turn: turn}
}

// Run executes the agent with the given prompt and collects a RunResult.
func (r *HeadlessRunner) Run(ctx context.Context, prompt, mode string) (*output.RunResult, error) {
	start := time.Now()

	ch, err := r.turn(ctx, prompt)
	if err != nil {
		return &output.RunResult{
			Prompt:   prompt,
			Mode:     mode,
			Duration: time.Since(start),
			Error:    err.Error(),
		}, nil
	}

	var textBuf strings.Builder
	var toolCalls []output.ToolCallLog
	var lastErr string
	turns := 0

	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			textBuf.WriteString(evt.Text)
		case "tool_call":
			if evt.ToolCall != nil {
				toolCalls = append(toolCalls, output.ToolCallLog{
					Name:  evt.ToolCall.Name,
					Input: json.RawMessage(evt.ToolCall.Input),
				})
			}
		case "tool_result":
			if evt.ToolResult != nil && len(toolCalls) > 0 {
				last := &toolCalls[len(toolCalls)-1]
				last.Result = evt.ToolResult.Content
				last.IsError = evt.ToolResult.IsError
			}
		case "error":
			if evt.Error != nil {
				lastErr = evt.Error.Error()
			}
		case "done":
			turns++
		}
	}

	return &output.RunResult{
		Prompt:    prompt,
		Response:  textBuf.String(),
		ToolCalls: toolCalls,
		TurnCount: turns,
		Duration:  time.Since(start),
		Mode:      mode,
		Error:     lastErr,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/ -run TestHeadlessRunner -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runner/headless.go internal/runner/headless_test.go
git commit -m "feat(runner): add headless runner with event collection"
```

---

### Task 6: Git diff extraction

**Files:**
- Create: `internal/pipeline/diff.go`
- Test: `internal/pipeline/diff_test.go`

**Step 1: Write the test**

```go
// internal/pipeline/diff_test.go
package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGitRepo creates a temp git repo with an initial commit and a second
// commit, returning the repo path. Caller uses t.TempDir for cleanup.
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v failed: %s", args, out)
	}

	// Add a file and commit
	err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0644)
	require.NoError(t, err)
	for _, args := range [][]string{
		{"git", "add", "hello.go"},
		{"git", "commit", "-m", "add hello"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cmd %v failed: %s", args, out)
	}

	return dir
}

func TestExtractDiff(t *testing.T) {
	dir := setupGitRepo(t)

	diff, err := ExtractDiff(context.Background(), dir, "HEAD~1..HEAD")
	require.NoError(t, err)
	assert.Contains(t, diff, "hello.go")
	assert.Contains(t, diff, "package main")
}

func TestExtractDiffDefault(t *testing.T) {
	dir := setupGitRepo(t)

	// Empty diffRange should default to HEAD~1..HEAD
	diff, err := ExtractDiff(context.Background(), dir, "")
	require.NoError(t, err)
	assert.Contains(t, diff, "hello.go")
}

func TestExtractDiffInvalidRange(t *testing.T) {
	dir := setupGitRepo(t)

	_, err := ExtractDiff(context.Background(), dir, "nonexistent..alsonotreal")
	require.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestExtractDiff -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
// internal/pipeline/diff.go
package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExtractDiff runs git diff in the given directory and returns the diff text.
// If diffRange is empty, defaults to "HEAD~1..HEAD".
func ExtractDiff(ctx context.Context, dir, diffRange string) (string, error) {
	if diffRange == "" {
		diffRange = "HEAD~1..HEAD"
	}

	parts := strings.SplitN(diffRange, "..", 2)
	args := []string{"diff"}
	if len(parts) == 2 {
		args = append(args, parts[0], parts[1])
	} else {
		args = append(args, diffRange)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return string(out), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestExtractDiff -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/diff.go internal/pipeline/diff_test.go
git commit -m "feat(pipeline): add git diff extraction"
```

---

### Task 7: Code review pipeline

**Files:**
- Create: `internal/pipeline/codereview.go`
- Test: `internal/pipeline/codereview_test.go`

**Step 1: Write the test**

```go
// internal/pipeline/codereview_test.go
package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReviewPrompt(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1,2 @@
 package main
+func hello() {}
`
	prompt := BuildReviewPrompt(diff)

	require.NotEmpty(t, prompt)
	assert.Contains(t, prompt, diff)
	assert.Contains(t, prompt, "review")
}

func TestBuildReviewPromptEmpty(t *testing.T) {
	prompt := BuildReviewPrompt("")
	assert.Contains(t, prompt, "no changes")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestBuildReviewPrompt -v`
Expected: FAIL — `BuildReviewPrompt` undefined

**Step 3: Write minimal implementation**

```go
// internal/pipeline/codereview.go
package pipeline

import "fmt"

// BuildReviewPrompt constructs a code review prompt from a git diff.
func BuildReviewPrompt(diff string) string {
	if diff == "" {
		return "There are no changes to review (no diff found, no changes detected)."
	}

	return fmt.Sprintf(`You are reviewing the following code changes. Analyze the diff and provide:
1. A brief summary of what changed
2. Issues found (bugs, security, performance, style) with severity (high/medium/low)
3. Suggestions for improvement

If there are no issues, say so.

<diff>
%s
</diff>`, diff)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestBuildReviewPrompt -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/codereview.go internal/pipeline/codereview_test.go
git commit -m "feat(pipeline): add code review prompt builder"
```

---

### Task 8: CLI integration — add headless flags and routing

**Files:**
- Modify: `cmd/rubichan/main.go`

This task wires everything together. It adds the headless flags to the root command and routes to the headless runner when `--headless` is set.

**Step 1: Write the test**

Since this involves CLI wiring, we test by building the binary and running it. Create a simple script-style integration test.

```go
// internal/runner/integration_test.go
//go:build integration

package runner_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadlessNoInput(t *testing.T) {
	cmd := exec.Command("go", "run", "../../cmd/rubichan/", "--headless")
	out, err := cmd.CombinedOutput()
	// Should fail with exit code 1 and error message about no input
	require.Error(t, err)
	assert.Contains(t, string(out), "no input")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestHeadlessNoInput -tags integration -v`
Expected: FAIL — `--headless` flag not recognized

**Step 3: Modify `cmd/rubichan/main.go`**

Add these new flag variables alongside the existing ones (after line 34):

```go
	headless     bool
	promptFlag   string
	fileFlag     string
	modeFlag     string
	outputFlag   string
	diffFlag     string
	maxTurnsFlag int
	timeoutFlag  time.Duration
	toolsFlag    string
```

Add these flags to the root command (after line 56):

```go
	rootCmd.PersistentFlags().BoolVar(&headless, "headless", false, "run in non-interactive headless mode")
	rootCmd.PersistentFlags().StringVar(&promptFlag, "prompt", "", "prompt text for headless mode")
	rootCmd.PersistentFlags().StringVar(&fileFlag, "file", "", "read prompt from file for headless mode")
	rootCmd.PersistentFlags().StringVar(&modeFlag, "mode", "", "headless mode (e.g. code-review)")
	rootCmd.PersistentFlags().StringVar(&outputFlag, "output", "markdown", "output format: json, markdown")
	rootCmd.PersistentFlags().StringVar(&diffFlag, "diff", "", "git diff range for code-review mode")
	rootCmd.PersistentFlags().IntVar(&maxTurnsFlag, "max-turns", 0, "override max agent turns")
	rootCmd.PersistentFlags().DurationVar(&timeoutFlag, "timeout", 120*time.Second, "headless execution timeout")
	rootCmd.PersistentFlags().StringVar(&toolsFlag, "tools", "", "comma-separated tool whitelist (empty = all)")
```

Change the root command's `RunE` (line 46-48) to route:

```go
		RunE: func(_ *cobra.Command, _ []string) error {
			if headless {
				return runHeadless()
			}
			return runInteractive()
		},
```

Add the `runHeadless` function. Add these imports: `"io"`, `"strings"`, `"github.com/julianshen/rubichan/internal/output"`, `"github.com/julianshen/rubichan/internal/pipeline"`, `"github.com/julianshen/rubichan/internal/runner"`:

```go
func runHeadless() error {
	// Load config (same as interactive)
	cfgPath := configPath
	if cfgPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		cfgPath = filepath.Join(home, ".config", "rubichan", "config.toml")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if modelFlag != "" {
		cfg.Provider.Model = modelFlag
	}
	if providerFlag != "" {
		cfg.Provider.Default = providerFlag
	}
	if maxTurnsFlag > 0 {
		cfg.Agent.MaxTurns = maxTurnsFlag
	}

	// Resolve input
	var stdinReader io.Reader
	stat, _ := os.Stdin.Stat()
	if stat.Mode()&os.ModeCharDevice == 0 {
		stdinReader = os.Stdin
	}

	promptText, err := runner.ResolveInput(promptFlag, fileFlag, stdinReader)
	if err != nil {
		return err
	}

	// Code review mode: extract diff and build prompt
	if modeFlag == "code-review" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
		defer cancel()

		diff, err := pipeline.ExtractDiff(ctx, cwd, diffFlag)
		if err != nil {
			return fmt.Errorf("extracting diff: %w", err)
		}
		promptText = pipeline.BuildReviewPrompt(diff)
	}

	// Create provider
	p, err := provider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Create tool registry with optional whitelist
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	registry := tools.NewRegistry()
	allowed := parseToolsFlag(toolsFlag)

	if shouldRegister("file", allowed) {
		if err := registry.Register(tools.NewFileTool(cwd)); err != nil {
			return fmt.Errorf("registering file tool: %w", err)
		}
	}
	if shouldRegister("shell", allowed) {
		if err := registry.Register(tools.NewShellTool(cwd, 120*time.Second)); err != nil {
			return fmt.Errorf("registering shell tool: %w", err)
		}
	}

	// Headless always auto-approves (tools are restricted via whitelist)
	approvalFunc := func(_ context.Context, _ string, _ json.RawMessage) (bool, error) {
		return true, nil
	}

	a := agent.New(p, registry, approvalFunc, cfg)

	// Run headless
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFlag)
	defer cancel()

	mode := modeFlag
	if mode == "" {
		mode = "generic"
	}

	hr := runner.NewHeadlessRunner(a.Turn)
	result, err := hr.Run(ctx, promptText, mode)
	if err != nil {
		return err
	}

	// Format output
	var formatter output.Formatter
	switch outputFlag {
	case "json":
		formatter = output.NewJSONFormatter()
	default:
		formatter = output.NewMarkdownFormatter()
	}

	out, err := formatter.Format(result)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	fmt.Print(string(out))

	if result.Error != "" {
		os.Exit(1)
	}

	return nil
}

// parseToolsFlag splits a comma-separated tools string into a set.
// Returns nil if the input is empty (meaning all tools allowed).
func parseToolsFlag(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, t := range strings.Split(s, ",") {
		if name := strings.TrimSpace(t); name != "" {
			m[name] = true
		}
	}
	return m
}

// shouldRegister returns true if the tool should be registered.
// If allowed is nil, all tools are allowed.
func shouldRegister(name string, allowed map[string]bool) bool {
	if allowed == nil {
		return true
	}
	return allowed[name]
}
```

**Step 4: Run test to verify it passes**

Run: `go build ./cmd/rubichan/ && go test ./internal/runner/ -run TestHeadlessNoInput -tags integration -v`
Expected: Build succeeds and integration test passes

Also verify the binary works:

Run: `./rubichan --headless 2>&1; echo "exit: $?"`
Expected: Error message about no input, exit code 1

**Step 5: Commit**

```bash
git add cmd/rubichan/main.go internal/runner/integration_test.go
git commit -m "feat(cli): wire headless mode with flags and routing"
```

---

### Task 9: Tool whitelist unit tests

**Files:**
- Create: `cmd/rubichan/helpers_test.go`

**Step 1: Write the test**

Note: Since `parseToolsFlag` and `shouldRegister` are in `package main`, either move them to a helper file or test them from the main package. The simplest approach is to move them to a separate file and test in the same package.

First, extract `parseToolsFlag` and `shouldRegister` into `cmd/rubichan/helpers.go`, then test:

```go
// cmd/rubichan/helpers_test.go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseToolsFlagEmpty(t *testing.T) {
	result := parseToolsFlag("")
	assert.Nil(t, result)
}

func TestParseToolsFlagSingle(t *testing.T) {
	result := parseToolsFlag("file")
	assert.True(t, result["file"])
	assert.False(t, result["shell"])
}

func TestParseToolsFlagMultiple(t *testing.T) {
	result := parseToolsFlag("file,shell")
	assert.True(t, result["file"])
	assert.True(t, result["shell"])
}

func TestShouldRegisterAllAllowed(t *testing.T) {
	assert.True(t, shouldRegister("file", nil))
	assert.True(t, shouldRegister("shell", nil))
}

func TestShouldRegisterFiltered(t *testing.T) {
	allowed := map[string]bool{"file": true}
	assert.True(t, shouldRegister("file", allowed))
	assert.False(t, shouldRegister("shell", allowed))
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./cmd/rubichan/ -run TestParseToolsFlag -v && go test ./cmd/rubichan/ -run TestShouldRegister -v`
Expected: PASS (functions already exist from Task 8)

**Step 3: Commit**

```bash
git add cmd/rubichan/helpers_test.go
git commit -m "test(cli): add unit tests for tool whitelist parsing"
```

---

### Task 10: Full run-through test

**Files:**
- Modify: `internal/runner/integration_test.go`

**Step 1: Run all unit tests**

Run: `go test ./...`
Expected: All tests pass across all packages

**Step 2: Manual smoke test** (not automated — requires LLM provider)

```bash
# Generic headless prompt with JSON output
OPENROUTER_API_KEY='...' ./rubichan --headless --prompt "What is 2+2? Answer briefly." --output json

# Generic headless prompt with Markdown output
OPENROUTER_API_KEY='...' ./rubichan --headless --prompt "What is 2+2?" --output markdown

# Stdin pipe
echo "Say hello briefly" | OPENROUTER_API_KEY='...' ./rubichan --headless --output json

# Code review mode
OPENROUTER_API_KEY='...' ./rubichan --headless --mode=code-review --diff HEAD~1..HEAD --output markdown

# Tool whitelist (file only, no shell)
OPENROUTER_API_KEY='...' ./rubichan --headless --prompt "List files" --tools=file --output json
```

**Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: address issues found during smoke testing"
```

---

## Summary

| Task | Component | New Files |
|------|-----------|-----------|
| 1 | Output types + Formatter interface | `internal/output/formatter.go` |
| 2 | JSON formatter | `internal/output/json.go` |
| 3 | Markdown formatter | `internal/output/markdown.go` |
| 4 | Input resolver | `internal/runner/input.go` |
| 5 | Headless runner | `internal/runner/headless.go` |
| 6 | Git diff extraction | `internal/pipeline/diff.go` |
| 7 | Code review prompt builder | `internal/pipeline/codereview.go` |
| 8 | CLI integration | `cmd/rubichan/main.go` (modified) |
| 9 | Tool whitelist tests | `cmd/rubichan/helpers_test.go` |
| 10 | Full run-through | Smoke testing |
