// texttools.go parses tool calls that models emit as plain text (JSON in prose or
// code fences) rather than structured tool_calls, enabling tool use with models that
// lack native function calling support.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/serjikisa/forge/internal/provider"
)

// textToolCall is the JSON shape models emit as text when they don't use native tool calling.
type textToolCall struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Parameters json.RawMessage `json:"parameters"`
}

// args returns Arguments if set, otherwise falls back to Parameters.
func (tc textToolCall) args() json.RawMessage {
	if len(tc.Arguments) > 0 {
		return tc.Arguments
	}
	return tc.Parameters
}

// parseTextToolCalls extracts tool calls from text that models output as JSON
// instead of structured tool_calls. Handles JSON embedded in prose or code fences.
// toolNames maps possible names (exact name, lowercased description) to canonical tool name.
func parseTextToolCalls(text string, toolNames map[string]string) ([]provider.ToolCall, string) {
	// Extract from markdown code fences first
	if calls, remaining := extractFromFences(text, toolNames); len(calls) > 0 {
		return calls, remaining
	}

	// Scan for bare JSON objects containing known tool names
	return extractBareJSON(text, toolNames)
}

func extractFromFences(text string, toolNames map[string]string) ([]provider.ToolCall, string) {
	remaining := text
	var calls []provider.ToolCall

	for {
		start := strings.Index(remaining, "```")
		if start == -1 {
			break
		}
		// Skip language tag (e.g. ```json)
		bodyStart := strings.Index(remaining[start+3:], "\n")
		if bodyStart == -1 {
			break
		}
		bodyStart += start + 3 + 1

		end := strings.Index(remaining[bodyStart:], "```")
		if end == -1 {
			break
		}
		end += bodyStart

		block := strings.TrimSpace(remaining[bodyStart:end])
		if parsed := tryParseToolCalls(block, toolNames); len(parsed) > 0 {
			calls = append(calls, parsed...)
			// Remove the fence from remaining text
			before := strings.TrimSpace(remaining[:start])
			after := strings.TrimSpace(remaining[end+3:])
			remaining = strings.TrimSpace(before + "\n" + after)
			continue
		}
		// Not a tool call fence, skip past it
		remaining = remaining[:start] + remaining[end+3:]
	}

	return calls, strings.TrimSpace(remaining)
}

func extractBareJSON(text string, toolNames map[string]string) ([]provider.ToolCall, string) {
	remaining := text
	var calls []provider.ToolCall

	for i := 0; i < len(remaining); i++ {
		if remaining[i] != '{' {
			continue
		}
		// Find matching closing brace
		depth := 0
		for j := i; j < len(remaining); j++ {
			switch remaining[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := remaining[i : j+1]
					if parsed := tryParseToolCalls(candidate, toolNames); len(parsed) > 0 {
						calls = append(calls, parsed...)
						before := strings.TrimSpace(remaining[:i])
						after := strings.TrimSpace(remaining[j+1:])
						remaining = strings.TrimSpace(before + "\n" + after)
						i = -1 // restart scan on modified string
					}
					goto next
				}
			}
		}
		next:
	}

	return calls, strings.TrimSpace(remaining)
}

func tryParseToolCalls(s string, toolNames map[string]string) []provider.ToolCall {
	// Single object
	var tc textToolCall
	if json.Unmarshal([]byte(s), &tc) == nil && tc.Name != "" {
		if canonical := resolveTool(tc.Name, toolNames); canonical != "" {
			return []provider.ToolCall{{
				ID:        fmt.Sprintf("text_call_0"),
				Name:      canonical,
				Arguments: tc.args(),
			}}
		}
	}

	// Array
	var tcs []textToolCall
	if json.Unmarshal([]byte(s), &tcs) == nil && len(tcs) > 0 {
		var calls []provider.ToolCall
		for i, t := range tcs {
			if t.Name != "" {
				if canonical := resolveTool(t.Name, toolNames); canonical != "" {
					calls = append(calls, provider.ToolCall{
						ID:        fmt.Sprintf("text_call_%d", i),
						Name:      canonical,
						Arguments: t.args(),
					})
				}
			}
		}
		return calls
	}

	return nil
}

// resolveTool looks up a name in the toolNames map (exact match or lowercased).
func resolveTool(name string, toolNames map[string]string) string {
	if c, ok := toolNames[name]; ok {
		return c
	}
	if c, ok := toolNames[strings.ToLower(name)]; ok {
		return c
	}
	return ""
}
