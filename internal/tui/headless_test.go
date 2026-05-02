package tui

import "testing"

// Compile-time check that both TUI and HeadlessTUI satisfy UI.
var (
	_ UI = (*TUI)(nil)
	_ UI = (*HeadlessTUI)(nil)
)

func TestHeadlessStreamCapture(t *testing.T) {
	h := NewHeadless()
	h.StreamToken("hello ")
	h.StreamToken("world")
	h.EndStream()

	events := h.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "text" || events[0].Text != "hello world" {
		t.Errorf("got %+v", events[0])
	}
}

func TestHeadlessToolEvents(t *testing.T) {
	h := NewHeadless()
	h.ToolStart("read_file", "main.go")
	h.ToolDone("read_file", "main.go (23 lines)")

	events := h.Events()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != "tool_start" || events[0].Tool != "read_file" {
		t.Errorf("event[0] = %+v", events[0])
	}
	if events[1].Type != "tool_done" || events[1].Detail != "main.go (23 lines)" {
		t.Errorf("event[1] = %+v", events[1])
	}
}

func TestHeadlessEventsResets(t *testing.T) {
	h := NewHeadless()
	h.Info("hello")
	events := h.Events()
	if len(events) != 1 {
		t.Fatalf("first call: got %d events", len(events))
	}
	events = h.Events()
	if len(events) != 0 {
		t.Fatalf("second call: got %d events, want 0", len(events))
	}
}

func TestHeadlessConfirmAutoApproves(t *testing.T) {
	h := NewHeadless()
	if !h.Confirm("allow?") {
		t.Fatal("headless Confirm should return true")
	}
}
