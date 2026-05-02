package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/term"
)

// stdRW reads from stdin, writes to stdout.
type stdRW struct{}

func (stdRW) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdRW) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// ctrlCReader wraps a reader and intercepts Ctrl+C (0x03).
// When a job is running, it cancels the job context and swallows the byte.
// When idle, it sets a flag and replaces 0x03 with newline so x/term returns
// an empty line instead of EOF, allowing ReadInput to show the exit hint.
type ctrlCReader struct {
	inner  io.ReadWriter
	mu     sync.Mutex
	cancel context.CancelFunc
	ctrlC  bool // set when Ctrl+C pressed while idle
}

func (r *ctrlCReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	for i := 0; i < n; i++ {
		if p[i] == 0x03 {
			r.mu.Lock()
			fn := r.cancel
			r.mu.Unlock()
			if fn != nil {
				fn()
			} else {
				r.mu.Lock()
				r.ctrlC = true
				r.mu.Unlock()
			}
			// Replace with \r so x/term returns empty line instead of EOF
			p[i] = '\r'
		}
	}
	return n, err
}

func (r *ctrlCReader) Write(p []byte) (int, error) { return r.inner.Write(p) }

func (r *ctrlCReader) setCancel(fn context.CancelFunc) {
	r.mu.Lock()
	r.cancel = fn
	r.mu.Unlock()
}

func (r *ctrlCReader) consumeCtrlC() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.ctrlC
	r.ctrlC = false
	return v
}

// toolVerbs maps tool names to Kiro-style action verbs.
var toolVerbs = map[string]string{
	"read_file":      "Read",
	"write_file":     "Write",
	"list_directory":  "Read",
	"shell_exec":     "Shell",
	"search_code":    "Search",
}

type TUI struct {
	term     *term.Terminal
	oldState *term.State
	provider string
	model    string
	sigCount int
	reader   *ctrlCReader
	spinnerFields
}

func New(provider, model string) *TUI {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		oldState = nil
	}

	r := &ctrlCReader{inner: stdRW{}}
	t := &TUI{
		term:     term.NewTerminal(r, ""),
		oldState: oldState,
		provider: provider,
		model:    model,
		reader:   r,
	}
	return t
}

// SetJobCancel sets the cancel function for the currently running job.
// Pass nil when no job is running.
func (t *TUI) SetJobCancel(fn context.CancelFunc) {
	t.reader.setCancel(fn)
}

func (t *TUI) Restore() {
	if t.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), t.oldState)
	}
}

func (t *TUI) PrintBanner() {
	logo := []string{
		" ⣿⣿⣿⣿⣿⣿⣿⡗  ⣴⣿⣿⣿⣿⣿⣿⣦  ⣿⣿⣿⣿⣿⣿⡟   ⣿⣿⣿⣿⣿⣿⡟  ⣿⣿⣿⣿⣿⣿⣿⡿",
		" ⣿⣿⠀⠀⠀⠀⠀⠀  ⣿⣿⠀⠀⠀⠀⣿⣿  ⣿⣿⠀⠀⠀⣿⣿  ⣿⣿⠀⠀⠀⠀⠀  ⣿⣿⠀⠀⠀⠀⠀⠀",
		" ⣿⣿⣿⣿⣿⡗⠀⠀  ⣿⣿⠀⠀⠀⠀⣿⣿  ⣿⣿⣿⣿⣿⡟⠀  ⣿⣿⠀⣿⣿⣿⡗  ⣿⣿⣿⣿⣿⡗⠀⠀",
		" ⣿⣿⠀⠀⠀⠀⠀⠀  ⣿⣿⠀⠀⠀⠀⣿⣿  ⣿⣿⠀⠀⣿⣿⠀  ⣿⣿⠀⠀⠀⣿⣿  ⣿⣿⠀⠀⠀⠀⠀⠀",
		" ⣿⣿⠀⠀⠀⠀⠀⠀  ⠻⣿⣿⣿⣿⣿⠟⠀  ⣿⣿⠀⠀⠻⣿⡆ ⠻⣿⣿⣿⣿⡿⠃  ⣿⣿⣿⣿⣿⣿⣿⡿",
	}
	fmt.Fprintln(t.term)
	for _, line := range logo {
		fmt.Fprintf(t.term, " %s\n", BoldOrange(line))
	}
	fmt.Fprintln(t.term)
	fmt.Fprintf(t.term, "  %s %s\n", BoldOrange("⚡ forge"), Dim("• "+t.provider+"/"+t.model))
	fmt.Fprintf(t.term, "  %s\n", Dim("Type / for commands, Ctrl+D or /exit to exit FORGE"))
}

