package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/serjikisa/forge/internal/provider"
)

// toolResult holds the result of a single tool execution, preserving order.
type toolResult struct {
	index  int
	callID string
	output string
}

// executeToolsConcurrently runs tool calls in parallel with bounded concurrency.
// Results are returned in the same order as the input calls.
func (a *Agent) executeToolsConcurrently(ctx context.Context, calls []provider.ToolCall) []provider.Message {
	if len(calls) == 1 {
		result := a.executeTool(ctx, calls[0])
		return []provider.Message{{Role: "tool", Content: result, ToolCallID: calls[0].ID}}
	}

	sem := make(chan struct{}, a.maxConcurrency)
	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup

	for i, tc := range calls {
		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(idx int, call provider.ToolCall) {
			defer wg.Done()
			defer func() { <-sem }() // release
			defer func() {
				if r := recover(); r != nil {
					results[idx] = toolResult{
						index:  idx,
						callID: call.ID,
						output: fmt.Sprintf("panic: %v", r),
					}
				}
			}()
			results[idx] = toolResult{
				index:  idx,
				callID: call.ID,
				output: a.executeTool(ctx, call),
			}
		}(i, tc)
	}

	wg.Wait()

	msgs := make([]provider.Message, len(results))
	for i, r := range results {
		msgs[i] = provider.Message{Role: "tool", Content: r.output, ToolCallID: r.callID}
	}
	return msgs
}
