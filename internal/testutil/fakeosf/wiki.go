package fakeosf

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// WikiPage is a fake wiki page: a name plus an ordered list of content
// versions (oldest-first; version N = Versions[N-1]).
type WikiPage struct {
	ID       string
	NodeID   string
	Name     string
	Versions [][]byte
}

// LatestContent returns the newest version's content, or nil when the page has
// no versions yet.
func (p *WikiPage) LatestContent() []byte {
	if len(p.Versions) == 0 {
		return nil
	}
	return p.Versions[len(p.Versions)-1]
}

// LatestVersion returns the newest version number (0 when empty).
func (p *WikiPage) LatestVersion() int { return len(p.Versions) }

// AddWiki registers a wiki page with a single version on a project.
func (s *Server) AddWiki(projectID, name string, content []byte) *WikiPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addWikiLocked(projectID, name, content)
}

func (s *Server) addWikiLocked(projectID, name string, content []byte) *WikiPage {
	s.nextID++
	p := &WikiPage{
		ID:       fmt.Sprintf("wk%d", s.nextID),
		NodeID:   projectID,
		Name:     name,
		Versions: [][]byte{content},
	}
	s.wikis[p.ID] = p
	s.touchWikiLocked(p)
	return p
}

// AddWikiVersion appends a new content version to an existing page (creating
// the page if absent).
func (s *Server) AddWikiVersion(projectID, name string, content []byte) *WikiPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.findWikiLocked(projectID, name); p != nil {
		p.Versions = append(p.Versions, content)
		s.touchWikiLocked(p)
		return p
	}
	return s.addWikiLocked(projectID, name, content)
}

// SetWikiDisabled makes the node's wiki endpoints respond with the OSF
// addon-disabled 404.
func (s *Server) SetWikiDisabled(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wikiDisabled[nodeID] = true
}

// GetWiki returns the page with the given name on a project, or nil.
func (s *Server) GetWiki(projectID, name string) *WikiPage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findWikiLocked(projectID, name)
}

// WikiDeletes returns the wiki IDs that received a DELETE request.
func (s *Server) WikiDeletes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.wikiDeletes...)
}

// WikiListRequests returns how many GET /v2/nodes/{id}/wikis/ list requests the
// server has received (used to assert scan memoization).
func (s *Server) WikiListRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wikiListReq
}

// WikiVersionRequests returns how many GET /v2/wikis/{id}/versions/ list
// requests the server has received.
func (s *Server) WikiVersionRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wikiVersionReq
}

// WikiVersionContentRequests returns how many historical version content
// fetches the server has received.
func (s *Server) WikiVersionContentRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wikiVersionContent
}

func (s *Server) findWikiLocked(projectID, name string) *WikiPage {
	for _, id := range s.wikiOrder[projectID] {
		if p := s.wikis[id]; p != nil && p.Name == name {
			return p
		}
	}
	return nil
}

// touchWikiLocked moves a page to the front of its project's order (OSF lists
// wikis most recently modified first).
func (s *Server) touchWikiLocked(p *WikiPage) {
	order := s.wikiOrder[p.NodeID]
	out := make([]string, 0, len(order)+1)
	out = append(out, p.ID)
	for _, id := range order {
		if id != p.ID {
			out = append(out, id)
		}
	}
	s.wikiOrder[p.NodeID] = out
}

func (s *Server) wikiJSON(p *WikiPage) map[string]any {
	base := s.URL()
	return map[string]any{
		"id":   p.ID,
		"type": "wikis",
		"attributes": map[string]any{
			"name":              p.Name,
			"kind":              "file",
			"size":              len(p.LatestContent()),
			"date_modified":     "2026-01-01T00:00:00",
			"content_type":      "text/markdown",
			"path":              "/" + p.ID,
			"materialized_path": "/" + strings.ToLower(p.Name),
			"extra":             map[string]any{"version": p.LatestVersion()},
		},
		"links": map[string]any{
			"download": fmt.Sprintf("%s/v2/wikis/%s/content/", base, p.ID),
			"info":     fmt.Sprintf("%s/v2/wikis/%s/", base, p.ID),
			"self":     fmt.Sprintf("%s/v2/wikis/%s/", base, p.ID),
		},
	}
}

func (s *Server) wikiVersionJSON(p *WikiPage, num int) map[string]any {
	return map[string]any{
		"id":   strconv.Itoa(num),
		"type": "wiki-versions",
		"attributes": map[string]any{
			"date_created": "2026-01-01T00:00:00",
			"size":         len(p.Versions[num-1]),
			"content_type": "text/markdown",
		},
	}
}

func wikiErrorJSON(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{
		"errors": []map[string]any{{"detail": detail}},
	})
}

// wikiDisabledLocked reports (and serves) the addon-disabled 404 for a node.
func (s *Server) wikiDisabled404(w http.ResponseWriter, nodeID string) bool {
	s.mu.Lock()
	disabled := s.wikiDisabled[nodeID]
	s.mu.Unlock()
	if disabled {
		wikiErrorJSON(w, http.StatusNotFound, "The wiki for this node has been disabled.")
	}
	return disabled
}

