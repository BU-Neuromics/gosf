//go:build live

package live

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// uniqueWikiPage returns a namespaced page name and registers cleanup that
// deletes it after the test (best effort), so live runs leave no residue.
func uniqueWikiPage(e *liveEnv) string {
	page := fmt.Sprintf("gosf-ci-%d-%d", time.Now().UnixNano(), os.Getpid())
	e.t.Cleanup(func() {
		e.runEventually("wiki", "rm", e.project+":"+page, "--yes", "--quiet")
	})
	return page
}

// TestLive_WikiRoundTripFidelity is the load-bearing live check: it validates
// the epic's byte-exact hashing assumption by pushing content with mixed line
// endings and a missing trailing newline, then reading it back and comparing
// bytes. If real OSF normalizes wiki content, this fails loudly (rather than the
// mismatch surfacing later as a spurious DIVERGED during sync).
func TestLive_WikiRoundTripFidelity(t *testing.T) {
	e := requireLive(t)
	page := uniqueWikiPage(e)

	content := "# Heading\r\nCRLF line\nLF line\n\ntrailing spaces   \nno final newline"
	e.writeFile("page.md", content)

	if _, stderr, code := e.runEventually("wiki", "push", "page.md", e.project+":"+page, "--quiet"); code != 0 {
		t.Fatalf("wiki push exit %d; stderr=%s", code, stderr)
	}

	var got string
	for i := 0; i < 40; i++ {
		out, _, code := e.run("wiki", "get", e.project+":"+page)
		if code == 0 {
			got = out
			break
		}
	}
	if got != content {
		t.Errorf("wiki content not byte-exact after round trip:\n got  %q\n want %q", got, content)
	}
}

// TestLive_WikiCreateVersionRenameDelete exercises the full write lifecycle.
func TestLive_WikiCreateVersionRenameDelete(t *testing.T) {
	e := requireLive(t)
	page := uniqueWikiPage(e)

	// Create.
	e.writeFile("p.md", "first\n")
	if _, stderr, code := e.runEventually("wiki", "push", "p.md", e.project+":"+page, "--quiet"); code != 0 {
		t.Fatalf("create exit %d; stderr=%s", code, stderr)
	}

	// New version.
	e.writeFile("p.md", "second\n")
	if _, stderr, code := e.runEventually("wiki", "push", "p.md", e.project+":"+page, "--quiet"); code != 0 {
		t.Fatalf("new version exit %d; stderr=%s", code, stderr)
	}

	// Versions reflect two entries.
	var nums []int
	for i := 0; i < 40; i++ {
		out, _, code := e.run("wiki", "versions", e.project+":"+page, "--output=json")
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
	if len(nums) < 2 || nums[0] != 2 {
		t.Fatalf("expected >=2 versions newest-first, got %v", nums)
	}

	// Rename, then register cleanup for the new name too.
	renamed := page + "-renamed"
	e.t.Cleanup(func() {
		e.runEventually("wiki", "rm", e.project+":"+renamed, "--yes", "--quiet")
	})
	if _, stderr, code := e.runEventually("wiki", "mv", e.project+":"+page, renamed, "--quiet"); code != 0 {
		t.Fatalf("rename exit %d; stderr=%s", code, stderr)
	}

	// Delete.
	if _, stderr, code := e.runEventually("wiki", "rm", e.project+":"+renamed, "--yes", "--quiet"); code != 0 {
		t.Fatalf("delete exit %d; stderr=%s", code, stderr)
	}
}

// TestLive_WikiIdempotentPush confirms a re-push of identical content mints no
// new version against real OSF.
func TestLive_WikiIdempotentPush(t *testing.T) {
	e := requireLive(t)
	page := uniqueWikiPage(e)

	e.writeFile("p.md", "stable content\n")
	if _, stderr, code := e.runEventually("wiki", "push", "p.md", e.project+":"+page, "--quiet"); code != 0 {
		t.Fatalf("create exit %d; stderr=%s", code, stderr)
	}

	out, stderr, code := e.runEventually("wiki", "push", "p.md", e.project+":"+page, "--output=json")
	if code != 0 {
		t.Fatalf("idempotent push exit %d; stderr=%s", code, stderr)
	}
	var r struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("push json: %v\n%s", err, out)
	}
	if r.Action != "skip" {
		t.Errorf("identical re-push action = %q, want skip", r.Action)
	}
}
