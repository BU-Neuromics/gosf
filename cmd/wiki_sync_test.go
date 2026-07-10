package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/testutil/fakeosf"
)

// --- no-transfer / refusal paths: nil client is safe ---

func TestProcessWikiPush_PinOnly(t *testing.T) {
	we := &manifest.WikiEntry{Local: "docs/home.md", Page: "home", Direction: "push"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "aaa"}}
	action, changed, err := processWikiPushEntry(context.Background(), nil, we, "abc12", "/tmp/x", "aaa",
		manifest.StatePinOnly, nil, false, false, "", versions)
	if err != nil || !changed || action != "pinned" {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
	if we.Version != 2 || we.MD5 != "aaa" {
		t.Errorf("pinned to v%d/%s", we.Version, we.MD5)
	}
}

func TestProcessWikiPush_RollbackRefusedWithoutForce(t *testing.T) {
	we := &manifest.WikiEntry{Local: "docs/home.md", Page: "home", Direction: "push", Version: 1, MD5: "old"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "new"}, {Version: 1, MD5: "old"}}
	_, changed, err := processWikiPushEntry(context.Background(), nil, we, "abc12", "/tmp/x", "old",
		manifest.StateRemoteNewer, nil, false, false, "", versions)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected rollback refusal, got %v", err)
	}
	if changed {
		t.Error("manifest must not change on refused rollback")
	}
}

func TestProcessWikiPush_DivergedFailsHard(t *testing.T) {
	we := &manifest.WikiEntry{Local: "docs/home.md", Page: "home", Direction: "push", Version: 1, MD5: "B"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "R"}, {Version: 1, MD5: "B"}}
	_, _, err := processWikiPushEntry(context.Background(), nil, we, "abc12", "/tmp/x", "L",
		manifest.StateDivergent, nil, false, false, "", versions)
	if err == nil || !strings.Contains(err.Error(), "divergence") {
		t.Fatalf("expected divergence error, got %v", err)
	}
}

func TestProcessWikiPull_DivergedFailsHard(t *testing.T) {
	we := &manifest.WikiEntry{Local: "docs/home.md", Page: "home", Direction: "pull", Version: 1, MD5: "B"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "R"}, {Version: 1, MD5: "B"}}
	_, _, err := processWikiPullEntry(context.Background(), nil, we, "abc12", "/tmp/x", "L",
		manifest.StateDivergent, nil, false, false, "", versions)
	if err == nil || !strings.Contains(err.Error(), "divergence") {
		t.Fatalf("expected divergence error, got %v", err)
	}
}

