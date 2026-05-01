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

type OpenAI struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAI(baseURL, apiKey, model string) *OpenAI {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
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

func (o *OpenAI) Name() string      { return "openai" }
func (o *OpenAI) Model() string     { return o.model }
func (o *OpenAI) SetModel(m string) { o.model = m }

func (o *OpenAI) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}

	models := make([]Model, 0, len(result.Data))
	for _, m := range result.Data {
		if strings.HasPrefix(m.ID, "gpt-") || strings.HasPrefix(m.ID, "o") {
			models = append(models, Model{ID: m.ID, Name: m.ID})
		}
	}
	return models, nil
}

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMsg     `json:"messages"`
	Tools    []openaiToolDef `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type openaiMsg struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []openaiTC      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type openaiTC struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func (o *OpenAI) ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	msgs := make([]openaiMsg, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := openaiMsg{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, openaiTC{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: tc.Name, Arguments: string(tc.Arguments)},
			})
		}
		msgs = append(msgs, msg)
	}

	var tools []openaiToolDef
	for _, t := range req.Tools {
		td := openaiToolDef{Type: "function"}
		td.Function.Name = t.Name
		td.Function.Description = t.Description
		td.Function.Parameters = t.Parameters
		tools = append(tools, td)
	}

	body, err := json.Marshal(openaiRequest{
		Model:    o.model,
		Messages: msgs,
		Tools:    tools,
		Stream:   true,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("openai chat: status %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 16)
	go o.streamResponse(ctx, resp, ch)
	return ch, nil
}

type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (o *OpenAI) streamResponse(ctx context.Context, resp *http.Response, ch chan<- ChatEvent) {
	defer resp.Body.Close()
	defer close(ch)

	type activeTC struct {
		id   string
		name string
		args strings.Builder
	}
	toolCalls := make(map[int]*activeTC)

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
		if data == "[DONE]" {
			break
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			slogr.Warn("openai: bad chunk", "err", err)
			continue
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				ch <- ChatEvent{Type: EventDone, Usage: &Usage{
					PromptTokens: chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				}}
				return
			}
			continue
		}

		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			ch <- ChatEvent{Type: EventText, Text: choice.Delta.Content}
		}

		for _, tc := range choice.Delta.ToolCalls {
			if tc.ID != "" {
				toolCalls[tc.Index] = &activeTC{id: tc.ID, name: tc.Function.Name}
			}
			if active, ok := toolCalls[tc.Index]; ok {
				active.args.WriteString(tc.Function.Arguments)
			}
		}

		if choice.FinishReason != nil {
			for _, tc := range toolCalls {
				args := json.RawMessage(tc.args.String())
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				ch <- ChatEvent{
					Type: EventToolCall,
					ToolCall: &ToolCall{
						ID:        tc.id,
						Name:      tc.name,
						Arguments: args,
					},
				}
			}
			break
		}
	}

	ch <- ChatEvent{Type: EventDone}
}
