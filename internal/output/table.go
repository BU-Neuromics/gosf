package output

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Cell is one table cell: Text is the plain content used for width/alignment,
// and Style (optional) wraps the padded cell in color. Keeping width based on
// the plain Text is what makes colored tables align correctly — ANSI escape
// bytes never enter the width computation.
type Cell struct {
	Text  string
	Style func(string) string
}

const tableGutter = "  " // two spaces between columns, matching the old tabwriter

// RenderTable writes an aligned table. The header row is bold; each cell is
// padded (on its plain text) to the column width, then colored. Columns line up
// regardless of color because padding happens before styling.
func RenderTable(w io.Writer, header []string, rows [][]Cell) {
	ncols := len(header)
	for _, r := range rows {
		if len(r) > ncols {
			ncols = len(r)
		}
	}

	widths := make([]int, ncols)
	for c := 0; c < ncols; c++ {
		if c < len(header) {
			widths[c] = utf8.RuneCountInString(header[c])
		}
		for _, r := range rows {
			if c < len(r) {
				if n := utf8.RuneCountInString(r[c].Text); n > widths[c] {
					widths[c] = n
				}
			}
		}
	}

	// Header (bold). The final column is never padded (like tabwriter), so no
	// line carries trailing whitespace and styled cells can't smuggle padding
	// spaces inside their ANSI wrap — keeping colored and plain layouts identical.
	headerCells := make([]string, ncols)
	for c := 0; c < ncols; c++ {
		text := ""
		if c < len(header) {
			text = header[c]
		}
		if c < ncols-1 {
			text = padRight(text, widths[c])
		}
		headerCells[c] = Bold(text)
	}
	fmt.Fprintln(w, strings.Join(headerCells, tableGutter))

	for _, r := range rows {
		cells := make([]string, ncols)
		for c := 0; c < ncols; c++ {
			var cell Cell
			if c < len(r) {
				cell = r[c]
			}
			text := cell.Text
			if c < ncols-1 {
				text = padRight(text, widths[c])
			}
			if cell.Style != nil {
				text = cell.Style(text)
			}
			cells[c] = text
		}
		fmt.Fprintln(w, strings.Join(cells, tableGutter))
	}
}

// padRight pads s with spaces to the given visible (rune) width.
func padRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
