// Package agent implements the core chat loop: reading user input, sending messages
// to the LLM provider, parsing tool calls, executing tools concurrently, and streaming
// responses back through the UI.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
	"github.com/serjikisa/forge/pkg/slogr"
)

type Agent struct {
	provider provider.Provider
	tools    []tool.Tool
	toolMap  map[string]tool.Tool
	tui      tui.UI
	history  []provider.Message
	model    string
	noTools        bool
	noToolStrikes  int
	textToolMode   bool
	autoApprove    bool
	maxConcurrency int
	perms          *Permissions
	chatLog        *os.File
	promptTokens   int
	outputTokens   int
}

func New(p provider.Provider, tools []tool.Tool, ui tui.UI, model string) *Agent {
	tm := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		tm[t.Name()] = t
	}
	small := isSmallModel(p)
	a := &Agent{
		provider:       p,
		tools:          tools,
		toolMap:        tm,
		tui:            ui,
		history:        []provider.Message{{Role: "system", Content: systemPrompt(small)}},
		model:          model,
		noTools:        isNoToolModel(model),
		maxConcurrency: 5,
		perms:          NewPermissions(),
	}
	return a
}

// isSmallModel checks if the model has <= 4B parameters.
func isSmallModel(p provider.Provider) bool {
	mi, ok := p.(provider.ModelInfo)
	if !ok {
		return false
	}
	size := mi.ParameterSize()
	if size == "" {
		return false
	}
	// Parse "3.2B" -> 3.2
	var n float64
	fmt.Sscanf(strings.TrimSuffix(strings.ToUpper(size), "B"), "%f", &n)
	return n > 0 && n <= 4
}

