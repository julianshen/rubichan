package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianshen/rubichan/internal/config"
	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/internal/tools"
	"github.com/julianshen/rubichan/pkg/agentsdk"
	"github.com/stretchr/testify/require"
)

// fakeConcurrencySafeTool is a minimal tool that implements the
// agentsdk.Tool interface + ConcurrencySafeTool marker. It sleeps for
// execDelay before returning returnText to simulate I/O latency. If
// onStart is non-nil, it is invoked on the very first line of Execute;
// onFinish fires just before Execute returns. Both hooks are used by
// timing-sensitive tests to synchronize with the tool's lifecycle.
type fakeConcurrencySafeTool struct {
	name       string
	execDelay  time.Duration
	called     atomic.Int32
	returnText string
	onStart    func()
	onFinish   func()
}

func (t *fakeConcurrencySafeTool) Name() string                 { return t.name }
func (t *fakeConcurrencySafeTool) Description() string          { return "" }
func (t *fakeConcurrencySafeTool) InputSchema() json.RawMessage { return nil }
func (t *fakeConcurrencySafeTool) IsConcurrencySafe() bool      { return true }
func (t *fakeConcurrencySafeTool) Execute(ctx context.Context, _ json.RawMessage) (agentsdk.ToolResult, error) {
	t.called.Add(1)
	if t.onStart != nil {
		t.onStart()
	}
	select {
	case <-time.After(t.execDelay):
	case <-ctx.Done():
		return agentsdk.ToolResult{}, ctx.Err()
	}
	if t.onFinish != nil {
		t.onFinish()
	}
	return agentsdk.ToolResult{Content: t.returnText}, nil
}

// runnerFromTool builds an execFn that invokes tool.Execute directly,
// applies the result cap, and wraps the outcome in a toolExecResult.
// This mirrors what the agent does via executeSingleTool, but without
// the pipeline — sufficient for executor-level unit tests.
func runnerFromTool(tool agentsdk.Tool) toolExecFn {
	return func(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
		res, err := tool.Execute(ctx, tc.Input)
		if err != nil {
			return toolErrorResult(tc, err.Error())
		}
		res = applyResultCap(tool, res)
		return toolExecResult{
			toolUseID: tc.ID,
			content:   res.Content,
			isError:   res.IsError,
			event:     makeToolResultEvent(tc.ID, tc.Name, res.Content, res.Display(), res.IsError),
		}
	}
}

// newExecutorWithTools wires a streamingToolExecutor whose run dispatches
// to one of the given fakes by tool name — the common test fixture for
// scenarios that exercise multiple tools in one executor.
func newExecutorWithTools(maxParallel int, byName map[string]agentsdk.Tool) *streamingToolExecutor {
	run := func(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
		return runnerFromTool(byName[tc.Name])(ctx, tc)
	}
	return newStreamingToolExecutor(maxParallel, run)
}

func TestStreamingExecutorDispatchesSafeToolsInParallel(t *testing.T) {
	t.Parallel()
	// Two slow concurrency-safe tools. Sequential execution would take
	// ~400ms; parallel dispatch should complete in ~200ms. The 350ms
	// budget gives 150ms headroom on the parallel path while still
	// catching a sequential regression (which would take ≥400ms).
	tool := &fakeConcurrencySafeTool{
		name:       "read_file",
		execDelay:  200 * time.Millisecond,
		returnText: "ok",
	}
	ex := newStreamingToolExecutor(2, runnerFromTool(tool))
	ctx := context.Background()

	start := time.Now()
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "a", Name: "read_file", Input: json.RawMessage(`{}`)})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "b", Name: "read_file", Input: json.RawMessage(`{}`)})
	results := ex.Drain()
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if elapsed > 350*time.Millisecond {
		t.Fatalf("tools ran sequentially (%v) — expected parallel dispatch", elapsed)
	}
	if tool.called.Load() != 2 {
		t.Fatalf("want 2 invocations, got %d", tool.called.Load())
	}
}

func TestStreamingExecutorPreservesDispatchOrder(t *testing.T) {
	t.Parallel()
	// Order-preserving test needs per-tool delays. We use a router
	// runner that dispatches to different fake tools based on tc.Name.
	fast := &fakeConcurrencySafeTool{name: "fast", execDelay: 10 * time.Millisecond, returnText: "fast"}
	slow := &fakeConcurrencySafeTool{name: "slow", execDelay: 80 * time.Millisecond, returnText: "slow"}
	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"fast": fast, "slow": slow})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "first", Name: "slow"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "second", Name: "fast"})
	results := ex.Drain()

	if len(results) != 2 || results[0].toolUseID != "first" || results[1].toolUseID != "second" {
		t.Fatalf("order not preserved: %+v", results)
	}
	if results[0].content != "slow" || results[1].content != "fast" {
		t.Fatalf("content mismatch: %+v", results)
	}
}

func TestStreamingExecutorCancelProducesErrorResults(t *testing.T) {
	t.Parallel()
	tool := &fakeConcurrencySafeTool{name: "read_file", execDelay: 50 * time.Millisecond, returnText: "ok"}
	ex := newStreamingToolExecutor(2, runnerFromTool(tool))
	ctx, cancel := context.WithCancel(context.Background())
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "a", Name: "read_file"})
	cancel()
	// Dispatch after cancel: must produce an error result, not hang.
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "b", Name: "read_file"})
	results := ex.Drain()
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	var bResult *toolExecResult
	for i := range results {
		if results[i].toolUseID == "b" {
			bResult = &results[i]
		}
	}
	if bResult == nil || !bResult.isError {
		t.Fatalf("dispatch-after-cancel should produce an error result, got %+v", bResult)
	}
}

func TestStreamingExecutorClampsMaxParallel(t *testing.T) {
	t.Parallel()
	// maxParallel <= 0 must be clamped to 1 — tools still run, just
	// serially. Without clamping, the sem channel would be nil and
	// Dispatch would hang.
	tool := &fakeConcurrencySafeTool{name: "read_file", execDelay: 5 * time.Millisecond, returnText: "ok"}
	ex := newStreamingToolExecutor(0, runnerFromTool(tool))
	ctx := context.Background()
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "a", Name: "read_file"})
	results := ex.Drain()
	if len(results) != 1 || results[0].content != "ok" {
		t.Fatalf("clamp failure: %+v", results)
	}
}

