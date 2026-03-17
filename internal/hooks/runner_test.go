package hooks_test

import (
	"context"
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
