// Package agent implements the core chat loop: reading user input, sending messages
// to the LLM provider, parsing tool calls, executing tools concurrently, and streaming
// responses back through the UI.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
	"github.com/serjikisa/forge/pkg/slogr"
)

type Agent struct {
	provider       provider.Provider
	tools          []tool.Tool
	toolMap        map[string]tool.Tool
	tui            tui.UI
	history        []provider.Message
	model          string
	noTools        bool
	noToolStrikes  int
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
	var small bool
	if mi, ok := p.(interface{ ParameterSize() string }); ok {
		small = isSmallModel(mi)
	}
	return &Agent{
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
}

func (a *Agent) SetAutoApprove(v bool) { a.autoApprove = v }

func (a *Agent) SetSystemPrompt(prompt string) {
	if len(a.history) > 0 && a.history[0].Role == "system" {
		a.history[0].Content = prompt
	}
}

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

		if strings.HasPrefix(input, "/") {
			if input == "/exit" || input == "/quit" {
				return
			}
			if a.handleCommand(ctx, input) {
				continue
			}
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

func (a *Agent) Continue(ctx context.Context) {
	a.runLoop(ctx)
}

func (a *Agent) runLoop(ctx context.Context) {
	startTime := time.Now()
	var suppressTools bool
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
			if !a.noTools && strings.Contains(err.Error(), "does not support tools") {
				a.noTools = true
				a.tui.Info("model does not support tools, continuing without them")
				continue
			}
			a.tui.Error(err.Error())
			return
		}

		text, toolCalls := a.consumeStream(events)

		if toolsSent == nil && len(toolCalls) > 0 {
			toolCalls = nil
		}

		if ctx.Err() != nil {
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
				elapsed := time.Since(startTime).Round(time.Millisecond)
				a.tui.Info(fmt.Sprintf("tokens: %d prompt + %d output = %d total | time: %s",
					a.promptTokens, a.outputTokens, a.promptTokens+a.outputTokens, elapsed))
			}
			return
		}
		a.noToolStrikes = 0

		results := a.executeToolsConcurrently(ctx, toolCalls)

		// Stop if all tools were denied
		allDenied := true
		for _, r := range results {
			if r.Content != "denied by user" && r.Content != "denied by policy" {
				allDenied = false
				break
			}
		}
		if allDenied {
			a.tui.EndStream()
			return
		}

		// Text-parsed tool calls get results injected as assistant message
		textParsed := len(toolCalls) > 0 && strings.HasPrefix(toolCalls[0].ID, "text_call_")
		if textParsed {
			var buf strings.Builder
			for i, tc := range toolCalls {
				buf.WriteString(fmt.Sprintf("I called %s(%s) and got:\n%s\n\n", tc.Name, string(tc.Arguments), results[i].Content))
			}
			a.history = append(a.history, provider.Message{Role: "assistant", Content: buf.String()})
			suppressTools = true
		} else {
			a.history = append(a.history, provider.Message{Role: "assistant", ToolCalls: toolCalls})
			a.history = append(a.history, results...)
		}
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
				if !streaming {
					a.tui.StreamToken("  ")
					streaming = true
				}
				a.tui.StreamToken(ev.Text)
			}
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

	// Check if model emitted tool calls as text
	if !a.noTools && len(toolCalls) == 0 && text.Len() > 0 {
		toolNames := a.buildToolNames()
		if parsed, remaining := parseTextToolCalls(text.String(), toolNames); len(parsed) > 0 {
			if remaining != "" {
				a.tui.StreamToken("  " + remaining + "\n")
			}
			return remaining, parsed
		}
	}

	// Flush buffered text
	if !a.noTools && text.Len() > 0 {
		output := text.String()
		if len(toolCalls) > 0 {
			toolNames := a.buildToolNames()
			if _, cleaned := parseTextToolCalls(output, toolNames); cleaned != output {
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

	if !a.autoApprove && t.Safety() >= tool.NeedsConfirmation {
		perm := a.perms.Check(tc.Name)
		if perm == PermDeny {
			a.tui.ToolError(tc.Name, fmt.Errorf("denied by policy"))
			return "denied by policy"
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
				a.tui.ToolError(tc.Name, fmt.Errorf("denied by user"))
				return "denied by user"
			}
		}
	}

	a.tui.ToolStart(tc.Name, summarizeArgs(tc.Arguments))
	a.logChat("TOOL", fmt.Sprintf("%s %s", tc.Name, summarizeArgs(tc.Arguments)))

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

	detail := toolDetail(tc.Name, tc.Arguments, result)
	a.tui.ToolDone(tc.Name, detail)
	slogr.Debug("tool executed", "tool", tc.Name, "result_len", len(result))
	return result
}
