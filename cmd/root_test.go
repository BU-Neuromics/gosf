package cmd

import "testing"

func TestReconcileVerbosity(t *testing.T) {
	if err := reconcileVerbosity(true, 1); err == nil {
		t.Error("--quiet with -v should be rejected")
	}
	if err := reconcileVerbosity(true, 0); err != nil {
		t.Errorf("--quiet alone should be fine, got %v", err)
	}
	if err := reconcileVerbosity(false, 3); err != nil {
		t.Errorf("-vvv alone should be fine, got %v", err)
	}
}

func TestLogQuiet(t *testing.T) {
	cases := []struct {
		quiet, jsonMode bool
		verbosity       int
		want            bool
	}{
		{false, false, 0, false}, // plain run: logs on
		{true, false, 0, true},   // --quiet: logs off
		{false, true, 0, true},   // json: logs off by default
		{false, true, 1, false},  // json -v: user asked for logs
		{false, false, 2, false}, // -vv: logs on
	}
	for _, c := range cases {
		if got := logQuiet(c.quiet, c.jsonMode, c.verbosity); got != c.want {
			t.Errorf("logQuiet(%v,%v,%d) = %v, want %v", c.quiet, c.jsonMode, c.verbosity, got, c.want)
		}
	}
}

func TestShowProgressBar(t *testing.T) {
	cases := []struct {
		name                       string
		progress, quiet, json, tty bool
		want                       bool
	}{
		{"off by default", false, false, false, true, false},
		{"on when requested on a tty", true, false, false, true, true},
		{"suppressed off a tty", true, false, false, false, false},
		{"suppressed under quiet", true, true, false, true, false},
		{"suppressed under json", true, false, true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := showProgressBar(c.progress, c.quiet, c.json, c.tty); got != c.want {
				t.Errorf("showProgressBar(%v,%v,%v,%v) = %v, want %v",
					c.progress, c.quiet, c.json, c.tty, got, c.want)
			}
		})
	}
}
