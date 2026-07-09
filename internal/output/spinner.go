package output

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// Spinner is a lightweight indeterminate progress indicator drawn on stderr. A
// nil or no-op Spinner is safe to Stop, so callers can unconditionally write
// `sp := output.NewSpinner(...); defer sp.Stop()`.
type Spinner struct {
	s *spinner.Spinner
}

// NewSpinner starts a spinner showing msg. It is a no-op (draws nothing) when
// color is disabled or stderr is not a terminal, so it never litters logs,
// pipes, or --quiet/--output=json runs.
func NewSpinner(msg string) *Spinner {
	if !spinnerAllowed(enabled, term.IsTerminal(int(os.Stderr.Fd()))) {
		return &Spinner{}
	}
	s := spinner.New(spinner.CharSets[14], 80*time.Millisecond, spinner.WithWriter(os.Stderr))
	s.Suffix = " " + msg
	s.Start()
	return &Spinner{s: s}
}

// spinnerAllowed reports whether a spinner should actually be drawn.
func spinnerAllowed(colorEnabled, stderrTTY bool) bool {
	return colorEnabled && stderrTTY
}

// Stop halts the spinner and clears its line. Safe on a nil or no-op Spinner.
func (sp *Spinner) Stop() {
	if sp != nil && sp.s != nil {
		sp.s.Stop()
	}
}
