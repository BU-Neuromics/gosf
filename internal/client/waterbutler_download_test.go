package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestDownload_ContextCancel verifies that cancelling the context mid-stream
// (e.g. Ctrl-C) aborts the download and removes the partial file — the behavior
// root.go relies on via signal.NotifyContext.
func TestDownload_ContextCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("partial"))
			f.Flush()
		}
		close(started)
		<-r.Context().Done() // block until the client cancels
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	dest := filepath.Join(t.TempDir(), "out.bin")
	errc := make(chan error, 1)
	go func() {
		errc <- NewWaterbutler("tok").Download(ctx, srv.URL, dest, 1000000, true)
	}()

	<-started
	cancel()

	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("expected an error when the context is cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download did not return after context cancel")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("partial file should be removed after a cancelled download, stat err = %v", statErr)
	}
}
