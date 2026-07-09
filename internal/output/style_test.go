package output

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestResolveColor(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		jsonMode   bool
		quiet      bool
		isTTY      bool
		noColorEnv bool
		want       bool
	}{
		{"never forces off even on a TTY", "never", false, false, true, false, false},
		{"always forces on even off a TTY", "always", false, false, false, false, true},
		{"always beats NO_COLOR", "always", false, false, false, true, true},
		{"auto on a TTY", "auto", false, false, true, false, true},
		{"auto off when not a TTY", "auto", false, false, false, false, false},
		{"auto off in json mode", "auto", true, false, true, false, false},
		{"json forces off even with always", "always", true, false, true, false, false},
		{"auto off when quiet", "auto", false, true, true, false, false},
		{"auto off with NO_COLOR", "auto", false, false, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveColor(tc.mode, tc.jsonMode, tc.quiet, tc.isTTY, tc.noColorEnv)
			if got != tc.want {
				t.Errorf("resolveColor(%q, json=%v, quiet=%v, tty=%v, nocolor=%v) = %v, want %v",
					tc.mode, tc.jsonMode, tc.quiet, tc.isTTY, tc.noColorEnv, got, tc.want)
			}
		})
	}
}

func TestStyleFuncs_PlainWhenDisabled(t *testing.T) {
	color.NoColor = true
	for name, fn := range map[string]func(string) string{
		"Green": Green, "Red": Red, "RedBold": RedBold, "Yellow": Yellow,
		"Cyan": Cyan, "Bold": Bold, "Dim": Dim,
	} {
		if got := fn("hello"); got != "hello" {
			t.Errorf("%s(%q) = %q with color disabled, want plain", name, "hello", got)
		}
	}
}

func TestStyleFuncs_ColoredWhenEnabled(t *testing.T) {
	color.NoColor = false
	defer func() { color.NoColor = true }()
	got := Green("hello")
	if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "hello") {
		t.Errorf("Green(%q) = %q, want ANSI-wrapped text when enabled", "hello", got)
	}
}

func TestInitColor_SetsEnabledAndNoColor(t *testing.T) {
	InitColor("always", false, false)
	if !Enabled() || color.NoColor {
		t.Errorf("InitColor(always) → Enabled=%v NoColor=%v, want true/false", Enabled(), color.NoColor)
	}
	InitColor("never", false, false)
	if Enabled() || !color.NoColor {
		t.Errorf("InitColor(never) → Enabled=%v NoColor=%v, want false/true", Enabled(), color.NoColor)
	}
}
