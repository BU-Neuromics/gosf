package output

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func render(header []string, rows [][]Cell) string {
	var buf bytes.Buffer
	RenderTable(&buf, header, rows)
	return buf.String()
}

func TestRenderTable_PlainAlignment(t *testing.T) {
	color.NoColor = true
	header := []string{"NAME", "SIZE"}
	rows := [][]Cell{
		{{Text: "a.csv"}, {Text: "1.2 KB"}},
		{{Text: "longer-name.csv"}, {Text: "3 B"}},
	}
	out := render(header, rows)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines:\n%s", len(lines), out)
	}
	// Widest name is "longer-name.csv" (15) + 2-space gutter → col 2 at index 17.
	const col2 = 17
	wants := []string{"SIZE", "1.2 KB", "3 B"}
	for i, ln := range lines {
		if len(ln) < col2 || ln[:col2] != padTo(strings.Fields(ln)[0], col2) {
			t.Errorf("line %d not left-padded to a %d-wide first column: %q", i, col2, ln)
		}
		if got := ln[col2:]; got != wants[i] {
			t.Errorf("line %d second column = %q, want %q (full: %q)", i, got, wants[i], ln)
		}
	}
	// No trailing whitespace on any line.
	for i, ln := range lines {
		if strings.TrimRight(ln, " ") != ln {
			t.Errorf("line %d has trailing whitespace: %q", i, ln)
		}
	}
}

func padTo(s string, n int) string { return s + strings.Repeat(" ", n-len(s)) }

func TestRenderTable_ColorPreservesAlignment(t *testing.T) {
	header := []string{"STATUS", "PATH"}
	rows := [][]Cell{
		{{Text: "IN_SYNC", Style: Green}, {Text: "data/a.csv"}},
		{{Text: "DIVERGED", Style: RedBold}, {Text: "b.csv"}},
	}

	color.NoColor = true
	plain := render(header, rows)

	color.NoColor = false
	colored := render(header, rows)
	defer func() { color.NoColor = true }()

	// Colored output must contain ANSI...
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("expected ANSI escapes in colored output")
	}
	// ...but stripping them must yield byte-identical layout to the plain render.
	if got := stripANSI(colored); got != plain {
		t.Errorf("stripped colored output != plain output\nplain:   %q\nstripped:%q", plain, got)
	}
}

func TestRenderTable_WideAndEmptyCells(t *testing.T) {
	color.NoColor = true
	out := render([]string{"A", "B"}, [][]Cell{{{Text: ""}, {Text: "x"}}})
	if !strings.Contains(out, "x") {
		t.Errorf("expected content rendered, got %q", out)
	}
}
