// Package process provides the external process skill backend. It starts a
// child process and communicates via JSON-RPC 2.0 over stdin/stdout.
//
// The ProcessBackend implements skills.SkillBackend. On Load, it starts the
// child process, sends an "initialize" request, and populates tools and hooks
// from the response. Tool execution and hook handling are forwarded as
// JSON-RPC calls. On Unload, it sends a "shutdown" request and kills the process.
//
// Crash detection: a goroutine monitors the child process via Wait(). If the
// process exits unexpectedly, the backend automatically restarts and
// re-initializes with exponential backoff.
package process

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julianshen/rubichan/internal/commands"
	"github.com/julianshen/rubichan/internal/skills"
	"github.com/julianshen/rubichan/internal/tools"
)

// Default timeout for RPC calls.
const defaultCallTimeout = 10 * time.Second

// readResult carries a line read by the dedicated reader goroutine.
type readResult struct {
	line string
	ok   bool
}

// Option configures a ProcessBackend.
type Option func(*ProcessBackend)

// WithCallTimeout sets the timeout for individual RPC calls.
func WithCallTimeout(d time.Duration) Option {
	return func(b *ProcessBackend) {
		b.callTimeout = d
	}
}

// ProcessBackend implements skills.SkillBackend for external process skills.
// It starts a child process and communicates via JSON-RPC 2.0 over stdin/stdout.
type ProcessBackend struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	callTimeout time.Duration
	nextID      atomic.Int64

	// readCh receives lines from the dedicated reader goroutine.
	readCh chan readResult

	manifest skills.SkillManifest
	checker  skills.PermissionChecker

	registeredTools []processTool
	registeredHooks map[skills.HookPhase]skills.HookHandler

	// stopCh signals the crash monitor to stop.
	stopCh chan struct{}
	// stopped indicates the backend has been unloaded.
	stopped bool
	// generation tracks restart cycles to prevent duplicate monitor goroutines.
	generation int64
}

// compile-time check: ProcessBackend implements skills.SkillBackend.
var _ skills.SkillBackend = (*ProcessBackend)(nil)

// NewProcessBackend creates a new process backend with the given options.
func NewProcessBackend(opts ...Option) *ProcessBackend {
	b := &ProcessBackend{
		callTimeout:     defaultCallTimeout,
		registeredHooks: make(map[skills.HookPhase]skills.HookHandler),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Load implements skills.SkillBackend. It starts the child process and sends
// an "initialize" request to get tool and hook declarations.
func (b *ProcessBackend) Load(manifest skills.SkillManifest, checker skills.PermissionChecker) error {
	if manifest.Implementation.Entrypoint == "" {
		return fmt.Errorf("load process: entrypoint is required")
	}

	// Validate entrypoint is an absolute path to prevent executing arbitrary binaries.
	if !filepath.IsAbs(manifest.Implementation.Entrypoint) {
		return fmt.Errorf("load process: entrypoint must be an absolute path, got %q", manifest.Implementation.Entrypoint)
	}

	b.manifest = manifest
	b.checker = checker
	b.stopped = false
	b.stopCh = make(chan struct{})

	if err := b.startProcess(); err != nil {
		return err
	}

	if err := b.initialize(); err != nil {
		b.mu.Lock()
		b.killAndCleanupLocked()
		b.mu.Unlock()
		return fmt.Errorf("load process %q: initialize failed: %w", manifest.Name, err)
	}

	// Start crash monitor goroutine with initial generation.
	go b.monitorProcess(b.generation)

	return nil
}

// Tools implements skills.SkillBackend. Returns tools registered from the
// initialize response.
func (b *ProcessBackend) Tools() []tools.Tool {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := make([]tools.Tool, len(b.registeredTools))
	for i := range b.registeredTools {
		result[i] = &b.registeredTools[i]
	}
	return result
}

// Hooks implements skills.SkillBackend. Returns a copy of hook handlers
// registered from the initialize response.
func (b *ProcessBackend) Hooks() map[skills.HookPhase]skills.HookHandler {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := make(map[skills.HookPhase]skills.HookHandler, len(b.registeredHooks))
	for k, v := range b.registeredHooks {
		result[k] = v
	}
	return result
}

// Commands returns nil — process skills do not provide slash commands.
func (b *ProcessBackend) Commands() []commands.SlashCommand { return nil }

// Unload implements skills.SkillBackend. Sends a "shutdown" request and
// stops the child process.
func (b *ProcessBackend) Unload() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cmd == nil {
		return nil
	}

	b.stopped = true

	// Signal the crash monitor to stop.
	close(b.stopCh)

	// Try to send shutdown gracefully.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b.callLocked(ctx, "shutdown", nil) //nolint: errcheck // best effort

	b.killAndCleanupLocked()

	b.registeredTools = nil
	b.registeredHooks = make(map[skills.HookPhase]skills.HookHandler)

	return nil
}

// call sends a JSON-RPC request and waits for the response with timeout.
// This is the public-facing method (acquires lock).
func (b *ProcessBackend) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.callLocked(ctx, method, params)
}

