// streaming.go implements StreamingTUI, which sends events to a channel
// in real-time for use with SSE endpoints.
package tui

import "context"

// StreamingTUI sends events to a channel as they occur.
type StreamingTUI struct {
	ch chan Event
}

func NewStreaming() (*StreamingTUI, <-chan Event) {
	ch := make(chan Event, 32)
	return &StreamingTUI{ch: ch}, ch
}

func (s *StreamingTUI) Close() { close(s.ch) }

func (s *StreamingTUI) PrintBanner()         {}
func (s *StreamingTUI) ReadInput() (string, bool) { return "", false }
func (s *StreamingTUI) PrintHelp()           {}
func (s *StreamingTUI) ResetSigCount()       {}
func (s *StreamingTUI) StartSpinner(_ string) {}
func (s *StreamingTUI) StopSpinner()         {}
func (s *StreamingTUI) SetJobCancel(_ context.CancelFunc) {}

func (s *StreamingTUI) StreamToken(token string) {
	s.ch <- Event{Type: "token", Text: token}
}

func (s *StreamingTUI) EndStream() {
	s.ch <- Event{Type: "done"}
}

func (s *StreamingTUI) ToolStart(name, detail string) {
	s.ch <- Event{Type: "tool_start", Tool: name, Detail: detail}
}

func (s *StreamingTUI) ToolDone(name, detail string) {
	s.ch <- Event{Type: "tool_done", Tool: name, Detail: detail}
}

func (s *StreamingTUI) ToolError(name string, err error) {
	s.ch <- Event{Type: "tool_error", Tool: name, Error: err.Error()}
}

func (s *StreamingTUI) Confirm(_ string) bool { return true }
func (s *StreamingTUI) ConfirmWithAlways(_, _ string) ConfirmResult { return ConfirmYes }

func (s *StreamingTUI) Error(msg string) {
	s.ch <- Event{Type: "error", Error: msg}
}

func (s *StreamingTUI) Info(msg string) {
	s.ch <- Event{Type: "info", Text: msg}
}
