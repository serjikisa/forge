package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type Ollama struct {
	host   string
	model  string
	client *http.Client
}

func NewOllama(host, model string) *Ollama {
	o := &Ollama{
		host:  host,
		model: model,
		client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	if o.model == "" {
		if m, err := o.detectModel(); err == nil && m != "" {
			o.model = m
		}
	}
	return o
}

func (o *Ollama) Model() string    { return o.model }
func (o *Ollama) SetModel(m string) { o.model = m }

func (o *Ollama) ParameterSize() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"name": o.model})
	req, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/show", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var info struct {
		Details struct {
			ParameterSize string `json:"parameter_size"`
		} `json:"details"`
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return ""
	}
	return info.Details.ParameterSize
}

func (o *Ollama) detectModel() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := o.ListModels(ctx)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no models installed")
	}
	return models[0].ID, nil
}

func (o *Ollama) Name() string { return "ollama" }

// --- ListModels ---

type ollamaTagsResp struct {
	Models []struct {
		Name    string `json:"name"`
		Details struct {
			ParameterSize string `json:"parameter_size"`
		} `json:"details"`
	} `json:"models"`
}

func (o *Ollama) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.host+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()

	var tags ollamaTagsResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&tags); err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}

	models := make([]Model, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = Model{ID: m.Name, Name: m.Name}
	}
	return models, nil
}

// --- ChatCompletion ---

type ollamaChatReq struct {
	Model    string           `json:"model"`
	Messages []ollamaMsg      `json:"messages"`
	Stream   bool             `json:"stream"`
	Tools    []ollamaToolDef  `json:"tools,omitempty"`
}

type ollamaMsg struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []ollamaToolCall  `json:"tool_calls,omitempty"`
}

type ollamaToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaStreamChunk struct {
	Message struct {
		Role      string            `json:"role"`
		Content   string            `json:"content"`
		ToolCalls []ollamaToolCall  `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done bool `json:"done"`
}

func (o *Ollama) ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	msgs := make([]ollamaMsg, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = ollamaMsg{Role: m.Role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			msgs[i].ToolCalls = append(msgs[i].ToolCalls, ollamaToolCall{
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
	}

	var tools []ollamaToolDef
	for _, t := range req.Tools {
		td := ollamaToolDef{Type: "function"}
		td.Function.Name = t.Name
		td.Function.Description = t.Description
		td.Function.Parameters = t.Parameters
		tools = append(tools, td)
	}

	body, err := json.Marshal(ollamaChatReq{
		Model:    o.model,
		Messages: msgs,
		Stream:   true,
		Tools:    tools,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama chat: status %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				ch <- ChatEvent{Type: EventError, Error: ctx.Err()}
				return
			default:
			}

			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk ollamaStreamChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				slog.Warn("ollama: bad chunk", "err", err)
				continue
			}

			// Tool calls
			if len(chunk.Message.ToolCalls) > 0 {
				for i, tc := range chunk.Message.ToolCalls {
					ch <- ChatEvent{
						Type: EventToolCall,
						ToolCall: &ToolCall{
							ID:        fmt.Sprintf("call_%d", i),
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
				}
			}

			// Text content
			if chunk.Message.Content != "" {
				ch <- ChatEvent{Type: EventText, Text: chunk.Message.Content}
			}

			if chunk.Done {
				ch <- ChatEvent{Type: EventDone}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- ChatEvent{Type: EventError, Error: err}
		}
	}()

	return ch, nil
}