// callLocked sends a JSON-RPC request and waits for the response.
// Caller must hold b.mu.
func (b *ProcessBackend) callLocked(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	if b.cmd == nil {
		return nil, fmt.Errorf("process not running")
	}

	id := int(b.nextID.Add(1))

	req, err := NewRequest(id, method, params)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Write request to stdin.
	if _, err := fmt.Fprintf(b.stdin, "%s\n", data); err != nil {
		return nil, fmt.Errorf("write to process stdin: %w", err)
	}

	// Apply call timeout.
	deadline := b.callTimeout
	if d, ok := ctx.Deadline(); ok {
		remaining := time.Until(d)
		if remaining < deadline {
			deadline = remaining
		}
	}

	// Read response from the dedicated reader goroutine's channel.
	select {
	case sr := <-b.readCh:
		if !sr.ok {
			return nil, fmt.Errorf("process closed stdout")
		}

		resp, err := DecodeResponse([]byte(sr.line))
		if err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		if resp.Error != nil {
			return nil, resp.Error
		}

		return resp, nil

	case <-time.After(deadline):
		return nil, fmt.Errorf("call %q: timeout after %v", method, deadline)
	}
}

// startProcess starts the child process, sets up stdin/stdout pipes, and
// spawns a dedicated reader goroutine that owns the stdout scanner for
// the lifetime of this process instance. The reader sends lines on b.readCh.
func (b *ProcessBackend) startProcess() error {
	cmd := exec.Command(b.manifest.Implementation.Entrypoint)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("start process: pipe setup: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("start process: pipe setup: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process %q: %w", b.manifest.Implementation.Entrypoint, err)
	}

	b.cmd = cmd
	b.stdin = stdin
	b.readCh = make(chan readResult, 1)

	// Dedicated reader goroutine — owns the scanner and terminates when
	// stdout is closed (process killed or exited).
	go func(ch chan<- readResult) {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			ch <- readResult{line: scanner.Text(), ok: true}
		}
		ch <- readResult{ok: false}
	}(b.readCh)

	return nil
}

// initialize sends the initialize request and populates tools and hooks.
func (b *ProcessBackend) initialize() error {
	manifestData := map[string]any{
		"name":    b.manifest.Name,
		"version": b.manifest.Version,
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.callTimeout)
	defer cancel()

	resp, err := b.callLocked(ctx, "initialize", manifestData)
	if err != nil {
		return err
	}

	// Parse initialize result for tools and hooks.
	var initResult struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
		Hooks []string `json:"hooks"`
	}
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	// Register tools.
	b.registeredTools = make([]processTool, len(initResult.Tools))
	for i, td := range initResult.Tools {
		b.registeredTools[i] = processTool{
			name:        td.Name,
			description: td.Description,
			inputSchema: td.InputSchema,
			backend:     b,
		}
	}

	// Register hooks.
	b.registeredHooks = make(map[skills.HookPhase]skills.HookHandler)
	for _, hookName := range initResult.Hooks {
		phase := parseHookPhase(hookName)
		if phase < 0 {
			continue
		}
		// Capture phase in closure.
		p := phase
		b.registeredHooks[p] = func(event skills.HookEvent) (skills.HookResult, error) {
			return b.handleHook(event, p)
		}
	}

	return nil
}

// handleHook sends a hook/handle RPC call and returns the result.
func (b *ProcessBackend) handleHook(event skills.HookEvent, phase skills.HookPhase) (skills.HookResult, error) {
	ctx := event.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := b.call(ctx, "hook/handle", map[string]any{
		"phase": phase.String(),
		"data":  event.Data,
	})
	if err != nil {
		return skills.HookResult{}, fmt.Errorf("hook/handle: %w", err)
	}

	var hookResp struct {
		Modified map[string]any `json:"modified"`
		Cancel   bool           `json:"cancel"`
	}
	if err := json.Unmarshal(resp.Result, &hookResp); err != nil {
		return skills.HookResult{}, fmt.Errorf("parse hook result: %w", err)
	}

	return skills.HookResult{
		Modified: hookResp.Modified,
		Cancel:   hookResp.Cancel,
	}, nil
}

