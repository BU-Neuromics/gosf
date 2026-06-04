package resolver

import (
	"context"
	"fmt"
	"strings"

	"github.com/BU-Neuromics/gosf/internal/client"
)

// Target holds the parsed components of an OSF path argument like
// "abc12:/data/results" or "abc12/xyz34:/path".
type Target struct {
	NodeID   string // the GUID to operate on (component GUID if present)
	ParentID string // parent node GUID for component addressing (may be empty)
	Path     string // path within OSF Storage; always starts with "/"
}

// ParseTarget parses a user-supplied OSF path argument.
//
// Accepted forms:
//   - abc12                → root listing of node abc12
//   - abc12:/path/to/dir  → path within node abc12
//   - abc12/xyz34:/path   → path within component xyz34 (child of abc12)
func ParseTarget(s string) (Target, error) {
	if s == "" {
		return Target{}, fmt.Errorf("empty target")
	}

	var nodePart, path string
	if idx := strings.IndexByte(s, ':'); idx >= 0 {
		nodePart = s[:idx]
		path = s[idx+1:]
	} else {
		nodePart = s
		path = "/"
	}

	if path == "" || path == "/" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	var t Target
	if idx := strings.IndexByte(nodePart, '/'); idx >= 0 {
		t.ParentID = nodePart[:idx]
		t.NodeID = nodePart[idx+1:]
	} else {
		t.NodeID = nodePart
	}

	if t.NodeID == "" {
		return Target{}, fmt.Errorf("missing node GUID in %q", s)
	}
	t.Path = path
	return t, nil
}

// Resolver resolves OSF path strings to file listings using the metadata client.
type Resolver struct {
	client *client.OSFClient
}

// New creates a Resolver backed by the given OSF metadata client.
func New(c *client.OSFClient) *Resolver {
	return &Resolver{client: c}
}

// ListDir returns the file/folder entries at the given path within a node.
// Path "/" lists the storage root. Intermediate components must be folders.
// If path resolves to a single file, that file is returned as a one-element slice.
func (r *Resolver) ListDir(ctx context.Context, nodeID, path string) ([]client.FileItem, error) {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return r.client.ListFiles(ctx, nodeID)
	}

	components := strings.Split(strings.TrimLeft(path, "/"), "/")
	items, err := r.client.ListFiles(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	for i, comp := range components {
		found, ok := findItem(items, comp)
		if !ok {
			return nil, fmt.Errorf("path not found: /%s", strings.Join(components[:i+1], "/"))
		}

		isLast := i == len(components)-1
		if found.Attributes.Kind == "folder" {
			folderURL := found.Relationships.Files.Links.Related.Href
			if folderURL == "" {
				return nil, fmt.Errorf("folder %q has no listing URL", comp)
			}
			if isLast {
				return r.client.ListFilesFromURL(ctx, folderURL)
			}
			items, err = r.client.ListFilesFromURL(ctx, folderURL)
			if err != nil {
				return nil, err
			}
		} else {
			if !isLast {
				return nil, fmt.Errorf("not a directory: /%s", strings.Join(components[:i+1], "/"))
			}
			// Single file match
			return []client.FileItem{found}, nil
		}
	}

	return items, nil
}

func findItem(items []client.FileItem, name string) (client.FileItem, bool) {
	for _, item := range items {
		if item.Attributes.Name == name {
			return item, true
		}
	}
	return client.FileItem{}, false
}
