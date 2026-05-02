package toolexec

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/julianshen/rubichan/pkg/agentsdk"
)

// ToolBatch groups consecutive tool calls with the same concurrency safety.
type ToolBatch struct {
	IsConcurrent bool
	Calls        []ToolCall
}

// partitionToolCalls groups adjacent tool calls into batches.
// Consecutive concurrency-safe tools share a batch; the first unsafe
// tool breaks the batch and starts a new sequential batch.
func partitionToolCalls(lookup ToolLookup, calls []ToolCall) []ToolBatch {
	if len(calls) == 0 {
		return nil
	}

	var batches []ToolBatch
	var current ToolBatch

	for _, tc := range calls {
		tool, ok := lookup.Get(tc.Name)
		if !ok {
			// Unknown tools are treated as unsafe (fail-closed).
			tool = nil
		}

		isSafe := isConcurrencySafe(tool, tc.Input)

		if len(current.Calls) == 0 {
			current.IsConcurrent = isSafe
			current.Calls = append(current.Calls, tc)
			continue
		}

		if current.IsConcurrent == isSafe {
			// Same safety level — extend current batch.
			current.Calls = append(current.Calls, tc)
		} else {
			// Safety changed — finalize current and start new.
			batches = append(batches, current)
			current = ToolBatch{IsConcurrent: isSafe, Calls: []ToolCall{tc}}
		}
	}

	if len(current.Calls) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// isConcurrencySafe checks whether a tool is safe to run in parallel
// with other tools. Falls back to false for unknown tools or tools
// that don't implement the marker interface.
//
// The cheaper ConcurrencySafeTool check comes first to avoid JSON
// parsing for tools that don't need per-invocation discrimination.
func isConcurrencySafe(tool agentsdk.Tool, input json.RawMessage) bool {
	if tool == nil {
		return false
	}
	// Fast path: static concurrency safety (no JSON parsing).
	if cs, ok := tool.(agentsdk.ConcurrencySafeTool); ok {
		// If the tool also implements InputConcurrencySafeTool, the
		// per-invocation check takes precedence — but only after we
		// know the tool participates in the concurrency safety protocol.
		if ics, ok := tool.(agentsdk.InputConcurrencySafeTool); ok {
			return ics.IsConcurrencySafeForInput(input)
		}
		return cs.IsConcurrencySafe()
	}
	return false
}

// defaultMaxParallel is the default concurrency limit for tool batch
// execution. Matches Claude Code's MAX_TOOL_USE_CONCURRENCY=10.
const defaultMaxParallel = 10

// BatchExecutor runs tool calls in batches, parallelizing safe batches
// and serializing unsafe batches.
type BatchExecutor struct {
	lookup      ToolLookup
	handler     HandlerFunc
	maxParallel int
}

// NewBatchExecutor creates a batch executor with the given lookup,
// handler, and max parallelism. maxParallel <= 0 defaults to 10.
func NewBatchExecutor(lookup ToolLookup, handler HandlerFunc, maxParallel int) *BatchExecutor {
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}
	return &BatchExecutor{
		lookup:      lookup,
		handler:     handler,
		maxParallel: maxParallel,
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

func (be *BatchExecutor) executeConcurrently(ctx context.Context, calls []ToolCall) []Result {
	sem := make(chan struct{}, be.maxParallel)
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
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = Result{Content: ctx.Err().Error(), IsError: true}
				return
			}
			defer func() { <-sem }()
			results[idx] = be.handler(ctx, call)
		}(i, tc)
	}
	wg.Wait()
	return results
}

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
