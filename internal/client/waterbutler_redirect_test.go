package client

import (
	"net/http"
	"net/url"
	"testing"
)

func TestIsOSFHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"osf.io", true},
		{"files.osf.io", true},
		{"api.osf.io", true},
		{"staging.osf.io", true},
		{"test.osf.io", true},
		{"s3.amazonaws.com", false},
		{"s3.us-east-1.amazonaws.com", false},
		{"storage.googleapis.com", false},
		{"127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"localhost", false},
		{"notosf.io", false}, // suffix match must be .osf.io, not osf.io
		{"myosf.io", false},
	}
	for _, tc := range cases {
		if got := isOSFHost(tc.host); got != tc.want {
			t.Errorf("isOSFHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestRedirectPolicy_KeepsAuthForOSFInternalRedirect(t *testing.T) {
	// osf.io → files.osf.io: both are OSF infrastructure, auth must be preserved.
	c := NewWaterbutler("mytoken")

	req := &http.Request{
		Header: http.Header{"Authorization": []string{"Bearer mytoken"}},
		URL:    &url.URL{Host: "files.osf.io"},
	}
	via := []*http.Request{{URL: &url.URL{Host: "osf.io"}}}

	if err := c.redirectPolicy(req, via); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Error("Authorization header was stripped on osf.io → files.osf.io redirect; should be kept")
	}
}

func TestRedirectPolicy_StripsAuthForExternalRedirect(t *testing.T) {
	// files.osf.io → s3.amazonaws.com: leaving OSF infra, auth must be stripped.
	c := NewWaterbutler("mytoken")

	req := &http.Request{
		Header: http.Header{"Authorization": []string{"Bearer mytoken"}},
		URL:    &url.URL{Host: "s3.amazonaws.com"},
	}
	via := []*http.Request{{URL: &url.URL{Host: "files.osf.io"}}}

	if err := c.redirectPolicy(req, via); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("Authorization header was not stripped on files.osf.io → s3.amazonaws.com redirect")
	}
}

func TestRedirectPolicy_StripsAuthForGCSRedirect(t *testing.T) {
	c := NewWaterbutler("mytoken")

	req := &http.Request{
		Header: http.Header{"Authorization": []string{"Bearer mytoken"}},
		URL:    &url.URL{Host: "storage.googleapis.com"},
	}
	via := []*http.Request{{URL: &url.URL{Host: "files.osf.io"}}}

	if err := c.redirectPolicy(req, via); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("Authorization header was not stripped for Google Cloud Storage redirect")
	}
}

func TestRedirectPolicy_TooManyRedirects(t *testing.T) {
	c := NewWaterbutler("mytoken")

	req := &http.Request{
		Header: http.Header{},
		URL:    &url.URL{Host: "files.osf.io"},
	}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = &http.Request{URL: &url.URL{Host: "files.osf.io"}}
	}

	err := c.redirectPolicy(req, via)
	if err == nil {
		t.Fatal("expected error after 10 redirects")
	}
}
