// bedrock.go implements the AWS Bedrock provider using the Converse Stream API
// with SigV4 request signing. No AWS SDK dependency — uses stdlib HTTP + crypto.
package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/serjikisa/forge/pkg/slogr"
)

type Bedrock struct {
	region    string
	model     string
	accessKey string
	secretKey string
	token     string
	client    *http.Client
}

func NewBedrock(region, model string) *Bedrock {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	token := os.Getenv("AWS_SESSION_TOKEN")

	// Fallback: read from ~/.aws/credentials
	if accessKey == "" || secretKey == "" {
		profile := os.Getenv("AWS_PROFILE")
		if profile == "" {
			profile = "default"
		}
		if creds := readAWSCredentials(profile); creds != nil {
			accessKey = creds["aws_access_key_id"]
			secretKey = creds["aws_secret_access_key"]
			if t := creds["aws_session_token"]; t != "" {
				token = t
			}
		}
	}

	return &Bedrock{
		region:    region,
		model:     model,
		accessKey: accessKey,
		secretKey: secretKey,
		token:     token,
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

func (b *Bedrock) Name() string      { return "bedrock" }
func (b *Bedrock) SetModel(m string) { b.model = m }
func (b *Bedrock) Model() string     { return b.model }

func (b *Bedrock) ListModels(_ context.Context) ([]Model, error) {
	return []Model{{ID: b.model, Name: b.model}}, nil
}

// --- Converse API types ---

type bedrockRequest struct {
	Messages    []bedrockMsg      `json:"messages"`
	System      []bedrockSysBlock `json:"system,omitempty"`
	InferenceConfig *bedrockInference `json:"inferenceConfig,omitempty"`
	ToolConfig  *bedrockToolConfig `json:"toolConfig,omitempty"`
}

type bedrockSysBlock struct {
	Text string `json:"text"`
}

type bedrockInference struct {
	MaxTokens int `json:"maxTokens,omitempty"`
}

type bedrockMsg struct {
	Role    string         `json:"role"`
	Content []bedrockBlock `json:"content"`
}

type bedrockBlock struct {
	Text      string          `json:"text,omitempty"`
	ToolUse   *bedrockToolUse `json:"toolUse,omitempty"`
	ToolResult *bedrockToolResult `json:"toolResult,omitempty"`
}

type bedrockToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type bedrockToolResult struct {
	ToolUseID string         `json:"toolUseId"`
	Content   []bedrockBlock `json:"content"`
}

type bedrockToolConfig struct {
	Tools []bedrockToolDef `json:"tools"`
}

type bedrockToolDef struct {
	ToolSpec bedrockToolSpec `json:"toolSpec"`
}

type bedrockToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema bedrockSchema   `json:"inputSchema"`
}

type bedrockSchema struct {
	JSON json.RawMessage `json:"json"`
}

// --- Streaming response types ---

type bedrockMsgStart struct {
	Role string `json:"role"`
}

type bedrockContentDelta struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Delta             struct {
		Text    string `json:"text,omitempty"`
		ToolUse *struct {
			Input string `json:"input"`
		} `json:"toolUse,omitempty"`
	} `json:"delta"`
}

type bedrockContentStart struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Start             struct {
		ToolUse *struct {
			ToolUseID string `json:"toolUseId"`
			Name      string `json:"name"`
		} `json:"toolUse,omitempty"`
	} `json:"start"`
}

type bedrockContentStop struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
}

type bedrockMsgStop struct {
	StopReason string `json:"stopReason"`
}

