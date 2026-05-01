package tui

import (
	"fmt"
	"os"
)

var noColor = os.Getenv("NO_COLOR") != ""

func color(code, s string) string {
	if noColor {
		return s
	}
	return fmt.Sprintf("\033[%sm%s\033[0m", code, s)
}

func Cyan(s string) string    { return color("36", s) }
func Green(s string) string   { return color("32", s) }
func Yellow(s string) string  { return color("33", s) }
func Red(s string) string     { return color("31", s) }
func Magenta(s string) string { return color("35", s) }
func Dim(s string) string     { return color("2", s) }
func Bold(s string) string    { return color("1", s) }
func BoldCyan(s string) string { return color("1;36", s) }