func TestStreamingExecutorCancelWhileWaitingForSlot(t *testing.T) {
	t.Parallel()
	// maxParallel=1 plus a slow first dispatch forces the second
	// dispatch to block on the semaphore; cancelling the context
	// should unblock it with an error result.
	tool := &fakeConcurrencySafeTool{name: "read_file", execDelay: 200 * time.Millisecond, returnText: "ok"}
	ex := newStreamingToolExecutor(1, runnerFromTool(tool))
	ctx, cancel := context.WithCancel(context.Background())
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "a", Name: "read_file"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "b", Name: "read_file"})
	// Give the second dispatch a moment to park on the semaphore.
	time.Sleep(20 * time.Millisecond)
	cancel()
	results := ex.Drain()
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	// b either errors from slot-wait cancel or runs with cancelled ctx;
	// either way it should be an error result, not a hang.
	var bResult *toolExecResult
	for i := range results {
		if results[i].toolUseID == "b" {
			bResult = &results[i]
		}
	}
	if bResult == nil || !bResult.isError {
		t.Fatalf("want b to be an error result after cancel, got %+v", bResult)
	}
}

func TestIsStreamingEligible(t *testing.T) {
	t.Parallel()
	safe := &fakeConcurrencySafeTool{name: "read_file"}

	if !isStreamingEligible(safe, AutoApproved) {
		t.Errorf("safe+auto-approved should be eligible")
	}
	if !isStreamingEligible(safe, TrustRuleApproved) {
		t.Errorf("safe+trust-rule-approved should be eligible")
	}
	if isStreamingEligible(safe, ApprovalRequired) {
		t.Errorf("safe+approval-required should NOT be eligible")
	}
	if isStreamingEligible(safe, AutoDenied) {
		t.Errorf("safe+auto-denied should NOT be eligible")
	}

	// Tool without the marker must never be eligible, even with
	// auto-approval.
	unsafe := &unmarkedTool{name: "write_file"}
	if isStreamingEligible(unsafe, AutoApproved) {
		t.Errorf("unmarked tool should NOT be eligible")
	}
}

// unmarkedTool implements agentsdk.Tool but NOT ConcurrencySafeTool.
type unmarkedTool struct{ name string }

func (u *unmarkedTool) Name() string                 { return u.name }
func (u *unmarkedTool) Description() string          { return "" }
func (u *unmarkedTool) InputSchema() json.RawMessage { return nil }
func (u *unmarkedTool) Execute(context.Context, json.RawMessage) (agentsdk.ToolResult, error) {
	return agentsdk.ToolResult{}, nil
}

// TestExecuteToolsAllStreamedFastPath drives executeTools directly with
// a pendingTools slice where every entry has a pre-populated
// streamedResults entry. It covers:
//   - the merge branch at agent.go:1651-1659 (streamed result moved into
//     results[i] + tool_call emitted)
//   - the all-streamed fast path at agent.go:1670-1679 (len(planned)==0,
//     jump straight to result emission without partition/execute dance)
//
// Verified: Execute is never called on the registered tool, and both
// tool_call + tool_result events are emitted per pending tool.
func TestExecuteToolsAllStreamedFastPath(t *testing.T) {
	t.Parallel()
	// Tool registered with executeFn that panics if called — any streamed
	// result must NOT re-run this tool.
	neverRun := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			t.Errorf("tool should not be executed when streamedResults already contains its result")
			return tools.ToolResult{Content: "unexpected"}, nil
		},
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(neverRun))

	cfg := config.DefaultConfig()
	// Approval checker required so executeTools takes the main path
	// (not the executePlannedToolsSequential fallback).
	a := New(&mockProvider{}, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	pendingTools := []provider.ToolUseBlock{
		{ID: "a", Name: "tool_a", Input: json.RawMessage(`{}`)},
		{ID: "b", Name: "tool_a", Input: json.RawMessage(`{}`)},
	}
	streamed := map[string]toolExecResult{
		"a": {
			toolUseID: "a",
			content:   "result_a",
			event:     makeToolResultEvent("a", "tool_a", "result_a", "", false),
		},
		"b": {
			toolUseID: "b",
			content:   "result_b",
			event:     makeToolResultEvent("b", "tool_a", "result_b", "", false),
		},
	}

	ch := make(chan TurnEvent, 16)
	cancelled := a.executeTools(context.Background(), ch, pendingTools, streamed)
	require.False(t, cancelled)

	var toolCalls, toolResults []TurnEvent
	for len(ch) > 0 {
		ev := <-ch
		switch ev.Type {
		case "tool_call":
			toolCalls = append(toolCalls, ev)
		case "tool_result":
			toolResults = append(toolResults, ev)
		}
	}
	require.Len(t, toolCalls, 2, "expected 2 tool_call events")
	require.Len(t, toolResults, 2, "expected 2 tool_result events")

	// Results must be emitted in original pendingTools order.
	require.Equal(t, "a", toolResults[0].ToolResult.ID)
	require.Equal(t, "result_a", toolResults[0].ToolResult.Content)
	require.Equal(t, "b", toolResults[1].ToolResult.ID)
	require.Equal(t, "result_b", toolResults[1].ToolResult.Content)

	// Conversation must contain the tool_result blocks.
	msgs := a.conversation.Messages()
	var toolResultBlocks int
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "tool_result" {
				toolResultBlocks++
			}
		}
	}
	require.Equal(t, 2, toolResultBlocks, "conversation should contain 2 tool_result blocks")
}

