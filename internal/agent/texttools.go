package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/serjikisa/forge/internal/provider"
)

// textToolCall is the JSON shape models emit as text when they don't use native tool calling.
type textToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// parseTextToolCalls extracts tool calls from text that models output as JSON
// instead of structured tool_calls. Handles JSON embedded in prose or code fences.
func parseTextToolCalls(text string, knownTools map[string]bool) ([]provider.ToolCall, string) {
	// Extract from markdown code fences first
	if calls, remaining := extractFromFences(text, knownTools); len(calls) > 0 {
		return calls, remaining
	}

	// Scan for bare JSON objects containing known tool names
	return extractBareJSON(text, knownTools)
}

func extractFromFences(text string, knownTools map[string]bool) ([]provider.ToolCall, string) {
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
		if parsed := tryParseToolCalls(block, knownTools); len(parsed) > 0 {
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

func extractBareJSON(text string, knownTools map[string]bool) ([]provider.ToolCall, string) {
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
					if parsed := tryParseToolCalls(candidate, knownTools); len(parsed) > 0 {
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

func tryParseToolCalls(s string, knownTools map[string]bool) []provider.ToolCall {
	// Single object
	var tc textToolCall
	if json.Unmarshal([]byte(s), &tc) == nil && tc.Name != "" && knownTools[tc.Name] {
		return []provider.ToolCall{{
			ID:        fmt.Sprintf("text_call_0"),
			Name:      tc.Name,
			Arguments: tc.Arguments,
		}}
	}

	// Array
	var tcs []textToolCall
	if json.Unmarshal([]byte(s), &tcs) == nil && len(tcs) > 0 {
		var calls []provider.ToolCall
		for i, t := range tcs {
			if t.Name != "" && knownTools[t.Name] {
				calls = append(calls, provider.ToolCall{
					ID:        fmt.Sprintf("text_call_%d", i),
					Name:      t.Name,
					Arguments: t.Arguments,
				})
			}
		}
		return calls
	}

	return nil
}
