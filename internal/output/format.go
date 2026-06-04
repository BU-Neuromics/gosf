package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// PrintJSON writes v as indented JSON to w.
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// NewTabWriter returns a tabwriter suitable for aligned columns.
func NewTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

// FormatSize returns a human-readable file size string.
func FormatSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	case bytes < gb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	default:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gb)
	}
}

// FormatDate parses an OSF ISO 8601 date string and returns YYYY-MM-DD HH:MM.
// Returns the raw string if parsing fails.
func FormatDate(s string) string {
	if s == "" {
		return "—"
	}
	formats := []string{
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC().Format("2006-01-02 15:04")
		}
	}
	// Trim to date only as last resort
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// PrintHeader prints the column header for a file listing.
func PrintHeader(w *tabwriter.Writer) {
	fmt.Fprintln(w, strings.Join([]string{"NAME", "SIZE", "MODIFIED"}, "\t"))
}
