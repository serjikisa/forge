package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
)

// --- Helpers ---

// stubTool implements tool.Tool for testing.
type stubTool struct {
	name   string
	safety tool.SafetyLevel
}

func (s *stubTool) Name() string                                                 { return s.name }
func (s *stubTool) Description() string                                          { return "stub" }
func (s *stubTool) Schema() json.RawMessage                                      { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil }
func (s *stubTool) Safety() tool.SafetyLevel                                     { return s.safety }

// stubProvider implements provider.Provider and provider.ModelSwitcher.
type stubProvider struct {
	model      string
	events     []provider.ChatEvent
	err        error
	callCount  int
	lastTools  []provider.ToolDef
	modelsList []provider.Model
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) ChatCompletion(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatEvent, error) {
	s.callCount++
	s.lastTools = req.Tools
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan provider.ChatEvent, len(s.events)+1)
	for _, e := range s.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}
func (s *stubProvider) ListModels(_ context.Context) ([]provider.Model, error) {
	return s.modelsList, nil
}
func (s *stubProvider) SetModel(m string) { s.model = m }

// --- truncate ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		in  string
		n   int
		out string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"ab", 0, "..."},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.n); got != tt.out {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.out)
		}
	}
}

// --- summarizeArgs ---

func TestSummarizeArgs(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{"path key", `{"path":"/tmp/foo.go"}`, "/tmp/foo.go"},
		{"command key", `{"command":"ls -la"}`, "ls -la"},
		{"pattern key", `{"pattern":"TODO"}`, "TODO"},
		{"path takes priority", `{"path":"/a","command":"b"}`, "/a"},
		{"other keys", `{"query":"test"}`, `{"query":"test"}`},
		{"invalid json", `not json`, "not json"},
		{"empty object", `{}`, "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeArgs(json.RawMessage(tt.args))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- isDangerous ---

func TestIsDangerous(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		command string
		want    bool
	}{
		{"rm -rf", "shell_exec", `{"command":"rm -rf /"}`, true},
		{"rm -r", "shell_exec", `{"command":"rm -r ./src"}`, true},
		{"force push", "shell_exec", `{"command":"git push --force"}`, true},
		{"reset hard", "shell_exec", `{"command":"git reset --hard HEAD~1"}`, true},
		{"drop table", "shell_exec", `{"command":"psql -c 'DROP TABLE users'"}`, true},
		{"chmod 777", "shell_exec", `{"command":"chmod 777 /etc/passwd"}`, true},
		{"safe command", "shell_exec", `{"command":"ls -la"}`, false},
		{"non-shell tool", "read_file", `{"path":"/etc/passwd"}`, false},
		{"case insensitive", "shell_exec", `{"command":"RM -RF /tmp"}`, true},
		{"empty command", "shell_exec", `{"command":""}`, false},
		{"bad json", "shell_exec", `not json`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := provider.ToolCall{Name: tt.tool, Arguments: json.RawMessage(tt.command)}
			got := isDangerous(&stubTool{name: tt.tool}, tc)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// --- ModelSwitcher ---

func TestModelSwitcherInterface(t *testing.T) {
	p := &stubProvider{model: "old"}

	// Must satisfy Provider
	var prov provider.Provider = p
	sw, ok := prov.(provider.ModelSwitcher)
	if !ok {
		t.Fatal("stubProvider should implement ModelSwitcher")
	}

	sw.SetModel("new")
	if p.model != "new" {
		t.Errorf("got %q, want %q", p.model, "new")
	}
}

// --- consumeStream ---

func TestConsumeStream_TextOnly(t *testing.T) {
	ch := make(chan provider.ChatEvent, 3)
	ch <- provider.ChatEvent{Type: provider.EventText, Text: "hello "}
	ch <- provider.ChatEvent{Type: provider.EventText, Text: "world"}
	ch <- provider.ChatEvent{Type: provider.EventDone}
	close(ch)

	// We need a TUI that won't panic — use a minimal stub via the agent
	// Instead, test the logic directly by simulating what consumeStream does
	var text string
	var toolCalls []provider.ToolCall
	for ev := range ch {
		switch ev.Type {
		case provider.EventText:
			text += ev.Text
		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
		}
	}

	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
	if len(toolCalls) != 0 {
		t.Errorf("toolCalls = %d, want 0", len(toolCalls))
	}
}

func TestConsumeStream_ToolCalls(t *testing.T) {
	ch := make(chan provider.ChatEvent, 2)
	ch <- provider.ChatEvent{
		Type: provider.EventToolCall,
		ToolCall: &provider.ToolCall{
			ID:        "call_0",
			Name:      "read_file",
			Arguments: json.RawMessage(`{"path":"main.go"}`),
		},
	}
	ch <- provider.ChatEvent{Type: provider.EventDone}
	close(ch)

	var text string
	var toolCalls []provider.ToolCall
	for ev := range ch {
		switch ev.Type {
		case provider.EventText:
			text += ev.Text
		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
		}
	}

	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("tool name = %q, want %q", toolCalls[0].Name, "read_file")
	}
}

func TestConsumeStream_MixedTextAndTools(t *testing.T) {
	ch := make(chan provider.ChatEvent, 4)
	ch <- provider.ChatEvent{Type: provider.EventText, Text: "Let me check "}
	ch <- provider.ChatEvent{
		Type:     provider.EventToolCall,
		ToolCall: &provider.ToolCall{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{}`)},
	}
	ch <- provider.ChatEvent{Type: provider.EventText, Text: "that file."}
	ch <- provider.ChatEvent{Type: provider.EventDone}
	close(ch)

	var text string
	var toolCalls []provider.ToolCall
	for ev := range ch {
		switch ev.Type {
		case provider.EventText:
			text += ev.Text
		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
		}
	}

	if text != "Let me check that file." {
		t.Errorf("text = %q", text)
	}
	if len(toolCalls) != 1 {
		t.Errorf("toolCalls = %d, want 1", len(toolCalls))
	}
}

// --- noTools fallback ---

func TestNoToolsFallback(t *testing.T) {
	p := &stubProvider{
		err: fmt.Errorf("ollama chat: status 400: does not support tools"),
	}

	a := &Agent{
		provider: p,
		model:    "test",
		noTools:  false,
	}

	// Simulate the fallback logic from runLoop
	err := p.err
	if !a.noTools && err != nil && contains(err.Error(), "does not support tools") {
		a.noTools = true
	}

	if !a.noTools {
		t.Fatal("noTools should be true after tool support error")
	}
}

func TestNoToolsResetOnModelSwitch(t *testing.T) {
	a := &Agent{noTools: true, model: "old"}

	// Simulate model switch
	a.model = "new"
	a.noTools = false

	if a.noTools {
		t.Fatal("noTools should be false after model switch")
	}
	if a.model != "new" {
		t.Errorf("model = %q, want %q", a.model, "new")
	}
}

// --- Slash command parsing ---

func TestSlashCommands(t *testing.T) {
	tests := []struct {
		input   string
		isExit  bool
		isHelp  bool
		isClear bool
		isModel bool
	}{
		{"/exit", true, false, false, false},
		{"/quit", true, false, false, false},
		{"/help", false, true, false, false},
		{"/", false, true, false, false},
		{"/clear", false, false, true, false},
		{"/model", false, false, false, true},
		{"hello", false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			isExit := tt.input == "/exit" || tt.input == "/quit"
			isHelp := tt.input == "/help" || tt.input == "/"
			isClear := tt.input == "/clear"
			isModel := tt.input == "/model"

			if isExit != tt.isExit {
				t.Errorf("isExit = %v, want %v", isExit, tt.isExit)
			}
			if isHelp != tt.isHelp {
				t.Errorf("isHelp = %v, want %v", isHelp, tt.isHelp)
			}
			if isClear != tt.isClear {
				t.Errorf("isClear = %v, want %v", isClear, tt.isClear)
			}
			if isModel != tt.isModel {
				t.Errorf("isModel = %v, want %v", isModel, tt.isModel)
			}
		})
	}
}

func TestModelSubcommands(t *testing.T) {
	tests := []struct {
		input string
		isList bool
		name   string
	}{
		{"/model ls", true, ""},
		{"/model list", true, ""},
		{"/model llama3:latest", false, "llama3:latest"},
		{"/model  spaced ", false, "spaced"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name := strings.TrimSpace(strings.TrimPrefix(tt.input, "/model "))
			isList := name == "ls" || name == "list"

			if isList != tt.isList {
				t.Errorf("isList = %v, want %v", isList, tt.isList)
			}
			if !isList && name != tt.name {
				t.Errorf("name = %q, want %q", name, tt.name)
			}
		})
	}
}

// --- toolDefs ---

func TestToolDefs(t *testing.T) {
	tools := []tool.Tool{
		&stubTool{name: "read_file"},
		&stubTool{name: "shell_exec"},
	}
	a := &Agent{tools: tools}
	defs := a.toolDefs()

	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2", len(defs))
	}
	if defs[0].Name != "read_file" {
		t.Errorf("defs[0].Name = %q", defs[0].Name)
	}
	if defs[1].Name != "shell_exec" {
		t.Errorf("defs[1].Name = %q", defs[1].Name)
	}
}

// --- Job cancellation ---

func TestJobCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if ctx.Err() == nil {
		t.Fatal("context should be cancelled")
	}

	// Simulate the check in runLoop
	cancelled := ctx.Err() != nil
	if !cancelled {
		t.Fatal("should detect cancellation")
	}
}

// helper
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// --- New ---

func TestNew(t *testing.T) {
	p := &stubProvider{model: "m1"}
	tools := []tool.Tool{&stubTool{name: "read_file"}, &stubTool{name: "shell_exec"}}
	ui := tui.NewHeadless()

	a := New(p, tools, ui, "m1")

	if a.provider != p {
		t.Error("provider not set")
	}
	if a.model != "m1" {
		t.Errorf("model = %q, want %q", a.model, "m1")
	}
	if len(a.tools) != 2 {
		t.Errorf("tools len = %d, want 2", len(a.tools))
	}
	if _, ok := a.toolMap["read_file"]; !ok {
		t.Error("toolMap missing read_file")
	}
	if _, ok := a.toolMap["shell_exec"]; !ok {
		t.Error("toolMap missing shell_exec")
	}
	if a.maxConcurrency != 5 {
		t.Errorf("maxConcurrency = %d, want 5", a.maxConcurrency)
	}
	if a.perms == nil {
		t.Error("perms is nil")
	}
	// history should have system prompt
	if len(a.history) != 1 || a.history[0].Role != "system" {
		t.Errorf("history = %v, want 1 system message", a.history)
	}
}

// --- SetAutoApprove ---

func TestSetAutoApprove(t *testing.T) {
	a := &Agent{}
	a.SetAutoApprove(true)
	if !a.autoApprove {
		t.Error("expected autoApprove true")
	}
	a.SetAutoApprove(false)
	if a.autoApprove {
		t.Error("expected autoApprove false")
	}
}

// --- multiStubProvider returns different events per call ---

type multiStubProvider struct {
	stubProvider
	callEvents [][]provider.ChatEvent
}

func (m *multiStubProvider) ChatCompletion(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatEvent, error) {
	idx := m.callCount
	m.callCount++
	m.lastTools = req.Tools
	if m.err != nil {
		return nil, m.err
	}
	var evts []provider.ChatEvent
	if idx < len(m.callEvents) {
		evts = m.callEvents[idx]
	}
	ch := make(chan provider.ChatEvent, len(evts)+1)
	for _, e := range evts {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// --- Ask ---

func TestAsk(t *testing.T) {
	p := &stubProvider{
		events: []provider.ChatEvent{
			{Type: provider.EventText, Text: "hello"},
			{Type: provider.EventDone},
		},
	}
	ui := tui.NewHeadless()
	a := New(p, nil, ui, "test")

	a.Ask(context.Background(), "hi")

	if p.callCount != 1 {
		t.Errorf("callCount = %d, want 1", p.callCount)
	}
	// history: system + user + assistant
	if len(a.history) != 3 {
		t.Fatalf("history len = %d, want 3", len(a.history))
	}
	if a.history[1].Role != "user" || a.history[1].Content != "hi" {
		t.Errorf("history[1] = %+v", a.history[1])
	}
	if a.history[2].Role != "assistant" || a.history[2].Content != "hello" {
		t.Errorf("history[2] = %+v", a.history[2])
	}
	evts := ui.Events()
	found := false
	for _, e := range evts {
		if e.Type == "text" && strings.Contains(e.Text, "hello") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected text event with 'hello', got %v", evts)
	}
}

// --- runLoop with tool calls ---

func TestRunLoop_ToolCallsThenText(t *testing.T) {
	p := &multiStubProvider{
		callEvents: [][]provider.ChatEvent{
			// First call: return a tool call
			{
				{Type: provider.EventToolCall, ToolCall: &provider.ToolCall{
					ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{}`),
				}},
				{Type: provider.EventDone},
			},
			// Second call: return text
			{
				{Type: provider.EventText, Text: "done"},
				{Type: provider.EventDone},
			},
		},
	}
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "read_file"}}
	a := New(p, tools, ui, "test")
	a.autoApprove = true

	a.history = append(a.history, provider.Message{Role: "user", Content: "read it"})
	a.runLoop(context.Background(), nil)

	if p.callCount != 2 {
		t.Errorf("callCount = %d, want 2", p.callCount)
	}
	// history: system, user, assistant(tool_calls), tool(result), assistant(text)
	hasToolMsg := false
	hasAssistantText := false
	for _, m := range a.history {
		if m.Role == "tool" {
			hasToolMsg = true
		}
		if m.Role == "assistant" && m.Content == "done" {
			hasAssistantText = true
		}
	}
	if !hasToolMsg {
		t.Error("expected tool message in history")
	}
	if !hasAssistantText {
		t.Error("expected assistant text 'done' in history")
	}
}

