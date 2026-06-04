package manifest

// FileState is the classification of a tracked file's sync status.
type FileState int

const (
	StateInSync          FileState = iota // local MD5 matches declared version
	StateMissing                          // local file does not exist
	StateBehind                           // local MD5 matches an older remote version
	StateAheadOfManifest                  // local MD5 doesn't match any remote version
	StateRemoteNewer                      // in sync with manifest but remote has newer versions
	StateNotPushed                        // version = 0
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
	default:
		return "UNKNOWN"
	}
}

// RemoteVersion holds the version number and MD5 hash of one remote version.
type RemoteVersion struct {
	Version int
	MD5     string
}

// ClassifyFile determines the sync state of a file.
//
//   - localMD5: MD5 of the local file, or "" if the file does not exist.
//   - remoteVersions: all known remote versions with their MD5s, sorted
//     newest-first. Nil or empty when noCheckRemote is true or no versions
//     have been fetched.
//   - noCheckRemote: when true, only IN_SYNC, MISSING, AHEAD_OF_MANIFEST,
//     and NOT_PUSHED can be returned (BEHIND and REMOTE_NEWER require network).
func ClassifyFile(entry Entry, localMD5 string, remoteVersions []RemoteVersion, noCheckRemote bool) FileState {
	// NOT_PUSHED takes priority: version=0 means the file has never been synced.
	if entry.Version == 0 {
		return StateNotPushed
	}

	// MISSING: local file does not exist.
	if localMD5 == "" {
		return StateMissing
	}

	// IN_SYNC: local MD5 matches the declared (pinned) version.
	if localMD5 == entry.MD5 {
		if noCheckRemote {
			return StateInSync
		}
		// Check whether remote has versions beyond the declared version.
		if hasNewerVersion(entry.Version, remoteVersions) {
			return StateRemoteNewer
		}
		return StateInSync
	}

	// From here, local MD5 != declared MD5.
	if noCheckRemote {
		return StateAheadOfManifest
	}

	// BEHIND: local MD5 matches a remote version that is older than declared.
	for _, rv := range remoteVersions {
		if rv.MD5 == localMD5 && rv.Version < entry.Version {
			return StateBehind
		}
	}

	// AHEAD_OF_MANIFEST: local MD5 doesn't match any remote version.
	return StateAheadOfManifest
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
