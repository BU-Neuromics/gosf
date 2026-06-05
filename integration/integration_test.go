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

func TestPull_AutoTracksFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("h5 content"))
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")

	_, stderr, code := env.run("pull", "abc12:/data/counts.h5", "--quiet")
	if code != 0 {
		t.Fatalf("pull exit %d; stderr=%s", code, stderr)
	}
	if !env.fileExists("data/counts.h5") {
		t.Fatal("file not downloaded")
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/data/counts.h5") {
		t.Errorf("expected manifest entry for /data/counts.h5:\n%s", toml)
	}
	if !strings.Contains(toml, "pull") {
		t.Errorf("expected direction pull in manifest:\n%s", toml)
	}
}

func TestPull_NoTrack(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("h5 content"))
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")

	_, stderr, code := env.run("pull", "abc12:/data/counts.h5", "--no-track", "--quiet")
	if code != 0 {
		t.Fatalf("pull exit %d; stderr=%s", code, stderr)
	}
	if !env.fileExists("data/counts.h5") {
		t.Fatal("file not downloaded")
	}
	toml := env.readFile("gosf.toml")
	if strings.Contains(toml, "counts.h5") {
		t.Errorf("--no-track: manifest should not contain counts.h5:\n%s", toml)
	}
}

func TestPull_DuplicateRemoteConflict(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "other/counts.h5"
remote = "/data/counts.h5"
direction = "pull"
version = 0
md5 = ""
`)
	// Pulling same remote path to a different local dest should error.
	_, stderr, code := env.run("pull", "abc12:/data/counts.h5", "new_dest/", "--quiet")
	if code == 0 {
		t.Error("expected error when pulling tracked remote to different local path")
	}
	if !strings.Contains(stderr, "already tracked") {
		t.Errorf("stderr should mention 'already tracked': %s", stderr)
	}
}

func TestPull_BareFollowsManifest(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/data/counts.h5", []byte("counts data"))
	env.writeFile("gosf.toml", fmt.Sprintf(`[project]
id = "abc12"

[[files]]
local = "data/counts.h5"
remote = "/data/counts.h5"
direction = "pull"
version = 1
md5 = "%s"
`, f.VersionMD5(1)))

	_, stderr, code := env.run("pull", "--quiet")
	if code != 0 {
		t.Fatalf("bare pull exit %d; stderr=%s", code, stderr)
	}
	if !env.fileExists("data/counts.h5") {
		t.Fatal("pull-eligible file not downloaded by bare pull")
	}
}

func TestPull_BareNoProjectID(t *testing.T) {
	env := newTestEnv(t)
	// gosf.toml exists but has no [project].id and no entries.
	env.writeFile("gosf.toml", "")

	_, stderr, code := env.run("pull")
	if code == 0 {
		t.Error("expected error for bare pull with no project configured")
	}
	if !strings.Contains(stderr, "gosf init") {
		t.Errorf("error should mention 'gosf init': %s", stderr)
	}
}

func TestPull_BareSkipsPushEntries(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "data/push_only.csv"
remote = "/data/push_only.csv"
direction = "push"
version = 0
md5 = ""
`)
	env.writeFile("data/push_only.csv", "data")

	// Bare pull should not touch push-direction entries.
	_, _, code := env.run("pull", "--quiet")
	if code != 0 {
		t.Fatalf("bare pull should succeed even with push-only entries")
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

func TestPush_AutoTracksFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")
	env.writeFile("data.csv", "col1,col2\n1,2\n")

	_, stderr, code := env.run("push", "data.csv", "abc12:/data.csv", "--quiet")
	if code != 0 {
		t.Fatalf("push exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/data.csv") {
		t.Errorf("expected /data.csv in gosf.toml:\n%s", toml)
	}
	if !strings.Contains(toml, "push") {
		t.Errorf("expected direction push in gosf.toml:\n%s", toml)
	}
	if !strings.Contains(toml, "version = 1") {
		t.Errorf("expected version = 1 in gosf.toml:\n%s", toml)
	}
}

func TestPush_NoTrack(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")
	env.writeFile("data.csv", "content")

	_, stderr, code := env.run("push", "data.csv", "abc12:/data.csv", "--no-track", "--quiet")
	if code != 0 {
		t.Fatalf("push exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if strings.Contains(toml, "data.csv") {
		t.Errorf("--no-track: manifest should not contain data.csv:\n%s", toml)
	}
}

func TestPush_DuplicateRemoteConflict(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "other/data.csv"
remote = "/data.csv"
direction = "push"
version = 0
md5 = ""
`)
	env.writeFile("data.csv", "new content")

	_, stderr, code := env.run("push", "data.csv", "abc12:/data.csv", "--quiet")
	if code == 0 {
		t.Error("expected error when pushing to already-tracked remote from different local path")
	}
	if !strings.Contains(stderr, "already tracked") {
		t.Errorf("stderr should mention 'already tracked': %s", stderr)
	}
}

func TestPush_BarePushFollowsManifest(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("data/report.csv", "report data")
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "data/report.csv"
remote = "/data/report.csv"
direction = "push"
version = 0
md5 = ""
`)

	_, stderr, code := env.run("push", "--quiet")
	if code != 0 {
		t.Fatalf("bare push exit %d; stderr=%s", code, stderr)
	}
	uploads := env.srv.Uploads()
	if len(uploads) == 0 {
		t.Fatal("expected upload from bare push")
	}
	if uploads[0].Path != "/data/report.csv" {
		t.Errorf("upload path = %q, want /data/report.csv", uploads[0].Path)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "version = 1") {
		t.Errorf("expected version = 1 after bare push:\n%s", toml)
	}
}

func TestPush_BarePushNoProjectID(t *testing.T) {
	env := newTestEnv(t)
	env.writeFile("gosf.toml", "")

	_, stderr, code := env.run("push")
	if code == 0 {
		t.Error("expected error for bare push with no project configured")
	}
	if !strings.Contains(stderr, "gosf init") {
		t.Errorf("error should mention 'gosf init': %s", stderr)
	}
}

func TestPush_BarePushSkipsPullEntries(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("data/pull_only.csv", "data")
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "data/pull_only.csv"
remote = "/data/pull_only.csv"
direction = "pull"
version = 0
md5 = ""
`)

	_, _, code := env.run("push", "--quiet")
	if code != 0 {
		t.Fatalf("bare push should succeed even with pull-only entries")
	}
	if uploads := env.srv.Uploads(); len(uploads) != 0 {
		t.Errorf("bare push should not upload pull-only entries; got %d uploads", len(uploads))
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

	_, stderr, code := env.run("add", "data/counts.h5", "abc12:/data/counts.h5")
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
	if !strings.Contains(toml, "push") {
		t.Errorf("expected direction push in gosf.toml:\n%s", toml)
	}
}

func TestAdd_NoRemoteFile(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	// No file on OSF — add should still work with version=0.

	_, stderr, code := env.run("add", "local/new.csv", "abc12:/data/new.csv")
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

	stdout, _, code := env.run("add", "model.pkl", "abc12:/results/model.pkl", "--output=json")
	if code != 0 {
		t.Fatalf("add exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	entries, ok := result["entries"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("expected non-empty entries array, got: %v", result["entries"])
	}
	entry := entries[0].(map[string]any)
	if entry["local"] != "model.pkl" {
		t.Errorf("local = %v", entry["local"])
	}
	if result["manifest_created"] != true {
		t.Errorf("manifest_created = %v", result["manifest_created"])
	}
}

// ---- Init ----

func TestInit_CreatesGOSFToml(t *testing.T) {
	env := newTestEnv(t)
	_, stderr, code := env.run("init", "abc12")
	if code != 0 {
		t.Fatalf("init exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "abc12") {
		t.Errorf("expected abc12 in gosf.toml:\n%s", toml)
	}
}

func TestInit_UpdatesProject(t *testing.T) {
	env := newTestEnv(t)
	env.writeFile("gosf.toml", "[project]\nid = \"old12\"\n")
	_, stderr, code := env.run("init", "new99")
	if code != 0 {
		t.Fatalf("init exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "new99") {
		t.Errorf("expected new99 in gosf.toml:\n%s", toml)
	}
	if strings.Contains(toml, "old12") {
		t.Errorf("old project id should be gone:\n%s", toml)
	}
}

func TestInit_JSON(t *testing.T) {
	env := newTestEnv(t)
	stdout, _, code := env.run("init", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("init exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["project"] != "abc12" {
		t.Errorf("project = %v", result["project"])
	}
	if result["created"] != true {
		t.Errorf("created = %v", result["created"])
	}
}

// ---- Add (new scp-style behaviour) ----

func TestAdd_NoDestMirrorsPath(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")

	_, stderr, code := env.run("add", "data/file.txt")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/data/file.txt") {
		t.Errorf("expected /data/file.txt in gosf.toml:\n%s", toml)
	}
	if !strings.Contains(toml, "push") {
		t.Errorf("expected direction push in gosf.toml:\n%s", toml)
	}
}

func TestAdd_DestDirectory(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")

	_, stderr, code := env.run("add", "local/file.txt", "abc12:/results/")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/results/file.txt") {
		t.Errorf("expected /results/file.txt in gosf.toml:\n%s", toml)
	}
}

func TestAdd_DirectoryRecursion(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")
	env.writeFile("data/dir/file1.txt", "content")
	env.writeFile("data/dir/sub/file2.txt", "content")

	// No trailing slash: dir name preserved in remote path.
	_, stderr, code := env.run("add", "data/dir", "abc12:/results/")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/results/dir/file1.txt") {
		t.Errorf("expected /results/dir/file1.txt:\n%s", toml)
	}
	if !strings.Contains(toml, "/results/dir/sub/file2.txt") {
		t.Errorf("expected /results/dir/sub/file2.txt:\n%s", toml)
	}
}

func TestAdd_DirTrailingSlash(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")
	env.writeFile("data/dir/file.txt", "content")

	// Trailing slash: dir name stripped, contents go directly under dest.
	_, stderr, code := env.run("add", "data/dir/", "abc12:/results/")
	if code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}
	toml := env.readFile("gosf.toml")
	if !strings.Contains(toml, "/results/file.txt") {
		t.Errorf("expected /results/file.txt (dir name stripped):\n%s", toml)
	}
}

func TestAdd_DirectionFlagRemoved(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", "[project]\nid = \"abc12\"\n")

	_, _, code := env.run("add", "file.txt", "abc12:/file.txt", "--direction=pull")
	if code == 0 {
		t.Error("expected non-zero exit: --direction flag should no longer exist")
	}
}

func TestAdd_AlreadyTracked(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.writeFile("gosf.toml", `[project]
id = "abc12"

[[files]]
local = "data/file.txt"
remote = "/data/file.txt"
direction = "push"
version = 0
md5 = ""
`)
	_, stderr, code := env.run("add", "data/file.txt", "abc12:/data/file.txt")
	if code == 0 {
		t.Error("expected error when adding already-tracked file")
	}
	if !strings.Contains(stderr, "already") {
		t.Errorf("stderr should mention 'already': %s", stderr)
	}
}

// ---- Ls ----

func TestLs_Text(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/results.csv", []byte("data"))
	env.srv.AddFile("abc12", "/report.pdf", []byte("pdf content"))

	stdout, stderr, code := env.run("ls", "abc12")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "results.csv") {
		t.Errorf("expected results.csv in output; got %q", stdout)
	}
	if !strings.Contains(stdout, "report.pdf") {
		t.Errorf("expected report.pdf in output; got %q", stdout)
	}
}

func TestLs_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("h5"))

	stdout, _, code := env.run("ls", "abc12:/data", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	attrs, _ := items[0]["attributes"].(map[string]any)
	if attrs["name"] != "counts.h5" {
		t.Errorf("name = %v, want counts.h5", attrs["name"])
	}
}

func TestLs_Empty(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	// No files — root listing should report empty.

	_, stderr, code := env.run("ls", "abc12")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "empty") {
		t.Errorf("expected '(empty)' message; got stderr=%q", stderr)
	}
}

func TestLs_JSON_Empty(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")

	stdout, _, code := env.run("ls", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var items []any
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if len(items) != 0 {
		t.Errorf("expected empty array [], got %v", items)
	}
}

// ---- Rm ----

func TestRm_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/old.csv", []byte("stale"))

	stdout, _, code := env.run("rm", "abc12:/old.csv", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	if len(env.srv.Deletes()) != 0 {
		t.Error("dry-run should not delete anything")
	}
	if !strings.Contains(stdout, "dry-run") {
		t.Errorf("expected dry-run in output; got %q", stdout)
	}
}

func TestRm_WithYes(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	f := env.srv.AddFile("abc12", "/scratch.csv", []byte("temp"))

	stdout, stderr, code := env.run("rm", "abc12:/scratch.csv", "--yes")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s stdout=%s", code, stderr, stdout)
	}
	deletes := env.srv.Deletes()
	if len(deletes) != 1 {
		t.Fatalf("deletes = %d, want 1", len(deletes))
	}
	if deletes[0] != f.ID {
		t.Errorf("deleted file ID = %q, want %q", deletes[0], f.ID)
	}
}

func TestRm_JSON_RequiresYes(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data.csv", []byte("x"))

	_, stderr, code := env.run("rm", "abc12:/data.csv", "--output=json")
	if code == 0 {
		t.Fatal("expected non-zero exit when --yes omitted in JSON mode")
	}
	if !strings.Contains(stderr, "yes") {
		t.Errorf("expected mention of --yes in error; got %q", stderr)
	}
}

func TestRm_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data.csv", []byte("content"))

	stdout, _, code := env.run("rm", "abc12:/data.csv", "--yes", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["node"] != "abc12" {
		t.Errorf("node = %v", result["node"])
	}
	if result["path"] != "/data.csv" {
		t.Errorf("path = %v", result["path"])
	}
	if result["kind"] != "file" {
		t.Errorf("kind = %v", result["kind"])
	}
	if result["dry_run"] != false {
		t.Errorf("dry_run = %v", result["dry_run"])
	}
}

func TestRm_JSON_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/tmp.csv", []byte("x"))

	stdout, _, code := env.run("rm", "abc12:/tmp.csv", "--dry-run", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["dry_run"] != true {
		t.Errorf("dry_run = %v, want true", result["dry_run"])
	}
	if len(env.srv.Deletes()) != 0 {
		t.Error("dry-run --output=json should not delete")
	}
}

// ---- Versions ----

func TestVersions_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data.csv", []byte("v1"))

	stdout, _, code := env.run("versions", "abc12:/data.csv", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	versions, ok := result["versions"].([]any)
	if !ok || len(versions) != 1 {
		t.Fatalf("versions = %v", result["versions"])
	}
	v := versions[0].(map[string]any)
	if v["version"] != float64(1) {
		t.Errorf("version = %v, want 1", v["version"])
	}
	if v["contributor"] != "test@example.com" {
		t.Errorf("contributor = %v, want test@example.com", v["contributor"])
	}
}

func TestVersions_Text(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/report.csv", []byte("content here"))

	stdout, stderr, code := env.run("versions", "abc12:/report.csv")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "VERSION") {
		t.Errorf("expected VERSION header; got %q", stdout)
	}
	if !strings.Contains(stdout, "1") {
		t.Errorf("expected version 1 in output; got %q", stdout)
	}
}

func TestVersions_FolderError(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	// Create a folder by adding a file inside it.
	env.srv.AddFile("abc12", "/mydir/file.csv", []byte("x"))

	_, stderr, code := env.run("versions", "abc12:/mydir")
	if code == 0 {
		t.Fatal("expected non-zero exit for folder target")
	}
	if !strings.Contains(stderr, "folder") {
		t.Errorf("expected 'folder' in error; got %q", stderr)
	}
}

// ---- Info ----

func TestInfo_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "My Research Project")

	stdout, _, code := env.run("info", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["id"] != "abc12" {
		t.Errorf("id = %v, want abc12", result["id"])
	}
	attrs, _ := result["attributes"].(map[string]any)
	if attrs["title"] != "My Research Project" {
		t.Errorf("title = %v", attrs["title"])
	}
}

func TestInfo_Text(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("prj99", "Neuroscience Study")

	stdout, stderr, code := env.run("info", "prj99")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "Neuroscience Study") {
		t.Errorf("expected title in output; got %q", stdout)
	}
	if !strings.Contains(stdout, "prj99") {
		t.Errorf("expected GUID in output; got %q", stdout)
	}
}

func TestInfo_NotFound(t *testing.T) {
	env := newTestEnv(t)
	// No projects registered — any lookup should 404.

	_, stderr, code := env.run("info", "xxxxx")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown project")
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("expected 'not found' in error; got %q", stderr)
	}
}

// ---- Projects ----

func TestProjects_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("aaa11", "Project Alpha")
	env.srv.AddProject("bbb22", "Project Beta")

	stdout, _, code := env.run("projects", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var nodes []map[string]any
	if err := json.Unmarshal([]byte(stdout), &nodes); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(nodes))
	}
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n["id"].(string)] = true
	}
	if !ids["aaa11"] || !ids["bbb22"] {
		t.Errorf("expected both project IDs; got %v", ids)
	}
}

func TestProjects_Text(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("ccc33", "The Study")

	stdout, stderr, code := env.run("projects")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "The Study") {
		t.Errorf("expected project title; got %q", stdout)
	}
	if !strings.Contains(stdout, "ccc33") {
		t.Errorf("expected project GUID; got %q", stdout)
	}
}

func TestProjects_NoToken(t *testing.T) {
	env := newTestEnv(t)

	// Override to remove the token.
	cmd := exec.Command(binaryPath, "projects")
	cmd.Dir = env.dir
	cmd.Env = append(os.Environ(),
		"GOSF_API_BASE="+env.srv.URL()+"/v2",
		"GOSF_FILES_BASE="+env.srv.URL(),
		"OSF_TOKEN=",
		"HOME="+env.dir,
		"XDG_CONFIG_HOME="+filepath.Join(env.dir, ".config"),
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	_ = cmd.Run()
	if cmd.ProcessState.ExitCode() == 0 {
		t.Fatal("expected non-zero exit without token")
	}
	if !strings.Contains(errBuf.String(), "auth") {
		t.Errorf("expected auth hint; got %q", errBuf.String())
	}
}

// ---- Open ----

func TestOpen_JSON_Root(t *testing.T) {
	env := newTestEnv(t)

	stdout, _, code := env.run("open", "abc12", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	want := "https://osf.io/abc12/"
	if result["url"] != want {
		t.Errorf("url = %v, want %q", result["url"], want)
	}
}

func TestOpen_JSON_File(t *testing.T) {
	env := newTestEnv(t)

	stdout, _, code := env.run("open", "abc12:/data/results.csv", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	want := "https://osf.io/abc12/files/osfstorage/data/results.csv"
	if result["url"] != want {
		t.Errorf("url = %v, want %q", result["url"], want)
	}
}

// ---- Mkdir ----

func TestMkdir_Basic(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")

	_, stderr, code := env.run("mkdir", "abc12:/results/2026")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	flds := env.srv.Folders()
	if len(flds) == 0 {
		t.Fatal("expected a folder to be created")
	}
	if flds[0] != "/results/2026" {
		t.Errorf("folder path = %q, want /results/2026", flds[0])
	}
}

func TestMkdir_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")

	stdout, _, code := env.run("mkdir", "abc12:/data/raw", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["path"] != "/data/raw" {
		t.Errorf("path = %v, want /data/raw", result["path"])
	}
	if result["created"] != true {
		t.Errorf("created = %v, want true", result["created"])
	}
}

func TestMkdir_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")

	_, _, code := env.run("mkdir", "abc12:/dry/folder", "--dry-run")
	if code != 0 {
		t.Fatalf("unexpected exit %d", code)
	}
	if len(env.srv.Folders()) != 0 {
		t.Error("dry-run should not create folder")
	}
}

// ---- Set ----

func TestSet_Description(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "My Project")

	_, stderr, code := env.run("set", "abc12", "--description", "Updated desc")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	patches := env.srv.NodePatches()
	if len(patches) == 0 {
		t.Fatal("expected a PATCH request")
	}
	if patches[0].Attrs["description"] != "Updated desc" {
		t.Errorf("description = %v", patches[0].Attrs["description"])
	}
}

func TestSet_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "My Project")

	stdout, _, code := env.run("set", "abc12", "--title", "New Title", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["id"] != "abc12" {
		t.Errorf("id = %v, want abc12", result["id"])
	}
}

func TestSet_NoFlags(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "My Project")

	_, _, code := env.run("set", "abc12")
	if code == 0 {
		t.Fatal("expected non-zero exit when no flags given")
	}
}

func TestSet_MultipleFields(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "My Project")

	_, stderr, code := env.run("set", "abc12",
		"--title", "Updated",
		"--category", "analysis",
		"--tags", "processed,qc-passed",
	)
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	patches := env.srv.NodePatches()
	if len(patches) == 0 {
		t.Fatal("expected a PATCH request")
	}
	attrs := patches[0].Attrs
	if attrs["title"] != "Updated" {
		t.Errorf("title = %v", attrs["title"])
	}
	if attrs["category"] != "analysis" {
		t.Errorf("category = %v", attrs["category"])
	}
	tags, _ := attrs["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2 elements", tags)
	}
}

// ---- Mv ----

func TestMv_Rename(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	_, stderr, code := env.run("mv", "abc12:/data/counts.h5", "abc12:/data/counts_v2.h5")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	moves := env.srv.Moves()
	if len(moves) == 0 {
		t.Fatal("expected a move action")
	}
	if moves[0].Action != "rename" {
		t.Errorf("action = %q, want rename", moves[0].Action)
	}
	if moves[0].NewName != "counts_v2.h5" {
		t.Errorf("rename = %q, want counts_v2.h5", moves[0].NewName)
	}
}

func TestMv_Move(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	_, stderr, code := env.run("mv", "abc12:/data/counts.h5", "abc12:/processed/counts.h5")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	moves := env.srv.Moves()
	if len(moves) == 0 {
		t.Fatal("expected a move action")
	}
	if moves[0].Action != "move" {
		t.Errorf("action = %q, want move", moves[0].Action)
	}
	if moves[0].DestPath != "/processed" {
		t.Errorf("path = %q, want /processed", moves[0].DestPath)
	}
}

func TestMv_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	_, _, code := env.run("mv", "abc12:/data/counts.h5", "abc12:/processed/counts.h5", "--dry-run")
	if code != 0 {
		t.Fatalf("unexpected exit %d", code)
	}
	if len(env.srv.Moves()) != 0 {
		t.Error("dry-run should not move any files")
	}
}

func TestMv_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	stdout, _, code := env.run("mv", "abc12:/data/counts.h5", "abc12:/processed/counts.h5", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["src"] == nil || result["dest"] == nil {
		t.Errorf("expected src and dest in result: %v", result)
	}
}

// ---- Cp ----

func TestCp_Basic(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	_, stderr, code := env.run("cp", "abc12:/data/counts.h5", "abc12:/backup/counts.h5")
	if code != 0 {
		t.Fatalf("exit %d; stderr=%s", code, stderr)
	}
	moves := env.srv.Moves()
	if len(moves) == 0 {
		t.Fatal("expected a copy action")
	}
	if moves[0].Action != "copy" {
		t.Errorf("action = %q, want copy", moves[0].Action)
	}
}

func TestCp_DryRun(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	_, _, code := env.run("cp", "abc12:/data/counts.h5", "abc12:/backup/counts.h5", "--dry-run")
	if code != 0 {
		t.Fatalf("unexpected exit %d", code)
	}
	if len(env.srv.Moves()) != 0 {
		t.Error("dry-run should not copy any files")
	}
}

func TestCp_JSON(t *testing.T) {
	env := newTestEnv(t)
	env.srv.AddProject("abc12", "Test Project")
	env.srv.AddFile("abc12", "/data/counts.h5", []byte("data"))

	stdout, _, code := env.run("cp", "abc12:/data/counts.h5", "abc12:/backup/counts.h5", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d; stdout=%s", code, stdout)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if result["src"] == nil || result["dest"] == nil {
		t.Errorf("expected src and dest in result: %v", result)
	}
}