// TestExecutePlannedToolsSequentialWithStreamedResults covers the
// streamedResults merge branch in executePlannedToolsSequential (the
// path taken when approvalChecker == nil and streaming dispatch already
// produced a result for the tool). Verifies the cached result is
// emitted without re-invoking the tool's Execute.
func TestExecutePlannedToolsSequentialWithStreamedResults(t *testing.T) {
	t.Parallel()
	neverRun := &mockTool{
		name:        "tool_a",
		description: "tool A",
		inputSchema: json.RawMessage(`{"type":"object"}`),
		executeFn: func(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
			t.Errorf("tool should not be executed when streamedResults already contains its result")
			return tools.ToolResult{Content: "unexpected"}, nil
		},
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(neverRun))

	cfg := config.DefaultConfig()
	// approvalChecker == nil => executeTools falls through to
	// executePlannedToolsSequential.
	a := New(&mockProvider{}, reg, autoApprove, cfg)

	pendingTools := []provider.ToolUseBlock{
		{ID: "t1", Name: "tool_a", Input: json.RawMessage(`{}`)},
	}
	streamed := map[string]toolExecResult{
		"t1": {
			toolUseID: "t1",
			content:   "streamed_result",
			event:     makeToolResultEvent("t1", "tool_a", "streamed_result", "", false),
		},
	}

	ch := make(chan TurnEvent, 8)
	cancelled := a.executeTools(context.Background(), ch, pendingTools, streamed)
	require.False(t, cancelled)

	var toolCall, toolResult *TurnEvent
	for len(ch) > 0 {
		ev := <-ch
		switch ev.Type {
		case "tool_call":
			e := ev
			toolCall = &e
		case "tool_result":
			e := ev
			toolResult = &e
		}
	}
	require.NotNil(t, toolCall, "expected a tool_call event")
	require.NotNil(t, toolResult, "expected a tool_result event")
	require.Equal(t, "t1", toolCall.ToolCall.ID)
	require.Equal(t, "t1", toolResult.ToolResult.ID)
	require.Equal(t, "streamed_result", toolResult.ToolResult.Content)
	require.False(t, toolResult.ToolResult.IsError)

	// Conversation contains the tool_result block.
	msgs := a.conversation.Messages()
	var toolResultBlocks int
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "tool_result" {
				toolResultBlocks++
			}
		}
	}
	require.Equal(t, 1, toolResultBlocks)
}

// TestRunLoopWiresStreamingToolsIntoTurnResults is a wiring test: it
// registers a fake concurrency-safe tool, drives the agent through a
// turn that emits a tool_use for it, and confirms via atomic counter
// that the tool was invoked exactly once and the result was emitted
// to the event channel. It proves end-to-end wiring (tool called,
// result emitted), but does NOT prove streaming-during-response
// timing — see TestRunLoopDispatchesStreamingToolDuringStream for
// that guarantee.
func TestRunLoopWiresStreamingToolsIntoTurnResults(t *testing.T) {
	t.Parallel()
	tool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  5 * time.Millisecond,
		returnText: "file contents",
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))

	// Two-turn dynamic provider: first turn emits a tool_use, second
	// acknowledges the result and stops.
	dmp := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID:    "call-1",
					Name:  "fake_read",
					Input: json.RawMessage(`{}`),
				}},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "done"},
				{Type: "stop"},
			},
		},
	}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	a := New(dmp, reg, autoApprove, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Turn(ctx, "read the file")
	require.NoError(t, err)
	var toolResults, doneCount int
	for ev := range events {
		switch ev.Type {
		case "tool_result":
			toolResults++
		case "done":
			doneCount++
		case "error":
			if ev.Error != nil {
				t.Logf("turn error event: %v", ev.Error)
			}
		}
	}
	if tool.called.Load() != 1 {
		t.Fatalf("want tool called exactly once, got %d", tool.called.Load())
	}
	if toolResults != 1 {
		t.Fatalf("want 1 tool_result event, got %d", toolResults)
	}
	if doneCount != 1 {
		t.Fatalf("want 1 done event, got %d", doneCount)
	}
}

// blockingStreamProvider is a minimal provider whose first Stream()
// emits two tool_use events back-to-back and then blocks until the
// first tool has started executing (waitForTool is closed) before
// emitting the stop event. This creates a hard liveness dependency:
// the stream only completes if the agent dispatches the first tool
// concurrently with the stream. If streaming dispatch regresses and
// tools run only post-stream, the provider goroutine blocks on
// waitForTool forever — the test detects that via context timeout.
//
// Subsequent Stream() calls (the agent's follow-up turn after tool
// results) simply emit a text_delta + stop.
type blockingStreamProvider struct {
	waitForTool <-chan struct{}
	callIdx     int
}

func (p *blockingStreamProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	idx := p.callIdx
	p.callIdx++
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		if idx == 0 {
			// First tool_use — agent stores as currentTool.
			ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
				ID: "call-1", Name: "fake_read", Input: json.RawMessage(`{}`),
			}}
			// Second tool_use — triggers finalizeTool on the first,
			// which dispatches it via streamingToolExecutor.
			ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
				ID: "call-2", Name: "fake_read", Input: json.RawMessage(`{}`),
			}}
			// Block until the first tool has actually started
			// executing. Without streaming dispatch, this never
			// happens and the test hits its context timeout.
			<-p.waitForTool
			ch <- provider.StreamEvent{Type: "stop"}
			return
		}
		ch <- provider.StreamEvent{Type: "text_delta", Text: "done"}
		ch <- provider.StreamEvent{Type: "stop"}
	}()
	return ch, nil
}

// singleToolBlockingProvider emits ONE tool_use (with full Input) and
// blocks on waitForTool before emitting stop. With the old behaviour —
// finalizeTool runs only on the NEXT tool_use or at stream end — the
// single tool was never dispatched mid-stream and the provider
// deadlocked. With the fix (immediate finalize on tool_use arrival)
// the tool runs as soon as its event is seen.
type singleToolBlockingProvider struct {
	waitForTool <-chan struct{}
	callIdx     int
}

func (p *singleToolBlockingProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	idx := p.callIdx
	p.callIdx++
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		if idx == 0 {
			ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
				ID: "call-solo", Name: "fake_read", Input: json.RawMessage(`{"path":"/tmp/x"}`),
			}}
			// content_block_stop is the finalize signal. Without it,
			// the old "finalize on next tool_use or stream end" path
			// would block on waitForTool forever (stop can't fire
			// until the tool runs, and the tool can't run until
			// dispatch happens).
			ch <- provider.StreamEvent{Type: agentsdk.EventContentBlockStop}
			<-p.waitForTool
			ch <- provider.StreamEvent{Type: "stop"}
			return
		}
		ch <- provider.StreamEvent{Type: "text_delta", Text: "done"}
		ch <- provider.StreamEvent{Type: "stop"}
	}()
	return ch, nil
}

// TestRunLoopDispatchesSingleToolResponseDuringStream proves that a
// response with ONE tool_use block dispatches the tool mid-stream.
// The provider deadlocks on waitForTool until the tool's Execute has
// started, so the test only completes if content_block_stop triggers
// finalizeTool (and thus streaming dispatch) before message_stop.
func TestRunLoopDispatchesSingleToolResponseDuringStream(t *testing.T) {
	t.Parallel()
	toolRan := make(chan struct{})
	var once sync.Once
	tool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  1 * time.Millisecond,
		returnText: "file contents",
		onStart:    func() { once.Do(func() { close(toolRan) }) },
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))

	prov := &singleToolBlockingProvider{waitForTool: toolRan}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	a := New(prov, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Turn(ctx, "read the file")
	require.NoError(t, err)
	for ev := range events {
		_ = ev
	}

	select {
	case <-toolRan:
		// Ok — tool dispatched during stream.
	default:
		t.Fatal("tool never ran — single-tool streaming dispatch regression")
	}
	if got := tool.called.Load(); got < 1 {
		t.Fatalf("want tool called at least once, got %d", got)
	}
}