type bedrockMetadata struct {
	Usage struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"usage"`
}

func (b *Bedrock) ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	br := b.buildRequest(req)
	body, err := json.Marshal(br)
	if err != nil {
		return nil, err
	}

	host := fmt.Sprintf("bedrock-runtime.%s.amazonaws.com", b.region)
	path := "/model/" + b.model + "/converse-stream"
	endpoint := "https://" + host + path
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")

	b.signRequest(httpReq, body)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("bedrock: status %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 16)
	go b.streamResponse(ctx, resp, ch)
	return ch, nil
}

func (b *Bedrock) buildRequest(req ChatRequest) bedrockRequest {
	br := bedrockRequest{
		InferenceConfig: &bedrockInference{MaxTokens: 4096},
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			br.System = append(br.System, bedrockSysBlock{Text: m.Content})
		case "user":
			br.Messages = append(br.Messages, bedrockMsg{
				Role:    "user",
				Content: []bedrockBlock{{Text: m.Content}},
			})
		case "assistant":
			msg := bedrockMsg{Role: "assistant"}
			if m.Content != "" {
				msg.Content = append(msg.Content, bedrockBlock{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				msg.Content = append(msg.Content, bedrockBlock{
					ToolUse: &bedrockToolUse{
						ToolUseID: tc.ID,
						Name:      tc.Name,
						Input:     tc.Arguments,
					},
				})
			}
			br.Messages = append(br.Messages, msg)
		case "tool":
			// Tool results go as user messages in Bedrock
			if len(br.Messages) > 0 && br.Messages[len(br.Messages)-1].Role == "user" {
				br.Messages[len(br.Messages)-1].Content = append(
					br.Messages[len(br.Messages)-1].Content,
					bedrockBlock{ToolResult: &bedrockToolResult{
						ToolUseID: m.ToolCallID,
						Content:   []bedrockBlock{{Text: m.Content}},
					}},
				)
			} else {
				br.Messages = append(br.Messages, bedrockMsg{
					Role: "user",
					Content: []bedrockBlock{{ToolResult: &bedrockToolResult{
						ToolUseID: m.ToolCallID,
						Content:   []bedrockBlock{{Text: m.Content}},
					}}},
				})
			}
		}
	}

	if len(req.Tools) > 0 {
		tc := &bedrockToolConfig{}
		for _, t := range req.Tools {
			tc.Tools = append(tc.Tools, bedrockToolDef{
				ToolSpec: bedrockToolSpec{
					Name:        t.Name,
					Description: t.Description,
					InputSchema: bedrockSchema{JSON: t.Parameters},
				},
			})
		}
		br.ToolConfig = tc
	}

	return br
}

func (b *Bedrock) streamResponse(ctx context.Context, resp *http.Response, ch chan<- ChatEvent) {
	defer resp.Body.Close()
	defer close(ch)

	type activeToolCall struct {
		id    string
		name  string
		input strings.Builder
	}
	toolCalls := make(map[int]*activeToolCall)
	var usage *Usage

	reader := resp.Body
	for {
		select {
		case <-ctx.Done():
			ch <- ChatEvent{Type: EventError, Error: ctx.Err()}
			return
		default:
		}

		// Read prelude: 4 bytes total length + 4 bytes headers length + 4 bytes CRC
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(reader, prelude); err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				ch <- ChatEvent{Type: EventError, Error: err}
			}
			return
		}

		totalLen := int(beUint32(prelude[0:4]))
		headersLen := int(beUint32(prelude[4:8]))

		// Read rest of frame
		frame := make([]byte, totalLen-12)
		if _, err := io.ReadFull(reader, frame); err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				ch <- ChatEvent{Type: EventError, Error: err}
			}
			return
		}

		// Extract event type from headers
		eventType := parseEventType(frame[:headersLen])

		// Payload
		payloadStart := headersLen
		payloadEnd := len(frame) - 4
		if payloadEnd <= payloadStart {
			continue
		}
		payload := frame[payloadStart:payloadEnd]

		switch eventType {
		case "contentBlockDelta":
			var delta bedrockContentDelta
			if json.Unmarshal(payload, &delta) == nil {
				if delta.Delta.Text != "" {
					ch <- ChatEvent{Type: EventText, Text: delta.Delta.Text}
				}
				if delta.Delta.ToolUse != nil {
					if tc, ok := toolCalls[delta.ContentBlockIndex]; ok {
						tc.input.WriteString(delta.Delta.ToolUse.Input)
					}
				}
			}
		case "contentBlockStart":
			var start bedrockContentStart
			if json.Unmarshal(payload, &start) == nil {
				if tu := start.Start.ToolUse; tu != nil {
					toolCalls[start.ContentBlockIndex] = &activeToolCall{
						id:   tu.ToolUseID,
						name: tu.Name,
					}
				}
			}
		case "contentBlockStop":
			var stop bedrockContentStop
			if json.Unmarshal(payload, &stop) == nil {
				if tc, ok := toolCalls[stop.ContentBlockIndex]; ok {
					args := json.RawMessage(tc.input.String())
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
					delete(toolCalls, stop.ContentBlockIndex)
				}
			}
		case "messageStop":
			ch <- ChatEvent{Type: EventDone, Usage: usage}
			return
		case "metadata":
			var meta bedrockMetadata
			if json.Unmarshal(payload, &meta) == nil {
				usage = &Usage{
					PromptTokens: meta.Usage.InputTokens,
					OutputTokens: meta.Usage.OutputTokens,
				}
			}
		}
	}
}

// parseEventType extracts the :event-type value from AWS Event Stream binary headers.
func parseEventType(headers []byte) string {
	i := 0
	for i < len(headers) {
		if i >= len(headers) {
			break
		}
		nameLen := int(headers[i])
		i++
		if i+nameLen > len(headers) {
			break
		}
		name := string(headers[i : i+nameLen])
		i += nameLen

		if i >= len(headers) {
			break
		}
		headerType := headers[i]
		i++

		switch headerType {
		case 7: // String type
			if i+2 > len(headers) {
				return ""
			}
			valLen := int(headers[i])<<8 | int(headers[i+1])
			i += 2
			if i+valLen > len(headers) {
				return ""
			}
			val := string(headers[i : i+valLen])
			i += valLen
			if name == ":event-type" {
				return val
			}
		default:
			// Skip unknown header types
			return ""
		}
	}
	return ""
}

func beUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// --- AWS SigV4 Signing ---

func (b *Bedrock) signRequest(req *http.Request, payload []byte) {
	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzdate := now.Format("20060102T150405Z")

	req.Header.Set("X-Amz-Date", amzdate)
	if b.token != "" {
		req.Header.Set("X-Amz-Security-Token", b.token)
	}

	service := "bedrock"
	credentialScope := datestamp + "/" + b.region + "/" + service + "/aws4_request"

	// Canonical request
	canonicalURI := uriEncode(req.URL.Path)
	canonicalQuerystring := req.URL.RawQuery
	signedHeaders := "content-type;host;x-amz-date"
	if b.token != "" {
		signedHeaders = "content-type;host;x-amz-date;x-amz-security-token"
	}

	payloadHash := sha256Hex(payload)
	canonicalHeaders := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.URL.Host + "\n" +
		"x-amz-date:" + amzdate + "\n"
	if b.token != "" {
		canonicalHeaders += "x-amz-security-token:" + b.token + "\n"
	}

	canonicalRequest := strings.Join([]string{
		"POST", canonicalURI, canonicalQuerystring,
		canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	// String to sign
	stringToSign := "AWS4-HMAC-SHA256\n" + amzdate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	// Signing key
	signingKey := deriveKey(b.secretKey, datestamp, b.region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// Authorization header
	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		b.accessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)

	slogr.Debug("bedrock: signed request", "model", b.model, "region", b.region)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func deriveKey(secret, datestamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// uriEncode encodes a URI path per AWS SigV4 rules (RFC 3986, preserving /).
func uriEncode(path string) string {
	var buf strings.Builder
	for _, c := range []byte(path) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == '/' {
			buf.WriteByte(c)
		} else {
			fmt.Fprintf(&buf, "%%%02X", c)
		}
	}
	return buf.String()
}

// readAWSCredentials parses ~/.aws/credentials for the given profile.
func readAWSCredentials(profile string) map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(home + "/.aws/credentials")
	if err != nil {
		return nil
	}

	creds := make(map[string]string)
	inProfile := false
	target := "[" + profile + "]"

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inProfile = (line == target)
			continue
		}
		if inProfile {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				creds[key] = val
			}
		}
	}

	if creds["aws_access_key_id"] == "" {
		return nil
	}
	return creds
}
