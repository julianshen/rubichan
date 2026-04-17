package hooks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/hooks"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerRegistersHandlers(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "session_start", Command: "echo hello", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{Phase: skills.HookOnConversationStart, Ctx: context.Background()})
	require.NoError(t, err)
	_ = result
}

func TestRunnerPreToolBlocksOnFailure(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"test.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "pre_tool with exit 1 should cancel")
}

func TestRunnerPostToolDoesNotBlock(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_tool", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "content": "ok"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestRunnerTemplateSubstitution(t *testing.T) {
	lm := skills.NewLifecycleManager()
	// {tool} is shell-quoted, so test that the expanded command runs correctly.
	// "test {tool} = 'shell'" expands to "test 'shell' = 'shell'" which is true.
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", Command: "test {tool} = 'shell'", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"ls"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestRunnerPreEditFiltersByPattern(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Pattern: "*.py", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())

	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"main.go"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "*.py pattern should not match main.go")
	}
}

func TestRunnerPreShellFilter(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_shell", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Shell tool should be blocked
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"ls"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "pre_shell should block shell tool")

	// File tool should NOT be blocked by pre_shell
	result2, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"x.go"}`},
	})
	require.NoError(t, err)
	if result2 != nil {
		assert.False(t, result2.Cancel, "pre_shell should not affect file tool")
	}
}

func TestRunnerPreEditMatchesPattern(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Pattern: "*.go", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// .go file SHOULD be blocked
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "*.go pattern should match main.go")
}

func TestRunnerPreEditSkipsRead(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Read operation should NOT trigger pre_edit
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"read","path":"main.go"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "read should not trigger pre_edit")
	}
}

func TestRunnerShellInjectionPrevented(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	// If {file} is not shell-escaped, $(whoami) would execute
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_edit", Command: "echo {file}", Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	// Inject shell metacharacters via file path
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"$(whoami).go"}`},
	})
	require.NoError(t, err)
	// Post-event should succeed without executing the injection
	_ = result
}

func TestRunnerInvalidEventSkipped(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "invalid_event", Command: "echo bad"},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)
}

func TestRunnerSetupEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "setup", Command: "echo setup-ran", Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnSetup,
		Ctx:   context.Background(),
		Data:  map[string]any{"mode": "init"},
	})
	require.NoError(t, err)
	// Setup is non-blocking (not a pre-event), so Cancel should be false.
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

// --- Enhancement 1: If Pattern Filter Tests ---

func TestIfPattern_ShellMatch(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_shell", If: "Bash(git *)", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// "git status" should match "git *"
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"git status"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "git status should match Bash(git *)")
}

func TestIfPattern_ShellNoMatch(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_shell", If: "Bash(git *)", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// "npm install" should NOT match "git *"
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"npm install"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "npm install should not match Bash(git *)")
	}
}

func TestIfPattern_FileMatch(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", If: "file(*.go)", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Writing a .go file should match "file(*.go)"
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "main.go should match file(*.go)")
}

func TestIfPattern_EmptyIfRunsAlways(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_shell", If: "", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Empty "if" means always match
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"anything"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "empty if should always match")
}

// --- Enhancement 2: stdin/stdout JSON Protocol Tests ---

func TestJSONProtocol_StdinReceived(t *testing.T) {
	dir := t.TempDir()
	lm := skills.NewLifecycleManager()
	// Hook reads stdin and writes it to a file so we can verify.
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_tool", Command: fmt.Sprintf("cat > %s/stdin_output.json", dir), Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	_, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"ls"}`},
	})
	require.NoError(t, err)

	// Verify the stdin file was written and contains expected JSON.
	data, err := os.ReadFile(filepath.Join(dir, "stdin_output.json"))
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "post_tool", parsed["event"])
	assert.Equal(t, "shell", parsed["tool_name"])
}

func TestJSONProtocol_BlockDecision(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", Command: `echo '{"decision":"block"}'`, Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"x.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "JSON block decision should cancel")
}

func TestJSONProtocol_ModifiedData(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_tool", Command: `echo '{"modified":{"content":"new value"}}'`, Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Modified)
	assert.Equal(t, "new value", result.Modified["content"])
}

func TestJSONProtocol_BackwardCompat(t *testing.T) {
	lm := skills.NewLifecycleManager()
	// Hook that does not read stdin and outputs plain text (not JSON).
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_tool", Command: "echo 'hello plain text'", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": `{"command":"ls"}`},
	})
	require.NoError(t, err)
	// Should not cancel and should not have modified data.
	if result != nil {
		assert.False(t, result.Cancel)
		assert.Nil(t, result.Modified)
	}
}

// --- Enhancement 3: Task Lifecycle Hook Tests ---

func TestTaskCreatedEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "task_created", Command: "echo task-created", Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnTaskCreated,
		Ctx:   context.Background(),
		Data:  map[string]any{"task_id": "bg-1", "description": "run tests"},
	})
	require.NoError(t, err)
	// Task events are non-blocking, so Cancel should be false.
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

func TestTaskCompletedEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	dir := t.TempDir()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "task_completed", Command: "echo task-done", Timeout: 5 * time.Second},
	}, dir)
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnTaskCompleted,
		Ctx:   context.Background(),
		Data:  map[string]any{"task_id": "bg-1", "status": "success", "result": "all passed"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

// --- Enhancement 4: Prompt/Response Lifecycle Hook Tests ---

func TestPrePromptEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_prompt", Command: "echo pre-prompt", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforePromptBuild,
		Ctx:   context.Background(),
		Data:  map[string]any{"prompt": "hello"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "pre_prompt success should not cancel")
	}
}

