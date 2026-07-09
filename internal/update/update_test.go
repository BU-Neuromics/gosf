package update

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewerAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.5.0", "v1.6.0", true},
		{"v1.5.0", "v1.5.1", true},
		{"v1.5.0", "v2.0.0", true},
		{"v1.5.0", "v1.5.0", false},
		{"v1.6.0", "v1.5.0", false},
		{"1.5.0", "1.6.0", true}, // no 'v' prefix
		{"v1.5.0", "v1.5.0-rc1", false},
		{"v1.5.0", "garbage", false},
		{"dev", "v1.6.0", false}, // unparseable current
	}
	for _, tc := range cases {
		if got := newerAvailable(tc.current, tc.latest); got != tc.want {
			t.Errorf("newerAvailable(%q,%q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestShouldNotify(t *testing.T) {
	cases := []struct {
		name                                 string
		current                              string
		quiet, jsonMode, stderrTTY, disabled bool
		want                                 bool
	}{
		{"interactive release build", "v1.5.0", false, false, true, false, true},
		{"disabled via env", "v1.5.0", false, false, true, true, false},
		{"quiet", "v1.5.0", true, false, true, false, false},
		{"json", "v1.5.0", false, true, true, false, false},
		{"not a TTY", "v1.5.0", false, false, false, false, false},
		{"dev build", "dev", false, false, true, false, false},
		{"empty version", "", false, false, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldNotify(tc.current, tc.quiet, tc.jsonMode, tc.stderrTTY, tc.disabled); got != tc.want {
				t.Errorf("shouldNotify = %v, want %v", got, tc.want)
			}
		})
	}
}

func githubServer(t *testing.T, tag string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits++
		}
		fmt.Fprintf(w, `{"tag_name": %q}`, tag)
	}))
}

func newChecker(current, apiURL, cachePath string, now time.Time) *Checker {
	return &Checker{
		Current:    current,
		APIURL:     apiURL,
		CachePath:  cachePath,
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
		Now:        func() time.Time { return now },
	}
}

func TestChecker_Notify_Outdated(t *testing.T) {
	srv := githubServer(t, "v1.6.0", nil)
	defer srv.Close()
	c := newChecker("v1.5.0", srv.URL, filepath.Join(t.TempDir(), "u.json"), time.Now())

	var buf bytes.Buffer
	c.Notify(context.Background(), &buf)
	out := buf.String()
	if !strings.Contains(out, "v1.6.0") || !strings.Contains(out, "v1.5.0") {
		t.Errorf("expected an update notice naming both versions, got %q", out)
	}
}

func TestChecker_Notify_UpToDate(t *testing.T) {
	srv := githubServer(t, "v1.5.0", nil)
	defer srv.Close()
	c := newChecker("v1.5.0", srv.URL, filepath.Join(t.TempDir(), "u.json"), time.Now())

	var buf bytes.Buffer
	c.Notify(context.Background(), &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no notice when up to date, got %q", buf.String())
	}
}

func TestChecker_Notify_UsesCacheWithinInterval(t *testing.T) {
	hits := 0
	srv := githubServer(t, "v1.6.0", &hits)
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "u.json")
	now := time.Now()

	// First call hits the network and caches.
	newChecker("v1.5.0", srv.URL, cache, now).Notify(context.Background(), &bytes.Buffer{})
	// Second call 1h later must NOT hit the network (cache is fresh).
	var buf bytes.Buffer
	newChecker("v1.5.0", srv.URL, cache, now.Add(time.Hour)).Notify(context.Background(), &buf)

	if hits != 1 {
		t.Errorf("network hit %d times, want 1 (second call should use cache)", hits)
	}
	if !strings.Contains(buf.String(), "v1.6.0") {
		t.Errorf("cached check should still notify, got %q", buf.String())
	}
}

func TestChecker_Notify_RefreshesAfterInterval(t *testing.T) {
	hits := 0
	srv := githubServer(t, "v1.6.0", &hits)
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "u.json")
	now := time.Now()

	newChecker("v1.5.0", srv.URL, cache, now).Notify(context.Background(), &bytes.Buffer{})
	newChecker("v1.5.0", srv.URL, cache, now.Add(25*time.Hour)).Notify(context.Background(), &bytes.Buffer{})

	if hits != 2 {
		t.Errorf("network hit %d times, want 2 (cache should be stale after 25h)", hits)
	}
}

func TestChecker_Notify_NetworkErrorIsSilent(t *testing.T) {
	c := newChecker("v1.5.0", "http://127.0.0.1:0/nope", filepath.Join(t.TempDir(), "u.json"), time.Now())
	var buf bytes.Buffer
	c.Notify(context.Background(), &buf) // must not panic
	if buf.Len() != 0 {
		t.Errorf("expected silence on network error, got %q", buf.String())
	}
}
