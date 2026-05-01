package agent

import (
	"testing"
)

var testTools = map[string]bool{
	"shell_exec":     true,
	"read_file":      true,
	"list_directory": true,
}

func TestParseTextToolCalls_SingleJSON(t *testing.T) {
	text := `{"name": "shell_exec", "arguments": {"command": "ls -la"}}`
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "shell_exec" {
		t.Errorf("name = %q", calls[0].Name)
	}
	if remaining != "" {
		t.Errorf("remaining = %q, want empty", remaining)
	}
}

func TestParseTextToolCalls_EmbeddedInProse(t *testing.T) {
	text := "Sure, I'll check the codebase:\n\n```json\n{\"name\": \"list_directory\", \"arguments\": {\"path\": \"/tmp\"}}\n```\n"
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "list_directory" {
		t.Errorf("name = %q", calls[0].Name)
	}
	if remaining == "" {
		t.Error("remaining should contain the prose text")
	}
}

func TestParseTextToolCalls_BareJSONInProse(t *testing.T) {
	text := "Let me run this:\n{\"name\": \"shell_exec\", \"arguments\": {\"command\": \"pwd\"}}\nDone."
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "shell_exec" {
		t.Errorf("name = %q", calls[0].Name)
	}
	if !contains(remaining, "Let me run this") {
		t.Errorf("remaining should have prose: %q", remaining)
	}
}

func TestParseTextToolCalls_MarkdownFence(t *testing.T) {
	text := "```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"main.go\"}}\n```"
	calls, _ := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("name = %q", calls[0].Name)
	}
}

func TestParseTextToolCalls_BareFence(t *testing.T) {
	text := "```\n{\"name\": \"shell_exec\", \"arguments\": {\"command\": \"pwd\"}}\n```"
	calls, _ := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
}

func TestParseTextToolCalls_Array(t *testing.T) {
	text := `[{"name": "read_file", "arguments": {"path": "a.go"}}, {"name": "read_file", "arguments": {"path": "b.go"}}]`
	calls, _ := parseTextToolCalls(text, testTools)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
}

func TestParseTextToolCalls_UnknownTool(t *testing.T) {
	text := `{"name": "unknown_tool", "arguments": {}}`
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 0 {
		t.Fatalf("got %d calls, want 0", len(calls))
	}
	if remaining != text {
		t.Errorf("remaining should be original text")
	}
}

func TestParseTextToolCalls_PlainText(t *testing.T) {
	text := "Here is my analysis of the codebase..."
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 0 {
		t.Fatalf("got %d calls, want 0", len(calls))
	}
	if remaining != text {
		t.Errorf("remaining = %q, want original", remaining)
	}
}

func TestParseTextToolCalls_Empty(t *testing.T) {
	calls, remaining := parseTextToolCalls("", testTools)
	if len(calls) != 0 {
		t.Fatalf("got %d calls, want 0", len(calls))
	}
	if remaining != "" {
		t.Errorf("remaining = %q", remaining)
	}
}

func TestParseTextToolCalls_Whitespace(t *testing.T) {
	text := `  {"name": "shell_exec", "arguments": {"command": "go test"}}  `
	calls, _ := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
}

func TestParseTextToolCalls_RealWorldQwen(t *testing.T) {
	// Exact output from qwen2.5-coder (compact JSON)
	text := "Sure, I'll check the current codebase for you. First, let's list the files in the directory to see what we have:\n\n```json\n{\"name\": \"list_directory\", \"arguments\": {\"path\": \"/Users/serjik.isagholian/Projects/workspace/forge\"}}\n```\n"
	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "list_directory" {
		t.Errorf("name = %q", calls[0].Name)
	}
	if !contains(remaining, "check the current codebase") {
		t.Errorf("remaining should have prose: %q", remaining)
	}
}

func TestParseTextToolCalls_RealWorldQwenPretty(t *testing.T) {
	// Exact output from qwen2.5-coder (pretty-printed JSON in fence)
	text := `To check the current codebase, I'll perform a few operations:

1. List all Go files in the directory.
2. Build the project to catch any compilation errors.
3. Run tests to ensure everything is working as expected.

Here's the plan:

` + "```json\n{\n \"name\": \"list_directory\",\n \"arguments\": {\n \"path\": \"./\"\n }\n}\n```" + `

Please execute this command and share the output so I can proceed with the next steps.`

	calls, remaining := parseTextToolCalls(text, testTools)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1; remaining=%q", len(calls), remaining)
	}
	if calls[0].Name != "list_directory" {
		t.Errorf("name = %q", calls[0].Name)
	}
}