// isNoToolModel returns true for models known to not support tool calling.
func isNoToolModel(model string) bool {
	lower := strings.ToLower(model)
	for _, pattern := range []string{"deepseek-r1", "deepseek-r2"} {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func (a *Agent) SetAutoApprove(v bool) { a.autoApprove = v }

// SetSystemPrompt replaces the system prompt (first message in history).
func (a *Agent) SetSystemPrompt(prompt string) {
	if len(a.history) > 0 && a.history[0].Role == "system" {
		a.history[0].Content = prompt
	}
}

// SetHistory replaces the conversation history (including system prompt).
func (a *Agent) SetHistory(msgs []provider.Message) { a.history = msgs }

func (a *Agent) SetChatLog(f *os.File) { a.chatLog = f }

func (a *Agent) logChat(role, content string) {
	if a.chatLog == nil {
		return
	}
	fmt.Fprintf(a.chatLog, "\n--- %s ---\n%s\n", role, content)
	a.chatLog.Sync()
}

func (a *Agent) Run(ctx context.Context) {
	a.tui.PrintBanner()
	a.tui.StreamToken("\n")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		input, ok := a.tui.ReadInput()
		if !ok {
			return
		}
		if input == "" {
			continue
		}

		// Slash commands
		switch {
		case input == "/exit" || input == "/quit":
			return
		case input == "/help" || input == "/":
			a.tui.PrintHelp()
			continue
		case input == "/clear":
			a.history = a.history[:1]
			a.tui.Info("conversation cleared")
			continue
		case input == "/model":
			a.tui.Info(fmt.Sprintf("provider: %s, model: %s", a.provider.Name(), a.model))
			continue
		case strings.HasPrefix(input, "/model "):
			name := strings.TrimSpace(strings.TrimPrefix(input, "/model "))
			if name == "ls" || name == "list" {
				models, err := a.provider.ListModels(ctx)
				if err != nil {
					a.tui.Error(err.Error())
				} else {
					for _, m := range models {
						if m.Name == a.model {
							a.tui.Info(fmt.Sprintf("  %s (active)", m.Name))
						} else {
							a.tui.Info(fmt.Sprintf("  %s", m.Name))
						}
					}
				}
				continue
			}
			if sw, ok := a.provider.(provider.ModelSwitcher); ok {
				models, err := a.provider.ListModels(ctx)
				if err == nil {
					found := false
					for _, m := range models {
						if m.Name == name || m.ID == name {
							found = true
							break
						}
					}
					if !found {
						a.tui.Error(fmt.Sprintf("model %q not found. Use /model ls to list available models", name))
						continue
					}
				}
				sw.SetModel(name)
				a.model = name
				a.noTools = isNoToolModel(name)
				a.tui.Info(fmt.Sprintf("switched to %s", name))
			} else {
				a.tui.Error("provider does not support model switching")
			}
			continue
		}

		a.history = append(a.history, provider.Message{Role: "user", Content: input})
		a.logChat("USER", input)
		a.noToolStrikes = 0
		jobCtx, jobCancel := context.WithCancel(ctx)
		a.tui.SetJobCancel(jobCancel)
		a.runLoop(jobCtx)
		a.tui.SetJobCancel(nil)
		a.tui.ResetSigCount()
	}
}

func (a *Agent) Ask(ctx context.Context, prompt string) {
	a.history = append(a.history, provider.Message{Role: "user", Content: prompt})
	a.runLoop(ctx)
}

// Continue runs the agent loop on the existing history without appending a new message.
func (a *Agent) Continue(ctx context.Context) {
	a.runLoop(ctx)
}

func (a *Agent) runLoop(ctx context.Context) {

	var suppressTools bool // set after text-parsed tool calls to force synthesis
	for {
		a.tui.StartSpinner("thinking... (press ESC or Ctrl-C to stop)")
		toolsSent := a.toolDefs()
		if a.noTools || a.isConversational() || suppressTools {
			toolsSent = nil
		}
		suppressTools = false
		a.compactHistory()
		events, err := a.provider.ChatCompletion(ctx, provider.ChatRequest{
			Messages: a.history,
			Tools:    toolsSent,
		})
		if err != nil {
			a.tui.StopSpinner()
			if ctx.Err() != nil {
				a.tui.Info("cancelled")
				return
			}
			// Retry without tools if model doesn't support them
			if !a.noTools && strings.Contains(err.Error(), "does not support tools") {
				a.noTools = true
				a.tui.Info("model does not support tools, continuing without them")
				continue
			}
			a.tui.Error(err.Error())
			return
		}

		text, toolCalls := a.consumeStream(events)

		// If tools were intentionally not sent, ignore any text-parsed tool calls
		if toolsSent == nil && len(toolCalls) > 0 {
			toolCalls = nil
		}

		if ctx.Err() != nil {
			// Log any text we got before cancellation
			if text != "" {
				a.logChat("ASSISTANT", text)
			}
			a.tui.Info("cancelled")
			return
		}

		if text != "" {
			a.history = append(a.history, provider.Message{Role: "assistant", Content: text})
			a.logChat("ASSISTANT", text)
		}

		if len(toolCalls) == 0 {
			// Track models that ignore tools — if they respond with text
			// but never call tools, disable tools to avoid wasting tokens.
			// Only count when tools were actually sent to the model.
			if !a.noTools && toolsSent != nil && text != "" {
				a.noToolStrikes++
				if a.noToolStrikes >= 2 {
					a.noTools = true
					a.tui.Info(fmt.Sprintf("model %s does not appear to use tools — disabling tool calls", a.model))
					slogr.Info("auto-disabled tools: model not using them")
				}
			}
			a.tui.EndStream()
			if a.promptTokens > 0 || a.outputTokens > 0 {
				a.tui.Info(fmt.Sprintf("tokens: %d prompt + %d output = %d total",
					a.promptTokens, a.outputTokens, a.promptTokens+a.outputTokens))
			}
			return
		}
		a.noToolStrikes = 0 // reset on successful tool use

		// Execute tools concurrently
		results := a.executeToolsConcurrently(ctx, toolCalls)

		// Check if these were text-parsed tool calls (IDs start with "text_call_").
		// Models that emit tool calls as text don't understand tool-role responses,
		// so we inject results as an assistant message summarising what was executed.
		textParsed := len(toolCalls) > 0 && strings.HasPrefix(toolCalls[0].ID, "text_call_")
		if textParsed {
			var buf strings.Builder
			for i, tc := range toolCalls {
				buf.WriteString(fmt.Sprintf("I called %s(%s) and got:\n%s\n\n", tc.Name, string(tc.Arguments), results[i].Content))
			}
			a.history = append(a.history, provider.Message{Role: "assistant", Content: buf.String()})
			suppressTools = true
		} else {
			a.history = append(a.history, provider.Message{
				Role:      "assistant",
				ToolCalls: toolCalls,
			})
			a.history = append(a.history, results...)
		}
		// Loop back to get the next response
	}
}

func (a *Agent) consumeStream(events <-chan provider.ChatEvent) (string, []provider.ToolCall) {
	var text strings.Builder
	var toolCalls []provider.ToolCall
	streaming := false
	spinnerStopped := false

	for ev := range events {
		if !spinnerStopped {
			a.tui.StopSpinner()
			spinnerStopped = true
		}
		switch ev.Type {
		case provider.EventText:
			text.WriteString(ev.Text)
			if a.noTools {
				// No tool parsing — stream immediately
				if !streaming {
					a.tui.StreamToken("  ")
					streaming = true
				}
				a.tui.StreamToken(ev.Text)
			}
			// When tools are active, buffer text to avoid showing raw JSON
		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
		case provider.EventError:
			a.tui.Error(ev.Error.Error())
		case provider.EventDone:
			if ev.Usage != nil {
				a.promptTokens += ev.Usage.PromptTokens
				a.outputTokens += ev.Usage.OutputTokens
			}
		}
	}

	// If no native tool calls, check if the model emitted tool calls as text
	if !a.noTools && len(toolCalls) == 0 && text.Len() > 0 {
		knownTools := make(map[string]bool, len(a.toolMap))
		for name := range a.toolMap {
			knownTools[name] = true
		}
		if parsed, remaining := parseTextToolCalls(text.String(), knownTools); len(parsed) > 0 {
			a.textToolMode = true
			if remaining != "" {
				a.tui.StreamToken("  " + remaining + "\n")
			}
			return remaining, parsed
		}
	}

	// Flush buffered text (when tools were active and text wasn't a tool call)
	if !a.noTools && text.Len() > 0 {
		output := text.String()
		// Strip echoed tool call JSON from text when native calls were received
		if len(toolCalls) > 0 {
			knownTools := make(map[string]bool, len(a.toolMap))
			for name := range a.toolMap {
				knownTools[name] = true
			}
			if _, cleaned := parseTextToolCalls(output, knownTools); cleaned != output {
				output = strings.TrimSpace(cleaned)
			}
		}
		if output != "" {
			a.tui.StreamToken("  " + output)
			streaming = true
		}
	}

	if streaming {
		a.tui.StreamToken("\n")
	}

	return text.String(), toolCalls
}

func (a *Agent) executeTool(ctx context.Context, tc provider.ToolCall) string {
	t, ok := a.toolMap[tc.Name]
	if !ok {
		msg := fmt.Sprintf("unknown tool: %s", tc.Name)
		a.tui.ToolError(tc.Name, fmt.Errorf("%s", msg))
		return msg
	}

	// Check permissions
	if !a.autoApprove && t.Safety() >= tool.NeedsConfirmation {
		perm := a.perms.Check(tc.Name)
		if perm == PermDeny {
			msg := "denied by policy"
			a.tui.ToolError(tc.Name, fmt.Errorf("%s", msg))
			return msg
		}
		if perm == PermAsk {
			detail := summarizeArgs(tc.Arguments)
			if isDangerous(t, tc) {
				detail = tui.Red(detail)
			}
			cat := CategoryName(tc.Name)
			prompt := fmt.Sprintf("%s wants to run: %s", tc.Name, detail)
			switch a.tui.ConfirmWithAlways(prompt, cat) {
			case tui.ConfirmAlways:
				a.perms.AllowCategory(tc.Name)
				a.tui.Info(fmt.Sprintf("auto-approving all %s operations for this session", cat))
			case tui.ConfirmYes:
				// proceed
			default:
				msg := "denied by user"
				a.tui.ToolError(tc.Name, fmt.Errorf("%s", msg))
				return msg
			}
		}
	}

	// Kiro-style: show "● Read /path" or "● Shell command"
	a.tui.ToolStart(tc.Name, summarizeArgs(tc.Arguments))
	a.logChat("TOOL", fmt.Sprintf("%s %s", tc.Name, summarizeArgs(tc.Arguments)))

	// Guard against malformed or empty arguments
	args := strings.TrimSpace(string(tc.Arguments))
	if args == "" || args == "null" {
		msg := fmt.Sprintf("error: %s called with empty arguments — provide valid JSON parameters", tc.Name)
		a.tui.ToolError(tc.Name, fmt.Errorf("empty arguments"))
		return msg
	}
	if !json.Valid(tc.Arguments) {
		msg := fmt.Sprintf("error: %s called with malformed JSON — check argument syntax", tc.Name)
		a.tui.ToolError(tc.Name, fmt.Errorf("malformed JSON arguments"))
		return msg
	}

	result, err := t.Execute(ctx, tc.Arguments)
	if err != nil {
		a.tui.ToolError(tc.Name, err)
		return fmt.Sprintf("error: %s", err)
	}

	// Kiro-style detail line: line counts for reads, diff summary for writes
	detail := toolDetail(tc.Name, tc.Arguments, result)
	a.tui.ToolDone(tc.Name, detail)
	slogr.Debug("tool executed", "tool", tc.Name, "result_len", len(result))
	return result
}

// toolDetail computes a Kiro-style detail string for a completed tool call.
func toolDetail(name string, args json.RawMessage, result string) string {
	var m map[string]any
	json.Unmarshal(args, &m)

	switch name {
	case "read_file":
		lines := strings.Count(result, "\n")
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v (%d lines)", shortName(fmt.Sprintf("%v", p)), lines)
		}
		return fmt.Sprintf("(%d lines)", lines)
	case "list_directory":
		entries := strings.Count(strings.TrimSpace(result), "\n") + 1
		if strings.TrimSpace(result) == "" {
			entries = 0
		}
		return fmt.Sprintf("(%d entries)", entries)
	case "write_file":
		lines := 0
		if c, ok := m["content"]; ok {
			lines = strings.Count(fmt.Sprintf("%v", c), "\n") + 1
		}
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v (%d lines written)", shortName(fmt.Sprintf("%v", p)), lines)
		}
		return fmt.Sprintf("(%d lines written)", lines)
	case "shell_exec":
		out := strings.TrimSpace(result)
		if len(out) > 80 {
			out = out[:80] + "..."
		}
		if out == "" {
			return "(no output)"
		}
		return ""
	case "search_code":
		matches := strings.Count(strings.TrimSpace(result), "\n") + 1
		if strings.TrimSpace(result) == "" || result == "no matches found" {
			return "(no matches)"
		}
		return fmt.Sprintf("(%d matches)", matches)
	}
	return ""
}

