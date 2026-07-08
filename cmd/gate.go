package cmd

import (
	"fmt"
	"strings"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/manifest"
)

// entryPlan is the classified, not-yet-executed reconciliation of one manifest
// entry: enough state to pre-flight (e.g. detect divergence across all entries)
// before any transfer runs.
type entryPlan struct {
	entry          *manifest.Entry
	proj           string
	localAbs       string
	localMD5       string
	state          manifest.FileState
	resolvedItem   *client.FileItem
	remoteVersions []manifest.RemoteVersion
	treatAsPush    bool
	treatAsPull    bool
}

// validateResolve checks a --resolve flag value.
func validateResolve(resolve string) error {
	switch resolve {
	case "", "ours", "theirs":
		return nil
	default:
		return fmt.Errorf("--resolve must be 'ours' or 'theirs', got %q", resolve)
	}
}

// latestRemoteVersionInfo returns the remote version with the highest version
// number, or a zero RemoteVersion when the list is empty.
func latestRemoteVersionInfo(remoteVersions []manifest.RemoteVersion) manifest.RemoteVersion {
	var latest manifest.RemoteVersion
	for _, rv := range remoteVersions {
		if rv.Version >= latest.Version {
			latest = rv
		}
	}
	return latest
}

// pushActionLabel returns the short verb describing what pushing an entry in the
// given state will do, for the confirmation display.
func pushActionLabel(state manifest.FileState) string {
	switch state {
	case manifest.StateNotPushed:
		return "new"
	case manifest.StateAheadOfManifest:
		return "update"
	case manifest.StatePinOnly:
		return "pin"
	case manifest.StateInSync:
		return "unchanged"
	case manifest.StateRemoteNewer, manifest.StateBehind:
		return "rollback"
	case manifest.StateDivergent:
		return "diverged"
	case manifest.StateMissing:
		return "missing"
	default:
		return "skip"
	}
}

// needsPushConfirmation reports whether any state will write bytes to the remote
// (a new file or a new version), warranting an interactive confirmation. Pins,
// unchanged, and skipped entries do not write remote data.
func needsPushConfirmation(states []manifest.FileState) bool {
	for _, s := range states {
		if s == manifest.StateNotPushed || s == manifest.StateAheadOfManifest ||
			s == manifest.StateRemoteNewer || s == manifest.StateBehind || s == manifest.StateDivergent {
			return true
		}
	}
	return false
}

// summarizePush renders a one-line tally of a push plan, e.g.
// "3 new, 2 updated, 3 unchanged (skipped)".
func summarizePush(states []manifest.FileState) string {
	var newN, updated, unchanged, rollback, missing int
	for _, s := range states {
		switch s {
		case manifest.StateNotPushed:
			newN++
		case manifest.StateAheadOfManifest:
			updated++
		case manifest.StatePinOnly, manifest.StateInSync:
			unchanged++
		case manifest.StateRemoteNewer, manifest.StateBehind:
			rollback++
		case manifest.StateMissing:
			missing++
		}
	}
	parts := []string{
		fmt.Sprintf("%d new", newN),
		fmt.Sprintf("%d updated", updated),
		fmt.Sprintf("%d unchanged (skipped)", unchanged),
	}
	if rollback > 0 {
		parts = append(parts, fmt.Sprintf("%d rollback", rollback))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", missing))
	}
	return strings.Join(parts, ", ")
}

// divergenceError builds the hard-failure diagnostic for an entry where both the
// local file and the remote have changed since the pinned baseline. It names the
// three states and gives copy-pasteable resolution commands, per epic #38.
func divergenceError(entry manifest.Entry, proj, localMD5 string, remoteVersions []manifest.RemoteVersion) error {
	latest := latestRemoteVersionInfo(remoteVersions)
	return fmt.Errorf(
		"divergence on %s — both local and remote changed since the pinned baseline\n"+
			"  baseline: v%d  md5 %s   (last synced)\n"+
			"  local:        md5 %s   (changed)\n"+
			"  remote:   v%d  md5 %s   (changed)\n"+
			"Both sides have unreconciled changes; refusing to overwrite either automatically.\n"+
			"Resolve explicitly:\n"+
			"  gosf pull  %s:%s --resolve=theirs   # take remote (discards local)\n"+
			"  gosf push  %s --resolve=ours     # take local  (discards remote v%d)",
		entry.Local,
		entry.Version, shortMD5(entry.MD5),
		shortMD5(localMD5),
		latest.Version, shortMD5(latest.MD5),
		proj, entry.Remote,
		entry.Local,
		latest.Version,
	)
}

// shortMD5 renders an MD5 for diagnostics, tolerating an empty value.
func shortMD5(md5 string) string {
	if md5 == "" {
		return "(none)"
	}
	return md5
}