func TestPrePromptModifiesPrompt(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_prompt", Command: `echo '{"modified":{"prompt":"rewritten"}}'`, Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforePromptBuild,
		Ctx:   context.Background(),
		Data:  map[string]any{"prompt": "original"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Modified)
	assert.Equal(t, "rewritten", result.Modified["prompt"])
}

func TestPostResponseEventMaps(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_response", Command: "echo post-response", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterResponse,
		Ctx:   context.Background(),
		Data:  map[string]any{"response": "hi"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "post_response is non-blocking")
	}
}

func TestPostResponseNonBlockingOnFailure(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_response", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterResponse,
		Ctx:   context.Background(),
		Data:  map[string]any{"response": "hi"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "post_response failure must not cancel")
	}
}

// --- ParseHookTimeout edge cases ---

func TestParseHookTimeoutUnparseable(t *testing.T) {
	// An unparseable string should return the default timeout (30s).
	d := hooks.ParseHookTimeout("not-a-duration")
	assert.Equal(t, 30*time.Second, d)
}

// --- extractPrimaryInput fallback paths ---

func TestIfPattern_FallbackToParsedCommandField(t *testing.T) {
	// When tool_name is an unrecognized tool (not shell/bash/file),
	// extractPrimaryInput should fall through to the fallback "command" field.
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", If: "git *", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Use an unknown tool name, but provide a "command" field in the input JSON.
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "custom_tool", "input": `{"command":"git status"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "fallback command extraction should match 'git *'")
}

func TestIfPattern_FallbackToPathField(t *testing.T) {
	// When tool_name is unrecognized, and no "command" field exists,
	// extractPrimaryInput should try "path" as a fallback.
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", If: "*.go", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "custom_tool", "input": `{"path":"main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "fallback path extraction should match '*.go'")
}

func TestIfPattern_NilParsedData(t *testing.T) {
	// When input JSON is not valid, extractPrimaryInput returns "".
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", If: "Bash(git *)", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "shell", "input": "not-json"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "unparseable input should not match")
	}
}

// --- globMatch edge cases ---

func TestGlobMatchBasePathFallback(t *testing.T) {
	// globMatch should try matching against the basename when the full
	// path doesn't match. E.g., "*.go" should match "src/internal/main.go"
	// because filepath.Base("src/internal/main.go") = "main.go".
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", If: "*.go", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"path":"src/internal/main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "*.go should match basename of src/internal/main.go")
}

// --- filterFileWritePatch edge cases ---

func TestPreEditInvalidJSON(t *testing.T) {
	// filterFileWritePatch should return false for invalid JSON input.
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": "not-json"},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel, "invalid JSON should not trigger pre_edit")
	}
}

func TestPreEditPatchOperation(t *testing.T) {
	// filterFileWritePatch should accept "patch" operations, not just "write".
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_edit", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"patch","path":"main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "patch operation should trigger pre_edit")
}

func TestPostEditFiltersByPattern(t *testing.T) {
	// post_edit with a pattern should match write operations and the glob.
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "post_edit", Pattern: "*.py", Command: "echo formatted", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Should NOT fire for a .go file.
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnAfterToolResult,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"operation":"write","path":"main.go"}`},
	})
	require.NoError(t, err)
	if result != nil {
		assert.Nil(t, result.Modified, "pattern *.py should not match main.go")
	}
}

// --- Hook with nil context ---

func TestRunnerNilContextUsesBackground(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "session_start", Command: "echo hello", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	// Dispatch with nil Ctx should not panic — runner falls back to context.Background().
	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnConversationStart,
		Ctx:   nil,
	})
	require.NoError(t, err)
	_ = result
}

// --- Empty event data should still pass (no stdin piped) ---

func TestRunnerEmptyEventData(t *testing.T) {
	lm := skills.NewLifecycleManager()
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "session_start", Command: "echo ok", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnConversationStart,
		Ctx:   context.Background(),
		Data:  map[string]any{},
	})
	require.NoError(t, err)
	if result != nil {
		assert.False(t, result.Cancel)
	}
}

// --- matchesIfPattern bare glob (no parentheses) ---

func TestIfPattern_BareGlobMatchesAnyTool(t *testing.T) {
	lm := skills.NewLifecycleManager()
	// Bare glob "*.go" without ToolName() wrapper — should match any tool.
	runner := hooks.NewUserHookRunner([]hooks.UserHookConfig{
		{Event: "pre_tool", If: "*.go", Command: "exit 1", Timeout: 5 * time.Second},
	}, t.TempDir())
	runner.RegisterIntoLM(lm)

	result, err := lm.Dispatch(skills.HookEvent{
		Phase: skills.HookOnBeforeToolCall,
		Ctx:   context.Background(),
		Data:  map[string]any{"tool_name": "file", "input": `{"path":"main.go"}`},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Cancel, "bare glob *.go should match file tool with path main.go")
}

// --- LoadHooksTOML read error (permissions) ---

func TestLoadHooksTOMLReadError(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, ".agent")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))

	hookFile := filepath.Join(agentDir, "hooks.toml")
	require.NoError(t, os.WriteFile(hookFile, []byte("[[hooks]]\nevent = \"setup\"\ncommand = \"echo ok\"\n"), 0o644))
	require.NoError(t, os.Chmod(hookFile, 0o000))
	defer os.Chmod(hookFile, 0o644)

	_, err := hooks.LoadHooksTOML(dir)
	assert.Error(t, err, "should fail when file is unreadable")
	assert.Contains(t, err.Error(), "reading")
}
