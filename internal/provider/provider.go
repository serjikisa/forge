package provider

import (
	"context"
	"encoding/json"
)

type Provider interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
	ListModels(ctx context.Context) ([]Model, error)
	Name() string
}

// ModelSwitcher is optionally implemented by providers that support runtime model changes.
type ModelSwitcher interface {
	SetModel(model string)
}

type ChatRequest struct {
	Messages []Message
	Tools    []ToolDef
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string
	Content    string
	Error      error
}

type EventType int

const (
	EventText EventType = iota
	EventToolCall
	EventError
	EventDone
)

type ChatEvent struct {
	Type     EventType
	Text     string
	ToolCall *ToolCall
	Error    error
}

type Model struct {
	ID          string
	Name        string
	ContextSize int
}
