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

func TestPlanUpload(t *testing.T) {
	existing := mkfile("data.csv")
	existing.Links.Upload = "https://files.osf.io/upload/existing"
	siblings := []client.FileItem{existing}

	t.Run("new file", func(t *testing.T) {
		p, err := planUpload("skip", nil, "abc12", "/data", "new.csv", nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.action != "upload" || p.name != "new.csv" {
			t.Errorf("got %+v", p)
		}
		if p.url != client.BuildUploadURL("abc12", "/data", "new.csv") {
			t.Errorf("url = %q", p.url)
		}
	})

	t.Run("skip existing", func(t *testing.T) {
		p, err := planUpload("skip", &existing, "abc12", "/data", "data.csv", siblings)
		if err != nil {
			t.Fatal(err)
		}
		if p.action != "skip" || p.url != "" {
			t.Errorf("got %+v, want skip with empty url", p)
		}
	})

	t.Run("overwrite existing", func(t *testing.T) {
		p, err := planUpload("overwrite", &existing, "abc12", "/data", "data.csv", siblings)
		if err != nil {
			t.Fatal(err)
		}
		if p.action != "overwrite" || p.url != existing.Links.Upload {
			t.Errorf("got %+v, want overwrite using existing upload link", p)
		}
	})

	t.Run("rename existing", func(t *testing.T) {
		p, err := planUpload("rename", &existing, "abc12", "/data", "data.csv", siblings)
		if err != nil {
			t.Fatal(err)
		}
		if p.action != "rename" || p.name != "data_1.csv" {
			t.Errorf("got %+v, want rename to data_1.csv", p)
		}
		if p.url != client.BuildUploadURL("abc12", "/data", "data_1.csv") {
			t.Errorf("url = %q", p.url)
		}
	})

	t.Run("unknown conflict mode", func(t *testing.T) {
		if _, err := planUpload("bogus", &existing, "abc12", "/data", "data.csv", siblings); err == nil {
			t.Error("expected error for unknown conflict mode")
		}
	})
}

func TestDeriveUploadTarget(t *testing.T) {
	cases := []struct {
		dest       string
		src        string
		wantParent string
		wantName   string
	}{
		// Explicit filename in dest.
		{"/data/out.csv", "in.csv", "/data", "out.csv"},
		{"/out.csv", "in.csv", "/", "out.csv"},
		{"/data/sub/out.csv", "in.csv", "/data/sub", "out.csv"},
		// Trailing slash => destination is a directory, keep source filename.
		{"/data/", "in.csv", "/data", "in.csv"},
		{"/data/sub/", "report.pdf", "/data/sub", "report.pdf"},
		{"/", "in.csv", "/", "in.csv"},
		// Bare/empty dest behaves like root directory.
		{"", "in.csv", "/", "in.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.dest, func(t *testing.T) {
			parent, name := deriveUploadTarget(tc.dest, tc.src)
			if parent != tc.wantParent || name != tc.wantName {
				t.Errorf("deriveUploadTarget(%q, %q) = (%q, %q), want (%q, %q)",
					tc.dest, tc.src, parent, name, tc.wantParent, tc.wantName)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	versions := []client.FileVersion{
		{ID: "3", Attributes: client.FileVersionAttributes{Version: 3}},
		{ID: "2", Attributes: client.FileVersionAttributes{Version: 2}},
		{ID: "1", Attributes: client.FileVersionAttributes{Version: 1}},
	}

	t.Run("valid version", func(t *testing.T) {
		if err := validateVersion(versions, 2); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("missing version", func(t *testing.T) {
		err := validateVersion(versions, 99)
		if err == nil {
			t.Fatal("expected error for missing version")
		}
		// Error should mention the requested version and how to list them.
		msg := err.Error()
		if !contains(msg, "99") || !contains(msg, "versions") {
			t.Errorf("unhelpful error: %q", msg)
		}
	})
	t.Run("no versions at all", func(t *testing.T) {
		if err := validateVersion(nil, 1); err == nil {
			t.Fatal("expected error when no versions exist")
		}
	})
}

// contains is a tiny helper so we don't import strings just for tests.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
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
