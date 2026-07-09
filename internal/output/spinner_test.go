package output

import "testing"

func TestSpinnerAllowed(t *testing.T) {
	cases := []struct {
		colorEnabled bool
		stderrTTY    bool
		want         bool
	}{
		{true, true, true},
		{true, false, false}, // not a TTY → no spinner (would litter a pipe)
		{false, true, false}, // color off → no spinner
		{false, false, false},
	}
	for _, tc := range cases {
		if got := spinnerAllowed(tc.colorEnabled, tc.stderrTTY); got != tc.want {
			t.Errorf("spinnerAllowed(%v,%v) = %v, want %v", tc.colorEnabled, tc.stderrTTY, got, tc.want)
		}
	}
}

func TestSpinner_NoopIsSafe(t *testing.T) {
	// With color disabled (default in tests), NewSpinner returns a no-op that
	// must be safe to Stop, and a nil *Spinner must be safe too.
	enabled = false
	sp := NewSpinner("working")
	sp.Stop()
	sp.Stop() // idempotent
	var nilSp *Spinner
	nilSp.Stop()
}
