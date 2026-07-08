package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/output"
)

// isInteractive reports whether stdin is a terminal, i.e. whether a
// confirmation prompt can actually be answered.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// printPushPlan writes the rich, per-file push description used by the
// confirmation gate: a header naming the project title and visibility (with a
// loud warning when the target is public), one line per file, and a summary.
func printPushPlan(w io.Writer, node *client.Node, projectID string, plans []entryPlan, states []manifest.FileState) {
	title := projectID
	visibility := "VISIBILITY UNKNOWN"
	public := false
	if node != nil {
		if node.Attributes.Title != "" {
			title = node.Attributes.Title
		}
		public = node.Attributes.Public
		if public {
			visibility = "PUBLIC"
		} else {
			visibility = "PRIVATE"
		}
	}

	fmt.Fprintf(w, "Pushing %d file(s) to %q (%s, %s)\n", len(plans), title, projectID, visibility)
	if public {
		fmt.Fprintf(w, "  ⚠  WARNING: this project is PUBLIC — pushed files are visible to everyone.\n")
	}
	for i := range plans {
		fmt.Fprintln(w, pushItemLine(plans[i], states[i]))
	}
	fmt.Fprintf(w, "Summary: %s\n", summarizePush(states))
}

// pushItemLine renders one file's line in the push plan.
func pushItemLine(p entryPlan, state manifest.FileState) string {
	label := pushActionLabel(state)
	target := p.proj + ":" + p.entry.Remote
	var size int64 = -1
	if fi, err := os.Stat(p.localAbs); err == nil {
		size = fi.Size()
	}
	sizeStr := "unknown size"
	if size >= 0 {
		sizeStr = output.FormatSize(size)
	}
	latest := latestRemoteVersionInfo(p.remoteVersions)

	var detail string
	switch state {
	case manifest.StateNotPushed:
		detail = fmt.Sprintf("%s, md5 %s", sizeStr, shortMD5(p.localMD5))
	case manifest.StateAheadOfManifest:
		detail = fmt.Sprintf("v%d→v%d, %s, md5 %s→%s", p.entry.Version, p.entry.Version+1, sizeStr, shortMD5(p.entry.MD5), shortMD5(p.localMD5))
	case manifest.StatePinOnly, manifest.StateInSync:
		detail = fmt.Sprintf("identical to remote v%d", latest.Version)
	case manifest.StateRemoteNewer, manifest.StateBehind:
		detail = fmt.Sprintf("would roll back over remote v%d", latest.Version)
	case manifest.StateDivergent:
		detail = fmt.Sprintf("local md5 %s vs remote v%d md5 %s", shortMD5(p.localMD5), latest.Version, shortMD5(latest.MD5))
	case manifest.StateMissing:
		detail = "missing locally"
	}
	return fmt.Sprintf("  %-9s %s → %s  (%s)", label, p.entry.Local, target, detail)
}
