package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/BU-Neuromics/gosf/internal/manifest"
)

// These paths return before any network client is dereferenced, so nil clients
// are safe and let us test the gate decisions in isolation.

func TestProcessPushEntry_PinOnly_RecordsPinNoTransfer(t *testing.T) {
	entry := &manifest.Entry{Local: "a.csv", Remote: "/a.csv", Direction: "push", Version: 0}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "aaa"}, {Version: 1, MD5: "bbb"}}
	action, changed, err := processPushEntry(context.Background(), entry, "abc12", "/tmp/a.csv", "aaa",
		manifest.StatePinOnly, nil, nil, nil, false, false, false, "", versions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed || action != "pinned" {
		t.Errorf("action=%q changed=%v, want pinned/true", action, changed)
	}
	if entry.Version != 2 || entry.MD5 != "aaa" {
		t.Errorf("entry pinned to v%d/%s, want v2/aaa", entry.Version, entry.MD5)
	}
}

func TestProcessPushEntry_RollbackRefusedWithoutForce(t *testing.T) {
	entry := &manifest.Entry{Local: "a.csv", Remote: "/a.csv", Direction: "push", Version: 1, MD5: "old"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "new"}, {Version: 1, MD5: "old"}}
	_, changed, err := processPushEntry(context.Background(), entry, "abc12", "/tmp/a.csv", "old",
		manifest.StateRemoteNewer, nil, nil, nil, false, false, false, "", versions)
	if err == nil {
		t.Fatal("expected a rollback refusal without --force")
	}
	if changed {
		t.Error("manifest must not change on a refused rollback")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should point to --force: %v", err)
	}
}

func TestProcessPushEntry_DivergedFailsHard(t *testing.T) {
	entry := &manifest.Entry{Local: "a.csv", Remote: "/a.csv", Direction: "push", Version: 1, MD5: "B"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "R"}, {Version: 1, MD5: "B"}}
	_, _, err := processPushEntry(context.Background(), entry, "abc12", "/tmp/a.csv", "L",
		manifest.StateDivergent, nil, nil, nil, false, false, false, "", versions)
	if err == nil || !strings.Contains(err.Error(), "divergence") {
		t.Fatalf("expected a divergence error, got %v", err)
	}
}

func TestProcessPullEntry_PinOnly_RecordsPinNoTransfer(t *testing.T) {
	entry := &manifest.Entry{Local: "a.csv", Remote: "/a.csv", Direction: "pull", Version: 0}
	versions := []manifest.RemoteVersion{{Version: 3, MD5: "ccc"}}
	action, changed, err := processPullEntry(context.Background(), entry, "abc12", "/tmp/a.csv", "ccc",
		manifest.StatePinOnly, nil, nil, nil, "", false, false, false, "", versions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed || action != "pinned" {
		t.Errorf("action=%q changed=%v, want pinned/true", action, changed)
	}
	if entry.Version != 3 || entry.MD5 != "ccc" {
		t.Errorf("entry pinned to v%d/%s, want v3/ccc", entry.Version, entry.MD5)
	}
}

func TestProcessPullEntry_DivergedFailsHardWithoutResolve(t *testing.T) {
	entry := &manifest.Entry{Local: "a.csv", Remote: "/a.csv", Direction: "pull", Version: 1, MD5: "B"}
	versions := []manifest.RemoteVersion{{Version: 2, MD5: "R"}, {Version: 1, MD5: "B"}}
	_, _, err := processPullEntry(context.Background(), entry, "abc12", "/tmp/a.csv", "L",
		manifest.StateDivergent, nil, nil, nil, "", false, false, false, "", versions)
	if err == nil || !strings.Contains(err.Error(), "divergence") {
		t.Fatalf("expected a divergence error, got %v", err)
	}
}
