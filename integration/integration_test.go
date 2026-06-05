//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BU-Neuromics/gosf/internal/testutil/fakeosf"
)

// binaryPath is set once in TestMain and shared across all tests.
var binaryPath string

func TestMain(m *testing.M) {
	bin, err := buildBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: build binary: %v\n", err)
		os.Exit(1)
	}
	binaryPath = bin
	os.Exit(m.Run())
}

func buildBinary() (string, error) {
	// When running "go test ./integration/", the working dir is integration/.
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repoRoot, err := filepath.Abs(filepath.Join(wd, ".."))
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp("", "gosf-integ-*")
	if err != nil {
		return "", err
	}

	bin := filepath.Join(tmp, "gosf")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build failed: %s", out)
	}
	return bin, nil
}

// testEnv wraps a temp working directory and fake server for one test.
type testEnv struct {
	t   *testing.T
	srv *fakeosf.Server
	dir string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	srv := fakeosf.New()
	t.Cleanup(srv.Close)
	return &testEnv{t: t, srv: srv, dir: t.TempDir()}
}

// run executes gosf with args in the test directory and returns stdout, stderr, exit code.
func (e *testEnv) run(args ...string) (stdout, stderr string, code int) {
	e.t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = e.dir
	cmd.Env = append(os.Environ(),
		// The OSF client appends "/nodes/...", "/files/..." etc. to the base,
		// so we must include the /v2 prefix that the real URL carries.
		"GOSF_API_BASE="+e.srv.URL()+"/v2",
		// The Waterbutler client uses the full path, so no suffix needed.
		"GOSF_FILES_BASE="+e.srv.URL(),
		"OSF_TOKEN=test-token",
		// Isolate from the developer's real keychain and config.
		"HOME="+e.dir,
		"XDG_CONFIG_HOME="+filepath.Join(e.dir, ".config"),
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return outBuf.String(), errBuf.String(), code
}

// writeFile creates a file (and any parent dirs) under the test directory.
func (e *testEnv) writeFile(name, content string) {
	e.t.Helper()
	p := filepath.Join(e.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		e.t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		e.t.Fatalf("write %s: %v", name, err)
	}
}

// readFile reads a file from the test directory.
func (e *testEnv) readFile(name string) string {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dir, filepath.FromSlash(name)))
	if err != nil {
		e.t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// fileExists reports whether name exists inside the test directory.
func (e *testEnv) fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(name)))
	return err == nil
}

// ---- Pull ----

func TestPull_SingleFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/results.csv", []byte("col1,col2\n1,2\n"))

	_, stderr, code := env.run("pull", "abc12:/results.csv", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !env.fileExists("results.csv") {
		t.Fatal("results.csv not created")
	}
	if got := env.readFile("results.csv"); got != "col1,col2\n1,2\n" {
		t.Errorf("content = %q", got)
	}
}

func TestPull_FolderTree(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/a.csv", []byte("aaa"))
	env.srv.AddFile("abc12", "/data/b.csv", []byte("bbb"))

	_, stderr, code := env.run("pull", "abc12:/data/", "out/", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if got := env.readFile("out/a.csv"); got != "aaa" {
		t.Errorf("a.csv = %q", got)
	}
	if got := env.readFile("out/b.csv"); got != "bbb" {
		t.Errorf("b.csv = %q", got)
	}
}

func TestPull_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/report.csv", []byte("data"))

	stdout, _, code := env.run("pull", "abc12:/report.csv", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if env.fileExists("report.csv") {
		t.Fatal("dry-run should not create file")
	}
	if !strings.Contains(stdout, "report.csv") {
		t.Errorf("expected filename in dry-run output; got %q", stdout)
	}
}

// ---- Push ----

func TestPush_NewFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("data.csv", "col1,col2\n1,2\n")

	_, stderr, code := env.run("push", "data.csv", "abc12:/data.csv", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	uploads := env.srv.Uploads()
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if string(uploads[0].Content) != "col1,col2\n1,2\n" {
		t.Errorf("upload content = %q", uploads[0].Content)
	}
	if uploads[0].Path != "/data.csv" {
		t.Errorf("upload path = %q, want /data.csv", uploads[0].Path)
	}
}

func TestPush_ConflictSkip(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data.csv", []byte("original"))
	env.writeFile("data.csv", "new content")

	_, _, code := env.run("push", "data.csv", "abc12:/data.csv", "--conflict=skip", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	// skip: no new upload should have been recorded.
	if uploads := env.srv.Uploads(); len(uploads) != 0 {
		t.Errorf("expected 0 uploads with --conflict=skip, got %d", len(uploads))
	}
}

func TestPush_ConflictOverwrite(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data.csv", []byte("original"))
	env.writeFile("data.csv", "new content")

	_, stderr, code := env.run("push", "data.csv", "abc12:/data.csv", "--conflict=overwrite", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	f := env.srv.GetFile("abc12", "/data.csv")
	if f == nil {
		t.Fatal("file not in server")
	}
	if f.LatestVersion() != 2 {
		t.Errorf("latest version = %d, want 2", f.LatestVersion())
	}
	if string(f.LatestContent()) != "new content" {
		t.Errorf("content = %q", f.LatestContent())
	}
}

func TestPush_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("report.csv", "x")

	stdout, _, code := env.run("push", "report.csv", "abc12:/report.csv", "--output=json", "--quiet")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	uploaded, ok := result["uploaded"].([]any)
	if !ok || len(uploaded) != 1 {
		t.Fatalf("uploaded = %v", result["uploaded"])
	}
	item := uploaded[0].(map[string]any)
	if item["path"] != "/report.csv" {
		t.Errorf("path = %v", item["path"])
	}
	if item["action"] != "upload" {
		t.Errorf("action = %v", item["action"])
	}
}

// ---- Sync ----

func TestSync_PushAheadOfManifest(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/data.csv", []byte("original"))

	env.writeFile("data.csv", "modified content")
	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "data.csv"
remote    = "/data.csv"
direction = "push"
version   = 1
md5       = "%s"
`, f.VersionMD5(1)))

	_, stderr, code := env.run("sync", "--quiet", "--no-check-remote")
	if code != 0 {
		t.Fatalf("sync exit %d; stderr=%s", code, stderr)
	}

	uploads := env.srv.Uploads()
	if len(uploads) == 0 {
		t.Fatal("expected an upload, got none")
	}
	if string(uploads[0].Content) != "modified content" {
		t.Errorf("upload content = %q", uploads[0].Content)
	}

	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "version = 2") {
		t.Errorf("expected version = 2 in gosf.toml:\n%s", toml)
	}
}

func TestSync_PullNew_MissingFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/data.csv", []byte("remote content"))

	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "data.csv"
remote    = "/data.csv"
direction = "pull"
version   = 1
md5       = "%s"
`, f.VersionMD5(1)))

	_, stderr, code := env.run("sync", "--pull-new", "--quiet")
	if code != 0 {
		t.Fatalf("sync --pull-new exit %d; stderr=%s", code, stderr)
	}
	if !env.fileExists("data.csv") {
		t.Fatal("data.csv not downloaded")
	}
	if got := env.readFile("data.csv"); got != "remote content" {
		t.Errorf("content = %q", got)
	}
}

