package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// stdRW reads from stdin, writes to stdout.
type stdRW struct{}

func (stdRW) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdRW) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

type TUI struct {
	term     *term.Terminal
	oldState *term.State
	provider string
	model    string
	sigCount int
	spinnerFields
}

func New(provider, model string) *TUI {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		oldState = nil
	}

	t := &TUI{
		term:     term.NewTerminal(stdRW{}, ""),
		oldState: oldState,
		provider: provider,
		model:    model,
	}
	return t
}

func (t *TUI) Restore() {
	if t.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), t.oldState)
	}
}

func (t *TUI) PrintBanner() {
	fmt.Fprintf(t.term, "  %s %s\n", BoldCyan("⚡ forge"), Dim("• "+t.provider+"/"+t.model))
	fmt.Fprintf(t.term, "  %s\n", Dim("Type / for commands, Ctrl+C twice to exit"))
}

func (t *TUI) ReadInput() (string, bool) {
	t.term.SetPrompt("  " + Cyan("❯") + " ")
	line, err := t.term.ReadLine()
	if err != nil {
		if err == io.EOF {
			return "", false
		}
		// Ctrl+C sends term.ErrPasteIndicator or returns empty with error
		t.sigCount++
		if t.sigCount >= 2 {
			fmt.Fprintln(t.term)
			return "", false
		}
		fmt.Fprintf(t.term, "  %s\n", Dim("Press Ctrl+C again to exit, or type /exit"))
		return "", true
	}
	t.sigCount = 0
	return strings.TrimSpace(line), true
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
	fmt.Fprintln(t.term, Dim("    Ctrl+C   — press twice to exit"))
	fmt.Fprintln(t.term, Dim("    Ctrl+D   — exit"))
	fmt.Fprintln(t.term)
}

func (t *TUI) StreamToken(token string) {
	fmt.Fprint(t.term, token)
}

func (t *TUI) EndStream() {
	fmt.Fprintln(t.term)
	fmt.Fprintln(t.term)
}

func (t *TUI) ToolStart(name, detail string) {
	fmt.Fprintf(t.term, "  %s %s %s\n", Yellow("◐"), Dim(name), Dim(detail))
}

func (t *TUI) ToolDone(name, detail string) {
	fmt.Fprintf(t.term, "  %s %s %s\n", Green("✓"), name, Dim(detail))
}

func (t *TUI) ToolError(name string, err error) {
	fmt.Fprintf(t.term, "  %s %s %s\n", Red("✗"), name, Red(err.Error()))
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

func (t *TUI) Error(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Red("✗"), msg)
}

func (t *TUI) Info(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Cyan("ℹ"), msg)
}
