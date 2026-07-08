package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns an OSFClient pointed at the given test server.
func newTestClient(token, baseURL string) *OSFClient {
	c := New(token)
	c.baseURL = baseURL
	return c
}

func TestGetNode(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		if r.URL.Path != "/nodes/abc12/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"id":"abc12","attributes":{
			"title":"My Project","description":"desc","public":true,
			"category":"project","date_created":"2024-01-01T00:00:00",
			"date_modified":"2024-02-02T12:00:00"}}}`)
	}))
	defer srv.Close()

	c := newTestClient("secret-token", srv.URL)
	node, err := c.GetNode(context.Background(), "abc12")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer secret-token")
	}
	if gotAccept != "application/vnd.api+json" {
		t.Errorf("Accept header = %q", gotAccept)
	}
	if node.ID != "abc12" {
		t.Errorf("ID = %q, want abc12", node.ID)
	}
	if node.Attributes.Title != "My Project" {
		t.Errorf("Title = %q", node.Attributes.Title)
	}
	if !node.Attributes.Public {
		t.Error("expected Public = true")
	}
}

func TestGetNode_NoTokenOmitsAuthHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		fmt.Fprint(w, `{"data":{"id":"abc12","attributes":{"title":"Public"}}}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	if _, err := c.GetNode(context.Background(), "abc12"); err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header should be absent in unauthenticated mode")
	}
}

func TestGetNode_ErrorStatuses(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		wantMsg string
	}{
		{404, `{"errors":[{"detail":"Not found."}]}`, "Not found."},
		{403, `{"errors":[{"detail":"You do not have permission."}]}`, "You do not have permission."},
		{401, `{"errors":[{"detail":"Authentication credentials were not provided."}]}`, "Authentication credentials were not provided."},
		{500, `not json at all`, http.StatusText(500)},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			c := newTestClient("tok", srv.URL)
			_, err := c.GetNode(context.Background(), "abc12")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMsg)
			}
		})
	}
}

func TestListFiles_Pagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "", "1":
			// First page links to page 2 on this same server.
			fmt.Fprintf(w, `{"data":[
				{"id":"f1","attributes":{"name":"a.csv","kind":"file","size":10}},
				{"id":"f2","attributes":{"name":"b.csv","kind":"file","size":20}}
			],"links":{"next":"%s/nodes/abc12/files/osfstorage/?page=2"}}`, srv.URL)
		case "2":
			fmt.Fprint(w, `{"data":[
				{"id":"f3","attributes":{"name":"c.csv","kind":"file","size":30}}
			],"links":{"next":null}}`)
		default:
			t.Errorf("unexpected page: %s", r.URL.Query().Get("page"))
		}
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	items, err := c.ListFiles(context.Background(), "abc12")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items across pages, want 3", len(items))
	}
	names := []string{items[0].Attributes.Name, items[1].Attributes.Name, items[2].Attributes.Name}
	want := []string{"a.csv", "b.csv", "c.csv"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("item[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestGetUserNodes_Pagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"data":[{"id":"n3","attributes":{"title":"Third"}}],"links":{"next":null}}`)
			return
		}
		fmt.Fprintf(w, `{"data":[
			{"id":"n1","attributes":{"title":"First"}},
			{"id":"n2","attributes":{"title":"Second"}}
		],"links":{"next":"%s/users/me/nodes/?page=2"}}`, srv.URL)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	nodes, err := c.GetUserNodes(context.Background())
	if err != nil {
		t.Fatalf("GetUserNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(nodes))
	}
}

func TestGetCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":{"id":"u1","attributes":{"full_name":"Ada Lovelace","email_primary":"ada@example.com"}}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	user, err := c.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentUser: %v", err)
	}
	if user.ID != "u1" || user.Attributes.FullName != "Ada Lovelace" {
		t.Errorf("unexpected user: %+v", user)
	}
}

func TestGetNode_RespectsContextCancellation(t *testing.T) {
	// Server blocks until the request context is cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient("tok", srv.URL)

	errc := make(chan error, 1)
	go func() {
		_, err := c.GetNode(ctx, "abc12")
		errc <- err
	}()

	cancel() // simulate Ctrl-C

	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetNode did not return promptly after cancellation")
	}
}

func TestListFilesParsesLinksAndKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"id":"folder1","attributes":{"name":"data","kind":"folder"},
			 "relationships":{"files":{"links":{"related":{"href":"https://api.osf.io/v2/nodes/abc12/files/osfstorage/data/"}}}}},
			{"id":"file1","attributes":{"name":"x.csv","kind":"file","size":99},
			 "links":{"download":"https://files.osf.io/download/x","delete":"https://files.osf.io/delete/x","upload":"https://files.osf.io/upload/x"}}
		],"links":{"next":null}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	items, err := c.ListFiles(context.Background(), "abc12")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if items[0].Attributes.Kind != "folder" {
		t.Errorf("item[0] kind = %q, want folder", items[0].Attributes.Kind)
	}
	if items[0].Relationships.Files.Links.Related.Href == "" {
		t.Error("folder related href not parsed")
	}
	if items[1].Links.Download == "" || items[1].Links.Delete == "" {
		t.Error("file links not parsed")
	}
}

func TestGetFileVersions_HappyPath(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"data":[
			{"id":"3","attributes":{"version":3,"size":300,"date_created":"2024-03-01T00:00:00","content_type":"text/csv",
			 "extra":{"hashes":{"md5":"aaa111"}}},
			 "embeds":{"user":{"data":{"id":"u1","attributes":{"full_name":"Ada Lovelace","email_primary":"ada@example.com"}}}}},
			{"id":"2","attributes":{"version":2,"size":200,"date_created":"2024-02-01T00:00:00","content_type":"text/csv",
			 "extra":{"hashes":{"md5":"bbb222"}}},
			 "embeds":{"user":{"data":{"id":"u2","attributes":{"full_name":"Bob Smith","email_primary":""}}}}},
			{"id":"1","attributes":{"version":1,"size":100,"date_created":"2024-01-01T00:00:00","content_type":"text/csv",
			 "extra":{"hashes":{"md5":"ccc333"}}},
			 "embeds":{"user":{"data":{"id":"u3","attributes":{"full_name":"","email_primary":""}}}}}
		],"links":{"next":null}}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	versions, err := c.GetFileVersions(context.Background(), "file123")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if gotPath != "/files/file123/versions/" {
		t.Errorf("path = %q, want /files/file123/versions/", gotPath)
	}
	// The OSF versions endpoint rejects embed=user with a 400 ("The following
	// fields are not embeddable: user"), so we must not send any embed param.
	if gotQuery != "" {
		t.Errorf("query = %q, want empty (no embed param)", gotQuery)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}

	// version 3: email_primary wins
	if versions[0].Attributes.Version != 3 {
		t.Errorf("versions[0].Version = %d, want 3", versions[0].Attributes.Version)
	}
	if versions[0].Attributes.Extra.Hashes.MD5 != "aaa111" {
		t.Errorf("versions[0].MD5 = %q, want aaa111", versions[0].Attributes.Extra.Hashes.MD5)
	}
	if versions[1].Attributes.Extra.Hashes.MD5 != "bbb222" {
		t.Errorf("versions[1].MD5 = %q, want bbb222", versions[1].Attributes.Extra.Hashes.MD5)
	}
	if versions[0].Contributor() != "ada@example.com" {
		t.Errorf("Contributor = %q, want ada@example.com", versions[0].Contributor())
	}
	// version 2: full_name fallback
	if versions[1].Contributor() != "Bob Smith" {
		t.Errorf("Contributor = %q, want Bob Smith", versions[1].Contributor())
	}
	// version 1: GUID fallback
	if versions[2].Contributor() != "u3" {
		t.Errorf("Contributor = %q, want u3", versions[2].Contributor())
	}
}

func TestGetFileVersions_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"errors":[{"detail":"Not found."}]}`)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	_, err := c.GetFileVersions(context.Background(), "badid")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestGetFileVersions_Pagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"data":[
				{"id":"1","attributes":{"version":1,"size":100,"date_created":"2024-01-01T00:00:00"},
				 "embeds":{"user":{"data":{"id":"u1","attributes":{"full_name":"A","email_primary":""}}}}}
			],"links":{"next":null}}`)
			return
		}
		fmt.Fprintf(w, `{"data":[
			{"id":"2","attributes":{"version":2,"size":200,"date_created":"2024-02-01T00:00:00"},
			 "embeds":{"user":{"data":{"id":"u1","attributes":{"full_name":"A","email_primary":""}}}}}
		],"links":{"next":"%s/files/fid/versions/?embed=user&page=2"}}`, srv.URL)
	}))
	defer srv.Close()

	c := newTestClient("tok", srv.URL)
	versions, err := c.GetFileVersions(context.Background(), "fid")
	if err != nil {
		t.Fatalf("GetFileVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions across pages, want 2", len(versions))
	}
}
