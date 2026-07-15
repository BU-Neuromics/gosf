//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWikiAdd_NewAndExisting(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("remote home\n"))
	e.run("init", "abc12")

	// Existing page: pinned to its remote version + content MD5.
	e.writeFile("docs/home.md", "remote home\n")
	stdout, stderr, code := e.run("wiki", "add", "docs/home.md", "abc12:home", "--direction=both", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	var r struct {
		Entries []struct {
			Local   string `json:"local"`
			Page    string `json:"page"`
			Version int    `json:"version"`
			MD5     string `json:"md5"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(r.Entries) != 1 || r.Entries[0].Version != 1 || r.Entries[0].MD5 == "" {
		t.Errorf("existing-page entry = %+v", r.Entries)
	}

	// New page: unpinned (version 0).
	e.writeFile("docs/new.md", "new page\n")
	stdout, _, code = e.run("wiki", "add", "docs/new.md", "abc12:brand new", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	json.Unmarshal([]byte(stdout), &r)
	if r.Entries[0].Version != 0 || r.Entries[0].Page != "brand new" {
		t.Errorf("new-page entry = %+v", r.Entries)
	}

	// Duplicate local path is rejected.
	_, stderr, code = e.run("wiki", "add", "docs/home.md", "abc12:whatever")
	if code == 0 || !strings.Contains(stderr, "already exists") {
		t.Errorf("expected duplicate rejection, code=%d stderr=%s", code, stderr)
	}
}

func TestStatus_MixedFilesAndWikis(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddFile("abc12", "/data/x.csv", []byte("csv\n"))
	e.srv.AddWiki("abc12", "home", []byte("home v1\n"))

	e.run("init", "abc12")
	// Track the file (already identical → will pin).
	e.writeFile("data/x.csv", "csv\n")
	e.run("add", "data/x.csv", "abc12:/data/x.csv")
	// Track the wiki, pinned & in sync.
	e.writeFile("docs/home.md", "home v1\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home", "--direction=both")

	// A remote wiki edit makes the wiki REMOTE_NEWER.
	e.srv.AddWikiVersion("abc12", "home", []byte("home v2 from web\n"))

	stdout, stderr, code := e.run("status", "--output=json")
	// Not in sync → exit 1.
	if code != 1 {
		t.Fatalf("exit %d (want 1), stderr: %s\n%s", code, stderr, stdout)
	}
	var items []struct {
		Path  string `json:"path"`
		Kind  string `json:"kind"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	var sawFile, sawWiki bool
	for _, it := range items {
		if it.Kind == "wiki" && it.Path == "docs/home.md" {
			sawWiki = true
			if it.State != "REMOTE_NEWER" {
				t.Errorf("wiki state = %s, want REMOTE_NEWER", it.State)
			}
		}
		if it.Kind == "file" && it.Path == "data/x.csv" {
			sawFile = true
		}
	}
	if !sawFile || !sawWiki {
		t.Errorf("expected both a file and a wiki row, items=%+v", items)
	}
}

func TestStatus_WikiTableRow(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("home\n"))
	e.run("init", "abc12")
	e.writeFile("docs/home.md", "home\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home", "--direction=both")

	stdout, _, _ := e.run("status")
	if !strings.Contains(stdout, "docs/home.md") || !strings.Contains(stdout, `wiki "home"`) {
		t.Errorf("status table missing wiki row:\n%s", stdout)
	}
}
