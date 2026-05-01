// Package tui implements the interactive terminal UI for forge. It handles raw-mode
// input (with Ctrl-C cancellation and Ctrl-J multiline), streaming output with inline
// markdown rendering, spinner animations, and Kiro-style tool annotations.
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

// ctrlCReader wraps a reader and intercepts Ctrl+C (0x03) and Ctrl+J (0x0A).
// When a job is running, Ctrl+C cancels the job context and swallows the byte.
// When idle, it sets a flag and replaces 0x03 with \r so x/term returns
// an empty line instead of EOF, allowing ReadInput to show the exit hint.
// Ctrl+J (0x0A) sets a multiline flag and replaces with \r so x/term returns
// the current line, allowing ReadInput to accumulate multiple lines.
type ctrlCReader struct {
	inner  io.ReadWriter
	mu     sync.Mutex
	cancel context.CancelFunc
	ctrlC  bool // set when Ctrl+C pressed while idle
	ctrlJ  bool // set when Ctrl+J pressed for multiline
}

func (r *ctrlCReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	for i := 0; i < n; i++ {
		switch p[i] {
		case 0x03:
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
			p[i] = '\r'
		case 0x0A:
			r.mu.Lock()
			r.ctrlJ = true
			r.mu.Unlock()
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

func (r *ctrlCReader) consumeCtrlJ() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.ctrlJ
	r.ctrlJ = false
	return v
}

// toolVerbs maps tool names to Kiro-style action verbs.
var toolVerbs = map[string]string{
	"read_file":      "Read",
	"write_file":     "Write",
	"edit_file":      "Edit",
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
	terminal := term.NewTerminal(r, "")

	// Set terminal width to actual size instead of default 80
	if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil && w > 0 {
		terminal.SetSize(w, 0)
	}

	t := &TUI{
		term:     terminal,
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
	fmt.Fprintf(t.term, "  %s\n", Dim("Type / for commands, Ctrl+J for newline, Ctrl+D to exit"))
}

func (t *TUI) ReadInput() (string, bool) {
	t.term.SetPrompt("  " + Cyan("❯") + " ")
	var lines []string
	for {
		line, err := t.term.ReadLine()
		if err != nil {
			return "", false
		}
		if line == "" && t.reader.consumeCtrlC() {
			fmt.Fprintf(t.term, "  %s\n", Dim("Ctrl+D or /exit to exit FORGE"))
			return "", true
		}
		lines = append(lines, line)
		if !t.reader.consumeCtrlJ() {
			break
		}
		// Ctrl-J pressed — continue reading next line
		t.term.SetPrompt("  " + Dim("…") + " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), true
}

func (t *TUI) PrintHelp() {
	fmt.Fprintln(t.term)
	fmt.Fprintln(t.term, Dim("  Commands:"))
	fmt.Fprintln(t.term, Dim("    /help    — show this help"))
	fmt.Fprintln(t.term, Dim("    /clear   — clear conversation history"))
	fmt.Fprintln(t.term, Dim("    /save    — save session to disk"))
	fmt.Fprintln(t.term, Dim("    /resume  — resume last saved session"))
	fmt.Fprintln(t.term, Dim("    /model   — show current provider and model"))
	fmt.Fprintln(t.term, Dim("    /model ls — list available models"))
	fmt.Fprintln(t.term, Dim("    /model <name> — switch model"))
	fmt.Fprintln(t.term, Dim("    /exit    — exit forge"))
	fmt.Fprintln(t.term, Dim("    Ctrl+D   — exit"))
	fmt.Fprintln(t.term, Dim("    Ctrl+C   — cancel running job"))
	fmt.Fprintln(t.term, Dim("    Ctrl+J   — newline (multiline input)"))
	fmt.Fprintln(t.term)
}

func (t *TUI) StreamToken(token string) {
	fmt.Fprint(t.term, renderInlineBold(token))
}

// renderInlineBold converts **text** to ANSI bold.
func renderInlineBold(s string) string {
	var out strings.Builder
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			out.WriteString(s)
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:start])
		out.WriteString(Bold(s[start+2 : start+2+end]))
		s = s[start+2+end+2:]
	}
	return out.String()
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
	t.term.SetPrompt(fmt.Sprintf("  %s %s %s/%s ", Yellow("🔒"), prompt, Green("y"), Red("n")))
	line, err := t.term.ReadLine()
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

func (t *TUI) ConfirmWithAlways(prompt, category string) ConfirmResult {
	t.term.SetPrompt(fmt.Sprintf("  %s %s %s/%s/%s ", Yellow("🔒"), prompt, Green("y"), Red("n"), Cyan("always")))
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
