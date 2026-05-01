package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/serjikisa/forge/pkg/slogr"
)

type Anthropic struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropic(apiKey, model string) *Anthropic {
	return &Anthropic{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (a *Anthropic) Name() string      { return "anthropic" }
func (a *Anthropic) Model() string     { return a.model }
func (a *Anthropic) SetModel(m string) { a.model = m }

func (a *Anthropic) ListModels(_ context.Context) ([]Model, error) {
	return []Model{
		{ID: "claude-opus-5", Name: "claude-opus-5"},
		{ID: "claude-sonnet-5", Name: "claude-sonnet-5"},
		{ID: "claude-fable-5", Name: "claude-fable-5"},
		{ID: "claude-opus-4-6-v1", Name: "claude-opus-4-6-v1"},
		{ID: "claude-sonnet-4-20250514", Name: "claude-sonnet-4-20250514"},
		{ID: "claude-haiku-4-5-20251001", Name: "claude-haiku-4-5-20251001"},
	}, nil
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMsg     `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
}

type anthropicMsg struct {
	Role    string            `json:"role"`
	Content json.RawMessage   `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func (a *Anthropic) ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	ar := a.buildRequest(req)
	body, err := json.Marshal(ar)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 16)
	go a.streamResponse(ctx, resp, ch)
	return ch, nil
}

func (a *Anthropic) buildRequest(req ChatRequest) anthropicRequest {
	ar := anthropicRequest{
		Model:     a.model,
		MaxTokens: 4096,
		Stream:    true,
	}

	var msgs []anthropicMsg
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			ar.System = m.Content
		case "user":
			content, _ := json.Marshal([]anthropicContentBlock{{Type: "text", Text: m.Content}})
			msgs = append(msgs, anthropicMsg{Role: "user", Content: content})
		case "assistant":
			var blocks []anthropicContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
			content, _ := json.Marshal(blocks)
			msgs = append(msgs, anthropicMsg{Role: "assistant", Content: content})
		case "tool":
			block := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			content, _ := json.Marshal([]anthropicContentBlock{block})
			msgs = append(msgs, anthropicMsg{Role: "user", Content: content})
		}
	}
	ar.Messages = msgs

	for _, t := range req.Tools {
		ar.Tools = append(ar.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	return ar
}

func (a *Anthropic) streamResponse(ctx context.Context, resp *http.Response, ch chan<- ChatEvent) {
	defer resp.Body.Close()
	defer close(ch)

	type activeTC struct {
		id    string
		name  string
		input strings.Builder
	}
	var currentTC *activeTC

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- ChatEvent{Type: EventError, Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Message *struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			slogr.Warn("anthropic: bad event", "err", err)
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				currentTC = &activeTC{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				ch <- ChatEvent{Type: EventText, Text: event.Delta.Text}
			}
			if event.Delta.Type == "input_json_delta" && currentTC != nil {
				currentTC.input.WriteString(event.Delta.PartialJSON)
			}
		case "content_block_stop":
			if currentTC != nil {
				args := json.RawMessage(currentTC.input.String())
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				ch <- ChatEvent{
					Type: EventToolCall,
					ToolCall: &ToolCall{
						ID:        currentTC.id,
						Name:      currentTC.name,
						Arguments: args,
					},
				}
				currentTC = nil
			}
		case "message_start":
			if event.Message != nil && event.Message.Usage.InputTokens > 0 {
				ch <- ChatEvent{Type: EventDone, Usage: &Usage{
					PromptTokens: event.Message.Usage.InputTokens,
					OutputTokens: event.Message.Usage.OutputTokens,
				}}
			}
		case "message_delta":
			if event.Usage != nil {
				ch <- ChatEvent{Type: EventDone, Usage: &Usage{OutputTokens: event.Usage.OutputTokens}}
			}
		case "message_stop":
			ch <- ChatEvent{Type: EventDone}
			return
		}
	}
}
