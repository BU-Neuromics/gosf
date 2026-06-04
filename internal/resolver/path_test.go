package resolver

import (
	"testing"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		input    string
		nodeID   string
		parentID string
		path     string
		wantErr  bool
	}{
		{"abc12", "abc12", "", "/", false},
		{"abc12:", "abc12", "", "/", false},
		{"abc12:/", "abc12", "", "/", false},
		{"abc12:/data", "abc12", "", "/data", false},
		{"abc12:/data/results/file.csv", "abc12", "", "/data/results/file.csv", false},
		{"abc12/xyz34:/path", "xyz34", "abc12", "/path", false},
		{"abc12/xyz34:", "xyz34", "abc12", "/", false},
		{"", "", "", "", true},
		{":/path", "", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseTarget(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q): unexpected error: %v", tc.input, err)
			}
			if got.NodeID != tc.nodeID {
				t.Errorf("NodeID: got %q, want %q", got.NodeID, tc.nodeID)
			}
			if got.ParentID != tc.parentID {
				t.Errorf("ParentID: got %q, want %q", got.ParentID, tc.parentID)
			}
			if got.Path != tc.path {
				t.Errorf("Path: got %q, want %q", got.Path, tc.path)
			}
		})
	}
}
