package tui

import (
	"context"
	"strings"
	"sync"
)

// Event represents a structured output event from the agent.
type Event struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// HeadlessTUI captures agent output as structured events instead of writing to a terminal.
type HeadlessTUI struct {
	mu     sync.Mutex
	events []Event
	text   strings.Builder
}

func NewHeadless() *HeadlessTUI { return &HeadlessTUI{} }

// Events returns all captured events and resets the buffer.
func (h *HeadlessTUI) Events() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flush()
	out := h.events
	h.events = nil
	return out
}

func (h *HeadlessTUI) add(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, e)
}

// flush converts accumulated streamed text into a text event.
func (h *HeadlessTUI) flush() {
	if h.text.Len() > 0 {
		h.events = append(h.events, Event{Type: "text", Text: strings.TrimSpace(h.text.String())})
		h.text.Reset()
	}
}

func (h *HeadlessTUI) PrintBanner()              {}
func (h *HeadlessTUI) ReadInput() (string, bool)  { return "", false }
func (h *HeadlessTUI) PrintHelp()                 {}
func (h *HeadlessTUI) ResetSigCount()             {}
func (h *HeadlessTUI) StartSpinner(_ string)      {}
func (h *HeadlessTUI) StopSpinner()               {}

func (h *HeadlessTUI) StreamToken(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text.WriteString(token)
}

func (h *HeadlessTUI) EndStream() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flush()
}

func (h *HeadlessTUI) ToolStart(name, detail string) {
	h.add(Event{Type: "tool_start", Tool: name, Detail: detail})
}

func (h *HeadlessTUI) ToolDone(name, detail string) {
	h.add(Event{Type: "tool_done", Tool: name, Detail: detail})
}

func (h *HeadlessTUI) ToolError(name string, err error) {
	h.add(Event{Type: "tool_error", Tool: name, Error: err.Error()})
}

func (h *HeadlessTUI) Confirm(_ string) bool { return true }

func (h *HeadlessTUI) ConfirmWithAlways(_, _ string) ConfirmResult { return ConfirmYes }

func (h *HeadlessTUI) Error(msg string) {
	h.add(Event{Type: "error", Error: msg})
}

func (h *HeadlessTUI) Info(msg string) {
	h.add(Event{Type: "info", Text: msg})
}

func (h *HeadlessTUI) SetJobCancel(_ context.CancelFunc) {}
