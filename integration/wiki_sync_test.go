//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestWikiSync_PushLifecycle creates a wiki page from a local file via the
// manifest, alongside a file, and confirms status settles to in-sync.
func TestWikiSync_PushLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.run("init", "abc12")

	// Track a brand-new wiki page and a file.
	e.writeFile("docs/home.md", "# Home\ninitial\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home")
	e.writeFile("data/x.csv", "a,b\n1,2\n")
	e.run("add", "data/x.csv", "abc12:/x.csv")

	// First sync: creates the wiki page and pushes the file.
	stdout, stderr, code := e.run("sync")
	if code != 0 {
		t.Fatalf("sync exit %d, stderr: %s\n%s", code, stderr, stdout)
	}
	p := e.srv.GetWiki("abc12", "home")
	if p == nil || string(p.LatestContent()) != "# Home\ninitial" {
		t.Fatalf("wiki not created by sync: %v", p)
	}

	// Status is now fully in sync (exit 0).
	_, _, code = e.run("status")
	if code != 0 {
		t.Errorf("status after sync should be in sync (exit 0), got %d", code)
	}

	// A second sync is a no-op (idempotent, no redundant version).
	_, _, code = e.run("sync")
	if code != 0 {
		t.Errorf("idempotent second sync exit %d", code)
	}
	if v := e.srv.GetWiki("abc12", "home").LatestVersion(); v != 1 {
		t.Errorf("no new version should be minted, got v%d", v)
	}
}

// TestWikiSync_PullFastForward tracks an existing remote wiki and confirms sync
// fast-forwards the local file after a web edit.
func TestWikiSync_PullFastForward(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.srv.AddWiki("abc12", "guide", []byte("guide v1\n"))
	e.run("init", "abc12")

	// Track the remote page — add pins v1, and the local file already matches.
	e.writeFile("docs/guide.md", "guide v1\n")
	e.run("wiki", "add", "docs/guide.md", "abc12:guide")

	_, _, code := e.run("status")
	if code != 0 {
		t.Fatalf("status after add should be in sync, got %d", code)
	}

	// Someone edits the wiki on the web → a new remote version.
	e.srv.AddWikiVersion("abc12", "guide", []byte("guide v2 from web\n"))

	// Sync fast-forwards the local file to the new remote content.
	_, stderr, code := e.run("sync")
	if code != 0 {
		t.Fatalf("sync exit %d, stderr: %s", code, stderr)
	}
	if got := e.readFile("docs/guide.md"); got != "guide v2 from web" {
		t.Errorf("local not fast-forwarded: %q", got)
	}
	_, _, code = e.run("status")
	if code != 0 {
		t.Errorf("status should be in sync after fast-forward, got %d", code)
	}
}

// A page whose remote moved ahead while local stayed at the pinned baseline is
// a safe fast-forward, whatever the entry was once created for. Before #81 the
// same setup hit a "would roll the remote back" refusal, because a page tracked
// for pushing was never routed to the pull handler.
func TestWikiSync_RemoteNewerFastForwards(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.run("init", "abc12")

	e.writeFile("docs/home.md", "v1\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home")
	e.run("sync") // creates v1, pins

	// Remote advances to v2 (web edit); local stays at pinned v1.
	e.srv.AddWikiVersion("abc12", "home", []byte("v2 web\n"))

	_, stderr, code := e.run("sync")
	if code != 0 {
		t.Fatalf("sync should fast-forward, not refuse; exit %d stderr=%s", code, stderr)
	}
	if got := e.readFile("docs/home.md"); got != "v2 web" {
		t.Errorf("local not fast-forwarded: %q", got)
	}
	if v := e.srv.GetWiki("abc12", "home").LatestVersion(); v != 2 {
		t.Errorf("no new remote version should be minted, got v%d", v)
	}
	if _, _, code = e.run("status"); code != 0 {
		t.Errorf("status should be in sync after fast-forward, got %d", code)
	}
}

func TestWikiSync_DivergenceBlocksAllTransfers(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.run("init", "abc12")

	// Pin at v1, then diverge both sides.
	e.writeFile("docs/home.md", "v1\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home")
	e.run("sync")
	e.srv.AddWikiVersion("abc12", "home", []byte("remote edit\n")) // remote moved
	e.writeFile("docs/home.md", "local edit\n")                    // local moved

	// Also stage a clean file push to prove the pre-flight blocks everything.
	e.writeFile("data/y.csv", "new\n")
	e.run("add", "data/y.csv", "abc12:/y.csv")

	_, stderr, code := e.run("sync")
	if code == 0 || !strings.Contains(stderr, "divergence") {
		t.Fatalf("expected divergence failure, code=%d stderr=%s", code, stderr)
	}
	// The clean file must NOT have been pushed (pre-flight fails before transfers).
	if e.srv.GetFile("abc12", "/y.csv") != nil {
		t.Error("pre-flight should block the clean file push when a wiki diverged")
	}

	// Resolve taking local: pushes local content as a new remote version.
	_, stderr, code = e.run("sync", "--resolve=ours")
	if code != 0 {
		t.Fatalf("resolve=ours exit %d, stderr: %s", code, stderr)
	}
	if got := string(e.srv.GetWiki("abc12", "home").LatestContent()); got != "local edit" {
		t.Errorf("remote content after resolve=ours = %q", got)
	}
}

func TestWikiSync_JSONKind(t *testing.T) {
	e := newTestEnv(t)
	e.srv.AddProject("abc12", "Wiki Project")
	e.run("init", "abc12")
	e.writeFile("docs/home.md", "hi\n")
	e.run("wiki", "add", "docs/home.md", "abc12:home")

	stdout, stderr, code := e.run("sync", "--output=json")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"kind": "wiki"`) {
		t.Errorf("sync JSON missing wiki kind:\n%s", stdout)
	}
}