// handleNodeWikis serves GET (list) and POST (create) on /v2/nodes/{id}/wikis/.
func (s *Server) handleNodeWikis(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// v2 / nodes / {id} / wikis
	if len(parts) != 4 {
		http.NotFound(w, r)
		return
	}
	nodeID := parts[2]
	if s.forbid(w, nodeID) || s.wikiDisabled404(w, nodeID) {
		return
	}
	s.mu.Lock()
	proj := s.projects[nodeID]
	s.mu.Unlock()
	if proj == nil {
		wikiErrorJSON(w, http.StatusNotFound, "Not found.")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		s.wikiListReq++
		data := make([]map[string]any, 0)
		for _, id := range s.wikiOrder[nodeID] {
			data = append(data, s.wikiJSON(s.wikis[id]))
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"data":  data,
			"links": map[string]any{"next": nil},
		})

	case http.MethodPost:
		var payload struct {
			Data struct {
				Type       string `json:"type"`
				Attributes struct {
					Name    string `json:"name"`
					Content string `json:"content"`
				} `json:"attributes"`
			} `json:"data"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil || payload.Data.Type != "wikis" {
			wikiErrorJSON(w, http.StatusBadRequest, "Invalid payload.")
			return
		}
		name := payload.Data.Attributes.Name
		if name == "" {
			wikiErrorJSON(w, http.StatusBadRequest, "Page name cannot be blank.")
			return
		}
		if strings.Contains(name, "/") {
			wikiErrorJSON(w, http.StatusBadRequest, "Page name cannot contain forward slashes.")
			return
		}
		if len(name) > 100 {
			wikiErrorJSON(w, http.StatusBadRequest, "Page name cannot be greater than 100 characters.")
			return
		}
		s.mu.Lock()
		exists := s.findWikiLocked(nodeID, name) != nil
		s.mu.Unlock()
		if exists {
			wikiErrorJSON(w, http.StatusConflict, fmt.Sprintf("A wiki page with the name '%s' already exists.", name))
			return
		}
		p := s.AddWiki(nodeID, name, []byte(payload.Data.Attributes.Content))
		s.mu.Lock()
		resp := s.wikiJSON(p)
		s.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"data": resp})

	default:
		http.NotFound(w, r)
	}
}

// handleWiki serves everything under /v2/wikis/{id}/: detail (GET/PATCH/DELETE),
// content, the versions list (GET/POST), and per-version detail/content.
func (s *Server) handleWiki(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// v2 / wikis / {id} [/ content | / versions [/ {n} [/ content]]]
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}
	wikiID := parts[2]
	s.mu.Lock()
	p := s.wikis[wikiID]
	s.mu.Unlock()
	if p == nil {
		wikiErrorJSON(w, http.StatusNotFound, "Not found.")
		return
	}
	if s.forbid(w, p.NodeID) || s.wikiDisabled404(w, p.NodeID) {
		return
	}
	rest := parts[3:]

	switch {
	case len(rest) == 0:
		s.handleWikiDetail(w, r, p)
	case len(rest) == 1 && rest[0] == "content" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write(p.LatestContent())
	case len(rest) == 1 && rest[0] == "versions":
		s.handleWikiVersions(w, r, p)
	case len(rest) >= 2 && rest[0] == "versions" && r.Method == http.MethodGet:
		num, err := strconv.Atoi(rest[1])
		if err != nil || num < 1 || num > len(p.Versions) {
			wikiErrorJSON(w, http.StatusNotFound, "Not found.")
			return
		}
		if len(rest) == 3 && rest[2] == "content" {
			s.mu.Lock()
			s.wikiVersionContent++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write(p.Versions[num-1])
			return
		}
		s.mu.Lock()
		resp := s.wikiVersionJSON(p, num)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"data": resp})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleWikiDetail(w http.ResponseWriter, r *http.Request, p *WikiPage) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		resp := s.wikiJSON(p)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"data": resp})

	case http.MethodPatch:
		if strings.EqualFold(p.Name, "home") {
			wikiErrorJSON(w, http.StatusBadRequest, "Cannot rename wiki home page")
			return
		}
		var payload struct {
			Data struct {
				Attributes struct {
					Name string `json:"name"`
				} `json:"attributes"`
			} `json:"data"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil || payload.Data.Attributes.Name == "" {
			wikiErrorJSON(w, http.StatusBadRequest, "Invalid payload.")
			return
		}
		newName := payload.Data.Attributes.Name
		s.mu.Lock()
		conflict := s.findWikiLocked(p.NodeID, newName)
		if conflict != nil && conflict.ID != p.ID {
			s.mu.Unlock()
			wikiErrorJSON(w, http.StatusConflict, fmt.Sprintf("Page already exists with name %s", newName))
			return
		}
		p.Name = newName
		s.touchWikiLocked(p)
		resp := s.wikiJSON(p)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"data": resp})

	case http.MethodDelete:
		if strings.EqualFold(p.Name, "home") {
			wikiErrorJSON(w, http.StatusBadRequest, "The home wiki page cannot be deleted.")
			return
		}
		s.mu.Lock()
		s.wikiDeletes = append(s.wikiDeletes, p.ID)
		delete(s.wikis, p.ID)
		order := s.wikiOrder[p.NodeID]
		out := order[:0]
		for _, id := range order {
			if id != p.ID {
				out = append(out, id)
			}
		}
		s.wikiOrder[p.NodeID] = out
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleWikiVersions(w http.ResponseWriter, r *http.Request, p *WikiPage) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		s.wikiVersionReq++
		data := make([]map[string]any, 0)
		for n := len(p.Versions); n >= 1; n-- { // newest-first
			data = append(data, s.wikiVersionJSON(p, n))
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"data":  data,
			"links": map[string]any{"next": nil},
		})

	case http.MethodPost:
		var payload struct {
			Data struct {
				Type       string `json:"type"`
				Attributes struct {
					Content string `json:"content"`
				} `json:"attributes"`
			} `json:"data"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil || payload.Data.Type != "wiki-versions" {
			wikiErrorJSON(w, http.StatusBadRequest, "Invalid payload.")
			return
		}
		s.mu.Lock()
		p.Versions = append(p.Versions, []byte(payload.Data.Attributes.Content))
		s.touchWikiLocked(p)
		resp := s.wikiVersionJSON(p, len(p.Versions))
		s.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"data": resp})

	default:
		http.NotFound(w, r)
	}
}
