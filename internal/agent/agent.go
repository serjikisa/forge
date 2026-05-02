package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
)

type Agent struct {
	provider provider.Provider
	tools    []tool.Tool
	toolMap  map[string]tool.Tool
	tui      tui.UI
	history  []provider.Message
	model    string
	noTools       bool
	noToolStrikes int
	autoApprove   bool
}

func New(p provider.Provider, tools []tool.Tool, ui tui.UI, model string) *Agent {
	tm := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		tm[t.Name()] = t
	}
	return &Agent{
		provider: p,
		tools:    tools,
		toolMap:  tm,
		tui:      ui,
		history:  []provider.Message{{Role: "system", Content: systemPrompt()}},
		model:    model,
	}
}

func (a *Agent) SetAutoApprove(v bool) { a.autoApprove = v }

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
			a.history = a.history[:1] // keep system prompt
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
				// Validate model exists
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
				a.noTools = false
				a.tui.Info(fmt.Sprintf("switched to %s", name))
			} else {
				a.tui.Error("provider does not support model switching")
			}
			continue
		}

		a.history = append(a.history, provider.Message{Role: "user", Content: input})
		a.runLoop(ctx)
		a.tui.ResetSigCount()
	}
}

func (a *Agent) Ask(ctx context.Context, prompt string) {
	a.history = append(a.history, provider.Message{Role: "user", Content: prompt})
	a.runLoop(ctx)
}

func (a *Agent) runLoop(ctx context.Context) {

	for {
		a.tui.StartSpinner("thinking...")
		tools := a.toolDefs()
		if a.noTools {
			tools = nil
		}
		events, err := a.provider.ChatCompletion(ctx, provider.ChatRequest{
			Messages: a.history,
			Tools:    tools,
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

		if ctx.Err() != nil {
			a.tui.Info("cancelled")
			return
		}

		if text != "" {
			a.history = append(a.history, provider.Message{Role: "assistant", Content: text})
		}

		if len(toolCalls) == 0 {
			// Track models that ignore tools — if they respond with text
			// but never call tools, disable tools to avoid wasting tokens.
			if !a.noTools && text != "" {
				a.noToolStrikes++
				if a.noToolStrikes >= 2 {
					a.noTools = true
					a.tui.Info(fmt.Sprintf("model %s does not appear to use tools — disabling tool calls", a.model))
					slog.Info("auto-disabled tools: model not using them")
				}
			}
			a.tui.EndStream()
			return
		}
		a.noToolStrikes = 0 // reset on successful tool use

		// Add assistant message with tool calls
		a.history = append(a.history, provider.Message{
			Role:      "assistant",
			ToolCalls: toolCalls,
		})

		// Execute tools and add results
		for _, tc := range toolCalls {
			result := a.executeTool(ctx, tc)
			a.history = append(a.history, provider.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
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
			if !streaming {
				a.tui.StreamToken("  ")
				streaming = true
			}
			a.tui.StreamToken(ev.Text)
			text.WriteString(ev.Text)
		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
		case provider.EventError:
			a.tui.Error(ev.Error.Error())
		case provider.EventDone:
			// done
		}
	}

	if streaming {
		a.tui.StreamToken("\n")
	}

	// If no native tool calls, check if the model emitted tool calls as text
	if len(toolCalls) == 0 && text.Len() > 0 {
		knownTools := make(map[string]bool, len(a.toolMap))
		for name := range a.toolMap {
			knownTools[name] = true
		}
		if parsed, remaining := parseTextToolCalls(text.String(), knownTools); len(parsed) > 0 {
			return remaining, parsed
		}
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

	// Check safety
	if !a.autoApprove && t.Safety() >= tool.NeedsConfirmation {
		detail := string(tc.Arguments)
		if isDangerous(t, tc) {
			detail = tui.Red(detail)
		}
		prompt := fmt.Sprintf("%s wants to run: %s", tc.Name, detail)
		if !a.tui.Confirm(prompt) {
			msg := "denied by user"
			a.tui.ToolError(tc.Name, fmt.Errorf("%s", msg))
			return msg
		}
	}

	// Kiro-style: show "● Read /path" or "● Shell command"
	a.tui.ToolStart(tc.Name, summarizeArgs(tc.Arguments))

	result, err := t.Execute(ctx, tc.Arguments)
	if err != nil {
		a.tui.ToolError(tc.Name, err)
		return fmt.Sprintf("error: %s", err)
	}

	// Kiro-style detail line: line counts for reads, diff summary for writes
	detail := toolDetail(tc.Name, tc.Arguments, result)
	a.tui.ToolDone(tc.Name, detail)
	slog.Debug("tool executed", "tool", tc.Name, "result_len", len(result))
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

func systemPrompt() string {
	dir, _ := os.Getwd()
	return fmt.Sprintf(`You are Forge, a terminal-based AI coding assistant. You help developers write, debug, and understand code directly from the command line.

You are direct and concise. You write complete, working code. You explain your reasoning when making decisions.

You have access to tools to interact with the user's system. You MUST use tools to gather any information you need. NEVER ask the user to provide file contents, directory listings, or command output — use the appropriate tool yourself.

Tool selection guide:
- read_file: Use when asked to read, show, check, or review a specific file. Use for "read X", "show me X", "check X.go".
- write_file: Use to create or overwrite files.
- list_directory: Use when asked to list, check, or explore a directory. Use for "list X/", "check internal/", "what's in X".
- shell_exec: Use ONLY for running commands (build, test, git, etc). Do NOT use shell_exec to read files — use read_file instead.
- search_code: Use to find patterns across files. Use for "search for X", "find X", "where is X defined".

CRITICAL: When you need to use a tool, output ONLY the JSON tool call. Do NOT describe what you plan to do — just call the tool directly.

Tool call format:
{"name": "<tool_name>", "arguments": {<args>}}

Examples:
- To list files: {"name": "list_directory", "arguments": {"path": "."}}
- To read a file: {"name": "read_file", "arguments": {"path": "internal/agent/agent.go"}}
- To run a command: {"name": "shell_exec", "arguments": {"command": "go test ./... -count=1"}}
- To search code: {"name": "search_code", "arguments": {"pattern": "func main", "include": "*.go"}}

Rules:
- ALWAYS use tools instead of asking the user for information.
- NEVER explain what tool you will use. Just call it.
- Use read_file to read files, NOT shell_exec with cat/head/tail.
- Read code before editing it.
- Keep responses concise.
- If a task requires multiple steps, call the first tool immediately.
- Do not fabricate file contents or command outputs.

Current directory: %s
Operating system: %s/%s`, dir, runtime.GOOS, runtime.GOARCH)
}
