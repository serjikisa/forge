// main is the entry point for the forge CLI. It dispatches subcommands (chat, ask,
// serve, models, version) and wires together the provider, tools, and TUI layers.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/serjikisa/forge/internal/config"
	"github.com/serjikisa/forge/pkg/slogr"
)

var version = "dev"

func main() {
	signal.Ignore(os.Interrupt)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	slogr.Setup(cfg.LogLevel, cfg.LogFormat)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "chat":
		runChat(ctx, cfg)
	case "ask":
		runAsk(ctx, cfg)
	case "serve":
		runServe(ctx, cfg)
	case "models":
		runModels(ctx, cfg)
	case "version":
		fmt.Println("forge", version)
	default:
		runChat(ctx, cfg)
	}
}
