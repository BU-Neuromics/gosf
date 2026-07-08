package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRedundantOverwrite(t *testing.T) {
	cases := []struct {
		name      string
		action    string
		localMD5  string
		remoteMD5 string
		want      bool
	}{
		{"overwrite identical", "overwrite", "aaa", "aaa", true},
		{"overwrite differing", "overwrite", "aaa", "bbb", false},
		{"overwrite unknown remote", "overwrite", "aaa", "", false},
		{"rename is a deliberate copy", "rename", "aaa", "aaa", false},
		{"upload has no existing to compare", "upload", "aaa", "aaa", false},
		{"skip already handled", "skip", "aaa", "aaa", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redundantOverwrite(tc.action, tc.localMD5, tc.remoteMD5); got != tc.want {
				t.Errorf("redundantOverwrite(%q,%q,%q) = %v, want %v",
					tc.action, tc.localMD5, tc.remoteMD5, got, tc.want)
			}
		})
	}
}

func TestLocalFileMatches(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	const helloMD5 = "5d41402abc4b2a76b9719d911017c592"

	cases := []struct {
		name      string
		path      string
		remoteMD5 string
		want      bool
	}{
		{"identical content", p, helloMD5, true},
		{"different content", p, "00000000000000000000000000000000", false},
		{"missing local file", filepath.Join(dir, "nope.txt"), helloMD5, false},
		{"empty remote md5", p, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := localFileMatches(tc.path, tc.remoteMD5); got != tc.want {
				t.Errorf("localFileMatches(%q, %q) = %v, want %v", tc.path, tc.remoteMD5, got, tc.want)
			}
		})
	}
}
