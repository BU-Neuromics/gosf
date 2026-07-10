package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListWikis(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/nodes/abc12/wikis/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"data":[
				{"id":"w2","attributes":{"name":"Analysis Notes","kind":"file","size":7,
				  "extra":{"version":1}}}
			],"links":{"next":null}}`)
			return
		}
		fmt.Fprintf(w, `{"data":[
			{"id":"w1","attributes":{"name":"home","kind":"file","size":42,
			  "date_modified":"2026-01-01T00:00:00","content_type":"text/markdown",
			  "path":"/w1","materialized_path":"/home","extra":{"version":3}},
			 "links":{"download":"%s/wikis/w1/content/","info":"%s/wikis/w1/"}}
		],"links":{"next":"%s/nodes/abc12/wikis/?page=2"}}`, srvURL(r), srvURL(r), srvURL(r))
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	wikis, err := c.ListWikis(context.Background(), "abc12")
	if err != nil {
		t.Fatalf("ListWikis: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if len(wikis) != 2 {
		t.Fatalf("got %d wikis, want 2 (pagination)", len(wikis))
	}
	if wikis[0].ID != "w1" || wikis[0].Attributes.Name != "home" {
		t.Errorf("first wiki = %+v", wikis[0])
	}
	if wikis[0].Attributes.Extra.Version != 3 {
		t.Errorf("extra.version = %d, want 3", wikis[0].Attributes.Extra.Version)
	}
	if wikis[0].Attributes.Size != 42 {
		t.Errorf("size = %d, want 42", wikis[0].Attributes.Size)
	}
	if wikis[1].Attributes.Name != "Analysis Notes" {
		t.Errorf("second wiki name = %q", wikis[1].Attributes.Name)
	}
}

// srvURL reconstructs the test server base URL from the request.
func srvURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestListWikis_Disabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"errors":[{"detail":"The wiki for this node has been disabled."}]}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	_, err := c.ListWikis(context.Background(), "abc12")
	if err == nil {
		t.Fatal("expected error for disabled wiki")
	}
	if !IsWikiDisabled(err) {
		t.Errorf("IsWikiDisabled = false for %v", err)
	}
	// A garden-variety 404 is not "wiki disabled".
	if IsWikiDisabled(&APIError{StatusCode: 404, Message: "Not found."}) {
		t.Error("IsWikiDisabled should be false for a plain 404")
	}
	if IsWikiDisabled(nil) {
		t.Error("IsWikiDisabled(nil) should be false")
	}
}

func TestGetWikiContent_ByteExact(t *testing.T) {
	// Content endpoints return plain text, not JSON. Bytes must round-trip
	// exactly, including trailing newline and CRLF.
	content := "# Title\r\nline two\n\nno trailing newline"
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		if r.URL.Path != "/wikis/w1/content/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		io.WriteString(w, content)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	got, err := c.GetWikiContent(context.Background(), "w1")
	if err != nil {
		t.Fatalf("GetWikiContent: %v", err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
	if hadAuth {
		t.Error("Authorization header should be absent in unauthenticated mode")
	}
}

func TestGetWikiContent_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, `{"errors":[{"detail":"The requested wiki is no longer available."}]}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	_, err := c.GetWikiContent(context.Background(), "w1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusGone {
		t.Fatalf("want APIError 410, got %v", err)
	}
}

func TestGetWikiVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wikis/w1/versions/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[
			{"id":"2","type":"wiki-versions","attributes":{"date_created":"2026-02-01T00:00:00","size":20,"content_type":"text/markdown"}},
			{"id":"1","type":"wiki-versions","attributes":{"date_created":"2026-01-01T00:00:00","size":10,"content_type":"text/markdown"}}
		],"links":{"next":null}}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	vs, err := c.GetWikiVersions(context.Background(), "w1")
	if err != nil {
		t.Fatalf("GetWikiVersions: %v", err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %d versions, want 2", len(vs))
	}
	if vs[0].Number() != 2 || vs[1].Number() != 1 {
		t.Errorf("version numbers = %d, %d; want 2, 1", vs[0].Number(), vs[1].Number())
	}
	if vs[0].Attributes.Size != 20 {
		t.Errorf("size = %d, want 20", vs[0].Attributes.Size)
	}
}

func TestGetWikiVersionContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wikis/w1/versions/2/content/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		io.WriteString(w, "old content v2\n")
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	got, err := c.GetWikiVersionContent(context.Background(), "w1", "2")
	if err != nil {
		t.Fatalf("GetWikiVersionContent: %v", err)
	}
	if string(got) != "old content v2\n" {
		t.Errorf("content = %q", got)
	}
}

func TestCreateWiki(t *testing.T) {
	var gotBody map[string]any
	var gotContentType, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"id":"w9","attributes":{"name":"protocol","kind":"file",
			"size":12,"extra":{"version":1}}}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	wiki, err := c.CreateWiki(context.Background(), "abc12", "protocol", "hello world\n")
	if err != nil {
		t.Fatalf("CreateWiki: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/nodes/abc12/wikis/" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotContentType != "application/vnd.api+json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	data := gotBody["data"].(map[string]any)
	if data["type"] != "wikis" {
		t.Errorf("payload type = %v, want wikis", data["type"])
	}
	attrs := data["attributes"].(map[string]any)
	if attrs["name"] != "protocol" || attrs["content"] != "hello world\n" {
		t.Errorf("payload attributes = %v", attrs)
	}
	if wiki.ID != "w9" || wiki.Attributes.Extra.Version != 1 {
		t.Errorf("wiki = %+v", wiki)
	}
}

func TestCreateWiki_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"errors":[{"detail":"A wiki page with the name 'protocol' already exists."}]}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	_, err := c.CreateWiki(context.Background(), "abc12", "protocol", "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("want APIError 409, got %v", err)
	}
	if apiErr.Message != "A wiki page with the name 'protocol' already exists." {
		t.Errorf("message = %q", apiErr.Message)
	}
}

func TestCreateWikiVersion(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"data":{"id":"4","type":"wiki-versions",
			"attributes":{"date_created":"2026-03-01T00:00:00","size":9}}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	v, err := c.CreateWikiVersion(context.Background(), "w1", "new text\n")
	if err != nil {
		t.Fatalf("CreateWikiVersion: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/wikis/w1/versions/" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	data := gotBody["data"].(map[string]any)
	if data["type"] != "wiki-versions" {
		t.Errorf("payload type = %v, want wiki-versions", data["type"])
	}
	if data["attributes"].(map[string]any)["content"] != "new text\n" {
		t.Errorf("payload attributes = %v", data["attributes"])
	}
	if v.Number() != 4 {
		t.Errorf("version number = %d, want 4", v.Number())
	}
}

func TestRenameWiki(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"data":{"id":"w1","attributes":{"name":"renamed","extra":{"version":3}}}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	wiki, err := c.RenameWiki(context.Background(), "w1", "renamed")
	if err != nil {
		t.Fatalf("RenameWiki: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/wikis/w1/" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	data := gotBody["data"].(map[string]any)
	if data["type"] != "wikis" || data["id"] != "w1" {
		t.Errorf("payload data = %v", data)
	}
	if data["attributes"].(map[string]any)["name"] != "renamed" {
		t.Errorf("payload attributes = %v", data["attributes"])
	}
	if wiki.Attributes.Name != "renamed" {
		t.Errorf("wiki name = %q", wiki.Attributes.Name)
	}
}

func TestDeleteWiki(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	if err := c.DeleteWiki(context.Background(), "w1"); err != nil {
		t.Fatalf("DeleteWiki: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/wikis/w1/" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestDeleteWiki_HomeRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"errors":[{"detail":"The home wiki page cannot be deleted."}]}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	err := c.DeleteWiki(context.Background(), "w1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("want APIError 400, got %v", err)
	}
}

func TestWikiVersionNumber_NonNumericID(t *testing.T) {
	v := WikiVersion{ID: "not-a-number"}
	if got := v.Number(); got != 0 {
		t.Errorf("Number() = %d, want 0 for non-numeric id", got)
	}
}