func shortName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

func isDangerous(t tool.Tool, tc provider.ToolCall) bool {
	if t.Name() != "shell_exec" {
		return false
	}
	var p struct{ Command string `json:"command"` }
	json.Unmarshal(tc.Arguments, &p)
	lower := strings.ToLower(p.Command)
	for _, pat := range []string{"rm -rf", "rm -r", "git push --force", "git reset --hard", "drop table", "chmod 777"} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

func (a *Agent) toolDefs() []provider.ToolDef {
	defs := make([]provider.ToolDef, len(a.tools))
	for i, t := range a.tools {
		defs[i] = provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		}
	}
	return defs
}

func summarizeArgs(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) == nil {
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v", p)
		}
		if c, ok := m["command"]; ok {
			return truncate(fmt.Sprintf("%v", c), 60)
		}
		if p, ok := m["pattern"]; ok {
			return fmt.Sprintf("%v", p)
		}
	}
	return truncate(string(args), 60)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}


// isConversational returns true if the last user message is a simple greeting
// or question that doesn't need tool access. This prevents models like llama3.2
// from calling tools on "hi" or "thanks".
func (a *Agent) isConversational() bool {
	// Find last user message
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
	// If conversation already has tool usage, short messages like "yes" or
	// "do it" are likely follow-up instructions, not greetings.
	for _, m := range a.history {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			return false
		}
	}
	// Short messages with no code/file indicators are likely conversational
	words := strings.Fields(lastMsg)
	if len(words) > 5 {
		return false
	}
	// Check for tool-triggering keywords
	for _, w := range words {
		for _, kw := range []string{"read", "write", "list", "check", "run", "search", "find", "show",
			"edit", "fix", "test", "build", "create", "delete", "open", "cat", "grep",
			"file", "dir", "code", "func", "error", "bug", "implement",
			"ls", "pwd", "cd", "git", "make", "go", "curl", "mv", "cp", "rm",
			".go", ".js", ".py", ".ts", ".md", ".json", ".yaml", ".yml",
			"internal", "cmd", "src", "~/", "./", "/"} {
			if strings.Contains(w, kw) {
				return false
			}
		}
	}
	return true
}
// compactHistory drops older messages when history exceeds the token budget.
// Keeps the system prompt (first message) and the most recent messages.
const maxHistoryTokens = 6000 // ~75% of 8k context, leaves room for response

