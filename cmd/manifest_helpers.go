package cmd

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/manifest"
)

// computeLocalMD5 returns the lowercase hex MD5 of the file at path.
// Returns "" (not an error) if the file does not exist.
func computeLocalMD5(path string) (string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("computing MD5 of %s: %w", path, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// fileVersionsToRemote converts client.FileVersion slice to manifest.RemoteVersion slice.
func fileVersionsToRemote(versions []client.FileVersion) []manifest.RemoteVersion {
	if versions == nil {
		return nil
	}
	out := make([]manifest.RemoteVersion, len(versions))
	for i, v := range versions {
		out[i] = manifest.RemoteVersion{
			Version: v.Attributes.Version,
			MD5:     v.Attributes.Extra.Hashes.MD5,
		}
	}
	return out
}

// latestRemoteVersion returns the highest version number in the slice, or 0 if empty.
func latestRemoteVersion(versions []manifest.RemoteVersion) int {
	max := 0
	for _, v := range versions {
		if v.Version > max {
			max = v.Version
		}
	}
	return max
}
