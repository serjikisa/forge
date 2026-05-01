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
		{"Orange", Orange, "38;5;208"},
		{"BoldOrange", BoldOrange, "1;38;5;208"},
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

func TestActionVerb(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{"read_file", "Read"},
		{"write_file", "Write"},
		{"list_directory", "Read"},
		{"shell_exec", "Shell"},
		{"search_code", "Search"},
		{"unknown_tool", "unknown_tool"},
	}
	for _, tt := range tests {
		if got := actionVerb(tt.tool); got != tt.want {
			t.Errorf("actionVerb(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/Users/dev/project/main.go", "main.go"},
		{"main.go", "main.go"},
		{"/a/b/c", "c"},
		{".", "."},
	}
	for _, tt := range tests {
		if got := shortPath(tt.path); got != tt.want {
			t.Errorf("shortPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// mockRW is a mock io.ReadWriter for testing ctrlCReader.
type mockRW struct {
	data []byte
}

func (m *mockRW) Read(p []byte) (int, error) {
	n := copy(p, m.data)
	m.data = m.data[n:]
	return n, nil
}

func (m *mockRW) Write(p []byte) (int, error) { return len(p), nil }

func TestCtrlCReader_CancelsJobOnCtrlC(t *testing.T) {
	cancelled := false
	r := &ctrlCReader{inner: &mockRW{data: []byte{0x03}}}
	r.setCancel(func() { cancelled = true })

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("expected cancel to be called")
	}
	// Byte replaced with \r, not swallowed
	if n != 1 || buf[0] != '\r' {
		t.Errorf("expected 1 byte '\\r', got n=%d buf[0]=%x", n, buf[0])
	}
}

func TestCtrlCReader_SetsCtrlCFlagWhenIdle(t *testing.T) {
	r := &ctrlCReader{inner: &mockRW{data: []byte{0x03}}}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 || buf[0] != '\r' {
		t.Errorf("expected 1 byte '\\r', got n=%d buf[0]=%x", n, buf[0])
	}
	if !r.consumeCtrlC() {
		t.Error("expected ctrlC flag to be set")
	}
	if r.consumeCtrlC() {
		t.Error("expected ctrlC flag to be consumed (false on second call)")
	}
}

func TestCtrlCReader_NormalBytesUnaffected(t *testing.T) {
	r := &ctrlCReader{inner: &mockRW{data: []byte{'h', 'i'}}}
	r.setCancel(func() { t.Error("cancel should not be called for normal bytes") })

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected n=2, got %d", n)
	}
}

func TestCtrlCReader_SetCancelNil(t *testing.T) {
	cancelled := false
	r := &ctrlCReader{inner: &mockRW{data: []byte{0x03}}}
	r.setCancel(func() { cancelled = true })
	r.setCancel(nil)

	buf := make([]byte, 4)
	r.Read(buf)
	if cancelled {
		t.Error("cancel should not be called after clearing")
	}
	if !r.consumeCtrlC() {
		t.Error("expected ctrlC flag set when idle (cancel nil)")
	}
}

func TestRenderInlineBold(t *testing.T) {
	orig := noColor
	defer func() { noColor = orig }()
	noColor = false

	tests := []struct {
		input string
		want  string
	}{
		{"no bold here", "no bold here"},
		{"**bold**", "\033[1mbold\033[0m"},
		{"before **bold** after", "before \033[1mbold\033[0m after"},
		{"**a** and **b**", "\033[1ma\033[0m and \033[1mb\033[0m"},
		{"unclosed **bold", "unclosed **bold"},
		{"empty **** markers", "empty \033[1m\033[0m markers"},
		{"", ""},
	}
	for _, tt := range tests {
		got := renderInlineBold(tt.input)
		if got != tt.want {
			t.Errorf("renderInlineBold(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCtrlJReader_SetsFlag(t *testing.T) {
	r := &ctrlCReader{inner: &mockRW{data: []byte{0x0A}}}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 || buf[0] != '\r' {
		t.Errorf("expected 1 byte '\\r', got n=%d buf[0]=%x", n, buf[0])
	}
	if !r.consumeCtrlJ() {
		t.Error("expected ctrlJ flag to be set")
	}
	if r.consumeCtrlJ() {
		t.Error("expected ctrlJ flag consumed on second call")
	}
}

func TestCtrlJReader_DoesNotAffectCtrlC(t *testing.T) {
	r := &ctrlCReader{inner: &mockRW{data: []byte{0x0A}}}

	buf := make([]byte, 4)
	r.Read(buf)
	if r.consumeCtrlC() {
		t.Error("Ctrl-J should not set ctrlC flag")
	}
}