// multiToolBlockingProvider emits two tool_use blocks each followed
// by a content_block_stop marker, blocking on waitForSecondTool before
// emitting stop. The second tool MUST dispatch mid-stream for the
// provider to unblock, proving the multi-tool content_block_stop
// path correctly finalizes each tool as its block ends (not just the
// last one at stream end).
type multiToolBlockingProvider struct {
	waitForSecondTool <-chan struct{}
	callIdx           int
}

func (p *multiToolBlockingProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	idx := p.callIdx
	p.callIdx++
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		if idx == 0 {
			ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
				ID: "m-1", Name: "fake_read", Input: json.RawMessage(`{"p":1}`),
			}}
			ch <- provider.StreamEvent{Type: agentsdk.EventContentBlockStop}
			ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
				ID: "m-2", Name: "fake_read", Input: json.RawMessage(`{"p":2}`),
			}}
			ch <- provider.StreamEvent{Type: agentsdk.EventContentBlockStop}
			<-p.waitForSecondTool
			ch <- provider.StreamEvent{Type: "stop"}
			return
		}
		ch <- provider.StreamEvent{Type: "text_delta", Text: "done"}
		ch <- provider.StreamEvent{Type: "stop"}
	}()
	return ch, nil
}

// TestRunLoopDispatchesMultiToolWithContentBlockStop verifies that when
// a response carries multiple tool_use blocks each followed by a
// content_block_stop marker, every tool is dispatched mid-stream. The
// provider deadlocks on waitForSecondTool until the SECOND tool's
// Execute has started, so finalizing only the first tool on
// content_block_stop would still hit the timeout.
func TestRunLoopDispatchesMultiToolWithContentBlockStop(t *testing.T) {
	t.Parallel()
	var secondToolStartedOnce sync.Once
	secondToolStarted := make(chan struct{})
	var startCount atomic.Int32
	tool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  1 * time.Millisecond,
		returnText: "ok",
		onStart: func() {
			if startCount.Add(1) == 2 {
				secondToolStartedOnce.Do(func() { close(secondToolStarted) })
			}
		},
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))

	prov := &multiToolBlockingProvider{waitForSecondTool: secondToolStarted}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	a := New(prov, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Turn(ctx, "read two files")
	require.NoError(t, err)
	for ev := range events {
		_ = ev
	}
	if got := tool.called.Load(); got != 2 {
		t.Fatalf("want both tools called, got %d invocations", got)
	}
}

// TestRunLoopDispatchesStreamingToolDuringStream genuinely proves
// that concurrency-safe tools are dispatched during the model stream,
// not after it. The blocking provider refuses to emit stop until the
// tool's Execute has started; if streaming dispatch is wired correctly
// the tool runs mid-stream and the turn completes, otherwise the
// provider deadlocks and the context times out.
func TestRunLoopDispatchesStreamingToolDuringStream(t *testing.T) {
	t.Parallel()
	toolRan := make(chan struct{})
	var once sync.Once
	tool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  1 * time.Millisecond,
		returnText: "file contents",
		onStart:    func() { once.Do(func() { close(toolRan) }) },
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))

	prov := &blockingStreamProvider{waitForTool: toolRan}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	// Streaming dispatch requires a non-nil approvalChecker returning
	// an auto-approval verdict (see agent.go:1296-1305).
	a := New(prov, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Turn(ctx, "read the file")
	require.NoError(t, err)
	for ev := range events {
		_ = ev
	}

	// toolRan must be closed. If the stream completed without the
	// tool ever executing (dispatch regression), the channel would
	// still be open — but the provider would have deadlocked before
	// ever emitting stop, so in practice we'd fail via context
	// timeout above, not here. This select is defence-in-depth.
	select {
	case <-toolRan:
		// Ok — tool was dispatched and started during the stream.
	default:
		t.Fatal("tool never ran — streaming dispatch regression")
	}
	if got := tool.called.Load(); got < 1 {
		t.Fatalf("want tool called at least once, got %d", got)
	}
}

// plainEmitter is a minimal eventEmitter for unit tests that drive
// surfaceStreamedResults without a full Agent. It writes to the
// channel directly — the context.Done branch is exercised by the
// integration test in TestTurnUnblocksWhenConsumerCancelsCtx.
type plainEmitter struct{}

func (plainEmitter) emit(_ context.Context, ch chan<- TurnEvent, ev TurnEvent) {
	ch <- ev
}

// TestSurfaceStreamedResultsEmitsCallAndResultInOrder verifies the
// happy-path: every drained result produces a tool_call followed by
// its cached tool_result on the channel.
func TestSurfaceStreamedResultsEmitsCallAndResultInOrder(t *testing.T) {
	t.Parallel()
	ch := make(chan TurnEvent, 16)
	pending := []provider.ToolUseBlock{
		{ID: "a", Name: "read_file", Input: json.RawMessage(`{"p":"/tmp/a"}`)},
		{ID: "b", Name: "read_file", Input: json.RawMessage(`{"p":"/tmp/b"}`)},
	}
	drained := []toolExecResult{
		{toolUseID: "a", content: "A", event: makeToolResultEvent("a", "read_file", "A", "", false)},
		{toolUseID: "b", content: "B", event: makeToolResultEvent("b", "read_file", "B", "", false)},
	}
	unmatched := surfaceStreamedResults(context.Background(), plainEmitter{}, ch, pending, drained)
	close(ch)
	if unmatched != 0 {
		t.Fatalf("want 0 unmatched, got %d", unmatched)
	}
	var seq []string
	for ev := range ch {
		switch ev.Type {
		case "tool_call":
			seq = append(seq, "call:"+ev.ToolCall.ID)
		case "tool_result":
			seq = append(seq, "result:"+ev.ToolResult.ID)
		}
	}
	want := []string{"call:a", "result:a", "call:b", "result:b"}
	if len(seq) != len(want) {
		t.Fatalf("event sequence length mismatch: want %v, got %v", want, seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("event[%d]: want %q, got %q", i, want[i], seq[i])
		}
	}
}