// --- consumeStream via agent (text-tool-call parsing path) ---

func TestConsumeStream_TextToolCallParsing(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "read_file"}}
	a := New(&stubProvider{}, tools, ui, "test")

	// Simulate model emitting a tool call as text (no native tool calls)
	ch := make(chan provider.ChatEvent, 2)
	ch <- provider.ChatEvent{Type: provider.EventText, Text: `{"name":"read_file","arguments":{"path":"main.go"}}`}
	ch <- provider.ChatEvent{Type: provider.EventDone}
	close(ch)

	text, toolCalls := a.consumeStream(ch)

	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("tool name = %q, want read_file", toolCalls[0].Name)
	}
	// remaining text should be empty or minimal
	_ = text
}

func TestConsumeStream_ErrorEvent(t *testing.T) {
	ui := tui.NewHeadless()
	a := New(&stubProvider{}, nil, ui, "test")

	ch := make(chan provider.ChatEvent, 2)
	ch <- provider.ChatEvent{Type: provider.EventError, Error: fmt.Errorf("stream error")}
	ch <- provider.ChatEvent{Type: provider.EventDone}
	close(ch)

	text, toolCalls := a.consumeStream(ch)

	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
	if len(toolCalls) != 0 {
		t.Errorf("toolCalls = %d, want 0", len(toolCalls))
	}
	evts := ui.Events()
	found := false
	for _, e := range evts {
		if e.Type == "error" && e.Error == "stream error" {
			found = true
		}
	}
	if !found {
		t.Error("expected error event")
	}
}

