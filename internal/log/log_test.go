package log

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

func TestResolveLevel(t *testing.T) {
	cases := []struct {
		name      string
		verbosity int
		quiet     bool
		want      slog.Level
	}{
		{"quiet beats verbosity", 3, true, slog.LevelError},
		{"quiet default", 0, true, slog.LevelError},
		{"default is info", 0, false, slog.LevelInfo},
		{"-v is debug", 1, false, slog.LevelDebug},
		{"-vv is trace", 2, false, LevelTrace},
		{"-vvv is trace2", 3, false, LevelTrace2},
		{"beyond -vvv clamps to trace2", 9, false, LevelTrace2},
		{"negative clamps to info", -1, false, slog.LevelInfo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveLevel(c.verbosity, c.quiet); got != c.want {
				t.Errorf("resolveLevel(%d,%v) = %v, want %v", c.verbosity, c.quiet, got, c.want)
			}
		})
	}
}

// capture points the global logger at a buffer at the given verbosity/quiet and
// returns the buffer for assertions.
func capture(t *testing.T, verbosity int, quiet bool) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	SetWriter(&buf, verbosity, quiet)
	return &buf
}

func TestLevelFiltering_Default(t *testing.T) {
	buf := capture(t, 0, false)
	Errorf("an error")
	Warnf("a warning")
	Infof("an info")
	Debugf("a debug")
	Tracef("a trace")
	out := buf.String()
	for _, want := range []string{"an error", "a warning", "an info"} {
		if !strings.Contains(out, want) {
			t.Errorf("default level should emit %q; got:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"a debug", "a trace"} {
		if strings.Contains(out, notWant) {
			t.Errorf("default level should NOT emit %q; got:\n%s", notWant, out)
		}
	}
}

func TestLevelFiltering_Quiet(t *testing.T) {
	buf := capture(t, 0, true)
	Errorf("an error")
	Warnf("a warning")
	Infof("an info")
	out := buf.String()
	if !strings.Contains(out, "an error") {
		t.Errorf("quiet should still emit errors; got:\n%s", out)
	}
	for _, notWant := range []string{"a warning", "an info"} {
		if strings.Contains(out, notWant) {
			t.Errorf("quiet should NOT emit %q; got:\n%s", notWant, out)
		}
	}
}

func TestLevelFiltering_Verbose(t *testing.T) {
	buf := capture(t, 1, false)
	Debugf("a debug")
	Tracef("a trace")
	out := buf.String()
	if !strings.Contains(out, "a debug") {
		t.Errorf("-v should emit debug; got:\n%s", out)
	}
	if strings.Contains(out, "a trace") {
		t.Errorf("-v should NOT emit trace; got:\n%s", out)
	}
}

func TestLevelTagsPresent(t *testing.T) {
	buf := capture(t, 1, false)
	Errorf("e")
	Warnf("w")
	Infof("i")
	Debugf("d")
	out := buf.String()
	for _, tag := range []string{"ERROR", "WARN", "INFO", "DEBUG"} {
		if !strings.Contains(out, tag) {
			t.Errorf("expected level tag %q in output:\n%s", tag, out)
		}
	}
}

func TestNoTimestampByDefault(t *testing.T) {
	ts := regexp.MustCompile(`\d\d:\d\d:\d\d`)
	buf := capture(t, 0, false)
	Infof("hello")
	if ts.MatchString(buf.String()) {
		t.Errorf("default output should carry no timestamp; got:\n%s", buf.String())
	}
}

func TestTimestampAndSourceAtTrace(t *testing.T) {
	ts := regexp.MustCompile(`\d\d:\d\d:\d\d`)
	buf := capture(t, 2, false)
	Infof("hello")
	out := buf.String()
	if !ts.MatchString(out) {
		t.Errorf("-vv output should carry a timestamp; got:\n%s", out)
	}
	if !strings.Contains(out, ".go:") {
		t.Errorf("-vv output should carry a source location; got:\n%s", out)
	}
}

func TestEnabled(t *testing.T) {
	capture(t, 0, false)
	if Enabled(slog.LevelDebug) {
		t.Error("Debug should be disabled at default level")
	}
	if !Enabled(slog.LevelInfo) {
		t.Error("Info should be enabled at default level")
	}
	capture(t, 1, false)
	if !Enabled(slog.LevelDebug) {
		t.Error("Debug should be enabled at -v")
	}
}
