// Package config loads forge configuration from ~/.forge/config.json with
// environment variable expansion and sensible defaults.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	DefaultProvider string              `json:"default_provider"`
	Theme           string              `json:"theme"`
	LogLevel        string              `json:"log_level"`
	LogFormat       string              `json:"log_format"`
	MaxConcurrency  int                 `json:"max_concurrency"`
	Providers       map[string]Provider `json:"providers"`
	ModelPrompts    map[string]string   `json:"model_prompts,omitempty"`
}

type Provider struct {
	Host    string `json:"host,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
	Region  string `json:"region,omitempty"`
}

func Load() (*Config, error) {
	cfg := defaults()

	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			createDefault(path, cfg)
		}
		return cfg, nil
	}

	expanded := os.ExpandEnv(string(data))
	if err := json.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}

	if cfg.MaxConcurrency < 1 {
		cfg.MaxConcurrency = 5
	}

	return cfg, nil
}

func createDefault(path string, cfg *Config) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, append(data, '\n'), 0o644)
}

func defaults() *Config {
	return &Config{
		DefaultProvider: "ollama",
		Theme:           "vibrant",
		LogLevel:        getEnv("FORGE_LOG_LEVEL", "info"),
		LogFormat:       getEnv("FORGE_LOG_FORMAT", "pretty"),
		MaxConcurrency:  5,
		Providers: map[string]Provider{
			"ollama": {
				Host:  "http://localhost:11434",
				Model: "",
			},
		},
	}
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".forge", "config.json")
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func GetEnv(key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return val, nil
}
