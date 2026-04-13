package agent

import (
	"context"
	"sync"

	"github.com/julianshen/rubichan/internal/provider"
	"github.com/julianshen/rubichan/pkg/agentsdk"
)

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
	sem     chan struct{}
	run     toolExecFn
	mu      sync.Mutex
	futures []*toolFuture
	wg      sync.WaitGroup
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
// If ctx is already cancelled, the future is completed immediately with
// an error result — the tool is not executed. Dispatch is O(1); actual
// execution runs in a background goroutine.
func (e *streamingToolExecutor) Dispatch(ctx context.Context, tc provider.ToolUseBlock) {
	f := &toolFuture{
		toolUseID: tc.ID,
		toolName:  tc.Name,
		done:      make(chan struct{}),
	}
	e.mu.Lock()
	e.futures = append(e.futures, f)
	e.mu.Unlock()

	if ctx.Err() != nil {
		f.result = toolErrorResult(tc, "context cancelled before tool dispatch")
		close(f.done)
		return
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
}

// Drain waits for every dispatched future to complete and returns
// their results in dispatch order. Safe to call only once per executor
// instance; subsequent calls return an empty slice.
//
// Lock scope is deliberately narrow: we snapshot the futures slice
// under e.mu, then release the lock before reading f.done/f.result.
// wg.Wait() has already guaranteed all dispatch goroutines have
// finished, so the <-f.done reads are redundant no-ops kept as a
// safety net. Holding the lock across those reads would create a
// lock-order hazard for any future dispatch goroutine that acquires
// e.mu — this shape keeps the no-deadlock invariant obvious.
func (e *streamingToolExecutor) Drain() []toolExecResult {
	e.wg.Wait()
	e.mu.Lock()
	futures := e.futures
	e.futures = nil
	e.mu.Unlock()
	out := make([]toolExecResult, 0, len(futures))
	for _, f := range futures {
		<-f.done // redundant after wg.Wait, kept as a safety net
		out = append(out, f.result)
	}
	return out
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
