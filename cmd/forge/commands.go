package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/serjikisa/forge/internal/agent"
	"github.com/serjikisa/forge/internal/config"
	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/server"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
)

func runChat(ctx context.Context, cfg *config.Config) {
	prov, model := resolveProviderModel(cfg)
	p := newProvider(cfg, prov, model)
	if o, ok := p.(*provider.Ollama); ok && model == "" {
		model = o.Model()
	}
	tools := tool.Registry()
	ui := tui.New(prov, model)
	defer ui.Restore()
	a := agent.New(p, tools, ui, model)
	if prompt, ok := cfg.ModelPrompts[model]; ok {
		a.SetSystemPrompt(prompt)
	}
	if hasFlag("--yes", "-y") {
		a.SetAutoApprove(true)
	}
	if logPath := flagValue("--log"); logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating log dir: %v\n", err)
			os.Exit(1)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening log: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		a.SetChatLog(f)
	}
	a.Run(ctx)
}

func runAsk(ctx context.Context, cfg *config.Config) {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	file := fs.String("file", "", "file to include in prompt")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prompt := fs.Arg(0)
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: forge ask \"<prompt>\" [--file <path>]")
		os.Exit(1)
	}

	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", *file, err)
			os.Exit(1)
		}
		prompt = fmt.Sprintf("%s\n\n---\nFile: %s\n```\n%s\n```", prompt, *file, string(data))
	}

	prov, model := resolveProviderModel(cfg)
	p := newProvider(cfg, prov, model)
	if o, ok := p.(*provider.Ollama); ok && model == "" {
		model = o.Model()
	}
	tools := tool.Registry()
	ui := tui.New(prov, model)
	defer ui.Restore()
	a := agent.New(p, tools, ui, model)
	if prompt, ok := cfg.ModelPrompts[model]; ok {
		a.SetSystemPrompt(prompt)
	}
	if hasFlag("--yes", "-y") {
		a.SetAutoApprove(true)
	}
	a.Ask(ctx, prompt)
}

func runServe(_ context.Context, cfg *config.Config) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 8080, "port to listen on")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prov, model := resolveProviderModel(cfg)
	p := newProvider(cfg, prov, model)
	if o, ok := p.(*provider.Ollama); ok && model == "" {
		model = o.Model()
	}

	s := server.New(p, model)
	addr := fmt.Sprintf(":%d", *port)
	fmt.Fprintf(os.Stderr, "forge serve • %s/%s • http://localhost%s\n", prov, model, addr)
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nshutting down...\n")
		os.Exit(0)
	}()

	if err := s.ListenAndServe(addr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runModels(ctx context.Context, cfg *config.Config) {
	prov, _ := resolveProviderModel(cfg)
	p := newProvider(cfg, prov, "")

	models, err := p.ListModels(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Models (%s):\n", prov)
	for _, m := range models {
		fmt.Printf("  %s\n", m.Name)
	}
}
