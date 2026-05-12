package agent

import "strings"

// maxHistoryTokens is ~75% of 8k context, leaves room for response.
const maxHistoryTokens = 6000

// compactHistory drops older messages when history exceeds the token budget.
// Keeps the system prompt (first message) and the most recent messages.
func (a *Agent) compactHistory() {
	if len(a.history) <= 3 {
		return
	}
	total := 0
	for _, m := range a.history {
		total += estimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			total += estimateTokens(string(tc.Arguments))
		}
	}
	if total <= maxHistoryTokens {
		return
	}
	budget := maxHistoryTokens - estimateTokens(a.history[0].Content)
	start := len(a.history)
	for i := len(a.history) - 1; i >= 1; i-- {
		budget -= estimateTokens(a.history[i].Content) + 1
		if budget < 0 {
			break
		}
		start = i
	}
	if start > 1 {
		a.history = append(a.history[:1], a.history[start:]...)
		a.tui.Info("compacted conversation history")
	}
}

// isConversational returns true if the last user message is a simple greeting
// that doesn't need tool access.
func (a *Agent) isConversational() bool {
	var lastMsg string
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == "user" {
			lastMsg = strings.ToLower(strings.TrimSpace(a.history[i].Content))
			break
		}
	}
	if lastMsg == "" {
		return false
	}
	// If conversation already has tool usage, short messages are likely follow-ups.
	for _, m := range a.history {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			return false
		}
	}
	words := strings.Fields(lastMsg)
	if len(words) > 5 {
		return false
	}
	for _, w := range words {
		for _, kw := range toolKeywords {
			if strings.Contains(w, kw) {
				return false
			}
		}
	}
	return true
}

var toolKeywords = []string{
	"read", "write", "list", "check", "run", "search", "find", "show",
	"edit", "fix", "test", "build", "create", "delete", "open", "cat", "grep",
	"file", "dir", "code", "func", "error", "bug", "implement",
	"ls", "pwd", "cd", "git", "make", "go", "curl", "mv", "cp", "rm",
	"web", "fetch", "url", "http", "browse", "site", "look", "download",
	".go", ".js", ".py", ".ts", ".md", ".json", ".yaml", ".yml",
	"internal", "cmd", "src", "~/", "./", "/",
}

func estimateTokens(s string) int {
	return len(s)/4 + 1
}
