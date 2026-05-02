package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// eventEmitter is the subset of Agent that surfaceStreamedResults
// needs to flush events without deadlocking on a stalled consumer.
// Uses an interface so unit tests can pass a plain channel wrapper.
type eventEmitter interface {
	emit(ctx context.Context, ch chan<- TurnEvent, ev TurnEvent)
}

// toolExecFn runs a single tool call and returns its toolExecResult.
// The executor is agnostic about where the tool actually runs — the
// agent wires in a closure that dispatches through the pipeline
// (executeSingleTool), while unit tests wire in a direct invocation.
type toolExecFn func(ctx context.Context, tc provider.ToolUseBlock) toolExecResult

// streamingToolExecutor dispatches concurrency-safe tools as soon as
// their tool_use block finalizes during a streaming model response.
// Results are collected and returned in dispatch order from Drain().
//
// Ordering: tools are dispatched in the order the model emits their
// tool_use blocks. Drain() returns them in that same dispatch order,
// regardless of execution wall time. This matches the wire protocol
// requirement that tool_result messages follow their matching tool_use
// in conversation order.
//
// Parallelism is bounded by maxParallel; excess dispatches block in a
// goroutine pool. Callers typically set this to maxParallelTools.
//
// Claude Code's query.ts uses this pattern to overlap slow tool I/O
// (file reads, greps) with the trailing portion of the model's
// streaming response, eliminating 1–3s of sequential wait time on
// typical reason-then-read turns.
type streamingToolExecutor struct {
	sem         chan struct{}
	run         toolExecFn
	mu          sync.Mutex
	futures     []*toolFuture
	wg          sync.WaitGroup
	barrierSeen bool // set when an unsafe (write) tool is encountered; blocks subsequent Dispatch calls
}

// toolFuture holds the in-flight state of one dispatched tool call.
// done is closed when the result is populated; callers block on it.
type toolFuture struct {
	toolUseID string
	toolName  string
	done      chan struct{}
	result    toolExecResult
}

// newStreamingToolExecutor creates an executor with the given
// parallelism bound and exec function. maxParallel is clamped to 1
// when zero or negative.
func newStreamingToolExecutor(maxParallel int, run toolExecFn) *streamingToolExecutor {
	if maxParallel < 1 {
		maxParallel = 1
	}
	return &streamingToolExecutor{
		sem: make(chan struct{}, maxParallel),
		run: run,
	}
}

// Dispatch starts execution of a concurrency-safe tool in the background.
// If a barrier has already been seen in this response, the tool is NOT
// dispatched — it will be executed later by executeTools. This prevents
// read-after-write races when the model emits [write, read] in the same
// response.
//
// If ctx is already cancelled, the future is completed immediately with
// an error result — the tool is not executed. Dispatch is O(1); actual
// execution runs in a background goroutine.
func (e *streamingToolExecutor) Dispatch(ctx context.Context, tc provider.ToolUseBlock) bool {
	e.mu.Lock()
	if e.barrierSeen {
		e.mu.Unlock()
		return false
	}
	f := &toolFuture{
		toolUseID: tc.ID,
		toolName:  tc.Name,
		done:      make(chan struct{}),
	}
	e.futures = append(e.futures, f)
	e.mu.Unlock()

	if ctx.Err() != nil {
		f.result = toolErrorResult(tc, "context cancelled before tool dispatch")
		close(f.done)
		return true
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		// Bound parallelism.
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			f.result = toolErrorResult(tc, "context cancelled while waiting for executor slot")
			close(f.done)
			return
		}
		defer func() { <-e.sem }()

		f.result = e.run(ctx, tc)
		close(f.done)
	}()
	return true
}

// SetBarrier marks that an unsafe (write) tool has been encountered.
// Subsequent Dispatch calls will return false, causing those tools to be
// handled by the post-stream executeTools pipeline instead.
func (e *streamingToolExecutor) SetBarrier() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.barrierSeen = true
}

// Barrier serializes a single tool call against every prior in-flight
// Dispatch. It waits for all currently-dispatched futures to complete,
// then runs the barrier tool synchronously, then appends its future to
// the executor's results list so Drain() surfaces it in dispatch order.
//
// Concurrency contract: the stream-event loop in agent.runLoop is the
// sole caller of Dispatch and Barrier on any one executor instance, so
// no two of these calls overlap. Barrier therefore does not need to
// guard against new Dispatches arriving during its wg.Wait(). Violating
// the contract — calling Dispatch concurrently with Barrier — would let
// a new Dispatch escape the wg.Wait() snapshot (the barrier tool would
// run alongside it instead of after it) and would race on the e.futures
// append below.
func (e *streamingToolExecutor) Barrier(ctx context.Context, tc provider.ToolUseBlock) toolExecResult {
	e.wg.Wait()
	f := &toolFuture{
		toolUseID: tc.ID,
		toolName:  tc.Name,
		done:      make(chan struct{}),
	}
	if ctx.Err() != nil {
		f.result = toolErrorResult(tc, "context cancelled before barrier dispatch")
	} else {
		f.result = e.run(ctx, tc)
	}
	close(f.done)
	e.mu.Lock()
	e.futures = append(e.futures, f)
	e.mu.Unlock()
	return f.result
}

