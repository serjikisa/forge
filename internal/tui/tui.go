package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// stdRW reads from stdin, writes to stdout.
type stdRW struct{}

func (stdRW) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdRW) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

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
	fmt.Fprintf(t.term, "  %s\n", Dim("Type / for commands, Ctrl+C twice to exit"))
}

func (t *TUI) ReadInput() (string, bool) {
	t.term.SetPrompt("  " + Cyan("❯") + " ")
	line, err := t.term.ReadLine()
	if err != nil {
		t.sigCount++
		if t.sigCount >= 2 {
			return "", false
		}
		t.term = term.NewTerminal(stdRW{}, "")
		os.Stdout.WriteString("\r\n  " + Dim("Press Ctrl+C again to exit, or type /exit") + "\r\n")
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

func (t *TUI) Error(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Red("●"), msg)
}

func (t *TUI) Info(msg string) {
	fmt.Fprintf(t.term, "  %s %s\n", Cyan("●"), msg)
}

func (t *TUI) ResetSigCount() {
	t.sigCount = 0
}