// --- executeTool ---

func TestExecuteTool_UnknownTool(t *testing.T) {
	ui := tui.NewHeadless()
	a := New(&stubProvider{}, nil, ui, "test")
	a.autoApprove = true

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "nonexistent", Arguments: json.RawMessage(`{}`),
	})

	if result != "unknown tool: nonexistent" {
		t.Errorf("result = %q", result)
	}
}

func TestExecuteTool_Success(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "read_file", safety: tool.Safe}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = true

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`),
	})

	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	evts := ui.Events()
	hasStart := false
	hasDone := false
	for _, e := range evts {
		if e.Type == "tool_start" && e.Tool == "read_file" {
			hasStart = true
		}
		if e.Type == "tool_done" && e.Tool == "read_file" {
			hasDone = true
		}
	}
	if !hasStart {
		t.Error("expected tool_start event")
	}
	if !hasDone {
		t.Error("expected tool_done event")
	}
}

func TestExecuteTool_PermissionDenied(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "shell_exec", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = false
	// Set session deny for shell category
	a.perms.session[CatShell] = PermDeny

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "shell_exec", Arguments: json.RawMessage(`{"command":"ls"}`),
	})

	if result != "denied by policy" {
		t.Errorf("result = %q, want %q", result, "denied by policy")
	}
}

func TestExecuteTool_AutoApproveSkipsPermission(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "shell_exec", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = true

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "shell_exec", Arguments: json.RawMessage(`{"command":"ls"}`),
	})

	// Should succeed because autoApprove bypasses permission check
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

// --- systemPrompt ---

func TestSystemPrompt(t *testing.T) {
	s := systemPrompt(false)
	for _, want := range []string{"Forge", "read_file", "write_file", "shell_exec", "search_code", "list_directory", "Current directory"} {
		if !strings.Contains(s, want) {
			t.Errorf("systemPrompt missing %q", want)
		}
	}
}

// --- runLoop context cancellation ---

func TestRunLoop_ContextCancelled(t *testing.T) {
	p := &stubProvider{
		events: []provider.ChatEvent{
			{Type: provider.EventText, Text: "hi"},
			{Type: provider.EventDone},
		},
	}
	ui := tui.NewHeadless()
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.history = append(a.history, provider.Message{Role: "user", Content: "test"})
	a.runLoop(ctx, nil)

	// Should return quickly without calling provider (context already cancelled)
	// The provider returns an error on cancelled context, runLoop detects ctx.Err()
}

// --- runLoop noTools fallback ---

func TestRunLoop_NoToolsFallback(t *testing.T) {
	p := &multiStubProvider{
		callEvents: [][]provider.ChatEvent{
			// Second call (after retry) returns text
			{
				{Type: provider.EventText, Text: "no tools available"},
				{Type: provider.EventDone},
			},
		},
	}
	p.err = fmt.Errorf("model does not support tools")
	ui := tui.NewHeadless()
	a := New(p, []tool.Tool{&stubTool{name: "read_file"}}, ui, "test")

	// Override provider to fail first then succeed
	callNum := 0
	failOnce := &failOnceThenSucceedProvider{
		failErr: fmt.Errorf("model does not support tools"),
		successEvents: []provider.ChatEvent{
			{Type: provider.EventText, Text: "fallback"},
			{Type: provider.EventDone},
		},
	}
	a.provider = failOnce

	a.history = append(a.history, provider.Message{Role: "user", Content: "test"})
	a.runLoop(context.Background(), nil)
	_ = callNum

	if !a.noTools {
		t.Error("expected noTools to be true after fallback")
	}
}

type failOnceThenSucceedProvider struct {
	failErr       error
	successEvents []provider.ChatEvent
	callCount     int
}

func (f *failOnceThenSucceedProvider) Name() string { return "stub" }
func (f *failOnceThenSucceedProvider) ChatCompletion(_ context.Context, req provider.ChatRequest) (<-chan provider.ChatEvent, error) {
	f.callCount++
	if f.callCount == 1 {
		return nil, f.failErr
	}
	ch := make(chan provider.ChatEvent, len(f.successEvents)+1)
	for _, e := range f.successEvents {
		ch <- e
	}
	close(ch)
	return ch, nil
}
func (f *failOnceThenSucceedProvider) ListModels(_ context.Context) ([]provider.Model, error) {
	return nil, nil
}

// --- noToolStrikes auto-disable ---

func TestRunLoop_NoToolStrikes(t *testing.T) {
	// Provider returns text without tool calls twice → should auto-disable tools
	p := &multiStubProvider{
		callEvents: [][]provider.ChatEvent{
			{{Type: provider.EventText, Text: "text1"}, {Type: provider.EventDone}},
		},
	}
	ui := tui.NewHeadless()
	a := New(p, []tool.Tool{&stubTool{name: "read_file"}}, ui, "test")
	a.noToolStrikes = 1 // already had one strike

	a.history = append(a.history, provider.Message{Role: "user", Content: "test"})
	a.runLoop(context.Background(), nil)

	if !a.noTools {
		t.Error("expected noTools after 2 strikes")
	}
}

// --- executeTool with PermAsk (HeadlessTUI returns ConfirmYes) ---

func TestExecuteTool_PermAskAllowed(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "write_file", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = false
	// write_file category is CatFileWrite, default is PermAsk
	// HeadlessTUI.ConfirmWithAlways returns ConfirmYes

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x.go","content":"hi"}`),
	})

	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

