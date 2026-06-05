package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Manifest is the in-memory representation of gosf.toml.
type Manifest struct {
	Project ProjectConfig `toml:"project"`
	Files   []Entry       `toml:"files"`
}

// ProjectConfig holds the default project GUID.
type ProjectConfig struct {
	ID string `toml:"id"`
}

// Entry describes one file tracked by the manifest.
type Entry struct {
	Local     string `toml:"local"`
	Remote    string `toml:"remote"`
	Direction string `toml:"direction"`
	Version   int    `toml:"version"`
	MD5       string `toml:"md5"`
	Project   string `toml:"project,omitempty"`
}

// ResolveProject returns the project GUID for this entry: the entry's own
// Project field if set, otherwise the manifest's default project ID.
func (e Entry) ResolveProject(defaultID string) string {
	if e.Project != "" {
		return e.Project
	}
	return defaultID
}

// NotFoundError is returned by FindManifest when no gosf.toml is found.
type NotFoundError struct{}

func (NotFoundError) Error() string { return "gosf.toml not found in this directory or any parent" }

// IsNotFound reports whether err is a NotFoundError.
func IsNotFound(err error) bool {
	_, ok := err.(NotFoundError)
	return ok
}

// Load parses and validates gosf.toml at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Save writes the manifest to path atomically (temp file + rename).
func Save(m *Manifest, path string) error {
	data, err := toml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gosf.toml.tmp.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// FindManifest walks up from the current working directory until it finds
// gosf.toml. Returns (manifestPath, repoRoot, error).
// Returns NotFoundError if none is found.
func FindManifest() (string, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	for {
		candidate := filepath.Join(dir, "gosf.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", "", NotFoundError{}
		}
		dir = parent
	}
}

// validate checks all manifest invariants.
func validate(m *Manifest) error {
	seenLocal := make(map[string]bool)
	seenRemote := make(map[string]bool) // key: "project|remote"

	for i, f := range m.Files {
		// direction is required
		if f.Direction == "" {
			return fmt.Errorf("files[%d] (%q): direction is required", i, f.Local)
		}
		switch f.Direction {
		case "push", "pull", "both":
		default:
			return fmt.Errorf("files[%d] (%q): direction %q is invalid (must be push, pull, or both)",
				i, f.Local, f.Direction)
		}

		// Resolve project
		proj := f.ResolveProject(m.Project.ID)
		if proj == "" {
			return fmt.Errorf("files[%d] (%q): no project GUID — set [project].id or per-entry project field",
				i, f.Local)
		}

		// No duplicate local paths
		if seenLocal[f.Local] {
			return fmt.Errorf("duplicate local path %q in manifest", f.Local)
		}
		seenLocal[f.Local] = true

		// No duplicate (project, remote) pairs
		key := proj + "|" + f.Remote
		if seenRemote[key] {
			return fmt.Errorf("duplicate (project, remote) pair: project=%q remote=%q", proj, f.Remote)
		}
		seenRemote[key] = true
	}
	return nil
}
