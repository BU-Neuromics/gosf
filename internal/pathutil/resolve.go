package pathutil

import (
	"path"
	"path/filepath"
	"strings"
)

// FileRemotePath returns the OSF remote path when pushing or adding a single local file.
// If remoteDest is empty: mirrors the full local path (e.g. "data/file.txt" → "/data/file.txt").
// If remoteDest ends with "/": destination is a directory; source filename is preserved.
// Otherwise: remoteDest is the exact destination path.
func FileRemotePath(localSrc, remoteDest string) string {
	if remoteDest == "" {
		return "/" + strings.TrimLeft(filepath.ToSlash(localSrc), "/")
	}
	destIsDir := strings.HasSuffix(remoteDest, "/")
	normalized := "/" + strings.Trim(remoteDest, "/")
	if destIsDir {
		return normalized + "/" + filepath.Base(localSrc)
	}
	return normalized
}

// FileLocalPath returns the local filesystem path when pulling a single remote file.
// If localDest is empty or ".": mirrors the remote path (strips leading "/").
// If localDest ends with "/": destination is a directory; remote filename is preserved.
// Otherwise: localDest is the exact destination path.
func FileLocalPath(remoteSrc, localDest string) string {
	if localDest == "" || localDest == "." {
		return strings.TrimLeft(remoteSrc, "/")
	}
	if strings.HasSuffix(localDest, "/") {
		return strings.TrimRight(localDest, "/") + "/" + path.Base(remoteSrc)
	}
	return localDest
}

// PushDirBases returns (localBase, remoteBase) for a directory push or add.
// MapFilePath(localBase, remoteBase, file) gives the remote path for each file.
//
// If srcTrailingSlash: directory contents are copied (dir name stripped from dest).
// Otherwise: the directory itself is copied (dir name preserved in dest).
// If remoteDest is empty: full local path is mirrored as remote path.
func PushDirBases(localSrc, remoteDest string, srcTrailingSlash bool) (localBase, remoteBase string) {
	localSrc = filepath.ToSlash(strings.TrimRight(localSrc, "/"))

	if remoteDest == "" {
		return "", "/"
	}

	remoteBase = "/" + strings.Trim(remoteDest, "/") + "/"

	if srcTrailingSlash {
		localBase = localSrc + "/"
	} else {
		parent := path.Dir(localSrc)
		if parent == "." || parent == "" {
			localBase = ""
		} else {
			localBase = parent + "/"
		}
	}
	return
}

// PullDirBases returns (remoteBase, localBase) for a directory pull.
// MapFilePath(remoteBase, localBase, file) gives the local path for each remote file.
//
// If srcTrailingSlash: directory contents are copied (dir name stripped from dest).
// Otherwise: the directory itself is copied (dir name preserved in dest).
// If localDest is empty or ".": files land in the current directory.
func PullDirBases(remoteSrc, localDest string, srcTrailingSlash bool) (remoteBase, localBase string) {
	remoteSrc = strings.TrimRight(remoteSrc, "/")
	if !strings.HasPrefix(remoteSrc, "/") {
		remoteSrc = "/" + remoteSrc
	}

	if localDest == "" || localDest == "." {
		localBase = "./"
	} else {
		localBase = strings.TrimRight(localDest, "/") + "/"
	}

	if srcTrailingSlash {
		remoteBase = remoteSrc + "/"
	} else {
		parent := path.Dir(remoteSrc)
		if parent == "/" || parent == "" || parent == "." {
			remoteBase = "/"
		} else {
			remoteBase = parent + "/"
		}
	}
	return
}

// MapFilePath maps a single file path from the source domain to the destination domain.
// srcBase is the prefix stripped from filePath; destBase is prepended to the result.
// The returned path is slash-separated; callers convert to OS path separators as needed.
func MapFilePath(srcBase, destBase, filePath string) string {
	filePath = filepath.ToSlash(filePath)
	srcBase = filepath.ToSlash(srcBase)

	rel := filePath
	if srcBase != "" {
		rel = strings.TrimPrefix(filePath, srcBase)
	}

	result := path.Join(destBase, rel)
	// path.Join cleans "./" prefixes; restore it when destBase was "./"
	if strings.HasPrefix(destBase, "./") && !strings.HasPrefix(result, "/") {
		// result is already relative; fine as-is
	}
	return result
}
