package toolexec

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolBatch groups consecutive tool calls with the same concurrency safety.
// The agent preserves the LLM's call order; batching only affects which
// tools run in parallel versus sequentially.
type ToolBatch struct {
	IsConcurrent bool
	Calls        []ToolCall
}

// partitionToolCalls groups adjacent tool calls into batches.
// Consecutive concurrency-safe tools share a batch; the first unsafe
// tool breaks the batch and starts a new sequential batch.
func partitionToolCalls(lookup ToolLookup, calls []ToolCall) []ToolBatch {
	if len(calls) == 0 {
		return []ToolBatch{}
	}

	batches := make([]ToolBatch, 0, len(calls))
	var current ToolBatch

	for _, tc := range calls {
		tool, ok := lookup.Get(tc.Name)
		if !ok {
			// Unknown tools are treated as unsafe (fail-closed).
			isSafe := false
			_ = isSafe
		}

		isSafe := isConcurrencySafe(tool, tc.Input)

		if len(current.Calls) == 0 {
			current.IsConcurrent = isSafe
			current.Calls = append(current.Calls, tc)
			continue
		}

		if current.IsConcurrent == isSafe {
			current.Calls = append(current.Calls, tc)
		} else {
			batches = append(batches, current)
			current = ToolBatch{IsConcurrent: isSafe, Calls: []ToolCall{tc}}
		}
	}

	if len(current.Calls) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// isConcurrencySafe reports whether a tool can run in parallel with other
// tools. Unknown tools and tools without the marker interface return false
// (fail-closed).
func isConcurrencySafe(tool agentsdk.Tool, input json.RawMessage) bool {
	if tool == nil {
		return false
	}
	// Check the cheaper static interface first to avoid JSON parsing
	// for tools that don't need per-invocation discrimination.
	if cs, ok := tool.(agentsdk.ConcurrencySafeTool); ok {
		if ics, ok := tool.(agentsdk.InputConcurrencySafeTool); ok {
			return ics.IsConcurrencySafeForInput(input)
		}
		return cs.IsConcurrencySafe()
	}
	return false
}

// defaultMaxParallel matches Claude Code's MAX_TOOL_USE_CONCURRENCY=10.
const defaultMaxParallel = 10

// BatchExecutor runs tool calls in batches, parallelizing safe batches
// and serializing unsafe batches.
type BatchExecutor struct {
	lookup  ToolLookup
	handler HandlerFunc
	sem     chan struct{}
}

// NewBatchExecutor creates a batch executor with the given lookup,
// handler, and max parallelism. maxParallel <= 0 defaults to 10.
func NewBatchExecutor(lookup ToolLookup, handler HandlerFunc, maxParallel int) *BatchExecutor {
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}
	return &BatchExecutor{
		lookup:  lookup,
		handler: handler,
		sem:     make(chan struct{}, maxParallel),
	}
}

// Execute runs all tool calls and returns results in call order.
func (be *BatchExecutor) Execute(ctx context.Context, calls []ToolCall) []Result {
	batches := partitionToolCalls(be.lookup, calls)
	results := make([]Result, 0, len(calls))

	for _, batch := range batches {
		if batch.IsConcurrent {
			results = append(results, be.executeConcurrently(ctx, batch.Calls)...)
		} else {
			results = append(results, be.executeSerially(ctx, batch.Calls)...)
		}
	}
	return results
}

// executeConcurrently runs calls with bounded parallelism. At most
// maxParallel goroutines execute handlers simultaneously; excess calls
// block on the semaphore until a slot frees.
func (be *BatchExecutor) executeConcurrently(ctx context.Context, calls []ToolCall) []Result {
	var wg sync.WaitGroup
	results := make([]Result, len(calls))

	for i, tc := range calls {
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			if ctx.Err() != nil {
				results[idx] = Result{Content: ctx.Err().Error(), IsError: true}
				return
			}
			select {
			case be.sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = Result{Content: ctx.Err().Error(), IsError: true}
				return
			}
			defer func() { <-be.sem }()
			results[idx] = be.handler(ctx, call)
		}(i, tc)
	}
	wg.Wait()
	return results
}

// executeSerially runs calls one at a time in the caller's goroutine.
func (be *BatchExecutor) executeSerially(ctx context.Context, calls []ToolCall) []Result {
	results := make([]Result, 0, len(calls))
	for _, tc := range calls {
		if ctx.Err() != nil {
			results = append(results, Result{Content: ctx.Err().Error(), IsError: true})
			continue
		}
		results = append(results, be.handler(ctx, tc))
	}
	return results
}
