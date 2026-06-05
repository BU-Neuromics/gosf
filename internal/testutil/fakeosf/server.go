// Package fakeosf provides a fake OSF + Waterbutler HTTP server for integration tests.
// Both the JSON:API metadata tier (/v2/) and the Waterbutler file tier (/v1/) are
// served by a single httptest.Server so tests only need one URL.
package fakeosf

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
)

// UploadRecord describes one upload received by the server.
type UploadRecord struct {
	NodeID  string
	Path    string
	Content []byte
}

type fileVer struct {
	num     int
	content []byte
	md5     string
}

// File is a fake file or folder in OSF Storage.
type File struct {
	ID       string
	Name     string
	FilePath string // full path within OSF Storage, starts with "/"
	IsFolder bool
	Versions []fileVer // oldest-first; latest = last element
	Children []*File
}

// LatestContent returns the content of the newest version, or nil for folders.
func (f *File) LatestContent() []byte {
	if len(f.Versions) == 0 {
		return nil
	}
	return f.Versions[len(f.Versions)-1].content
}

// LatestMD5 returns the MD5 hex string of the newest version.
func (f *File) LatestMD5() string {
	if len(f.Versions) == 0 {
		return ""
	}
	return f.Versions[len(f.Versions)-1].md5
}

// LatestVersion returns the version number of the newest version.
func (f *File) LatestVersion() int {
	if len(f.Versions) == 0 {
		return 0
	}
	return f.Versions[len(f.Versions)-1].num
}

// VersionMD5 returns the MD5 for the given version number, or "" if not found.
func (f *File) VersionMD5(ver int) string {
	for _, v := range f.Versions {
		if v.num == ver {
			return v.md5
		}
	}
	return ""
}

// versionContent returns the content for the given version number, or latest if not found.
func (f *File) versionContent(ver int) []byte {
	for _, v := range f.Versions {
		if v.num == ver {
			return v.content
		}
	}
	return f.LatestContent()
}

type fakeProject struct {
	id     string
	title  string
	root   []*File
	byID   map[string]*File
	byPath map[string]*File
}

// Server is a combined fake OSF metadata + Waterbutler HTTP server.
type Server struct {
	srv      *httptest.Server
	projects map[string]*fakeProject
	allFiles map[string]*File // all files across projects, keyed by ID
	uploads  []UploadRecord
	nextID   int
	mu       sync.Mutex
}

// New creates and starts a new fake server.
func New() *Server {
	s := &Server{
		projects: make(map[string]*fakeProject),
		allFiles: make(map[string]*File),
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handler))
	return s
}

// URL returns the server base URL, used for both GOSF_API_BASE and GOSF_FILES_BASE.
func (s *Server) URL() string { return s.srv.URL }

// Close shuts the server down.
func (s *Server) Close() { s.srv.Close() }

// AddProject registers a project with no files.
func (s *Server) AddProject(id, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[id] = &fakeProject{
		id: id, title: title,
		byID: make(map[string]*File), byPath: make(map[string]*File),
	}
}

// AddFile adds a file with a single version to a project.
// filePath must start with "/" (e.g. "/data/results.csv"). Intermediate
// folders are created automatically.
func (s *Server) AddFile(projectID, filePath string, content []byte) *File {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addFileLocked(projectID, filePath, content)
}

func (s *Server) addFileLocked(projectID, filePath string, content []byte) *File {
	proj := s.projects[projectID]
	h := md5.Sum(content)
	s.nextID++
	f := &File{
		ID:       fmt.Sprintf("f%d", s.nextID),
		Name:     path.Base(filePath),
		FilePath: filePath,
		Versions: []fileVer{{num: 1, content: content, md5: fmt.Sprintf("%x", h[:])}},
	}
	proj.byID[f.ID] = f
	proj.byPath[filePath] = f
	s.allFiles[f.ID] = f

	parent := path.Dir(filePath)
	if parent == "/" || parent == "." {
		proj.root = append(proj.root, f)
	} else {
		folder := s.ensureFolderLocked(proj, parent)
		folder.Children = append(folder.Children, f)
	}
	return f
}