// Drain waits for every dispatched future to complete and returns
// their results in dispatch order. Safe to call only once per executor
// instance; subsequent calls return an empty slice.
//
// wg.Wait() establishes a happens-before for every f.result write, so
// we only hold e.mu long enough to snapshot the futures slice and then
// iterate it lock-free.
func (e *streamingToolExecutor) Drain() []toolExecResult {
	e.wg.Wait()
	e.mu.Lock()
	futures := e.futures
	e.futures = nil
	e.mu.Unlock()
	out := make([]toolExecResult, 0, len(futures))
	for _, f := range futures {
		out = append(out, f.result)
	}
	return out
}

// surfaceStreamedResults is a terminal-only event flush for the
// stream-error exit path. Without it, executeSingleTool's tool_progress
// events from a dispatched tool have no matching tool_call or
// tool_result and the UI sees an incomplete event loop.
//
// Events only — the conversation state is intentionally NOT updated
// (no AddToolResult / persistToolResult / recordToolProgress). The
// only caller is the streamErr branch, which discards all partial-turn
// mutations because the assistant message was never committed. A
// future caller that wants conversation-side persistence must not
// reuse this helper.
//
// Returns the count of drained results whose toolUseID was NOT found
// in pendingTools. This is an invariant check: every dispatched tool
// is appended to pendingTools in finalizeTool before Dispatch is
// called, so unmatched should always be 0. A non-zero count means the
// invariant broke and the caller should log it so future regressions
// are visible instead of silently emitting orphan tool_result events.
func surfaceStreamedResults(ctx context.Context, em eventEmitter, ch chan<- TurnEvent, pendingTools []provider.ToolUseBlock, drained []toolExecResult) (unmatched int) {
	if len(drained) == 0 {
		return 0
	}
	byID := make(map[string]provider.ToolUseBlock, len(pendingTools))
	for _, tc := range pendingTools {
		byID[tc.ID] = tc
	}
	for _, r := range drained {
		if tc, ok := byID[r.toolUseID]; ok {
			em.emit(ctx, ch, makeToolCallEvent(tc))
		} else {
			unmatched++
		}
		em.emit(ctx, ch, r.event)
	}
	return unmatched
}

// isStreamingEligible returns true if a tool can be dispatched during
// streaming. Requires the ConcurrencySafeTool marker returning true AND
// auto-approval (either AutoApproved or TrustRuleApproved).
//
// Tools that need user approval are NOT dispatched during streaming
// because approval prompts would interrupt the model's response and
// the semantic boundary between "model output" and "tool execution"
// must remain clear for the user.
func isStreamingEligible(tool agentsdk.Tool, result ApprovalResult) bool {
	cs, ok := tool.(agentsdk.ConcurrencySafeTool)
	if !ok || !cs.IsConcurrencySafe() {
		return false
	}
	return result == AutoApproved || result == TrustRuleApproved
}

// isWriteOperation reports whether a tool call mutates external state.
// Per-input sensitivity takes precedence over static declaration.
// Unknown tools and tools without the marker interface return false
// (fail-closed — treated as safe, not a barrier).
func isWriteOperation(tool agentsdk.Tool, input json.RawMessage) bool {
	if tool == nil {
		return false
	}
	// Per-input write detection takes precedence over static declaration.
	if iwt, ok := tool.(agentsdk.InputSensitiveWriteTool); ok {
		return iwt.IsWriteOperationForInput(input)
	}
	// Fall back to static declaration.
	if wt, ok := tool.(agentsdk.WriteTool); ok {
		return wt.IsWriteOperation()
	}
	return false
}

// isWriteOperationForInput checks write status on an
// InputConcurrencySafeTool that also implements InputSensitiveWriteTool.
// This avoids re-asserting the interface in the hot path.
func isWriteOperationForInput(ic agentsdk.InputConcurrencySafeTool, input json.RawMessage) bool {
	if iwt, ok := ic.(agentsdk.InputSensitiveWriteTool); ok {
		return iwt.IsWriteOperationForInput(input)
	}
	if wt, ok := ic.(agentsdk.WriteTool); ok {
		return wt.IsWriteOperation()
	}
	return false
}