func TestSync_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/data.csv", []byte("original"))

	env.writeFile("data.csv", "changed")
	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "data.csv"
remote    = "/data.csv"
direction = "push"
version   = 1
md5       = "%s"
`, f.VersionMD5(1)))

	stdout, _, code := env.run("sync", "--dry-run", "--no-check-remote")
	if code != 0 {
		t.Fatalf("sync --dry-run exit %d", code)
	}
	if len(env.srv.Uploads()) != 0 {
		t.Error("dry-run should not upload")
	}
	if !strings.Contains(stdout, "dry-run") {
		t.Errorf("expected dry-run in output; got %q", stdout)
	}
}

func TestSync_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/report.csv", []byte("v1"))

	env.writeFile("report.csv", "v1") // in sync
	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "report.csv"
remote    = "/report.csv"
direction = "push"
version   = 1
md5       = "%s"
`, f.VersionMD5(1)))

	stdout, _, code := env.run("sync", "--output=json", "--no-check-remote")
	if code != 0 {
		t.Fatalf("sync exit %d; stdout=%s", code, stdout)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0]["state"] != "IN_SYNC" {
		t.Errorf("state = %v, want IN_SYNC", items[0]["state"])
	}
}

// ---- Status ----

func TestStatus_JSON_States(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/synced.csv", []byte("synced"))

	env.writeFile("synced.csv", "synced") // matches pinned MD5 → IN_SYNC
	// missing.csv does not exist locally → MISSING

	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "synced.csv"
remote    = "/synced.csv"
direction = "pull"
version   = 1
md5       = "%s"

[[files]]
local     = "missing.csv"
remote    = "/missing.csv"
direction = "pull"
version   = 1
md5       = "abc123def456"
`, f.VersionMD5(1)))

	stdout, _, code := env.run("status", "--output=json", "--no-check-remote")
	if code != 1 {
		t.Fatalf("status exit %d, want 1 (not all in sync); stdout=%s", code, stdout)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	states := map[string]string{}
	for _, item := range items {
		states[item["path"].(string)] = item["state"].(string)
	}
	if states["synced.csv"] != "IN_SYNC" {
		t.Errorf("synced.csv state = %q, want IN_SYNC", states["synced.csv"])
	}
	if states["missing.csv"] != "MISSING" {
		t.Errorf("missing.csv state = %q, want MISSING", states["missing.csv"])
	}
}

func TestStatus_ExitCode(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/ok.csv", []byte("ok"))

	env.writeFile("ok.csv", "ok")
	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local     = "ok.csv"
remote    = "/ok.csv"
direction = "pull"
version   = 1
md5       = "%s"
`, f.VersionMD5(1)))

	_, _, code := env.run("status", "--no-check-remote")
	if code != 0 {
		t.Errorf("status exit %d, want 0 when all in sync", code)
	}
}

// ---- Add ----

func TestAdd_FetchesRemoteVersion(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/data/counts.h5", []byte("h5 data"))

	_, stderr, code := env.run("add", "data/counts.h5", "abc12:/data/counts.h5", "--direction=pull")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}

	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "version = 1") {
		t.Errorf("expected version = 1 in gosf.toml:\n%s", toml)
	}
	if !strings.Contains(toml, f.VersionMD5(1)) {
		t.Errorf("expected md5 %q in gosf.toml:\n%s", f.VersionMD5(1), toml)
	}
}

func TestAdd_NoRemoteFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	// No file on OSF — add should still work with version=0.

	_, stderr, code := env.run("add", "local/new.csv", "abc12:/data/new.csv", "--direction=push")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}

	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "version = 0") {
		t.Errorf("expected version = 0 in gosf.toml:\n%s", toml)
	}
}

func TestAdd_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")

	stdout, _, code := env.run("add", "model.pkl", "abc12:/results/model.pkl", "--direction=push", "--output=json")
	if code != 0 {
		t.Fatalf("add exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["local"] != "model.pkl" {
		t.Errorf("local = %v", result["local"])
	}
	if result["direction"] != "push" {
		t.Errorf("direction = %v", result["direction"])
	}
	if result["manifest_created"] != true {
		t.Errorf("manifest_created = %v", result["manifest_created"])
	}
}
