package cmd

import (
	"testing"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

func mkfile(name string) client.FileItem {
	fi := client.FileItem{}
	fi.Attributes.Name = name
	fi.Attributes.Kind = "file"
	return fi
}

func TestFindFreeName(t *testing.T) {
	cases := []struct {
		name     string
		siblings []string
		want     string
	}{
		{"data.csv", nil, "data_1.csv"},
		{"data.csv", []string{"data.csv"}, "data_1.csv"},
		{"data.csv", []string{"data.csv", "data_1.csv"}, "data_2.csv"},
		{"data.csv", []string{"data.csv", "data_1.csv", "data_2.csv"}, "data_3.csv"},
		{"noext", []string{"noext"}, "noext_1"},
		{"archive.tar.gz", []string{"archive.tar.gz"}, "archive.tar_1.gz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sibs []client.FileItem
			for _, s := range tc.siblings {
				sibs = append(sibs, mkfile(s))
			}
			if got := findFreeName(tc.name, sibs); got != tc.want {
				t.Errorf("findFreeName(%q, %v) = %q, want %q", tc.name, tc.siblings, got, tc.want)
			}
		})
	}
}

func TestSplitPath(t *testing.T) {
	cases := []struct {
		path       string
		wantParent string
		wantName   string
	}{
		{"/data/results", "/data/", "results"},
		{"/data/results/", "/data/", "results"},
		{"/file.csv", "/", "file.csv"},
		{"/data/sub/file.csv", "/data/sub/", "file.csv"},
		{"file.csv", "/", "file.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			parent, name := splitPath(tc.path)
			if parent != tc.wantParent || name != tc.wantName {
				t.Errorf("splitPath(%q) = (%q, %q), want (%q, %q)",
					tc.path, parent, name, tc.wantParent, tc.wantName)
			}
		})
	}
}

func TestBuildOSFWebURL(t *testing.T) {
	cases := []struct {
		target resolver.Target
		want   string
	}{
		{resolver.Target{NodeID: "abc12", Path: "/"}, "https://osf.io/abc12/"},
		{resolver.Target{NodeID: "abc12", Path: ""}, "https://osf.io/abc12/"},
		{resolver.Target{NodeID: "abc12", Path: "/data"}, "https://osf.io/abc12/files/osfstorage/data"},
		{resolver.Target{NodeID: "abc12", Path: "/data/file.csv"}, "https://osf.io/abc12/files/osfstorage/data/file.csv"},
	}
	for _, tc := range cases {
		got := buildOSFWebURL(tc.target)
		if got != tc.want {
			t.Errorf("buildOSFWebURL(%+v) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
