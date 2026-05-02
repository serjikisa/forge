package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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

var version = "dev"

func main() {
	cfg := config.Load()
	setupLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "chat":
		// In raw terminal mode, Ctrl+C is handled by x/term as byte 0x03.
		// Ignore SIGINT so it doesn't kill the process.
		signal.Ignore(os.Interrupt)
		runChat(ctx, cfg)
	case "ask":
		signal.Ignore(os.Interrupt)
		runAsk(ctx, cfg)
	case "serve":
		runServe(ctx, cfg)
	case "models":
		runModels(ctx, cfg)
	case "version":
		fmt.Println("forge", version)
	default:
		signal.Ignore(os.Interrupt)
		runChat(ctx, cfg)
	}
}

func runChat(ctx context.Context, cfg *config.Config) {
	prov, model := resolveProviderModel(cfg)
	p := newProvider(cfg, prov, model)
	// Get resolved model name (may have been auto-detected)
	if o, ok := p.(*provider.Ollama); ok && model == "" {
		model = o.Model()
	}
	tools := tool.Registry()
	ui := tui.New(prov, model)
	defer ui.Restore()
	a := agent.New(p, tools, ui, model)
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
	// Parse flags after "ask"
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

	// Graceful shutdown on SIGINT/SIGTERM
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

func newProvider(cfg *config.Config, name, model string) provider.Provider {
	switch name {
	case "ollama":
		pc := cfg.Providers["ollama"]
		host := pc.Host
		if host == "" {
			host = "http://localhost:11434"
		}
		if model == "" {
			model = pc.Model
		}
		return provider.NewOllama(host, model)
	default:
		fmt.Fprintf(os.Stderr, "unknown provider: %s\n", name)
		os.Exit(1)
		return nil
	}
}

func resolveProviderModel(cfg *config.Config) (string, string) {
	prov := cfg.DefaultProvider
	model := ""
	if p, ok := cfg.Providers[prov]; ok {
		model = p.Model
	}

	// Env vars
	if v := os.Getenv("FORGE_PROVIDER"); v != "" {
		prov = v
	}
	if v := os.Getenv("FORGE_MODEL"); v != "" {
		model = v
	}

	// CLI flags (scan all args for --provider/--model)
	for i := 1; i < len(os.Args)-1; i++ {
		switch os.Args[i] {
		case "--provider":
			prov = os.Args[i+1]
		case "--model":
			model = os.Args[i+1]
		}
	}

	return prov, model
}

func setupLogger(level, format string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func hasFlag(flags ...string) bool {
	for _, arg := range os.Args[1:] {
		for _, f := range flags {
			if arg == f {
				return true
			}
		}
	}
	return false
}

func flagValue(name string) string {
	for i, arg := range os.Args[1:] {
		if arg == name && i+2 < len(os.Args) {
			return os.Args[i+2]
		}
	}
	return ""
}