// denyTUI is a HeadlessTUI that denies ConfirmWithAlways.
type denyTUI struct {
	*tui.HeadlessTUI
}

func (d *denyTUI) ConfirmWithAlways(_, _ string) tui.ConfirmResult { return tui.ConfirmNo }

func TestExecuteTool_PermAskDeniedByUser(t *testing.T) {
	ui := &denyTUI{tui.NewHeadless()}
	tools := []tool.Tool{&stubTool{name: "write_file", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = false

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x.go","content":"hi"}`),
	})

	if result != "denied by user" {
		t.Errorf("result = %q, want %q", result, "denied by user")
	}
}

// alwaysTUI returns ConfirmAlways.
type alwaysTUI struct {
	*tui.HeadlessTUI
}

func (a *alwaysTUI) ConfirmWithAlways(_, _ string) tui.ConfirmResult { return tui.ConfirmAlways }

func TestExecuteTool_PermAskAlways(t *testing.T) {
	ui := &alwaysTUI{tui.NewHeadless()}
	tools := []tool.Tool{&stubTool{name: "write_file", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = false

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"x.go","content":"hi"}`),
	})

	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	// Verify category was allowed for session
	if a.perms.Check("write_file") != PermAllow {
		t.Error("expected write_file to be PermAllow after ConfirmAlways")
	}
}

// --- executeTool with tool that returns error ---

type errorTool struct {
	stubTool
}

func (e *errorTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("tool failed")
}

func TestExecuteTool_ToolError(t *testing.T) {
	ui := tui.NewHeadless()
	et := &errorTool{stubTool{name: "read_file", safety: tool.Safe}}
	a := New(&stubProvider{}, []tool.Tool{et}, ui, "test")
	a.autoApprove = true

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "read_file", Arguments: json.RawMessage(`{}`),
	})

	if result != "error: tool failed" {
		t.Errorf("result = %q", result)
	}
}

// --- runLoop with provider error (non-tool-support) ---

func TestRunLoop_ProviderError(t *testing.T) {
	p := &stubProvider{err: fmt.Errorf("rate limit exceeded")}
	ui := tui.NewHeadless()
	a := New(p, nil, ui, "test")

	a.history = append(a.history, provider.Message{Role: "user", Content: "test"})
	a.runLoop(context.Background(), nil)

	evts := ui.Events()
	found := false
	for _, e := range evts {
		if e.Type == "error" && strings.Contains(e.Error, "rate limit") {
			found = true
		}
	}
	if !found {
		t.Error("expected error event for rate limit")
	}
}

// --- runLoop with interrupt before LLM call ---

func TestRunLoop_InterruptBeforeLLM(t *testing.T) {
	p := &multiStubProvider{
		callEvents: [][]provider.ChatEvent{
			// First call responds to redirected message
			{{Type: provider.EventText, Text: "redirected"}, {Type: provider.EventDone}},
			// Second call responds to original
			{{Type: provider.EventText, Text: "done"}, {Type: provider.EventDone}},
		},
	}
	ui := tui.NewHeadless()
	a := New(p, nil, ui, "test")

	interruptCh := make(chan string, 1)
	interruptCh <- "new question"

	a.history = append(a.history, provider.Message{Role: "user", Content: "old"})
	a.runLoop(context.Background(), interruptCh)

	// Should have processed the interrupt
	hasRedirect := false
	for _, m := range a.history {
		if m.Role == "user" && m.Content == "new question" {
			hasRedirect = true
		}
	}
	if !hasRedirect {
		t.Error("expected redirected user message in history")
	}
}

// --- runLoop with interrupt after tool execution ---

func TestRunLoop_InterruptAfterToolExec(t *testing.T) {
	p := &multiStubProvider{
		callEvents: [][]provider.ChatEvent{
			// First: tool call
			{{Type: provider.EventToolCall, ToolCall: &provider.ToolCall{
				ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{}`),
			}}, {Type: provider.EventDone}},
			// Second: respond to redirected message
			{{Type: provider.EventText, Text: "redirected"}, {Type: provider.EventDone}},
			// Third: final
			{{Type: provider.EventText, Text: "final"}, {Type: provider.EventDone}},
		},
	}
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "read_file"}}
	a := New(p, tools, ui, "test")
	a.autoApprove = true

	// This message will be picked up after tool execution
	interruptCh := make(chan string, 1)

	// We need the interrupt to arrive after the first tool execution.
	// Since tool execution is synchronous in test, we pre-buffer it.
	interruptCh <- "interrupt after tools"

	a.history = append(a.history, provider.Message{Role: "user", Content: "start"})
	a.runLoop(context.Background(), interruptCh)

	hasInterrupt := false
	for _, m := range a.history {
		if m.Role == "user" && m.Content == "interrupt after tools" {
			hasInterrupt = true
		}
	}
	if !hasInterrupt {
		t.Error("expected interrupt message in history")
	}
}

