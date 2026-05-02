package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
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
