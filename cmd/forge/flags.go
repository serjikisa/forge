package main

import (
	"fmt"
	"os"

	"github.com/serjikisa/forge/internal/config"
	"github.com/serjikisa/forge/internal/provider"
)

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
	case "bedrock":
		pc := cfg.Providers["bedrock"]
		region := pc.Region
		if region == "" {
			region = "us-east-1"
		}
		if model == "" {
			model = pc.Model
		}
		return provider.NewBedrock(region, model)
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

	if v := os.Getenv("FORGE_PROVIDER"); v != "" {
		prov = v
		if p, ok := cfg.Providers[prov]; ok && model == "" {
			model = p.Model
		}
	}
	if v := os.Getenv("FORGE_MODEL"); v != "" {
		model = v
	}

	for i := 1; i < len(os.Args)-1; i++ {
		switch os.Args[i] {
		case "--provider":
			prov = os.Args[i+1]
			if p, ok := cfg.Providers[prov]; ok {
				model = p.Model
			}
		case "--model":
			model = os.Args[i+1]
		}
	}

	return prov, model
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

// resolveSystemPrompt returns a custom system prompt from --system-prompt or --system-prompt-file flags.
func resolveSystemPrompt() string {
	if sp := flagValue("--system-prompt"); sp != "" {
		return sp
	}
	if path := flagValue("--system-prompt-file"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading system prompt file: %v\n", err)
			os.Exit(1)
		}
		return string(data)
	}
	return ""
}
