package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaSetModel(t *testing.T) {
	o := &Ollama{host: "http://localhost:11434", model: "llama3"}

	if o.Model() != "llama3" {
		t.Fatalf("initial model = %q, want %q", o.Model(), "llama3")
	}

	o.SetModel("qwen3:8b")
	if o.Model() != "qwen3:8b" {
		t.Errorf("after SetModel = %q, want %q", o.Model(), "qwen3:8b")
	}
}

func TestOllamaImplementsModelSwitcher(t *testing.T) {
	o := &Ollama{model: "test"}
	var p Provider = o

	sw, ok := p.(ModelSwitcher)
	if !ok {
		t.Fatal("Ollama should implement ModelSwitcher")
	}

	sw.SetModel("new")
	if o.Model() != "new" {
		t.Errorf("got %q, want %q", o.Model(), "new")
	}
}

func TestOllamaName(t *testing.T) {
	o := &Ollama{}
	if o.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", o.Name(), "ollama")
	}
}

func TestNewOllama(t *testing.T) {
	// Mock server that returns a model list for detectModel
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"name":"llama3","details":{"parameter_size":"8B"}}]}`)
	}))
	defer srv.Close()

	t.Run("with explicit model", func(t *testing.T) {
		o := NewOllama(srv.URL, "mymodel")
		if o.host != srv.URL {
			t.Errorf("host = %q, want %q", o.host, srv.URL)
		}
		if o.Model() != "mymodel" {
			t.Errorf("model = %q, want %q", o.Model(), "mymodel")
		}
	})

	t.Run("auto-detect model", func(t *testing.T) {
		o := NewOllama(srv.URL, "")
		if o.Model() != "llama3" {
			t.Errorf("model = %q, want %q", o.Model(), "llama3")
		}
	})
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/tags" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"name":"llama3","details":{"parameter_size":"8B"}},{"name":"qwen3:8b","details":{"parameter_size":"8B"}}]}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "x", client: http.DefaultClient}
	models, err := o.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "llama3" {
		t.Errorf("models[0].ID = %q, want %q", models[0].ID, "llama3")
	}
	if models[1].Name != "qwen3:8b" {
		t.Errorf("models[1].Name = %q, want %q", models[1].Name, "qwen3:8b")
	}
}

func TestChatCompletionTextOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/chat" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hello"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":" world"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "test", client: http.DefaultClient}
	ch, err := o.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var texts []string
	var gotDone bool
	for ev := range ch {
		switch ev.Type {
		case EventText:
			texts = append(texts, ev.Text)
		case EventDone:
			gotDone = true
		case EventError:
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}
	if got := len(texts); got != 2 {
		t.Fatalf("got %d text events, want 2", got)
	}
	if texts[0] != "Hello" || texts[1] != " world" {
		t.Errorf("texts = %v, want [Hello  world]", texts)
	}
	if !gotDone {
		t.Error("missing EventDone")
	}
}

func TestChatCompletionToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"main.go"}}}]},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "test", client: http.DefaultClient}
	ch, err := o.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "read main.go"}},
		Tools: []ToolDef{{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var toolCalls []*ToolCall
	for ev := range ch {
		switch ev.Type {
		case EventToolCall:
			toolCalls = append(toolCalls, ev.ToolCall)
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("tool call name = %q, want %q", toolCalls[0].Name, "read_file")
	}
	if !strings.HasPrefix(toolCalls[0].ID, "call_") {
		t.Errorf("tool call ID = %q, want prefix %q", toolCalls[0].ID, "call_")
	}
}

func TestChatCompletionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "test", client: http.DefaultClient}
	_, err := o.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got == "" {
		t.Error("error message is empty")
	}
}

func TestChatCompletionWithToolCallsInMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request body includes tool_calls in messages
		var req ollamaChatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(req.Messages) != 1 || len(req.Messages[0].ToolCalls) != 1 {
			t.Errorf("expected 1 message with 1 tool call, got %d messages", len(req.Messages))
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"done"},"done":true}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "test", client: http.DefaultClient}
	ch, err := o.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{{
				ID:        "call_0",
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"x"}`),
			}},
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	for range ch {
	}
}

func TestDetectModelNoModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	o := &Ollama{host: srv.URL, model: "", client: http.DefaultClient}
	m, err := o.detectModel()
	if err == nil {
		t.Fatal("expected error for no models")
	}
	if m != "" {
		t.Errorf("model = %q, want empty", m)
	}
}
