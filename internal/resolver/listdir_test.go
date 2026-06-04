package resolver

import (
	"context"
	"testing"

	"github.com/BU-Neuromics/gosf/internal/client"
)

// fakeLister implements FileLister with an in-memory tree.
// root holds the storage-root listing; byURL maps a folder's related href to
// its children.
type fakeLister struct {
	root     []client.FileItem
	byURL    map[string][]client.FileItem
	rootErr  error
	urlErr   map[string]error
	rootCalls int
	urlCalls  map[string]int
}

func (f *fakeLister) ListFiles(_ context.Context, _ string) ([]client.FileItem, error) {
	f.rootCalls++
	if f.rootErr != nil {
		return nil, f.rootErr
	}
	return f.root, nil
}

func (f *fakeLister) ListFilesFromURL(_ context.Context, url string) ([]client.FileItem, error) {
	if f.urlCalls == nil {
		f.urlCalls = map[string]int{}
	}
	f.urlCalls[url]++
	if e, ok := f.urlErr[url]; ok {
		return nil, e
	}
	return f.byURL[url], nil
}

func folder(name, href string) client.FileItem {
	fi := client.FileItem{ID: name}
	fi.Attributes.Name = name
	fi.Attributes.Kind = "folder"
	fi.Relationships.Files.Links.Related.Href = href
	return fi
}

func file(name string, size int64) client.FileItem {
	fi := client.FileItem{ID: name}
	fi.Attributes.Name = name
	fi.Attributes.Kind = "file"
	fi.Attributes.Size = size
	return fi
}

// sampleTree builds:
//   /
//   ├── data/            (href "u-data")
//   │   ├── results/     (href "u-results")
//   │   │   └── out.csv
//   │   └── a.csv
//   └── readme.txt
func sampleTree() *fakeLister {
	return &fakeLister{
		root: []client.FileItem{
			folder("data", "u-data"),
			file("readme.txt", 5),
		},
		byURL: map[string][]client.FileItem{
			"u-data": {
				folder("results", "u-results"),
				file("a.csv", 10),
			},
			"u-results": {
				file("out.csv", 20),
			},
		},
	}
}

func names(items []client.FileItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Attributes.Name
	}
	return out
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListDir_Root(t *testing.T) {
	r := New(sampleTree())
	items, err := r.ListDir(context.Background(), "abc12", "/")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if got, want := names(items), []string{"data", "readme.txt"}; !equalNames(got, want) {
		t.Errorf("root listing = %v, want %v", got, want)
	}
}

func TestListDir_Subfolder(t *testing.T) {
	r := New(sampleTree())
	items, err := r.ListDir(context.Background(), "abc12", "/data")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if got, want := names(items), []string{"results", "a.csv"}; !equalNames(got, want) {
		t.Errorf("/data listing = %v, want %v", got, want)
	}
}

func TestListDir_NestedFolder(t *testing.T) {
	r := New(sampleTree())
	items, err := r.ListDir(context.Background(), "abc12", "/data/results")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if got, want := names(items), []string{"out.csv"}; !equalNames(got, want) {
		t.Errorf("/data/results listing = %v, want %v", got, want)
	}
}

func TestListDir_SingleFile(t *testing.T) {
	r := New(sampleTree())
	items, err := r.ListDir(context.Background(), "abc12", "/data/a.csv")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(items) != 1 || items[0].Attributes.Name != "a.csv" {
		t.Errorf("expected single file a.csv, got %v", names(items))
	}
	if items[0].Attributes.Kind != "file" {
		t.Errorf("expected kind file, got %q", items[0].Attributes.Kind)
	}
}

func TestListDir_TrailingSlashOnFolder(t *testing.T) {
	r := New(sampleTree())
	items, err := r.ListDir(context.Background(), "abc12", "/data/")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if got, want := names(items), []string{"results", "a.csv"}; !equalNames(got, want) {
		t.Errorf("/data/ listing = %v, want %v", got, want)
	}
}

func TestListDir_NotFound(t *testing.T) {
	r := New(sampleTree())
	_, err := r.ListDir(context.Background(), "abc12", "/nope")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestListDir_NotFoundNested(t *testing.T) {
	r := New(sampleTree())
	_, err := r.ListDir(context.Background(), "abc12", "/data/missing")
	if err == nil {
		t.Fatal("expected error for missing nested path")
	}
}

func TestListDir_TraverseThroughFile(t *testing.T) {
	r := New(sampleTree())
	_, err := r.ListDir(context.Background(), "abc12", "/readme.txt/sub")
	if err == nil {
		t.Fatal("expected error when traversing through a file")
	}
}
