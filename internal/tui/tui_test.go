package tui

import (
	"os"
	"strings"
	"testing"
)

func TestColorFunctions(t *testing.T) {
	// Save and restore NO_COLOR
	orig := noColor
	defer func() { noColor = orig }()

	noColor = false
	tests := []struct {
		name string
		fn   func(string) string
		code string
	}{
		{"Cyan", Cyan, "36"},
		{"Green", Green, "32"},
		{"Yellow", Yellow, "33"},
		{"Red", Red, "31"},
		{"Magenta", Magenta, "35"},
		{"Dim", Dim, "2"},
		{"Bold", Bold, "1"},
		{"BoldCyan", BoldCyan, "1;36"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn("test")
			wantPrefix := "\033[" + tt.code + "m"
			wantSuffix := "\033[0m"
			if !strings.HasPrefix(got, wantPrefix) {
				t.Errorf("missing prefix %q in %q", wantPrefix, got)
			}
			if !strings.HasSuffix(got, wantSuffix) {
				t.Errorf("missing suffix %q in %q", wantSuffix, got)
			}
			if !strings.Contains(got, "test") {
				t.Errorf("missing text in %q", got)
			}
		})
	}
}

func TestColorNoColor(t *testing.T) {
	orig := noColor
	defer func() { noColor = orig }()

	noColor = true
	if got := Cyan("hello"); got != "hello" {
		t.Errorf("NO_COLOR: Cyan(%q) = %q, want plain", "hello", got)
	}
	if got := Red("err"); got != "err" {
		t.Errorf("NO_COLOR: Red(%q) = %q, want plain", "err", got)
	}
	if got := BoldCyan("bold"); got != "bold" {
		t.Errorf("NO_COLOR: BoldCyan(%q) = %q, want plain", "bold", got)
	}
}

func TestColorEmptyString(t *testing.T) {
	orig := noColor
	defer func() { noColor = orig }()

	noColor = false
	got := Cyan("")
	if got != "\033[36m\033[0m" {
		t.Errorf("Cyan empty = %q", got)
	}
}

func TestStdRW(t *testing.T) {
	rw := stdRW{}

	// Write should go to stdout (fd 1)
	// Just verify it doesn't panic and returns correct count
	n, err := rw.Write([]byte(""))
	if err != nil {
		t.Errorf("Write empty: %v", err)
	}
	if n != 0 {
		t.Errorf("Write empty: n = %d", n)
	}
}

func TestStdRWImplementsReadWriter(t *testing.T) {
	// Compile-time check that stdRW has Read and Write
	var rw interface{ Read([]byte) (int, error) } = stdRW{}
	_ = rw
	var ww interface{ Write([]byte) (int, error) } = stdRW{}
	_ = ww
}

func TestNoColorEnvVar(t *testing.T) {
	// Verify the noColor variable reads from env
	// (it's set at init time, so we just check the mechanism)
	val := os.Getenv("NO_COLOR")
	expected := val != ""
	// noColor may or may not match depending on test env,
	// but the logic should be: noColor = os.Getenv("NO_COLOR") != ""
	_ = expected // just verify it compiles and the var exists
	_ = noColor
}
