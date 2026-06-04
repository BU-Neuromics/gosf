package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBuildUploadURL(t *testing.T) {
	cases := []struct {
		nodeID     string
		parentPath string
		filename   string
		want       string
	}{
		{
			"abc12", "/", "file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/?name=file.csv&kind=file",
		},
		{
			"abc12", "", "file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/?name=file.csv&kind=file",
		},
		{
			"abc12", "/data", "result.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/data/?name=result.csv&kind=file",
		},
		{
			"abc12", "/data/results", "out.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/data/results/?name=out.csv&kind=file",
		},
		{
			"abc12", "/path with spaces", "my file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/path%20with%20spaces/?name=my+file.csv&kind=file",
		},
	}

	for _, tc := range cases {
		got := BuildUploadURL(tc.nodeID, tc.parentPath, tc.filename)
		if got != tc.want {
			t.Errorf("BuildUploadURL(%q, %q, %q)\n  got  %s\n  want %s",
				tc.nodeID, tc.parentPath, tc.filename, got, tc.want)
		}
	}
}

func TestUpload_ReturnsVersionAndMD5(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want PUT", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"attributes":{"version":2,"extra":{"hashes":{"md5":"deadbeef"}}}}}`)
	}))
	defer srv.Close()

	// Write a tiny temp file to upload.
	f, err := os.CreateTemp("", "gosf-upload-test-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("hello")
	f.Close()
	defer os.Remove(f.Name())

	wb := NewWaterbutler("tok")
	result, err := wb.Upload(context.Background(), f.Name(), srv.URL, true)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if result.Version != 2 {
		t.Errorf("Version = %d, want 2", result.Version)
	}
	if result.MD5 != "deadbeef" {
		t.Errorf("MD5 = %q, want deadbeef", result.MD5)
	}
}

func TestRevisionURL(t *testing.T) {
	cases := []struct {
		base     string
		revision int
		want     string
	}{
		{
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/file.csv",
			2,
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/file.csv?revision=2",
		},
		{
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/file.csv?mode=render",
			3,
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/file.csv?mode=render&revision=3",
		},
	}

	for _, tc := range cases {
		got := RevisionURL(tc.base, tc.revision)
		if got != tc.want {
			t.Errorf("RevisionURL(%q, %d)\n  got  %s\n  want %s", tc.base, tc.revision, got, tc.want)
		}
		if !strings.Contains(got, "revision=") {
			t.Errorf("RevisionURL missing revision parameter: %s", got)
		}
	}
}