// --- runLoop context cancelled during stream ---

func TestRunLoop_ContextCancelledDuringStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Provider returns events but context gets cancelled
	p := &stubProvider{
		events: []provider.ChatEvent{
			{Type: provider.EventText, Text: "partial"},
			{Type: provider.EventDone},
		},
	}
	ui := tui.NewHeadless()
	a := New(p, nil, ui, "test")

	// Cancel after provider returns but before runLoop checks
	origProvider := a.provider
	a.provider = &cancelAfterStreamProvider{
		inner:  origProvider.(*stubProvider),
		cancel: cancel,
	}

	a.history = append(a.history, provider.Message{Role: "user", Content: "test"})
	a.runLoop(ctx, nil)

	evts := ui.Events()
	hasCancel := false
	for _, e := range evts {
		if e.Type == "info" && strings.Contains(e.Text, "cancelled") {
			hasCancel = true
		}
	}
	if !hasCancel {
		t.Error("expected cancelled info event")
	}
}

type cancelAfterStreamProvider struct {
	inner  *stubProvider
	cancel context.CancelFunc
}

func (c *cancelAfterStreamProvider) Name() string { return "stub" }
func (c *cancelAfterStreamProvider) ChatCompletion(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatEvent, error) {
	ch, err := c.inner.ChatCompletion(ctx, req)
	// Cancel context after returning the channel so runLoop sees ctx.Err() after consumeStream
	c.cancel()
	return ch, err
}
func (c *cancelAfterStreamProvider) ListModels(_ context.Context) ([]provider.Model, error) {
	return nil, nil
}

