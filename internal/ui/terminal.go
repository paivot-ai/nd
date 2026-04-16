// Package ui provides terminal styling and output helpers for nd CLI.
package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// colorOverride is set by the --color flag. Empty string means "auto".
var colorOverride string

// SetColorMode sets the color mode and re-applies the lipgloss color profile.
// mode must be "always", "never", or "auto". Returns error for invalid values.
func SetColorMode(mode string) error {
	switch mode {
	case "always", "never", "auto":
		colorOverride = mode
	default:
		return fmt.Errorf("invalid --color value %q: must be always, auto, or never", mode)
	}
	// Re-apply lipgloss profile now that we know the flag value.
	if ShouldUseColor() {
		lipgloss.SetColorProfile(termenv.TrueColor)
	} else {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return nil
}

// IsTerminal returns true if stdout is connected to a terminal (TTY).
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// ShouldUseColor determines if ANSI color codes should be used.
// When a --color flag override is set, it takes precedence over everything.
// Otherwise respects standard conventions:
//   - NO_COLOR: https://no-color.org/ - disables color if set
//   - CLICOLOR=0: disables color
//   - CLICOLOR_FORCE: forces color even in non-TTY
//   - Falls back to TTY detection
func ShouldUseColor() bool {
	switch colorOverride {
	case "always":
		return true
	case "never":
		return false
	}
	// "auto" or unset: existing env var + TTY logic unchanged.
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	return IsTerminal()
}
