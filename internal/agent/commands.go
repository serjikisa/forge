package agent

import (
	"context"
	"fmt"
	"strings"
)

// handleCommand processes slash commands. Returns true if the input was a command.
func (a *Agent) handleCommand(ctx context.Context, input string) bool {
	switch {
	case input == "/exit" || input == "/quit":
		return true // caller checks for exit
	case input == "/help" || input == "/":
		a.tui.PrintHelp()
	case input == "/clear":
		a.history = a.history[:1]
		a.tui.Info("conversation cleared")
	case input == "/model":
		a.tui.Info(fmt.Sprintf("provider: %s, model: %s", a.provider.Name(), a.model))
	case strings.HasPrefix(input, "/model "):
		a.handleModelCommand(ctx, strings.TrimSpace(strings.TrimPrefix(input, "/model ")))
	default:
		return false
	}
	return true
}

func (a *Agent) handleModelCommand(ctx context.Context, name string) {
	if name == "ls" || name == "list" {
		a.listModels(ctx)
		return
	}
	a.switchModel(ctx, name)
}

func (a *Agent) listModels(ctx context.Context) {
	models, err := a.provider.ListModels(ctx)
	if err != nil {
		a.tui.Error(err.Error())
		return
	}
	for _, m := range models {
		info := m.Name
		if m.Size > 0 {
			info += fmt.Sprintf("  %s", formatSize(m.Size))
		}
		if m.ModifiedAt != "" {
			info += fmt.Sprintf("  %s", formatModified(m.ModifiedAt))
		}
		if m.Name == a.model {
			info += " (active)"
		}
		a.tui.Info(fmt.Sprintf("  %s", info))
	}
}

func (a *Agent) switchModel(ctx context.Context, name string) {
	sw, ok := a.provider.(providerModelSwitcher)
	if !ok {
		a.tui.Error("provider does not support model switching")
		return
	}
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
			return
		}
	}
	sw.SetModel(name)
	a.model = name
	a.noTools = isNoToolModel(name)
	a.tui.Info(fmt.Sprintf("switched to %s", name))
}

// providerModelSwitcher is a local alias to avoid importing provider in this file's switch.
type providerModelSwitcher = interface{ SetModel(string) }
