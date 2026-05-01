package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/serjikisa/forge/internal/provider"
)

type Session struct {
	Model     string             `json:"model"`
	CreatedAt string             `json:"created_at"`
	Messages  []provider.Message `json:"messages"`
}

func sessionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".forge", "sessions")
}

func (a *Agent) SaveSession() error {
	dir := sessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s := Session{
		Model:     a.model,
		CreatedAt: time.Now().Format(time.RFC3339),
		Messages:  a.history,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s.json", time.Now().Format("2006-01-02_15-04-05"))
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func LoadSession(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func LatestSession() (string, error) {
	dir := sessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no saved sessions")
	}
	return filepath.Join(dir, entries[len(entries)-1].Name()), nil
}
