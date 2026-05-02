package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/serjikisa/forge/internal/provider"
)

type stubProvider struct {
	events []provider.ChatEvent
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) ChatCompletion(_ context.Context, _ provider.ChatRequest) (<-chan provider.ChatEvent, error) {
	ch := make(chan provider.ChatEvent, len(s.events)+1)
	for _, e := range s.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}
func (s *stubProvider) ListModels(_ context.Context) ([]provider.Model, error) { return nil, nil }

func TestHealthEndpoint(t *testing.T) {
	s := New(&stubProvider{}, "test")
	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestChatEndpoint(t *testing.T) {
	p := &stubProvider{events: []provider.ChatEvent{
		{Type: provider.EventText, Text: "Hello!"},
		{Type: provider.EventDone},
	}}
	s := New(p, "test")
	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	body, _ := json.Marshal(ChatRequest{Message: "hi"})
	resp, err := http.Post(ts.URL+"/v1/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var cr ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		t.Fatal(err)
	}
	if len(cr.Events) == 0 {
		t.Fatal("expected events")
	}
	// Should have a text event with "Hello!"
	found := false
	for _, e := range cr.Events {
		if e.Type == "text" && e.Text == "Hello!" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing text event, got %+v", cr.Events)
	}
}

func TestChatEndpointEmptyMessage(t *testing.T) {
	s := New(&stubProvider{}, "test")
	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	body, _ := json.Marshal(ChatRequest{Message: ""})
	resp, err := http.Post(ts.URL+"/v1/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