func (a *Agent) compactHistory() {
	if len(a.history) <= 3 {
		return
	}
	total := 0
	for _, m := range a.history {
		total += len(m.Content)/4 + 1
	}
	if total <= maxHistoryTokens {
		return
	}
	// Keep system prompt + most recent messages that fit in budget
	budget := maxHistoryTokens - len(a.history[0].Content)/4
	start := len(a.history)
	for i := len(a.history) - 1; i >= 1; i-- {
		budget -= len(a.history[i].Content)/4 + 1
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

func systemPrompt(small bool) string {
	dir, _ := os.Getwd()
	if small {
		return fmt.Sprintf(`You are Forge, an AI coding assistant in the terminal.

You have tools: read_file, write_file, list_directory, shell_exec, search_code, web_search, web_fetch.
Always use your tools proactively. Never ask the user to paste code or provide information you can get yourself.
Use read_file and list_directory to explore code. Use web_search to find information online. Use web_fetch to read web pages.

Current directory: %s
Operating system: %s/%s`, dir, runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf(`You are Forge, a terminal-based AI coding assistant. You help developers write, debug, and understand code directly from the command line.

You are direct and concise. You write complete, working code. You explain your reasoning when making decisions.

You have access to tools to interact with the user's system. You MUST use tools to gather any information you need. NEVER ask the user to provide file contents, directory listings, or command output — use the appropriate tool yourself. Be proactive: if the user asks you to review code, immediately read the files. If they ask to search something, use web_search.

Tool selection guide:
- read_file: Use when asked to read, show, check, or review a specific file. Use for "read X", "show me X", "check X.go".
- write_file: Use to create or overwrite files.
- list_directory: Use when asked to list, check, or explore a directory. Use for "list X/", "check internal/", "what's in X".
- shell_exec: Use for running commands (build, test, git, curl, etc). Do NOT use shell_exec to read local files — use read_file instead.
- search_code: Use to find patterns across files. Use for "search for X", "find X", "where is X defined".
- web_search: Use to search the internet. Use for any question about external information, looking up docs, finding websites, or researching topics.
- web_fetch: Use to fetch and read a web page. Use after web_search to get details from a result, or when the user provides a URL.

CRITICAL: When you need to use a tool, output ONLY the JSON tool call. Do NOT describe what you plan to do — just call the tool directly.

Tool call format:
{"name": "<tool_name>", "arguments": {<args>}}

Examples:
- To list files: {"name": "list_directory", "arguments": {"path": "."}}
- To read a file: {"name": "read_file", "arguments": {"path": "internal/agent/agent.go"}}
- To run a command: {"name": "shell_exec", "arguments": {"command": "go test ./... -count=1"}}
- To search code: {"name": "search_code", "arguments": {"pattern": "func main", "include": "*.go"}}
- To search the web: {"name": "web_search", "arguments": {"query": "golang context best practices"}}
- To fetch a URL: {"name": "web_fetch", "arguments": {"url": "https://example.com"}}

Rules:
- Be proactive. If the user asks to review code, read the files immediately. If they ask to explore the codebase, list directories and read files without asking.
- Only respond with plain text for greetings, general questions, or conversation that doesn't need tools.
- NEVER explain what tool you will use. Just call it.
- NEVER ask the user to paste code, provide file contents, or look something up themselves. You have tools — use them.
- Use read_file to read files, NOT shell_exec with cat/head/tail.
- Read code before editing it.
- Keep responses concise.
- If a task requires multiple steps, call the first tool immediately.
- Do not fabricate file contents or command outputs.

Current directory: %s
Operating system: %s/%s`, dir, runtime.GOOS, runtime.GOARCH)
}
