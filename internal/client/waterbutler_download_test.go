package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload_Success(t *testing.T) {
	body := "hello,world\n1,2\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.csv")
	wb := NewWaterbutler("tok")
	if err := wb.Download(context.Background(), srv.URL, dest, int64(len(body)), true); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading dest: %v", err)
	}
	if string(got) != body {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
}

// TestDownload_PartialCleanup verifies that a download which fails mid-stream
// does not leave a partial file behind on disk.
func TestDownload_PartialCleanup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise 1000 bytes but only send a few, then drop the connection.
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Handler returns without writing the rest → truncated body.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "broken.csv")
	wb := NewWaterbutler("tok")
	err := wb.Download(context.Background(), srv.URL, dest, 1000, true)
	if err == nil {
		t.Fatal("expected error from truncated download, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("partial file should have been removed, but it still exists at %s", dest)
	}
}

func TestDownload_ErrorStatusNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"errors":[{"detail":"no access"}]}`)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "denied.csv")
	wb := NewWaterbutler("tok")
	err := wb.Download(context.Background(), srv.URL, dest, -1, true)
	if err == nil {
		t.Fatal("expected error for 403 download")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("no file should be created on an error status")
	}
}
