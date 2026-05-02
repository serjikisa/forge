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
