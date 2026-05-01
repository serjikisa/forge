package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type ShellExec struct{}

func (s *ShellExec) Name() string        { return "shell_exec" }
func (s *ShellExec) Description() string { return "Execute a shell command and return its output" }
func (s *ShellExec) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"shell command to execute"}},"required":["command"]}`)
}

var dangerousPatterns = []string{
	"rm -rf", "rm -r", "git push --force", "git reset --hard",
	"drop table", "drop database", "chmod 777", "mkfs", "killall",
}

func (s *ShellExec) Safety() SafetyLevel {
	return NeedsConfirmation
}

func isDangerous(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (s *ShellExec) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p struct{ Command string `json:"command"` }
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}

	cmd := exec.CommandContext(ctx, shell, flag, p.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if result.Len() > 0 {
			result.WriteByte('\n')
		}
		result.WriteString(stderr.String())
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Sprintf("%s\nexit code: %d", result.String(), exitErr.ExitCode()), nil
		}
		return "", err
	}

	return result.String(), nil
}
