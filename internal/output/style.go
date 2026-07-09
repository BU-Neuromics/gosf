package output

import (
	"os"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// enabled tracks whether colorized output is active for this process. It is set
// once by InitColor and read by spinner/table code that must degrade to plain.
var enabled bool

// InitColor resolves whether ANSI color should be emitted and configures the
// global color state accordingly. Called once at startup from the root command.
func InitColor(mode string, jsonMode, quiet bool) {
	enabled = resolveColor(mode, jsonMode, quiet,
		term.IsTerminal(int(os.Stdout.Fd())), os.Getenv("NO_COLOR") != "")
	// color.NoColor is fatih/color's global kill-switch; every style helper
	// respects it, so setting it here makes all output degrade automatically.
	color.NoColor = !enabled
}

// resolveColor decides whether color should be on, given the --color mode, the
// output mode, and the environment. Pure so it can be table-tested.
//
//   - json mode → always off (machine output is never colored, even --color=always)
//   - "never"   → off
//   - "always"  → on (even off a TTY / with NO_COLOR set)
//   - "auto"    → on only when interactive: off for quiet, NO_COLOR, non-TTY
func resolveColor(mode string, jsonMode, quiet, isTTY, noColorEnv bool) bool {
	if jsonMode {
		return false
	}
	switch mode {
	case "never":
		return false
	case "always":
		return true
	default: // "auto"
		if quiet || noColorEnv || !isTTY {
			return false
		}
		return true
	}
}

// Enabled reports whether colorized output is active.
func Enabled() bool { return enabled }

var (
	cGreen  = color.New(color.FgGreen)
	cRed    = color.New(color.FgRed)
	cRedB   = color.New(color.FgRed, color.Bold)
	cYellow = color.New(color.FgYellow)
	cCyan   = color.New(color.FgCyan)
	cBold   = color.New(color.Bold)
	cDim    = color.New(color.Faint)
)

// Style helpers return s wrapped in ANSI color, or s unchanged when color is
// disabled (fatih/color honors the global color.NoColor set by InitColor).
func Green(s string) string   { return cGreen.Sprint(s) }
func Red(s string) string     { return cRed.Sprint(s) }
func RedBold(s string) string { return cRedB.Sprint(s) }
func Yellow(s string) string  { return cYellow.Sprint(s) }
func Cyan(s string) string    { return cCyan.Sprint(s) }
func Bold(s string) string    { return cBold.Sprint(s) }
func Dim(s string) string     { return cDim.Sprint(s) }
