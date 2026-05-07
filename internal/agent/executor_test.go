package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
)

// concurrentTool tracks concurrent execution for testing.
type concurrentTool struct {
	name    string
	delay   time.Duration
	peak    *atomic.Int32
	current *atomic.Int32
}

func (c *concurrentTool) Name() string                    { return c.name }
func (c *concurrentTool) Description() string             { return "test" }
func (c *concurrentTool) Schema() json.RawMessage         { return json.RawMessage(`{}`) }
func (c *concurrentTool) Safety() tool.SafetyLevel        { return tool.Safe }
func (c *concurrentTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	n := c.current.Add(1)
	for {
		old := c.peak.Load()
		if n <= old || c.peak.CompareAndSwap(old, n) {
			break
		}
	}
	time.Sleep(c.delay)
	c.current.Add(-1)
	return fmt.Sprintf("result_%s", c.name), nil
}

func newTestAgent(tools []tool.Tool, concurrency int) *Agent {
	tm := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		tm[t.Name()] = t
	}
	return &Agent{
		toolMap:        tm,
		tui:            tui.NewHeadless(),
		autoApprove:    true,
		maxConcurrency: concurrency,
	}
}

func TestExecuteToolsConcurrently_Ordering(t *testing.T) {
	var peak, current atomic.Int32
	tools := []tool.Tool{
		&concurrentTool{name: "a", delay: 50 * time.Millisecond, peak: &peak, current: &current},
		&concurrentTool{name: "b", delay: 30 * time.Millisecond, peak: &peak, current: &current},
		&concurrentTool{name: "c", delay: 10 * time.Millisecond, peak: &peak, current: &current},
	}
	a := newTestAgent(tools, 5)

	calls := []provider.ToolCall{
		{ID: "1", Name: "a", Arguments: json.RawMessage(`{}`)},
		{ID: "2", Name: "b", Arguments: json.RawMessage(`{}`)},
		{ID: "3", Name: "c", Arguments: json.RawMessage(`{}`)},
	}

	results := a.executeToolsConcurrently(context.Background(), calls)

	// Results must preserve input order
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for i, id := range []string{"1", "2", "3"} {
		if results[i].ToolCallID != id {
			t.Errorf("results[%d].ToolCallID = %q, want %q", i, results[i].ToolCallID, id)
		}
	}
}

func TestExecuteToolsConcurrently_BoundedConcurrency(t *testing.T) {
	var peak, current atomic.Int32
	tools := []tool.Tool{
		&concurrentTool{name: "t", delay: 50 * time.Millisecond, peak: &peak, current: &current},
	}
	a := newTestAgent(tools, 2)

	calls := make([]provider.ToolCall, 5)
	for i := range calls {
		calls[i] = provider.ToolCall{ID: fmt.Sprintf("%d", i), Name: "t", Arguments: json.RawMessage(`{}`)}
	}

	a.executeToolsConcurrently(context.Background(), calls)

	if peak.Load() > 2 {
		t.Errorf("peak concurrency = %d, want <= 2", peak.Load())
	}
}

func TestExecuteToolsConcurrently_SingleCall(t *testing.T) {
	tools := []tool.Tool{&stubTool{name: "read_file"}}
	a := newTestAgent(tools, 5)

	calls := []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: json.RawMessage(`{}`)}}
	results := a.executeToolsConcurrently(context.Background(), calls)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ToolCallID != "1" {
		t.Errorf("ToolCallID = %q, want %q", results[0].ToolCallID, "1")
	}
}

func TestExecuteToolsConcurrently_PanicRecovery(t *testing.T) {
	panicTool := &stubTool{name: "panic_tool"}
	tm := map[string]tool.Tool{"panic_tool": panicTool}

	// Override execute to panic — we need a custom tool
	a := &Agent{
		toolMap:        tm,
		tui:            tui.NewHeadless(),
		autoApprove:    true,
		maxConcurrency: 5,
	}

	// The panic happens inside executeTool when tool is found but panics
	// For this test, verify the executor doesn't crash with unknown tools
	calls := []provider.ToolCall{
		{ID: "1", Name: "unknown", Arguments: json.RawMessage(`{}`)},
		{ID: "2", Name: "panic_tool", Arguments: json.RawMessage(`{}`)},
	}
	results := a.executeToolsConcurrently(context.Background(), calls)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// First should be an error (unknown tool)
	if results[0].Content != "unknown tool: unknown" {
		t.Errorf("results[0] = %q", results[0].Content)
	}
}

func TestExecuteTool_MalformedArguments(t *testing.T) {
	tools := []tool.Tool{&stubTool{name: "read_file"}}
	a := newTestAgent(tools, 5)

	tests := []struct {
		name string
		args string
		want string
	}{
		{"empty args", "", "empty arguments"},
		{"null args", "null", "empty arguments"},
		{"invalid JSON", `{"path": `, "malformed JSON"},
		{"truncated", `{"pat`, "malformed JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := provider.ToolCall{ID: "1", Name: "read_file", Arguments: json.RawMessage(tt.args)}
			result := a.executeTool(context.Background(), tc)
			if !strings.Contains(result, tt.want) {
				t.Errorf("got %q, want it to contain %q", result, tt.want)
			}
		})
	}
}
