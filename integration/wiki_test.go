//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWikiLs(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("# Home\n"))
	e.srv.AddWiki("abc12", "Analysis Notes", []byte("notes body\n"))

	stdout, stderr, code := e.run("wiki", "ls", "abc12")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "home") || !strings.Contains(stdout, "Analysis Notes") {
		t.Errorf("table missing pages:\n%s", stdout)
	}
	if !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "VER") {
		t.Errorf("missing header:\n%s", stdout)
	}
}

func TestWikiLs_JSON(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("# Home\n"))
	e.srv.AddWikiVersion("abc12", "home", []byte("# Home v2\n"))

	stdout, stderr, code := e.run("wiki", "ls", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	var items []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Version      int    `json:"version"`
		Size         int64  `json:"size"`
		DateModified string `json:"date_modified"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(items) != 1 || items[0].Name != "home" || items[0].Version != 2 {
		t.Errorf("items = %+v", items)
	}
	if items[0].Size != int64(len("# Home v2\n")) {
		t.Errorf("size = %d", items[0].Size)
	}
}

func TestWikiLs_EmptyJSON(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")

	stdout, _, code := e.run("wiki", "ls", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("empty list should serialize as [], got %q", stdout)
	}
}

func TestWikiLs_Disabled(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.SetWikiDisabled("abc12")

	_, stderr, code := e.run("wiki", "ls", "abc12")
	if code == 0 {
		t.Fatal("expected non-zero exit for disabled wiki")
	}
	if !strings.Contains(stderr, "wiki for project abc12 is disabled") {
		t.Errorf("stderr should explain the disabled wiki, got: %s", stderr)
	}
}

func TestWikiGet_Stdout(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	content := "# Title\r\nline two\n\nno trailing newline"
	e.srv.AddWiki("abc12", "home", []byte(content))

	stdout, stderr, code := e.run("wiki", "get", "abc12")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if stdout != content {
		t.Errorf("content not byte-exact:\ngot  %q\nwant %q", stdout, content)
	}
}

func TestWikiGet_NamedPageAndDest(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "Analysis Notes", []byte("notes\n"))

	_, stderr, code := e.run("wiki", "get", "abc12:Analysis Notes", "out/notes.md")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if got := e.readFile("out/notes.md"); got != "notes\n" {
		t.Errorf("dest file = %q", got)
	}

	// Refuses to overwrite without --force.
	_, stderr, code = e.run("wiki", "get", "abc12:Analysis Notes", "out/notes.md")
	if code == 0 || !strings.Contains(stderr, "--force") {
		t.Errorf("expected overwrite refusal, code=%d stderr=%s", code, stderr)
	}
	// --force overwrites.
	_, _, code = e.run("wiki", "get", "abc12:Analysis Notes", "out/notes.md", "--force")
	if code != 0 {
		t.Errorf("get --force failed: %d", code)
	}
}

func TestWikiGet_Version(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("v1 content\n"))
	e.srv.AddWikiVersion("abc12", "home", []byte("v2 content\n"))

	stdout, stderr, code := e.run("wiki", "get", "abc12:home", "--version=1")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if stdout != "v1 content\n" {
		t.Errorf("historical content = %q", stdout)
	}

	// A version beyond latest errors early.
	_, stderr, code = e.run("wiki", "get", "abc12:home", "--version=9")
	if code == 0 || !strings.Contains(stderr, "does not exist") {
		t.Errorf("expected version-not-found, code=%d stderr=%s", code, stderr)
	}
}

func TestWikiGet_JSON(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("# Home\n"))
	e.srv.AddWikiVersion("abc12", "home", []byte("# Home v2\n"))

	stdout, stderr, code := e.run("wiki", "get", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	var r struct {
		Project string `json:"project"`
		Page    string `json:"page"`
		Version int    `json:"version"`
		Size    int64  `json:"size"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if r.Project != "abc12" || r.Page != "home" || r.Version != 2 || r.Content != "# Home v2\n" {
		t.Errorf("result = %+v", r)
	}
}

func TestWikiGet_PageNotFound(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("x"))

	_, stderr, code := e.run("wiki", "get", "abc12:missing")
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if !strings.Contains(stderr, `wiki page "missing" not found`) {
		t.Errorf("stderr = %s", stderr)
	}
}

func TestWikiGet_CaseInsensitiveMatch(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "Protocol", []byte("steps\n"))

	stdout, stderr, code := e.run("wiki", "get", "abc12:protocol")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if stdout != "steps\n" {
		t.Errorf("content = %q", stdout)
	}
}

func TestWikiVersions(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "home", []byte("v1\n"))
	e.srv.AddWikiVersion("abc12", "home", []byte("v2 longer\n"))

	stdout, stderr, code := e.run("wiki", "versions", "abc12:home")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "VERSION") {
		t.Errorf("missing header:\n%s", stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 { // header + 2 versions
		t.Fatalf("got %d lines:\n%s", len(lines), stdout)
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "2") {
		t.Errorf("newest version should be first:\n%s", stdout)
	}

	// JSON contract.
	stdout, _, code = e.run("wiki", "versions", "abc12:home", "--output=json")
	if code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var r struct {
		Versions []struct {
			Version int   `json:"version"`
			Size    int64 `json:"size"`
		} `json:"versions"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if len(r.Versions) != 2 || r.Versions[0].Version != 2 {
		t.Errorf("versions = %+v", r.Versions)
	}
}

func TestWikiVersions_RequiresPage(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")

	_, stderr, code := e.run("wiki", "versions", "abc12")
	if code == 0 || !strings.Contains(stderr, "page name required") {
		t.Errorf("expected page-required error, code=%d stderr=%s", code, stderr)
	}
}

func TestWikiOpen_JSON(t *testing.T) {
	e := newTestEnv(t)

	stdout, _, code := e.run("wiki", "open", "abc12:Analysis Notes", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if r.URL != "https://osf.io/abc12/wiki/Analysis%20Notes/" {
		t.Errorf("url = %q", r.URL)
	}
}

func TestWikiLs_AnonymousRead(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Public Wiki")
	e.srv.AddWiki("abc12", "home", []byte("public\n"))

	// No token at all: reads must still work on public projects.
	stdout, stderr, code := e.runClean("", "wiki", "ls", "abc12")
	if code != 0 {
		t.Fatalf("anonymous wiki ls failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "home") {
		t.Errorf("missing page in table:\n%s", stdout)
	}
}