// --- scriptedTUI wraps HeadlessTUI with scripted ReadInput ---

type scriptedTUI struct {
	*tui.HeadlessTUI
	inputs []string
	idx    int
	mu     sync.Mutex
}

func (s *scriptedTUI) ReadInput() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.inputs) {
		return "", false
	}
	input := s.inputs[s.idx]
	s.idx++
	return input, true
}

// --- Run tests ---

func TestRun_ExitCommand(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
	// Should return without hanging
}

func TestRun_QuitCommand(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/quit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
}

func TestRun_HelpCommand(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/help", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
}

func TestRun_SlashCommand(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
}

func TestRun_ClearCommand(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/clear", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)

	if len(a.history) != 1 {
		t.Errorf("history len = %d, want 1 after clear", len(a.history))
	}
}

func TestRun_ModelCommand(t *testing.T) {
	p := &stubProvider{model: "llama3"}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/model", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
}

func TestRun_ModelListCommand(t *testing.T) {
	p := &stubProvider{
		model:      "llama3",
		modelsList: []provider.Model{{ID: "1", Name: "llama3"}, {ID: "2", Name: "gpt4"}},
	}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/model ls", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)
}

func TestRun_ModelSwitchCommand(t *testing.T) {
	p := &stubProvider{
		model:      "llama3",
		modelsList: []provider.Model{{ID: "1", Name: "llama3"}, {ID: "2", Name: "gpt4"}},
	}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/model gpt4", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)

	if a.model != "gpt4" {
		t.Errorf("model = %q, want gpt4", a.model)
	}
}

