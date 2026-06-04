package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const metaBase = "https://api.osf.io/v2"

// OSFClient is an HTTP client for the OSF JSON:API metadata endpoints.
type OSFClient struct {
	token string
	http  *http.Client
}

// New returns an OSFClient. Pass an empty token for unauthenticated (public) access.
func New(token string) *OSFClient {
	return &OSFClient{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// --- domain types ---

// Node represents a project or component.
type Node struct {
	ID         string         `json:"id"`
	Attributes NodeAttributes `json:"attributes"`
}

// NodeAttributes holds the metadata fields we care about for a node.
type NodeAttributes struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	DateCreated  string `json:"date_created"`
	DateModified string `json:"date_modified"`
	Public       bool   `json:"public"`
	Category     string `json:"category"`
}

// FileItem represents a single file or folder entry in OSF Storage.
type FileItem struct {
	ID            string            `json:"id"`
	Attributes    FileAttributes    `json:"attributes"`
	Links         FileLinks         `json:"links"`
	Relationships FileRelationships `json:"relationships"`
}

// FileAttributes holds metadata about a file or folder.
type FileAttributes struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"` // "file" or "folder"
	Size             int64  `json:"size"`
	DateModified     string `json:"date_modified"`
	DateCreated      string `json:"date_created"`
	MaterializedPath string `json:"materialized_path"`
	ContentType      string `json:"content_type"`
}

// FileLinks contains action URLs for a file.
type FileLinks struct {
	Download string `json:"download"`
	Upload   string `json:"upload"`
	Delete   string `json:"delete"`
	Self     string `json:"self"`
	Move     string `json:"move"`
}

// FileRelationships holds links to related resources (e.g. folder contents).
type FileRelationships struct {
	Files struct {
		Links struct {
			Related struct {
				Href string `json:"href"`
			} `json:"related"`
		} `json:"links"`
	} `json:"files"`
}

// User represents an OSF user.
type User struct {
	ID         string         `json:"id"`
	Attributes UserAttributes `json:"attributes"`
}

// UserAttributes holds user profile fields.
type UserAttributes struct {
	FullName     string `json:"full_name"`
	GivenName    string `json:"given_name"`
	FamilyName   string `json:"family_name"`
	EmailPrimary string `json:"email_primary"`
	Active       bool   `json:"active"`
}

// APIError represents a non-2xx response from the OSF API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OSF API %d: %s", e.StatusCode, e.Message)
}

// --- internal HTTP helpers ---

func (c *OSFClient) doGet(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func (c *OSFClient) getJSON(ctx context.Context, url string, v any) error {
	body, status, err := c.doGet(ctx, url)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return parseAPIError(status, body)
	}
	return json.Unmarshal(body, v)
}

func parseAPIError(statusCode int, body []byte) *APIError {
	var errResp struct {
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	msg := http.StatusText(statusCode)
	if json.Unmarshal(body, &errResp) == nil && len(errResp.Errors) > 0 && errResp.Errors[0].Detail != "" {
		msg = errResp.Errors[0].Detail
	}
	return &APIError{StatusCode: statusCode, Message: msg}
}

// --- public API ---

// GetFileItem returns metadata for a single file by its OSF file ID.
func (c *OSFClient) GetFileItem(ctx context.Context, fileID string) (*FileItem, error) {
	var result struct {
		Data FileItem `json:"data"`
	}
	url := fmt.Sprintf("%s/files/%s/", metaBase, fileID)
	if err := c.getJSON(ctx, url, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// GetUserNodes returns all nodes (projects and components) the authenticated
// user has access to. Requires auth.
func (c *OSFClient) GetUserNodes(ctx context.Context) ([]Node, error) {
	return c.listNodesFromURL(ctx, metaBase+"/users/me/nodes/")
}

type nodesPage struct {
	Data  []Node `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

func (c *OSFClient) listNodesFromURL(ctx context.Context, url string) ([]Node, error) {
	var all []Node
	for url != "" {
		var page nodesPage
		if err := c.getJSON(ctx, url, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		url = page.Links.Next
	}
	return all, nil
}

// GetCurrentUser returns the authenticated user's profile.
// Returns APIError with StatusCode 401 if the token is invalid.
func (c *OSFClient) GetCurrentUser(ctx context.Context) (*User, error) {
	var result struct {
		Data User `json:"data"`
	}
	if err := c.getJSON(ctx, metaBase+"/users/me/", &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// GetNode returns metadata for the given project/component GUID.
func (c *OSFClient) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	var result struct {
		Data Node `json:"data"`
	}
	url := fmt.Sprintf("%s/nodes/%s/", metaBase, nodeID)
	if err := c.getJSON(ctx, url, &result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

// ListFiles returns all items (files and folders) at the root of a node's OSF Storage.
// Pages are followed automatically.
func (c *OSFClient) ListFiles(ctx context.Context, nodeID string) ([]FileItem, error) {
	url := fmt.Sprintf("%s/nodes/%s/files/osfstorage/", metaBase, nodeID)
	return c.listFilesFromURL(ctx, url)
}

// ListFilesFromURL lists files at an arbitrary OSF files endpoint URL, following pagination.
func (c *OSFClient) ListFilesFromURL(ctx context.Context, url string) ([]FileItem, error) {
	return c.listFilesFromURL(ctx, url)
}

type filesPage struct {
	Data  []FileItem `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

func (c *OSFClient) listFilesFromURL(ctx context.Context, url string) ([]FileItem, error) {
	var all []FileItem
	for url != "" {
		var page filesPage
		if err := c.getJSON(ctx, url, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		url = page.Links.Next
	}
	return all, nil
}