// TestSurfaceStreamedResultsReportsUnmatchedIDs verifies the invariant
// check: if a drained result's toolUseID is NOT in pendingTools, the
// helper skips the synthetic tool_call, still emits the tool_result,
// and returns a non-zero unmatched count so the caller can log it.
func TestSurfaceStreamedResultsReportsUnmatchedIDs(t *testing.T) {
	t.Parallel()
	ch := make(chan TurnEvent, 8)
	pending := []provider.ToolUseBlock{
		{ID: "a", Name: "read_file", Input: json.RawMessage(`{}`)},
	}
	drained := []toolExecResult{
		{toolUseID: "ghost", content: "orphan", event: makeToolResultEvent("ghost", "read_file", "orphan", "", false)},
	}
	unmatched := surfaceStreamedResults(context.Background(), plainEmitter{}, ch, pending, drained)
	close(ch)
	if unmatched != 1 {
		t.Fatalf("want 1 unmatched, got %d", unmatched)
	}
	var sawCall, sawResult bool
	for ev := range ch {
		if ev.Type == "tool_call" {
			sawCall = true
		}
		if ev.Type == "tool_result" && ev.ToolResult.ID == "ghost" {
			sawResult = true
		}
	}
	if sawCall {
		t.Fatal("ghost ID should not produce a synthetic tool_call event")
	}
	if !sawResult {
		t.Fatal("ghost tool_result event should still be emitted")
	}
}

// TestSurfaceStreamedResultsEmptyDrainIsNoOp verifies the early-return
// path — no drained results means no events emitted.
func TestSurfaceStreamedResultsEmptyDrainIsNoOp(t *testing.T) {
	t.Parallel()
	ch := make(chan TurnEvent, 4)
	unmatched := surfaceStreamedResults(context.Background(), plainEmitter{}, ch, nil, nil)
	close(ch)
	if unmatched != 0 {
		t.Fatalf("want 0 unmatched, got %d", unmatched)
	}
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Fatalf("want 0 events emitted, got %d", count)
	}
}

// streamErrAfterDispatchProvider emits a tool_use, a second tool_use
// (to trigger finalizeTool on the first and dispatch it via the
// streaming executor), waits until the dispatched tool has completed,
// then emits a stream error. Used to verify that runLoop surfaces
// drained streamed results to the event channel before exiting the
// streamErr path — otherwise the UI sees orphan tool_progress events
// with no tool_call or tool_result to close them out.
type streamErrAfterDispatchProvider struct {
	toolDone <-chan struct{}
}

func (p *streamErrAfterDispatchProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() {
		defer close(ch)
		ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
			ID: "call-1", Name: "fake_read", Input: json.RawMessage(`{}`),
		}}
		ch <- provider.StreamEvent{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
			ID: "call-2", Name: "fake_read", Input: json.RawMessage(`{}`),
		}}
		// Wait for call-1 to finish executing before erroring.
		<-p.toolDone
		ch <- provider.StreamEvent{Type: "error", Error: fmt.Errorf("simulated stream failure")}
		ch <- provider.StreamEvent{Type: "stop"}
	}()
	return ch, nil
}

// TestRunLoopStreamErrorSurfacesStreamedToolResults verifies that when
// the stream errors after a concurrency-safe tool has been dispatched
// and completed, runLoop surfaces the drained tool_call + tool_result
// events so the UI doesn't see orphan tool_progress events with no
// terminal event.
func TestRunLoopStreamErrorSurfacesStreamedToolResults(t *testing.T) {
	t.Parallel()
	toolDone := make(chan struct{})
	var once sync.Once
	tool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  1 * time.Millisecond,
		returnText: "streamed result",
		onFinish:   func() { once.Do(func() { close(toolDone) }) },
	}
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tool))

	prov := &streamErrAfterDispatchProvider{toolDone: toolDone}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	a := New(prov, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := a.Turn(ctx, "read the file")
	require.NoError(t, err)

	// Index tracking so we can assert tool_call arrives before tool_result
	// for the same ID — out-of-order emission would reintroduce the orphan
	// UX bug this test exists to prevent.
	callIdx := map[string]int{}
	resultIdx := map[string]int{}
	var sawError, sawDone bool
	var doneReason agentsdk.TurnExitReason
	evIdx := 0
	for ev := range events {
		switch ev.Type {
		case "tool_call":
			if ev.ToolCall != nil {
				if _, ok := callIdx[ev.ToolCall.ID]; !ok {
					callIdx[ev.ToolCall.ID] = evIdx
				}
			}
		case "tool_result":
			if ev.ToolResult != nil {
				if _, ok := resultIdx[ev.ToolResult.ID]; !ok {
					resultIdx[ev.ToolResult.ID] = evIdx
				}
			}
		case "error":
			sawError = true
		case "done":
			sawDone = true
			doneReason = ev.ExitReason
		}
		evIdx++
	}

	if !sawError {
		t.Fatal("expected an error event from the stream failure")
	}
	if !sawDone {
		t.Fatal("expected a done event")
	}
	if doneReason != agentsdk.ExitProviderError {
		t.Fatalf("want ExitProviderError, got %v", doneReason)
	}
	// The dispatched call-1 must surface both tool_call and tool_result
	// events so the UI has a closed event loop for its progress display.
	ci, gotCall := callIdx["call-1"]
	ri, gotResult := resultIdx["call-1"]
	if !gotCall {
		t.Fatalf("streamed tool dispatch did not emit a tool_call event; got call IDs %v", callIdx)
	}
	if !gotResult {
		t.Fatalf("streamed tool dispatch did not emit a tool_result event; got result IDs %v", resultIdx)
	}
	if ci >= ri {
		t.Fatalf("tool_call for call-1 (evIdx=%d) must arrive before tool_result (evIdx=%d)", ci, ri)
	}
}

