package manifest

// FileState is the classification of a tracked file's sync status.
type FileState int

const (
	StateInSync          FileState = iota // local MD5 matches declared version
	StateMissing                          // local file does not exist
	StateBehind                           // local MD5 matches an older remote version
	StateAheadOfManifest                  // local MD5 doesn't match any remote version
	StateRemoteNewer                      // in sync with manifest but remote has newer versions
	StateNotPushed                        // version = 0 and no remote to compare against
	StatePinOnly                          // local content == remote latest, but pin is stale/absent → record, no transfer
	StateDivergent                        // both local and remote changed from the baseline → unsafe to auto-transfer
)

func (s FileState) String() string {
	switch s {
	case StateInSync:
		return "IN_SYNC"
	case StateMissing:
		return "MISSING"
	case StateBehind:
		return "BEHIND"
	case StateAheadOfManifest:
		return "AHEAD_OF_MANIFEST"
	case StateRemoteNewer:
		return "REMOTE_NEWER"
	case StateNotPushed:
		return "NOT_PUSHED"
	case StatePinOnly:
		return "PIN_ONLY"
	case StateDivergent:
		return "DIVERGED"
	default:
		return "UNKNOWN"
	}
}

// RemoteVersion holds the version number and MD5 hash of one remote version.
type RemoteVersion struct {
	Version int
	MD5     string
}

// ClassifyFile determines the sync state of a file by comparing three values:
// local content (L), the pinned baseline (B = entry.Version + entry.MD5), and
// the latest remote version (R).
//
//   - localMD5: MD5 of the local file, or "" if the file does not exist.
//   - remoteVersions: all known remote versions with their MD5s, sorted
//     newest-first. Nil or empty when noCheckRemote is true or the remote path
//     does not exist / has not been fetched.
//   - noCheckRemote: when true only IN_SYNC, MISSING, AHEAD_OF_MANIFEST, and
//     NOT_PUSHED can be returned (the remote-comparing states need network).
//
// When the entry is unpinned (version == 0) but the remote path exists, the
// content is compared against the remote instead of blanket-reporting
// NOT_PUSHED — this is what lets "already in sync, just record it" be
// expressible (StatePinOnly) rather than forcing a redundant transfer.
func ClassifyFile(entry Entry, localMD5 string, remoteVersions []RemoteVersion, noCheckRemote bool) FileState {
	localExists := localMD5 != ""
	pinned := entry.Version > 0

	// Without a remote to compare against we can only reason about L vs B.
	if noCheckRemote {
		if !pinned {
			return StateNotPushed
		}
		if !localExists {
			return StateMissing
		}
		if localMD5 == entry.MD5 {
			return StateInSync
		}
		return StateAheadOfManifest
	}

	remoteExists := len(remoteVersions) > 0
	latest := latestVersion(remoteVersions)
	localMatchesRemoteLatest := remoteExists && localExists && localMD5 == latest.MD5

	// Unpinned entry (version == 0): reconcile purely by content against remote.
	if !pinned {
		if !remoteExists {
			return StateNotPushed
		}
		if !localExists {
			return StateMissing
		}
		if localMatchesRemoteLatest {
			return StatePinOnly
		}
		if matchesAnyRemote(localMD5, remoteVersions) {
			return StateBehind // local content is an older remote version
		}
		return StateAheadOfManifest // local content is not on the remote at all
	}

	// Pinned entry (version > 0).
	if !localExists {
		return StateMissing
	}

	remoteBeyondBaseline := hasNewerVersion(entry.Version, remoteVersions)

	// L == B: local still matches the pinned baseline.
	if localMD5 == entry.MD5 {
		if remoteBeyondBaseline {
			return StateRemoteNewer // R ≠ B, safe fast-forward for pull
		}
		return StateInSync
	}

	// L ≠ B from here.
	if localMatchesRemoteLatest {
		// Local content already equals remote HEAD; only the pin is stale.
		return StatePinOnly
	}
	if matchesAnyRemote(localMD5, remoteVersions) {
		// Local is a known (older) remote version — fast-forward-able, no
		// unique local work at risk.
		return StateBehind
	}
	if remoteBeyondBaseline {
		// Both sides moved off the baseline and local matches no remote
		// version → genuine divergence.
		return StateDivergent
	}
	// Only local moved (R == B) → a real local update / would clobber on pull.
	return StateAheadOfManifest
}

// latestVersion returns the remote version with the highest version number.
// Order-independent, so callers need not guarantee newest-first input.
func latestVersion(remoteVersions []RemoteVersion) RemoteVersion {
	var latest RemoteVersion
	for _, rv := range remoteVersions {
		if rv.Version >= latest.Version {
			latest = rv
		}
	}
	return latest
}

// matchesAnyRemote reports whether localMD5 equals the MD5 of any remote version.
func matchesAnyRemote(localMD5 string, remoteVersions []RemoteVersion) bool {
	if localMD5 == "" {
		return false
	}
	for _, rv := range remoteVersions {
		if rv.MD5 == localMD5 {
			return true
		}
	}
	return false
}

// hasNewerVersion reports whether any remote version has a version number
// greater than the declared version.
func hasNewerVersion(declared int, remoteVersions []RemoteVersion) bool {
	for _, rv := range remoteVersions {
		if rv.Version > declared {
			return true
		}
	}
	return false
}