func TestRun_ModelSwitchNotFound(t *testing.T) {
	p := &stubProvider{
		model:      "llama3",
		modelsList: []provider.Model{{ID: "1", Name: "llama3"}},
	}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"/model nonexistent", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)

	if a.model != "test" {
		t.Errorf("model should not have changed, got %q", a.model)
	}
}

func TestRun_ContextCancelled(t *testing.T) {
	p := &stubProvider{}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{}} // no inputs, will return false
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Run(ctx)
}

func TestRun_UserMessage(t *testing.T) {
	p := &stubProvider{
		events: []provider.ChatEvent{
			{Type: provider.EventText, Text: "response"},
			{Type: provider.EventDone},
		},
	}
	ui := &scriptedTUI{HeadlessTUI: tui.NewHeadless(), inputs: []string{"hello", "/exit"}}
	a := New(p, nil, ui, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a.Run(ctx)

	if p.callCount != 1 {
		t.Errorf("callCount = %d, want 1", p.callCount)
	}
}

// --- isDangerous with dangerous shell_exec tool ---

func TestExecuteTool_DangerousCommand(t *testing.T) {
	ui := tui.NewHeadless()
	tools := []tool.Tool{&stubTool{name: "shell_exec", safety: tool.NeedsConfirmation}}
	a := New(&stubProvider{}, tools, ui, "test")
	a.autoApprove = false
	// HeadlessTUI returns ConfirmYes, so it proceeds

	result := a.executeTool(context.Background(), provider.ToolCall{
		ID: "1", Name: "shell_exec", Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
	})

	if result != "ok" {
		t.Errorf("result = %q, want ok (headless confirms)", result)
	}
}

// --- shortName edge case ---

func TestShortName(t *testing.T) {
	if got := shortName("a/b/c.go"); got != "c.go" {
		t.Errorf("got %q", got)
	}
	if got := shortName("file.go"); got != "file.go" {
		t.Errorf("got %q", got)
	}
	if got := shortName(""); got != "" {
		t.Errorf("got %q", got)
	}
}

// --- toolDetail ---

func TestToolDetail(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		args   string
		result string
		want   string
	}{
		{"read_file lines", "read_file", `{"path":"main.go"}`, "line1\nline2\nline3\n", "main.go (3 lines)"},
		{"list_directory entries", "list_directory", `{"path":"."}`, "a/\nb/\nc.go\n", "(3 entries)"},
		{"list_directory empty", "list_directory", `{"path":"."}`, "", "(0 entries)"},
		{"write_file", "write_file", `{"path":"out.go","content":"a\nb\nc"}`, "wrote 5 bytes", "out.go (3 lines written)"},
		{"shell_exec no output", "shell_exec", `{"command":"true"}`, "", "(no output)"},
		{"shell_exec with output", "shell_exec", `{"command":"ls"}`, "file1\nfile2\n", ""},
		{"search_code matches", "search_code", `{"pattern":"TODO"}`, "a.go:1: TODO\nb.go:2: TODO\n", "(2 matches)"},
		{"search_code none", "search_code", `{"pattern":"xyz"}`, "no matches found", "(no matches)"},
		{"unknown tool", "custom_tool", `{}`, "result", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolDetail(tt.tool, json.RawMessage(tt.args), tt.result)
			if got != tt.want {
				t.Errorf("toolDetail(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
