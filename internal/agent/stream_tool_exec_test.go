package agent

import (
	"context"
	"encoding/json"
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
// onStart is non-nil, it is invoked on the very first line of Execute
// — used by timing-sensitive tests to signal "tool has started".
type fakeConcurrencySafeTool struct {
	name       string
	execDelay  time.Duration
	called     atomic.Int32
	returnText string
	onStart    func()
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
	byName := map[string]agentsdk.Tool{"fast": fast, "slow": slow}
	run := func(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
		return runnerFromTool(byName[tc.Name])(ctx, tc)
	}
	ex := newStreamingToolExecutor(2, run)
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