func TestStreamingExecutor_Barrier_WaitsForInFlight(t *testing.T) {
	t.Parallel()
	var inFlightFinished atomic.Bool
	slow := &fakeConcurrencySafeTool{
		name:       "slow_read",
		execDelay:  150 * time.Millisecond,
		returnText: "ok",
		onFinish:   func() { inFlightFinished.Store(true) },
	}
	barrierTool := &fakeConcurrencySafeTool{
		name:       "write",
		execDelay:  10 * time.Millisecond,
		returnText: "wrote",
		onStart: func() {
			if !inFlightFinished.Load() {
				panic("barrier tool started before in-flight Dispatch finished")
			}
		},
	}
	ex := newExecutorWithTools(4, map[string]agentsdk.Tool{"slow_read": slow, "write": barrierTool})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "slow_read"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r2", Name: "slow_read"})
	res := ex.Barrier(ctx, provider.ToolUseBlock{ID: "w1", Name: "write"})

	require.Equal(t, "w1", res.toolUseID)
	require.False(t, res.isError, "barrier result should not be an error")
	require.True(t, inFlightFinished.Load(), "barrier returned before in-flight tools finished")
}

func TestStreamingExecutor_Barrier_AppendedToFutures(t *testing.T) {
	t.Parallel()
	read := &fakeConcurrencySafeTool{name: "read", execDelay: 5 * time.Millisecond, returnText: "r"}
	write := &fakeConcurrencySafeTool{name: "write", execDelay: 5 * time.Millisecond, returnText: "w"}
	ex := newExecutorWithTools(4, map[string]agentsdk.Tool{"read": read, "write": write})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "read"})
	ex.Barrier(ctx, provider.ToolUseBlock{ID: "w1", Name: "write"})
	results := ex.Drain()

	require.Len(t, results, 2)
	require.Equal(t, "r1", results[0].toolUseID)
	require.Equal(t, "w1", results[1].toolUseID)
	require.Equal(t, "r", results[0].content)
	require.Equal(t, "w", results[1].content)
}

func TestStreamingExecutor_Barrier_PreservesOrderInMixedSequence(t *testing.T) {
	t.Parallel()
	fast := &fakeConcurrencySafeTool{name: "fast", execDelay: 10 * time.Millisecond, returnText: "f"}
	slow := &fakeConcurrencySafeTool{name: "slow", execDelay: 60 * time.Millisecond, returnText: "s"}
	write := &fakeConcurrencySafeTool{name: "write", execDelay: 10 * time.Millisecond, returnText: "w"}
	ex := newExecutorWithTools(4, map[string]agentsdk.Tool{"fast": fast, "slow": slow, "write": write})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "1", Name: "slow"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "2", Name: "fast"})
	ex.Barrier(ctx, provider.ToolUseBlock{ID: "3", Name: "write"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "4", Name: "fast"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "5", Name: "slow"})
	results := ex.Drain()

	require.Len(t, results, 5)
	for i, want := range []string{"1", "2", "3", "4", "5"} {
		require.Equal(t, want, results[i].toolUseID, "result[%d]", i)
	}
}

func TestStreamingExecutor_Barrier_ContextCancelled(t *testing.T) {
	t.Parallel()
	write := &fakeConcurrencySafeTool{name: "write", execDelay: 5 * time.Millisecond, returnText: "w"}
	run := runnerFromTool(write)
	ex := newStreamingToolExecutor(2, run)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := ex.Barrier(ctx, provider.ToolUseBlock{ID: "w1", Name: "write"})

	require.Equal(t, "w1", res.toolUseID)
	require.True(t, res.isError, "Barrier with cancelled ctx should produce an error result")
	require.Equal(t, int32(0), write.called.Load(), "tool should not have executed under cancelled ctx")

	results := ex.Drain()
	require.Len(t, results, 1, "barrier result must still appear in Drain")
	require.Equal(t, "w1", results[0].toolUseID)
}

func TestStreamingExecutor_Barrier_FirstCallNoInFlight(t *testing.T) {
	t.Parallel()
	write := &fakeConcurrencySafeTool{name: "write", execDelay: 5 * time.Millisecond, returnText: "w"}
	ex := newStreamingToolExecutor(2, runnerFromTool(write))

	res := ex.Barrier(context.Background(), provider.ToolUseBlock{ID: "w1", Name: "write"})

	require.Equal(t, "w1", res.toolUseID)
	require.False(t, res.isError)
	require.Equal(t, "w", res.content)

	results := ex.Drain()
	require.Len(t, results, 1)
	require.Equal(t, "w1", results[0].toolUseID)
}

func TestStreamingExecutor_Barrier_ConsecutiveBarriers(t *testing.T) {
	t.Parallel()
	w1Tool := &fakeConcurrencySafeTool{name: "w1", execDelay: 5 * time.Millisecond, returnText: "first"}
	w2Tool := &fakeConcurrencySafeTool{name: "w2", execDelay: 5 * time.Millisecond, returnText: "second"}
	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"w1": w1Tool, "w2": w2Tool})
	ctx := context.Background()

	ex.Barrier(ctx, provider.ToolUseBlock{ID: "b1", Name: "w1"})
	ex.Barrier(ctx, provider.ToolUseBlock{ID: "b2", Name: "w2"})

	results := ex.Drain()
	require.Len(t, results, 2)
	require.Equal(t, "b1", results[0].toolUseID)
	require.Equal(t, "b2", results[1].toolUseID)
	require.Equal(t, "first", results[0].content)
	require.Equal(t, "second", results[1].content)
}

func TestStreamingExecutor_SetBarrierBlocksSubsequentDispatch(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex
	run := func(_ context.Context, tc provider.ToolUseBlock) toolExecResult {
		mu.Lock()
		callOrder = append(callOrder, tc.ID)
		mu.Unlock()
		return toolExecResult{toolUseID: tc.ID, content: tc.Name}
	}
	ex := newStreamingToolExecutor(2, run)

	// Dispatch a safe tool first
	ok1 := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r1", Name: "read"})
	require.True(t, ok1, "first dispatch should succeed")

	// Set barrier (simulating a write tool)
	ex.SetBarrier()

	// Subsequent dispatch should be rejected
	ok2 := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r2", Name: "read"})
	require.False(t, ok2, "dispatch after barrier should be rejected")

	results := ex.Drain()
	require.Len(t, results, 1, "only first dispatch should execute")
	require.Equal(t, "r1", results[0].toolUseID)
}

func TestStreamingExecutor_SetBarrierIdempotent(t *testing.T) {
	ex := newStreamingToolExecutor(1, func(_ context.Context, tc provider.ToolUseBlock) toolExecResult {
		return toolExecResult{toolUseID: tc.ID}
	})

	ex.SetBarrier()
	ex.SetBarrier()
	ex.SetBarrier()

	ok := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r1", Name: "read"})
	require.False(t, ok, "dispatch should be rejected after barrier")
}