func (t *TUI) ReadInput() (string, bool) {
	t.term.SetPrompt("  " + Cyan("❯") + " ")
	line, err := t.term.ReadLine()
	if err != nil {
		// Only Ctrl+D (io.EOF from x/term) reaches here.
		// Ctrl+C is converted to \r by ctrlCReader, so x/term returns empty line.
		return "", false
	}
	line = strings.TrimSpace(line)
	if line == "" && t.reader.consumeCtrlC() {
		fmt.Fprintf(t.term, "  %s\n", Dim("Ctrl+D or /exit to exit FORGE"))
		return "", true
	}
	return line, true
}

func (t *TUI) PrintHelp() {
	fmt.Fprintln(t.term)
	fmt.Fprintln(t.term, Dim("  Commands:"))
	fmt.Fprintln(t.term, Dim("    /help    — show this help"))
	fmt.Fprintln(t.term, Dim("    /clear   — clear conversation history"))
	fmt.Fprintln(t.term, Dim("    /model   — show current provider and model"))
	fmt.Fprintln(t.term, Dim("    /model ls — list available models"))
	fmt.Fprintln(t.term, Dim("    /model <name> — switch model"))
	fmt.Fprintln(t.term, Dim("    /exit    — exit forge"))
	fmt.Fprintln(t.term, Dim("    Ctrl+D   — exit"))
	fmt.Fprintln(t.term, Dim("    Ctrl+C   — cancel running job"))
	fmt.Fprintln(t.term)
}

func (t *TUI) StreamToken(token string) {
	fmt.Fprint(t.term, token)
}

func (t *TUI) EndStream() {
	fmt.Fprintln(t.term)
	fmt.Fprintln(t.term)
}

// actionVerb returns the Kiro-style verb for a tool name.
func actionVerb(toolName string) string {
	if v, ok := toolVerbs[toolName]; ok {
		return v
	}
	return toolName
}

// shortPath returns the basename of a path for compact display.
func shortPath(p string) string {
	return filepath.Base(p)
}

// ToolStart prints a Kiro-style "● Action detail" line.
func (t *TUI) ToolStart(name, detail string) {
	verb := actionVerb(name)
	fmt.Fprintf(t.term, "%s %s %s\n", Cyan("●"), Bold(verb), detail)
}

// ToolDone prints a dimmed detail line under the tool annotation.
func (t *TUI) ToolDone(name, detail string) {
	if detail != "" {
		fmt.Fprintf(t.term, "  %s\n", Dim(detail))
	}
}

// ToolError prints a red bullet error line.
func (t *TUI) ToolError(name string, err error) {
	verb := actionVerb(name)
	fmt.Fprintf(t.term, "%s %s %s\n", Red("●"), verb, Red(err.Error()))
}

// Separator prints a Kiro-style horizontal rule.
func (t *TUI) Separator() {
	fmt.Fprintf(t.term, "%s\n", Dim(strings.Repeat("─", 80)))
}

func (t *TUI) Confirm(prompt string) bool {
	t.term.SetPrompt(fmt.Sprintf("  %s %s %s ", Yellow("🔒"), prompt, Dim("[y/N]")))
	line, err := t.term.ReadLine()
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

func (t *TUI) ConfirmWithAlways(prompt, category string) ConfirmResult {
	t.term.SetPrompt(fmt.Sprintf("  %s %s %s ", Yellow("🔒"), prompt, Dim("[y/n/a(lways)]")))
	line, err := t.term.ReadLine()
	if err != nil {
		t.sigCount++
		return ConfirmNo
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return ConfirmYes
	case "a", "always":
		return ConfirmAlways
	default:
		return ConfirmNo
	}
}

func (t *TUI) Error(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Red("●"), msg)
}

func (t *TUI) Info(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Cyan("●"), msg)
}

func (t *TUI) ResetSigCount() {
	t.sigCount = 0
}
