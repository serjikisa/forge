package tui

// UI is the interface the agent uses for all user interaction.
// TUI implements it for terminal mode; HeadlessTUI implements it for server mode.
type UI interface {
	PrintBanner()
	ReadInput() (string, bool)
	PrintHelp()
	StreamToken(token string)
	EndStream()
	ToolStart(name, detail string)
	ToolDone(name, detail string)
	ToolError(name string, err error)
	Confirm(prompt string) bool
	Error(msg string)
	Info(msg string)
	StartSpinner(msg string)
	StopSpinner()
	ResetSigCount()
}
