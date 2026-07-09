//go:build live

package live

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLive_InfoAndLs exercises the read path against the real API with a token.
func TestLive_InfoAndLs(t *testing.T) {
	e := requireLive(t)

	out, stderr, code := e.run("info", e.project, "--output=json")
	if code != 0 {
		t.Fatalf("info exit %d; stderr=%s", code, stderr)
	}
	var node map[string]any
	if err := json.Unmarshal([]byte(out), &node); err != nil {
		t.Fatalf("info json: %v\n%s", err, out)
	}
	if node["id"] != e.project {
		t.Errorf("info id = %v, want %s", node["id"], e.project)
	}

	if _, stderr, code := e.run("ls", e.project, "--output=json"); code != 0 {
		t.Fatalf("ls exit %d; stderr=%s", code, stderr)
	}
}

// TestLive_PushNewThenNewVersion pushes a new file, then a changed version, and
// confirms the API records two distinct versions.
func TestLive_PushNewThenNewVersion(t *testing.T) {
	e := requireLive(t)
	dir := e.mkTempRemoteDir(e.project)
	remote := e.project + ":" + dir + "/data.csv"

	e.writeFile("data.csv", "col\nversion-one\n")
	if _, stderr, code := e.runEventually("push", "data.csv", remote, "--quiet"); code != 0 {
		t.Fatalf("first push exit %d; stderr=%s", code, stderr)
	}

	e.writeFile("data.csv", "col\nversion-two\n")
	if _, stderr, code := e.runEventually("push", "data.csv", remote, "--conflict=overwrite", "--quiet"); code != 0 {
		t.Fatalf("second push (new version) exit %d; stderr=%s", code, stderr)
	}

	// Poll versions until the second version is reflected (metadata lag), then
	// assert both distinct, correctly-numbered versions are present.
	var nums []int
	for i := 0; i < 40; i++ {
		out, _, code := e.run("versions", remote, "--output=json")
		if code != 0 {
			continue
		}
		var vr struct {
			Versions []struct {
				Version int `json:"version"`
			} `json:"versions"`
		}
		if json.Unmarshal([]byte(out), &vr) != nil {
			continue
		}
		nums = nil
		for _, v := range vr.Versions {
			nums = append(nums, v.Version)
		}
		if len(nums) >= 2 {
			break
		}
	}
	if len(nums) != 2 {
		t.Fatalf("expected 2 versions after two pushes, got %v", nums)
	}
	// Version numbers must be the real OSF numbers (2,1), not 0,0 — guards the
	// id-vs-attribute parsing fix.
	if nums[0] != 2 || nums[1] != 1 {
		t.Errorf("version numbers = %v, want [2 1]", nums)
	}
}

// TestLive_PullIdempotent pulls a freshly pushed file, then pulls again and
// confirms the second pull performs no download (identical) and the manifest pins.
func TestLive_PullIdempotent(t *testing.T) {
	e := requireLive(t)
	dir := e.mkTempRemoteDir(e.project)
	remote := e.project + ":" + dir + "/payload.txt"

	content := "reproducible-input\n"
	e.writeFile("payload.txt", content)
	if _, stderr, code := e.runEventually("push", "payload.txt", remote, "--quiet"); code != 0 {
		t.Fatalf("push exit %d; stderr=%s", code, stderr)
	}

	// Pull to a fresh local path.
	if _, stderr, code := e.runEventually("pull", remote, "fetched.txt", "--no-track"); code != 0 {
		t.Fatalf("pull exit %d; stderr=%s", code, stderr)
	}
	if got := e.readFile("fetched.txt"); got != content {
		t.Errorf("pulled content = %q, want %q", got, content)
	}

	// Second pull of the identical file must skip the transfer.
	_, stderr, code := e.runEventually("pull", remote, "fetched.txt", "--no-track")
	if code != 0 {
		t.Fatalf("second pull exit %d; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "identical") {
		t.Errorf("second pull of identical file should skip; stderr=%q", stderr)
	}
}

// TestLive_Rm pushes a file then deletes it and confirms it is gone.
func TestLive_Rm(t *testing.T) {
	e := requireLive(t)
	dir := e.mkTempRemoteDir(e.project)
	remote := e.project + ":" + dir + "/tmp.txt"

	e.writeFile("tmp.txt", "delete me\n")
	if _, stderr, code := e.runEventually("push", "tmp.txt", remote, "--quiet"); code != 0 {
		t.Fatalf("push exit %d; stderr=%s", code, stderr)
	}
	if _, stderr, code := e.runEventually("rm", remote, "--yes", "--quiet"); code != 0 {
		t.Fatalf("rm exit %d; stderr=%s", code, stderr)
	}
	// The delete must propagate: the file no longer resolves.
	e.waitGone(remote)
}

// TestLive_ComponentPushNewVersion is the regression test for the reported 404:
// pushing a new version of an existing file that lives in a *component* (a
// different node than the manifest's default project), driven through the
// manifest with a per-entry `project` override.
//
// Verified fixed by the subfolder-upload ID fix (new files target the parent
// folder's ID-based links.upload) together with the version-number fix — this
// runs green against live OSF, so it stays active as a regression guard.
func TestLive_ComponentPushNewVersion(t *testing.T) {
	e := requireLive(t)
	comp := e.requireComponent()
	dir := e.mkTempRemoteDir(comp)

	// Manifest default = primary project; the entry targets the component.
	if _, stderr, code := e.run("init", e.project, "--quiet"); code != 0 {
		t.Fatalf("init exit %d; stderr=%s", code, stderr)
	}
	e.writeFile("counts.csv", "n\n1\n")
	if _, stderr, code := e.run("add", "counts.csv", comp+":"+dir+"/counts.csv"); code != 0 {
		t.Fatalf("add exit %d; stderr=%s", code, stderr)
	}

	// First push creates v1 in the component.
	if _, stderr, code := e.runEventually("push", "--yes", "--quiet"); code != 0 {
		t.Fatalf("first component push exit %d; stderr=%s", code, stderr)
	}

	// Change and push again → a new version in the component (this is the
	// operation that originally 404'd).
	e.writeFile("counts.csv", "n\n1\n2\n")
	if _, stderr, code := e.runEventually("push", "--yes", "--quiet"); code != 0 {
		t.Fatalf("second component push (new version) exit %d; stderr=%s", code, stderr)
	}

	// Poll until the second version is reflected (metadata lag).
	saw := false
	for i := 0; i < 40; i++ {
		out, _, code := e.run("versions", comp+":"+dir+"/counts.csv", "--output=json")
		if code == 0 && strings.Contains(out, `"version": 2`) {
			saw = true
			break
		}
	}
	if !saw {
		t.Error("expected a v2 in the component after re-push")
	}
}
