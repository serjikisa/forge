package tool

import "testing"

func TestExtractTargetPath(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"cat /etc/passwd", "/etc/passwd"},
		{"head /etc/shadow", "/etc/shadow"},
		{"tail -n 10 /var/log/syslog", "/var/log/syslog"},
		{"cat main.go", ""},           // relative path — allowed
		{"cat ./internal/agent.go", ""}, // relative path — allowed
		{"echo hello", ""},
		{"go build ./...", ""},
		{"ls -la", ""},
		{"echo foo > /tmp/evil", "/tmp/evil"},
		{"echo foo >> /tmp/evil", "/tmp/evil"},
		{"cp foo.go /etc/bar", "/etc/bar"},
		{"mv foo.go /etc/bar", "/etc/bar"},
		{"tee /etc/passwd", "/etc/passwd"},
		// Piped commands
		{"cat /etc/passwd | grep root", "/etc/passwd"},
		{"echo hi | tee /etc/shadow", "/etc/shadow"},
		// Chained commands
		{"echo hi && cat /etc/passwd", "/etc/passwd"},
		{"echo hi || cat /etc/shadow", "/etc/shadow"},
		{"echo hi; cat /etc/passwd", "/etc/passwd"},
		// Subshells
		{"echo $(cat /etc/passwd)", "/etc/passwd"},
		// Safe piped commands
		{"echo hello | grep h", ""},
		{"ls -la | wc -l", ""},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := extractTargetPath(tt.cmd)
			if got != tt.want {
				t.Errorf("extractTargetPath(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}
