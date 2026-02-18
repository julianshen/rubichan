package process

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/julianshen/rubichan/internal/skills"
)

// echoSkillBinary is the path to the built test helper binary.
var echoSkillBinary string

// TestMain builds the echo_skill test helper binary before running tests.
func TestMain(m *testing.M) {
	// Build the echo_skill binary from testdata.
	tmpDir, err := os.MkdirTemp("", "process-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	echoSkillBinary = tmpDir + "/echo_skill"
	cmd := exec.Command("go", "build", "-o", echoSkillBinary, "./testdata/echo_skill.go")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build echo_skill: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// --- Mocks ---

// mockPermissionChecker is a test double for skills.PermissionChecker.
type mockPermissionChecker struct {
	allowAll bool
}

func (m *mockPermissionChecker) CheckPermission(_ skills.Permission) error {
	if m.allowAll {
		return nil
	}
	return fmt.Errorf("permission denied")
}

func (m *mockPermissionChecker) CheckRateLimit(_ string) error { return nil }
func (m *mockPermissionChecker) ResetTurnLimits()              {}

// --- Helpers ---

func newTestManifest(binary string) skills.SkillManifest {
	return skills.SkillManifest{
		Name:        "echo-skill",
		Version:     "1.0.0",
		Description: "A test echo skill",
		Types:       []skills.SkillType{skills.SkillTypeTool},
		Implementation: skills.ImplementationConfig{
			Backend:    skills.BackendProcess,
			Entrypoint: binary,
		},
		Permissions: []skills.Permission{skills.PermShellExec},
	}
}

// --- Tests ---

func TestProcessStartStop(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	// Verify it implements SkillBackend.
	var _ skills.SkillBackend = backend

	// Load should start the process and initialize.
	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)

	// After loading, should have tools from initialize response.
	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)
	assert.Equal(t, "echo", registeredTools[0].Name())
	assert.Equal(t, "Echoes back the input", registeredTools[0].Description())

	// After loading, should have hooks from initialize response.
	hooks := backend.Hooks()
	require.Contains(t, hooks, skills.HookOnBeforeToolCall)

	// Unload should send shutdown and stop the process.
	err = backend.Unload()
	require.NoError(t, err)

	// After unload, tools and hooks should be empty.
	assert.Empty(t, backend.Tools())
	assert.Empty(t, backend.Hooks())
}

func TestProcessToolExecute(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Get the echo tool.
	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)

	echoTool := registeredTools[0]
	assert.Equal(t, "echo", echoTool.Name())

	// Execute the tool with some input.
	input := json.RawMessage(`{"message":"hello world"}`)
	ctx := context.Background()
	result, err := echoTool.Execute(ctx, input)
	require.NoError(t, err)

	// The echo_skill responds with "echo: tool=<name> input=<raw>".
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "echo: tool=echo")
	assert.Contains(t, result.Content, "hello world")
}

func TestProcessHookHandle(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	hooks := backend.Hooks()
	require.Contains(t, hooks, skills.HookOnBeforeToolCall)

	handler := hooks[skills.HookOnBeforeToolCall]

	// Invoke the hook.
	hookResult, err := handler(skills.HookEvent{
		Phase:     skills.HookOnBeforeToolCall,
		SkillName: "echo-skill",
		Data:      map[string]any{"tool_name": "shell"},
		Ctx:       context.Background(),
	})
	require.NoError(t, err)

	// The echo_skill responds with cancel=false and empty modified.
	assert.False(t, hookResult.Cancel)
}

func TestProcessCrashRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping crash restart test in short mode")
	}

	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Verify tools are available before crash.
	require.Len(t, backend.Tools(), 1)

	// Kill the child process to simulate a crash.
	backend.killProcess()

	// Give the backend time to detect the crash and restart.
	time.Sleep(500 * time.Millisecond)

	// After restart, the process should be re-initialized.
	// Execute a tool to verify the process is alive.
	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)

	input := json.RawMessage(`{"message":"after restart"}`)
	ctx := context.Background()
	result, err := registeredTools[0].Execute(ctx, input)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "after restart")
}

func TestProcessTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	backend := NewProcessBackend(WithCallTimeout(100 * time.Millisecond))
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Call the slow method which sleeps for 5 seconds.
	// With a 100ms timeout, this should fail.
	ctx := context.Background()
	_, err = backend.call(ctx, "slow/method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestProcessLoadMissingEntrypoint(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	manifest := newTestManifest("")
	err := backend.Load(manifest, checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entrypoint")
}

func TestProcessLoadBadBinary(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	manifest := newTestManifest("/nonexistent/binary/path")
	err := backend.Load(manifest, checker)
	require.Error(t, err)
}

func TestProcessUnloadWithoutLoad(t *testing.T) {
	backend := NewProcessBackend()
	// Unloading without loading should not panic and return no error.
	err := backend.Unload()
	require.NoError(t, err)
}

func TestProcessToolInputSchema(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)

	// Verify the tool has a valid input schema.
	schema := registeredTools[0].InputSchema()
	require.NotNil(t, schema)

	var schemaMap map[string]interface{}
	err = json.Unmarshal(schema, &schemaMap)
	require.NoError(t, err)
	assert.Equal(t, "object", schemaMap["type"])
}

func TestParseHookPhase(t *testing.T) {
	tests := []struct {
		name     string
		expected skills.HookPhase
	}{
		{"OnActivate", skills.HookOnActivate},
		{"OnDeactivate", skills.HookOnDeactivate},
		{"OnConversationStart", skills.HookOnConversationStart},
		{"OnBeforePromptBuild", skills.HookOnBeforePromptBuild},
		{"OnBeforeToolCall", skills.HookOnBeforeToolCall},
		{"OnAfterToolResult", skills.HookOnAfterToolResult},
		{"OnAfterResponse", skills.HookOnAfterResponse},
		{"OnBeforeWikiSection", skills.HookOnBeforeWikiSection},
		{"OnSecurityScanComplete", skills.HookOnSecurityScanComplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseHookPhase(tt.name))
		})
	}

	// Unknown hook phase should return -1.
	assert.Equal(t, skills.HookPhase(-1), parseHookPhase("UnknownPhase"))
}