func TestProcessWikiPull_AheadSkippedWithoutForce(t *testing.T) {
	we := &manifest.WikiEntry{Local: "docs/home.md", Page: "home", Direction: "pull", Version: 1, MD5: "B"}
	versions := []manifest.RemoteVersion{{Version: 1, MD5: "B"}}
	action, changed, err := processWikiPullEntry(context.Background(), nil, we, "abc12", "/tmp/x", "L",
		manifest.StateAheadOfManifest, nil, false, false, "", versions)
	if err != nil || changed || !strings.HasPrefix(action, "skipped") {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
}

// --- transfer paths against fakeosf ---

func wikiSyncEnv(t *testing.T) (*fakeosf.Server, *client.OSFClient, string) {
	t.Helper()
	srv := fakeosf.New()
	t.Cleanup(srv.Close)
	t.Setenv("GOSF_API_BASE", srv.URL()+"/v2")
	dir := t.TempDir()
	return srv, client.New("tok"), dir
}

func TestProcessWikiPush_CreatesPage(t *testing.T) {
	srv, c, dir := wikiSyncEnv(t)
	srv.AddProject("abc12", "P")

	local := filepath.Join(dir, "home.md")
	if err := writeFileHelper(local, "new content\n"); err != nil {
		t.Fatal(err)
	}
	we := &manifest.WikiEntry{Local: "home.md", Page: "home", Direction: "push"}
	action, changed, err := processWikiPushEntry(context.Background(), c, we, "abc12", local, md5of("new content\n"),
		manifest.StateNotPushed, nil, false, false, "", nil)
	if err != nil || !changed {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
	if p := srv.GetWiki("abc12", "home"); p == nil || string(p.LatestContent()) != "new content\n" {
		t.Errorf("page not created with expected content")
	}
	if we.Version != 1 || we.MD5 != md5of("new content\n") {
		t.Errorf("entry pinned to v%d/%s", we.Version, we.MD5)
	}
}

func TestProcessWikiPush_NewVersion(t *testing.T) {
	srv, c, dir := wikiSyncEnv(t)
	srv.AddProject("abc12", "P")
	page := srv.AddWiki("abc12", "home", []byte("v1\n"))

	local := filepath.Join(dir, "home.md")
	writeFileHelper(local, "v2\n")
	wiki := &client.Wiki{ID: page.ID, Attributes: client.WikiAttributes{Name: "home", Extra: client.WikiExtra{Version: 1}}}
	we := &manifest.WikiEntry{Local: "home.md", Page: "home", Direction: "push", Version: 1, MD5: md5of("v1\n")}
	action, changed, err := processWikiPushEntry(context.Background(), c, we, "abc12", local, md5of("v2\n"),
		manifest.StateAheadOfManifest, wiki, false, false, "", []manifest.RemoteVersion{{Version: 1, MD5: md5of("v1\n")}})
	if err != nil || !changed {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
	if we.Version != 2 || we.MD5 != md5of("v2\n") {
		t.Errorf("entry pinned to v%d/%s", we.Version, we.MD5)
	}
	if v := srv.GetWiki("abc12", "home").LatestVersion(); v != 2 {
		t.Errorf("server version = %d", v)
	}
}

func TestProcessWikiPull_FastForward(t *testing.T) {
	srv, c, dir := wikiSyncEnv(t)
	srv.AddProject("abc12", "P")
	page := srv.AddWiki("abc12", "home", []byte("v1\n"))
	srv.AddWikiVersion("abc12", "home", []byte("v2 remote\n"))

	local := filepath.Join(dir, "home.md")
	writeFileHelper(local, "v1\n")
	wiki := &client.Wiki{ID: page.ID, Attributes: client.WikiAttributes{Name: "home", Extra: client.WikiExtra{Version: 2}}}
	we := &manifest.WikiEntry{Local: "home.md", Page: "home", Direction: "pull", Version: 1, MD5: md5of("v1\n")}
	action, changed, err := processWikiPullEntry(context.Background(), c, we, "abc12", local, md5of("v1\n"),
		manifest.StateRemoteNewer, wiki, false, false, "",
		[]manifest.RemoteVersion{{Version: 2, MD5: md5of("v2 remote\n")}})
	if err != nil || !changed {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
	if got := readFileHelper(t, local); got != "v2 remote\n" {
		t.Errorf("local content = %q", got)
	}
	if we.Version != 2 || we.MD5 != md5of("v2 remote\n") {
		t.Errorf("entry pinned to v%d/%s", we.Version, we.MD5)
	}
}

func TestProcessWikiPull_Missing_WritesFile(t *testing.T) {
	srv, c, dir := wikiSyncEnv(t)
	srv.AddProject("abc12", "P")
	page := srv.AddWiki("abc12", "home", []byte("only version\n"))

	local := filepath.Join(dir, "sub", "home.md") // does not exist yet
	wiki := &client.Wiki{ID: page.ID, Attributes: client.WikiAttributes{Name: "home", Extra: client.WikiExtra{Version: 1}}}
	we := &manifest.WikiEntry{Local: "sub/home.md", Page: "home", Direction: "pull"}
	action, changed, err := processWikiPullEntry(context.Background(), c, we, "abc12", local, "",
		manifest.StateMissing, wiki, false, false, "",
		[]manifest.RemoteVersion{{Version: 1, MD5: md5of("only version\n")}})
	if err != nil || !changed {
		t.Fatalf("action=%q changed=%v err=%v", action, changed, err)
	}
	if got := readFileHelper(t, local); got != "only version\n" {
		t.Errorf("local content = %q", got)
	}
}

func TestProcessWikiPush_DryRunNoWrite(t *testing.T) {
	srv, c, dir := wikiSyncEnv(t)
	srv.AddProject("abc12", "P")
	local := filepath.Join(dir, "home.md")
	writeFileHelper(local, "x\n")
	we := &manifest.WikiEntry{Local: "home.md", Page: "home", Direction: "push"}
	_, changed, err := processWikiPushEntry(context.Background(), c, we, "abc12", local, md5of("x\n"),
		manifest.StateNotPushed, nil, true /*dryRun*/, false, "", nil)
	if err != nil || changed {
		t.Fatalf("dry-run: changed=%v err=%v", changed, err)
	}
	if srv.GetWiki("abc12", "home") != nil {
		t.Error("dry-run must not create the page")
	}
}