// monitorProcess watches for child process exit and restarts if unexpected.
// The gen parameter prevents stale monitors from racing with newer ones:
// only the goroutine whose generation matches b.generation will restart.
func (b *ProcessBackend) monitorProcess(gen int64) {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Wait for the process to exit (does not need the lock — we captured cmd).
	cmd.Wait() //nolint: errcheck

	// Check if we were intentionally stopped.
	select {
	case <-b.stopCh:
		return
	default:
	}

	// Verify we are still the active monitor (generation hasn't changed).
	b.mu.Lock()
	if b.generation != gen || b.stopped {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	// Process crashed unexpectedly -- restart with backoff.
	b.restart(gen)
}

// restart attempts to restart the child process with exponential backoff.
func (b *ProcessBackend) restart(gen int64) {
	backoff := 50 * time.Millisecond
	maxBackoff := 5 * time.Second
	maxAttempts := 5

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-b.stopCh:
			return
		case <-time.After(backoff):
		}

		b.mu.Lock()
		if b.stopped || b.generation != gen {
			b.mu.Unlock()
			return
		}

		// Clean up old process state.
		b.cmd = nil
		b.stdin = nil
		b.readCh = nil

		if err := b.startProcess(); err != nil {
			b.mu.Unlock()
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		if err := b.initialize(); err != nil {
			b.killAndCleanupLocked()
			b.mu.Unlock()
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Bump generation so the old monitor cannot race.
		b.generation++
		newGen := b.generation
		b.mu.Unlock()

		// Successfully restarted. Start monitoring again with new generation.
		go b.monitorProcess(newGen)
		return
	}
}

// killProcess kills the child process. This is exposed for testing crash scenarios.
func (b *ProcessBackend) killProcess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill() //nolint: errcheck
	}
}

// killAndCleanupLocked kills the child process. Caller must hold b.mu.
// Killing the process closes stdout, which terminates the reader goroutine.
func (b *ProcessBackend) killAndCleanupLocked() {
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill() //nolint: errcheck
		b.cmd.Wait()         //nolint: errcheck
	}
	if b.stdin != nil {
		b.stdin.Close() //nolint: errcheck
	}
	b.cmd = nil
	b.stdin = nil
	b.readCh = nil
}

// --- processTool ---

// processTool implements tools.Tool by forwarding execution to the child process.
type processTool struct {
	name        string
	description string
	inputSchema json.RawMessage
	backend     *ProcessBackend
}

// compile-time check: processTool implements tools.Tool.
var _ tools.Tool = (*processTool)(nil)

// Name implements tools.Tool.
func (pt *processTool) Name() string { return pt.name }

// Description implements tools.Tool.
func (pt *processTool) Description() string { return pt.description }

// InputSchema implements tools.Tool.
func (pt *processTool) InputSchema() json.RawMessage { return pt.inputSchema }

// Execute implements tools.Tool. It sends a tool/execute RPC call to the child
// process and returns the result.
func (pt *processTool) Execute(ctx context.Context, input json.RawMessage) (tools.ToolResult, error) {
	resp, err := pt.backend.call(ctx, "tool/execute", map[string]any{
		"name":  pt.name,
		"input": input,
	})
	if err != nil {
		return tools.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var toolResp struct {
		Content string `json:"content"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(resp.Result, &toolResp); err != nil {
		return tools.ToolResult{IsError: true, Content: err.Error()}, fmt.Errorf("parse tool result: %w", err)
	}

	return tools.ToolResult{
		Content: toolResp.Content,
		IsError: toolResp.IsError,
	}, nil
}

// --- Hook phase parsing ---

// parseHookPhase converts a hook phase string name to its constant.
// Returns -1 if the name is not recognized.
func parseHookPhase(name string) skills.HookPhase {
	switch name {
	case "OnActivate":
		return skills.HookOnActivate
	case "OnDeactivate":
		return skills.HookOnDeactivate
	case "OnConversationStart":
		return skills.HookOnConversationStart
	case "OnBeforePromptBuild":
		return skills.HookOnBeforePromptBuild
	case "OnBeforeToolCall":
		return skills.HookOnBeforeToolCall
	case "OnAfterToolResult":
		return skills.HookOnAfterToolResult
	case "OnAfterResponse":
		return skills.HookOnAfterResponse
	case "OnBeforeWikiSection":
		return skills.HookOnBeforeWikiSection
	case "OnSecurityScanComplete":
		return skills.HookOnSecurityScanComplete
	default:
		return -1
	}
}