func (s *Server) ensureFolderLocked(proj *fakeProject, folderPath string) *File {
	if f, ok := proj.byPath[folderPath]; ok {
		return f
	}
	s.nextID++
	folder := &File{
		ID:       fmt.Sprintf("d%d", s.nextID),
		Name:     path.Base(folderPath),
		FilePath: folderPath,
		IsFolder: true,
	}
	proj.byID[folder.ID] = folder
	proj.byPath[folderPath] = folder
	s.allFiles[folder.ID] = folder

	parent := path.Dir(folderPath)
	if parent == "/" || parent == "." {
		proj.root = append(proj.root, folder)
	} else {
		pf := s.ensureFolderLocked(proj, parent)
		pf.Children = append(pf.Children, folder)
	}
	return folder
}

// Uploads returns a copy of all recorded upload operations.
func (s *Server) Uploads() []UploadRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]UploadRecord, len(s.uploads))
	copy(result, s.uploads)
	return result
}

// GetFile returns the File at filePath in projectID, or nil.
func (s *Server) GetFile(projectID, filePath string) *File {
	s.mu.Lock()
	defer s.mu.Unlock()
	proj := s.projects[projectID]
	if proj == nil {
		return nil
	}
	return proj.byPath[filePath]
}

// --- HTTP routing ---

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v2/nodes/") && strings.Contains(p, "/files/osfstorage/"):
		s.handleFileList(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v2/nodes/"):
		s.handleNode(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v2/files/") && strings.HasSuffix(p, "/versions/"):
		s.handleVersions(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1/files/") && !strings.HasSuffix(p, "/upload") && !strings.HasSuffix(p, "/delete"):
		s.handleDownload(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(p, "/v1/resources/"):
		s.handleNewUpload(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(p, "/v1/files/") && strings.HasSuffix(p, "/upload"):
		s.handleVersionUpload(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(p, "/v1/files/"):
		s.handleDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/vnd.api+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Header already sent; nothing useful we can do.
		_ = err
	}
}

func (s *Server) fileItemJSON(nodeID string, f *File) map[string]any {
	base := s.URL()
	kind := "file"
	if f.IsFolder {
		kind = "folder"
	}
	size := int64(0)
	if !f.IsFolder && len(f.Versions) > 0 {
		size = int64(len(f.Versions[len(f.Versions)-1].content))
	}
	folderHref := ""
	if f.IsFolder {
		folderHref = fmt.Sprintf("%s/v2/nodes/%s/files/osfstorage/?parent=%s", base, nodeID, f.ID)
	}
	return map[string]any{
		"id": f.ID,
		"attributes": map[string]any{
			"name":              f.Name,
			"kind":              kind,
			"size":              size,
			"date_modified":     "2024-01-01T00:00:00",
			"date_created":      "2024-01-01T00:00:00",
			"materialized_path": f.FilePath,
		},
		"links": map[string]any{
			"download": fmt.Sprintf("%s/v1/files/%s", base, f.ID),
			"upload":   fmt.Sprintf("%s/v1/files/%s/upload", base, f.ID),
			"delete":   fmt.Sprintf("%s/v1/files/%s/delete", base, f.ID),
		},
		"relationships": map[string]any{
			"files": map[string]any{
				"links": map[string]any{
					"related": map[string]any{
						"href": folderHref,
					},
				},
			},
		},
	}
}

// --- Handler implementations ---

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeID(r.URL.Path)
	s.mu.Lock()
	proj, ok := s.projects[nodeID]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id": proj.id,
			"attributes": map[string]any{
				"title":         proj.title,
				"description":   "",
				"date_created":  "2024-01-01T00:00:00",
				"date_modified": "2024-01-01T00:00:00",
				"public":        true,
				"category":      "project",
			},
		},
	})
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	nodeID := extractNodeID(r.URL.Path)
	parentID := r.URL.Query().Get("parent")

	s.mu.Lock()
	proj, ok := s.projects[nodeID]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	var items []*File
	if parentID == "" {
		items = proj.root
	} else {
		folder, exists := proj.byID[parentID]
		if !exists || !folder.IsFolder {
			http.NotFound(w, r)
			return
		}
		items = folder.Children
	}

	data := make([]map[string]any, 0, len(items))
	for _, f := range items {
		data = append(data, s.fileItemJSON(nodeID, f))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  data,
		"links": map[string]any{"next": nil},
	})
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	// /v2/files/{fileID}/versions/
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	fileID := parts[2]

	s.mu.Lock()
	f, ok := s.allFiles[fileID]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Return versions newest-first (matching OSF API behaviour).
	versions := make([]map[string]any, 0, len(f.Versions))
	for i := len(f.Versions) - 1; i >= 0; i-- {
		v := f.Versions[i]
		versions = append(versions, map[string]any{
			"id": fmt.Sprintf("ver%d-%s", v.num, f.ID),
			"attributes": map[string]any{
				"version":      v.num,
				"size":         int64(len(v.content)),
				"date_created": "2024-01-01T00:00:00",
				"extra": map[string]any{
					"hashes": map[string]any{"md5": v.md5},
				},
			},
			"embeds": map[string]any{
				"user": map[string]any{
					"data": map[string]any{
						"id": "user1",
						"attributes": map[string]any{
							"full_name":     "Test User",
							"email_primary": "test@example.com",
						},
					},
				},
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  versions,
		"links": map[string]any{"next": nil},
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/v1/files/")

	s.mu.Lock()
	f, ok := s.allFiles[fileID]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	rev := 0
	if revStr := r.URL.Query().Get("revision"); revStr != "" {
		fmt.Sscanf(revStr, "%d", &rev) //nolint:errcheck
	}

	var content []byte
	if rev > 0 {
		content = f.versionContent(rev)
	} else {
		content = f.LatestContent()
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) handleNewUpload(w http.ResponseWriter, r *http.Request) {
	// /v1/resources/{nodeID}/providers/osfstorage/{parentPath}?name={filename}&kind=file
	rest := strings.TrimPrefix(r.URL.Path, "/v1/resources/")
	const provSuffix = "/providers/osfstorage/"
	slashIdx := strings.Index(rest, provSuffix)
	if slashIdx < 0 {
		http.Error(w, "invalid upload URL", http.StatusBadRequest)
		return
	}
	nodeID := rest[:slashIdx]
	pathPart := rest[slashIdx+len(provSuffix):]

	filename := r.URL.Query().Get("name")
	if filename == "" {
		http.Error(w, "missing name param", http.StatusBadRequest)
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var parentPath string
	if pathPart == "" || strings.Trim(pathPart, "/") == "" {
		parentPath = "/"
	} else {
		parentPath = "/" + strings.Trim(pathPart, "/")
	}
	fullPath := parentPath
	if fullPath == "/" {
		fullPath = "/" + filename
	} else {
		fullPath = fullPath + "/" + filename
	}

	h := md5.Sum(content)
	md5Str := fmt.Sprintf("%x", h[:])

	s.mu.Lock()
	if proj, ok := s.projects[nodeID]; ok {
		s.nextID++
		f := &File{
			ID:       fmt.Sprintf("f%d", s.nextID),
			Name:     filename,
			FilePath: fullPath,
			Versions: []fileVer{{num: 1, content: content, md5: md5Str}},
		}
		proj.byID[f.ID] = f
		proj.byPath[fullPath] = f
		s.allFiles[f.ID] = f
		if parentPath == "/" {
			proj.root = append(proj.root, f)
		}
	}
	s.uploads = append(s.uploads, UploadRecord{NodeID: nodeID, Path: fullPath, Content: content})
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"attributes": map[string]any{
				"version": 1,
				"size":    int64(len(content)),
				"extra":   map[string]any{"hashes": map[string]any{"md5": md5Str}},
			},
		},
	})
}

func (s *Server) handleVersionUpload(w http.ResponseWriter, r *http.Request) {
	// /v1/files/{fileID}/upload
	fileID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/files/"), "/upload")

	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h := md5.Sum(content)
	md5Str := fmt.Sprintf("%x", h[:])

	s.mu.Lock()
	f, ok := s.allFiles[fileID]
	newVer := 1
	var fullPath string
	if ok {
		newVer = f.LatestVersion() + 1
		f.Versions = append(f.Versions, fileVer{num: newVer, content: content, md5: md5Str})
		fullPath = f.FilePath
	}
	s.uploads = append(s.uploads, UploadRecord{NodeID: "", Path: fullPath, Content: content})
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"attributes": map[string]any{
				"version": newVer,
				"size":    int64(len(content)),
				"extra":   map[string]any{"hashes": map[string]any{"md5": md5Str}},
			},
		},
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// --- URL parsing helpers ---

// extractNodeID pulls the node GUID from a /v2/nodes/{nodeID}/... path.
func extractNodeID(p string) string {
	rest := strings.TrimPrefix(p, "/v2/nodes/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}
