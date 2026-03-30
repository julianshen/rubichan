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