func TestStreamingExecutor_DispatchReturnValue(t *testing.T) {
	ex := newStreamingToolExecutor(1, func(_ context.Context, tc provider.ToolUseBlock) toolExecResult {
		return toolExecResult{toolUseID: tc.ID}
	})

	ok := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r1", Name: "read"})
	require.True(t, ok, "dispatch should return true when accepted")

	ex.SetBarrier()

	ok = ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r2", Name: "read"})
	require.False(t, ok, "dispatch should return false when rejected by barrier")
}

// fakeWriteTool implements WriteTool and ConcurrencySafeTool. It is
// concurrency-safe but also a write operation, so it acts as a barrier.
type fakeWriteTool struct {
	fakeConcurrencySafeTool
	write bool
}

func (t *fakeWriteTool) IsWriteOperation() bool { return t.write }

// fakeInputSensitiveTool implements InputConcurrencySafeTool and
// InputSensitiveWriteTool. Its safety and write status depend on input.
type fakeInputSensitiveTool struct {
	fakeConcurrencySafeTool
	write bool
}

func (t *fakeInputSensitiveTool) IsConcurrencySafeForInput(_ json.RawMessage) bool { return true }
func (t *fakeInputSensitiveTool) IsWriteOperationForInput(_ json.RawMessage) bool  { return t.write }

// fakeInputSensitiveToolWithStaticWrite implements both InputSensitiveWriteTool
// and WriteTool. Used to test precedence: per-input should override static.
type fakeInputSensitiveToolWithStaticWrite struct {
	fakeInputSensitiveTool
}

func (t *fakeInputSensitiveToolWithStaticWrite) IsWriteOperation() bool { return true }

func TestIsWriteOperation(t *testing.T) {
	t.Parallel()

	// Tool without marker: not a write operation.
	plain := &fakeConcurrencySafeTool{name: "read"}
	if isWriteOperation(plain, nil) {
		t.Error("plain tool without WriteTool marker should not be a write operation")
	}

	// Tool with WriteTool returning true.
	write := &fakeWriteTool{fakeConcurrencySafeTool: fakeConcurrencySafeTool{name: "write"}, write: true}
	if !isWriteOperation(write, nil) {
		t.Error("WriteTool returning true should be a write operation")
	}

	// Input-sensitive: write=true takes precedence over static.
	inputWrite := &fakeInputSensitiveTool{write: true}
	inputWrite.name = "shell"
	if !isWriteOperation(inputWrite, json.RawMessage(`{}`)) {
		t.Error("InputSensitiveWriteTool returning true should be a write operation")
	}

	// Input-sensitive: write=false means not a write operation.
	inputRead := &fakeInputSensitiveTool{write: false}
	inputRead.name = "shell"
	if isWriteOperation(inputRead, json.RawMessage(`{}`)) {
		t.Error("InputSensitiveWriteTool returning false should not be a write operation")
	}

	// Input-sensitive fallback: per-input returns false, static WriteTool returns true.
	// Per-input takes precedence, so result should be false (not a write).
	inputReadStaticWrite := &fakeInputSensitiveToolWithStaticWrite{fakeInputSensitiveTool: fakeInputSensitiveTool{write: false}}
	inputReadStaticWrite.name = "shell"
	if isWriteOperation(inputReadStaticWrite, json.RawMessage(`{}`)) {
		t.Error("InputSensitiveWriteTool(false) should override static WriteTool(true)")
	}

	// Tool with WriteTool returning false.
	noWrite := &fakeWriteTool{fakeConcurrencySafeTool: fakeConcurrencySafeTool{name: "nowrite"}, write: false}
	if isWriteOperation(noWrite, nil) {
		t.Error("WriteTool returning false should not be a write operation")
	}

	// nil tool: not a write operation (fail-closed to safe).
	if isWriteOperation(nil, nil) {
		t.Error("nil tool should not be a write operation")
	}
}

func TestStreamingExecutor_WriteToolSetsBarrier(t *testing.T) {
	t.Parallel()
	var callOrder []string
	var mu sync.Mutex
	run := func(_ context.Context, tc provider.ToolUseBlock) toolExecResult {
		mu.Lock()
		callOrder = append(callOrder, tc.ID)
		mu.Unlock()
		return toolExecResult{toolUseID: tc.ID, content: tc.Name}
	}
	ex := newStreamingToolExecutor(2, run)

	// Simulate: read (safe, dispatched), write (barrier, not dispatched),
	// read-after-write (safe but barrier seen, not dispatched).
	ok1 := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r1", Name: "read"})
	require.True(t, ok1, "first read should dispatch")

	// Write tool sets barrier.
	ex.SetBarrier()

	ok2 := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "r2", Name: "read"})
	require.False(t, ok2, "read after write barrier should be rejected")

	results := ex.Drain()
	require.Len(t, results, 1, "only first dispatch should execute")
	require.Equal(t, "r1", results[0].toolUseID)
}

func TestStreamingExecutor_MultipleBarriersInSequence(t *testing.T) {
	t.Parallel()
	var callOrder []string
	var mu sync.Mutex
	run := func(_ context.Context, tc provider.ToolUseBlock) toolExecResult {
		mu.Lock()
		callOrder = append(callOrder, tc.ID)
		mu.Unlock()
		return toolExecResult{toolUseID: tc.ID, content: tc.Name}
	}
	ex := newStreamingToolExecutor(2, run)

	// Dispatch two safe tools, then two barriers, then one safe.
	ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "s1", Name: "read"})
	ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "s2", Name: "read"})
	ex.SetBarrier()
	ex.SetBarrier() // idempotent
	ok := ex.Dispatch(context.Background(), provider.ToolUseBlock{ID: "s3", Name: "read"})
	require.False(t, ok, "safe tool after barrier should be rejected")

	results := ex.Drain()
	require.Len(t, results, 2)
	require.Equal(t, "s1", results[0].toolUseID)
	require.Equal(t, "s2", results[1].toolUseID)
}

func TestStreamingExecutor_BarrierWithNoQueuedTools(t *testing.T) {
	t.Parallel()
	write := &fakeConcurrencySafeTool{name: "write", execDelay: 5 * time.Millisecond, returnText: "w"}
	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"write": write})
	ctx := context.Background()

	// Barrier with no prior dispatches.
	res := ex.Barrier(ctx, provider.ToolUseBlock{ID: "w1", Name: "write"})
	require.Equal(t, "w1", res.toolUseID)
	require.False(t, res.isError)

	results := ex.Drain()
	require.Len(t, results, 1)
	require.Equal(t, "w1", results[0].toolUseID)
}

