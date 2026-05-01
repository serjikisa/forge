// shell.go implements the shell_exec tool with timeout, project boundary checks,
// and dangerous command detection.
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

type ShellExec struct {
	timeout int // seconds
}

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

	// Block shell commands that read/write files outside the project boundary
	if path := extractTargetPath(p.Command); path != "" && !inProject(path) {
		return "", fmt.Errorf("command targets path %q which is outside project directory", path)
	}

	timeout := time.Duration(s.timeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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

// extractTargetPath detects if a shell command targets a specific file path
// outside the project. This is advisory — it catches common cases but cannot
// prevent all escape vectors (e.g. python -c, curl file://, env variable expansion).
// The confirmation prompt is the primary safety gate; this is defense-in-depth.
func extractTargetPath(cmd string) string {
	segments := splitShellSegments(cmd)
	for _, seg := range segments {
		if p := extractPathFromSegment(seg); p != "" {
			return p
		}
	}
	// Check for redirection to absolute paths
	for _, op := range []string{"> /", ">> /"} {
		if idx := strings.Index(cmd, op); idx >= 0 {
			fields := strings.Fields(cmd[idx+len(op)-1:])
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	// Check for absolute path arguments anywhere in the command
	for _, field := range strings.Fields(cmd) {
		if strings.HasPrefix(field, "/etc/") || strings.HasPrefix(field, "/root/") ||
			strings.HasPrefix(field, "/var/") || strings.HasPrefix(field, "/tmp/") {
			return field
		}
	}
	return ""
}

// splitShellSegments splits a command on |, &&, ||, ; to get individual commands.
func splitShellSegments(cmd string) []string {
	// Replace operators with a common separator
	for _, op := range []string{"&&", "||", "|", ";"} {
		cmd = strings.ReplaceAll(cmd, op, "\x00")
	}
	// Strip subshell wrappers
	cmd = strings.ReplaceAll(cmd, "$(", "\x00")
	cmd = strings.ReplaceAll(cmd, "(", "\x00")
	cmd = strings.ReplaceAll(cmd, ")", "")
	parts := strings.Split(cmd, "\x00")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			segments = append(segments, s)
		}
	}
	return segments
}

func extractPathFromSegment(seg string) string {
	fileCommands := []string{"cat ", "head ", "tail ", "less ", "more ", "tee ", "cp ", "mv ", "nano ", "vi ", "vim "}
	lower := strings.ToLower(strings.TrimSpace(seg))
	for _, prefix := range fileCommands {
		if strings.HasPrefix(lower, prefix) {
			parts := strings.Fields(seg)
			if len(parts) >= 2 {
				target := parts[len(parts)-1]
				if strings.HasPrefix(target, "/") {
					return target
				}
			}
		}
	}
	return ""
}
