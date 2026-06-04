package output

import "testing"

func TestFormatSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(1.5 * 1024 * 1024), "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tc := range cases {
		got := FormatSize(tc.bytes)
		if got != tc.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "—"},
		{"2024-01-15T10:23:45.678901", "2024-01-15 10:23"},
		{"2024-01-15T10:23:45", "2024-01-15 10:23"},
		{"2024-01-15T10:23:45Z", "2024-01-15 10:23"},
		{"2024-01-15", "2024-01-15"}, // fallback to raw 10-char slice
	}

	for _, tc := range cases {
		got := FormatDate(tc.input)
		if got != tc.want {
			t.Errorf("FormatDate(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