// TestRunLoop_WriteToolSetsBarrierDuringStream proves that a write tool
// in the model response sets the barrier, causing subsequent safe tools
// to be queued for post-stream execution rather than dispatched during
// the stream.
func TestRunLoop_WriteToolSetsBarrierDuringStream(t *testing.T) {
	t.Parallel()

	readTool := &fakeConcurrencySafeTool{
		name:       "fake_read",
		execDelay:  1 * time.Millisecond,
		returnText: "ok",
	}
	writeTool := &fakeWriteTool{fakeConcurrencySafeTool: fakeConcurrencySafeTool{name: "fake_write", execDelay: 1 * time.Millisecond, returnText: "wrote"}, write: true}
	// Second read tool with a distinct name so we can count it separately.
	read2Tool := &fakeConcurrencySafeTool{
		name:       "fake_read2",
		execDelay:  1 * time.Millisecond,
		returnText: "ok2",
	}

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(readTool))
	require.NoError(t, reg.Register(writeTool))
	require.NoError(t, reg.Register(read2Tool))

	// Provider emits read, write, read — then stops.
	// The write tool sets a barrier. We verify by driving the agent
	// and checking that only the first read was dispatched during streaming.
	prov := &dynamicMockProvider{
		responses: [][]provider.StreamEvent{
			{
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "r1", Name: "fake_read", Input: json.RawMessage(`{"p":1}`),
				}},
				{Type: agentsdk.EventContentBlockStop},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "w1", Name: "fake_write", Input: json.RawMessage(`{"p":2}`),
				}},
				{Type: agentsdk.EventContentBlockStop},
				{Type: "tool_use", ToolUse: &provider.ToolUseBlock{
					ID: "r2", Name: "fake_read2", Input: json.RawMessage(`{"p":3}`),
				}},
				{Type: agentsdk.EventContentBlockStop},
				{Type: "stop"},
			},
			{
				{Type: "text_delta", Text: "done"},
				{Type: "stop"},
			},
		},
	}
	cfg := config.DefaultConfig()
	cfg.Agent.MaxTurns = 5
	a := New(prov, reg, autoApprove, cfg,
		WithApprovalChecker(&countingApprovalChecker{result: AutoApproved}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := a.Turn(ctx, "read then write then read")
	require.NoError(t, err)

	var toolResults int
	for ev := range events {
		if ev.Type == "tool_result" {
			toolResults++
		}
	}

	// All three tools should have executed by turn end.
	require.Equal(t, 3, toolResults, "expected 3 tool_result events")

	// The first read should have been dispatched during streaming (count=1).
	// The write should NOT have been dispatched during streaming (it's unsafe).
	// The second read should NOT have been dispatched during streaming (barrier set).
	// Since all three are different tool instances, each called=1.
	require.Equal(t, int32(1), readTool.called.Load(), "only first read should dispatch during stream")
	require.Equal(t, int32(1), writeTool.called.Load(), "write should execute once post-stream")
	require.Equal(t, int32(1), read2Tool.called.Load(), "second read should execute once post-stream")
}

// fakeShellTool is a concurrency-safe tool that simulates shell execution.
// It returns an error when returnError is true, triggering sibling abort.
type fakeShellTool struct {
	fakeConcurrencySafeTool
	returnError bool
}

func (t *fakeShellTool) Execute(ctx context.Context, _ json.RawMessage) (agentsdk.ToolResult, error) {
	t.called.Add(1)
	select {
	case <-time.After(t.execDelay):
	case <-ctx.Done():
		return agentsdk.ToolResult{}, ctx.Err()
	}
	if t.returnError {
		return agentsdk.ToolResult{Content: "shell error", IsError: true}, nil
	}
	return agentsdk.ToolResult{Content: t.returnText}, nil
}

func TestStreamingExecutor_SiblingAbortOnShellError(t *testing.T) {
	t.Parallel()
	// Two concurrent tools: shell (will error) and read_file (should be aborted)
	shell := &fakeShellTool{fakeConcurrencySafeTool: fakeConcurrencySafeTool{name: "shell", execDelay: 50 * time.Millisecond, returnText: "error"}, returnError: true}
	read := &fakeConcurrencySafeTool{name: "read_file", execDelay: 100 * time.Millisecond, returnText: "ok"}

	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"shell": shell, "read_file": read})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "s1", Name: "shell"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "read_file"})
	results := ex.Drain()

	require.Len(t, results, 2)

	// Shell result is an error
	var shellResult, readResult *toolExecResult
	for i := range results {
		if results[i].toolUseID == "s1" {
			shellResult = &results[i]
		}
		if results[i].toolUseID == "r1" {
			readResult = &results[i]
		}
	}
	require.NotNil(t, shellResult)
	require.NotNil(t, readResult)
	require.True(t, shellResult.isError, "shell should return error")
	// Read was aborted by sibling error — check it's an error (either aborted message or context cancelled)
	require.True(t, readResult.isError, "read should be aborted when shell sibling errors")
}

func TestStreamingExecutor_NonShellErrorNoSiblingAbort(t *testing.T) {
	t.Parallel()
	// read_file errors, but grep should complete normally
	read := &fakeConcurrencySafeTool{name: "read_file", execDelay: 50 * time.Millisecond, returnText: "error"}
	grep := &fakeConcurrencySafeTool{name: "grep", execDelay: 100 * time.Millisecond, returnText: "ok"}

	ex := newExecutorWithTools(2, map[string]agentsdk.Tool{"read_file": read, "grep": grep})
	ctx := context.Background()

	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "r1", Name: "read_file"})
	ex.Dispatch(ctx, provider.ToolUseBlock{ID: "g1", Name: "grep"})
	results := ex.Drain()

	require.Len(t, results, 2)

	var readResult, grepResult *toolExecResult
	for i := range results {
		if results[i].toolUseID == "r1" {
			readResult = &results[i]
		}
		if results[i].toolUseID == "g1" {
			grepResult = &results[i]
		}
	}
	require.NotNil(t, readResult)
	require.NotNil(t, grepResult)
	// read_file returns "error" as text but not as an IsError result
	require.False(t, readResult.isError, "read_file returning text 'error' should not be an error result")
	require.False(t, grepResult.isError, "non-shell error should not abort siblings")
	require.Equal(t, "ok", grepResult.content)
}