func TestProcessHookHandleNilCtx(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	hooks := backend.Hooks()
	require.Contains(t, hooks, skills.HookOnBeforeToolCall)

	handler := hooks[skills.HookOnBeforeToolCall]

	// Invoke the hook with a nil Ctx -- should not panic, uses context.Background().
	hookResult, err := handler(skills.HookEvent{
		Phase:     skills.HookOnBeforeToolCall,
		SkillName: "echo-skill",
		Data:      map[string]any{"tool_name": "shell"},
		Ctx:       nil,
	})
	require.NoError(t, err)
	assert.False(t, hookResult.Cancel)
}

func TestProcessCallNotRunning(t *testing.T) {
	backend := NewProcessBackend()

	// Calling without a running process should return an error.
	ctx := context.Background()
	_, err := backend.call(ctx, "test/method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "process not running")
}

func TestProcessCallWithContextDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping context deadline test in short mode")
	}

	backend := NewProcessBackend(WithCallTimeout(10 * time.Second))
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Use a context deadline shorter than the call timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = backend.call(ctx, "slow/method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestProcessToolExecuteOnDeadProcess(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)

	// Mark as stopped to prevent the crash monitor from restarting.
	backend.mu.Lock()
	backend.stopped = true
	close(backend.stopCh)
	backend.mu.Unlock()

	// Kill the process so execute will fail.
	backend.killProcess()
	time.Sleep(100 * time.Millisecond)

	// Nil out the cmd to simulate a dead process.
	backend.mu.Lock()
	backend.cmd = nil
	backend.stdin = nil
	backend.readCh = nil
	backend.mu.Unlock()

	// Try to execute a tool on the dead process.
	registeredTools := backend.Tools()
	require.Len(t, registeredTools, 1)

	ctx := context.Background()
	result, err := registeredTools[0].Execute(ctx, json.RawMessage(`{"message":"fail"}`))

	// Should get an error.
	require.Error(t, err)
	assert.True(t, result.IsError)
}

func TestProcessMultipleLoadUnload(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	// Load, use, unload cycle twice.
	for i := 0; i < 2; i++ {
		err := backend.Load(newTestManifest(echoSkillBinary), checker)
		require.NoError(t, err)

		registeredTools := backend.Tools()
		require.Len(t, registeredTools, 1)

		err = backend.Unload()
		require.NoError(t, err)
		assert.Empty(t, backend.Tools())
	}
}

func TestProcessLoadInitializeFails(t *testing.T) {
	// Use a binary that exits immediately without responding to initialize.
	// The "true" command on Unix exits immediately with code 0.
	backend := NewProcessBackend(WithCallTimeout(500 * time.Millisecond))
	checker := &mockPermissionChecker{allowAll: true}

	manifest := newTestManifest("/usr/bin/true")
	err := backend.Load(manifest, checker)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialize failed")
}

func TestProcessCallRPCError(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Call an unknown method -- the echo_skill responds with an RPC error.
	ctx := context.Background()
	_, err = backend.call(ctx, "unknown/method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method not found")
}

func TestProcessCallProcessClosedStdout(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)

	// Prevent crash monitor from restarting.
	backend.mu.Lock()
	backend.stopped = true
	close(backend.stopCh)
	backend.mu.Unlock()

	// Close stdin to make the child process exit, which closes stdout.
	backend.mu.Lock()
	backend.stdin.Close()
	backend.mu.Unlock()

	time.Sleep(100 * time.Millisecond)

	// Next call should detect closed stdout.
	ctx := context.Background()
	_, err = backend.call(ctx, "test/method", nil)
	require.Error(t, err)
}

func TestProcessCallWriteStdinError(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)

	// Prevent crash monitor from restarting.
	backend.mu.Lock()
	backend.stopped = true
	close(backend.stopCh)

	// Close stdin but keep the cmd non-nil so callLocked gets past the nil check.
	backend.stdin.Close()
	backend.mu.Unlock()

	// This should trigger a write error.
	ctx := context.Background()
	_, err = backend.call(ctx, "test/method", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write to process stdin")
}

func TestProcessHookHandleOnDeadProcess(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)

	// Capture the hook handler before killing.
	hooks := backend.Hooks()
	require.Contains(t, hooks, skills.HookOnBeforeToolCall)
	handler := hooks[skills.HookOnBeforeToolCall]

	// Prevent crash monitor from restarting.
	backend.mu.Lock()
	backend.stopped = true
	close(backend.stopCh)
	backend.mu.Unlock()

	// Kill and clean up the process.
	backend.mu.Lock()
	backend.killAndCleanupLocked()
	backend.mu.Unlock()

	// Calling the hook handler should return an error.
	_, err = handler(skills.HookEvent{
		Phase:     skills.HookOnBeforeToolCall,
		SkillName: "echo-skill",
		Data:      map[string]any{"tool_name": "shell"},
		Ctx:       context.Background(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hook/handle")
}

func TestProcessCallCreateRequestError(t *testing.T) {
	backend := NewProcessBackend()
	checker := &mockPermissionChecker{allowAll: true}

	err := backend.Load(newTestManifest(echoSkillBinary), checker)
	require.NoError(t, err)
	defer backend.Unload()

	// Passing a channel as params should trigger a marshal error in NewRequest.
	ctx := context.Background()
	_, err = backend.call(ctx, "test/method", make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create request")
}
