package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/schollz/progressbar/v3"
)

const wbBase = "https://files.osf.io"

// WaterbutlerClient handles file uploads, downloads, and deletes via
// the Waterbutler service at files.osf.io.
type WaterbutlerClient struct {
	token string
	http  *http.Client
}

// NewWaterbutler returns a WaterbutlerClient. Pass an empty token for
// unauthenticated (public project) access.
func NewWaterbutler(token string) *WaterbutlerClient {
	c := &WaterbutlerClient{token: token}
	// No timeout — callers stream large files.
	c.http = &http.Client{
		CheckRedirect: c.redirectPolicy,
	}
	return c
}

// redirectPolicy preserves the Authorization header when redirecting within
// OSF infrastructure (osf.io and *.osf.io), and strips it when redirecting
// to external backends such as S3 or GCS presigned URLs.
func (c *WaterbutlerClient) redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) > 0 && !isOSFHost(req.URL.Host) {
		req.Header.Del("Authorization")
	}
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	return nil
}

// isOSFHost reports whether host is OSF infrastructure (osf.io or any subdomain).
func isOSFHost(host string) bool {
	return host == "osf.io" || strings.HasSuffix(host, ".osf.io")
}

func (c *WaterbutlerClient) authHeader() string {
	if c.token != "" {
		return "Bearer " + c.token
	}
	return ""
}

// Download fetches content from a Waterbutler download URL and writes it to
// destPath, showing a progress bar unless quiet is true.
// size is the expected byte count for the progress bar; pass -1 if unknown.
func (c *WaterbutlerClient) Download(ctx context.Context, downloadURL, destPath string, size int64, quiet bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	if auth := c.authHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	if size <= 0 {
		size = resp.ContentLength
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}
	defer f.Close()

	var dst io.Writer = f
	if !quiet {
		bar := progressbar.NewOptions64(size,
			progressbar.OptionSetDescription(truncateName(destPath, 30)),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(30),
			progressbar.OptionThrottle(65),
			progressbar.OptionOnCompletion(func() { fmt.Fprintln(os.Stderr) }),
			progressbar.OptionSetWriter(os.Stderr),
		)
		dst = io.MultiWriter(f, bar)
	}

	if _, err := io.Copy(dst, resp.Body); err != nil {
		// Don't leave a truncated file behind on a failed transfer.
		_ = f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// UploadResult contains metadata returned by Waterbutler after a successful upload.
type UploadResult struct {
	Version int
	MD5     string
}

// Upload sends a local file to a Waterbutler upload URL, showing a progress
// bar unless quiet is true. Use BuildUploadURL for new files.
// Returns UploadResult with the new version number and MD5 hash.
func (c *WaterbutlerClient) Upload(ctx context.Context, srcPath, uploadURL string, quiet bool) (UploadResult, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return UploadResult{}, fmt.Errorf("opening %s: %w", srcPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return UploadResult{}, err
	}
	size := info.Size()

	var body io.Reader = f
	if !quiet {
		bar := progressbar.NewOptions64(size,
			progressbar.OptionSetDescription(truncateName(srcPath, 30)),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(30),
			progressbar.OptionThrottle(65),
			progressbar.OptionOnCompletion(func() { fmt.Fprintln(os.Stderr) }),
			progressbar.OptionSetWriter(os.Stderr),
		)
		r := progressbar.NewReader(f, bar)
		body = &r
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, body)
	if err != nil {
		return UploadResult{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	if auth := c.authHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadResult{}, parseAPIError(resp.StatusCode, respBody)
	}

	return parseUploadResult(respBody), nil
}

// parseUploadResult extracts version number and MD5 from a Waterbutler upload response.
// Returns zero value if the response cannot be parsed — callers handle gracefully.
func parseUploadResult(body []byte) UploadResult {
	var resp struct {
		Data struct {
			Attributes struct {
				Version int `json:"version"`
				Extra   struct {
					Hashes struct {
						MD5 string `json:"md5"`
					} `json:"hashes"`
				} `json:"extra"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return UploadResult{}
	}
	return UploadResult{
		Version: resp.Data.Attributes.Version,
		MD5:     resp.Data.Attributes.Extra.Hashes.MD5,
	}
}

// Delete sends a DELETE request to a Waterbutler delete URL.
func (c *WaterbutlerClient) Delete(ctx context.Context, deleteURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}
	if auth := c.authHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}
	return nil
}

// BuildUploadURL constructs the Waterbutler URL for uploading a *new* file.
//   - nodeID:     OSF project/component GUID
//   - parentPath: absolute path of the parent folder (e.g. "/" or "/data/results")
//   - filename:   name of the new file
//
// Set GOSF_FILES_BASE to override the Waterbutler base URL (useful in tests).
func BuildUploadURL(nodeID, parentPath, filename string) string {
	filesBase := os.Getenv("GOSF_FILES_BASE")
	if filesBase == "" {
		filesBase = wbBase
	}
	base := filesBase + "/v1/resources/" + nodeID + "/providers/osfstorage/"

	p := strings.Trim(parentPath, "/")
	if p != "" {
		parts := strings.Split(p, "/")
		encoded := make([]string, len(parts))
		for i, seg := range parts {
			encoded[i] = url.PathEscape(seg)
		}
		base += strings.Join(encoded, "/") + "/"
	}

	return base + "?name=" + url.QueryEscape(filename) + "&kind=file"
}

// RevisionURL appends ?revision=N (or &revision=N) to a Waterbutler download URL
// so that a specific historical version is fetched.
func RevisionURL(downloadURL string, revision int) string {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Sprintf("%s?revision=%d", downloadURL, revision)
	}
	q := u.Query()
	q.Set("revision", fmt.Sprintf("%d", revision))
	u.RawQuery = q.Encode()
	return u.String()
}

// Rename renames a file to newName within the same folder.
// moveURL is the file's Waterbutler move link.
func (c *WaterbutlerClient) Rename(ctx context.Context, moveURL, newName string) error {
	return c.postAction(ctx, moveURL, map[string]any{"action": "rename", "rename": newName})
}

// Move moves a file to a different folder (and optionally renames it).
// moveURL is the file's Waterbutler move link. destNodeID may be empty for
// same-project moves. destFolder is the destination folder path (e.g. "/results").
// newName is the filename at the destination; empty keeps the original name.
// conflict is "keep", "replace", or "warn" (Waterbutler default if empty).
func (c *WaterbutlerClient) Move(ctx context.Context, moveURL, destNodeID, destFolder, newName, conflict string) error {
	body := map[string]any{"action": "move", "path": destFolder}
	if newName != "" {
		body["rename"] = newName
	}
	if conflict != "" {
		body["conflict"] = conflict
	}
	if destNodeID != "" {
		body["resource"] = destNodeID
	}
	return c.postAction(ctx, moveURL, body)
}

// Copy copies a file to a destination folder.
// Parameters mirror Move; the original file is left untouched.
func (c *WaterbutlerClient) Copy(ctx context.Context, moveURL, destNodeID, destFolder, newName, conflict string) error {
	body := map[string]any{"action": "copy", "path": destFolder}
	if newName != "" {
		body["rename"] = newName
	}
	if conflict != "" {
		body["conflict"] = conflict
	}
	if destNodeID != "" {
		body["resource"] = destNodeID
	}
	return c.postAction(ctx, moveURL, body)
}

// CreateFolder creates a new folder inside parentPath within a node's OSF Storage.
func (c *WaterbutlerClient) CreateFolder(ctx context.Context, nodeID, parentPath, name string) error {
	u := BuildFolderURL(nodeID, parentPath, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, http.NoBody)
	if err != nil {
		return err
	}
	if auth := c.authHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}
	return nil
}

// postAction sends a JSON POST to actionURL with the given body map.
func (c *WaterbutlerClient) postAction(ctx context.Context, actionURL string, body map[string]any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, actionURL, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth := c.authHeader(); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, respBody)
	}
	return nil
}

// BuildFolderURL constructs the Waterbutler URL for creating a new folder.
func BuildFolderURL(nodeID, parentPath, name string) string {
	filesBase := os.Getenv("GOSF_FILES_BASE")
	if filesBase == "" {
		filesBase = wbBase
	}
	base := filesBase + "/v1/resources/" + nodeID + "/providers/osfstorage/"
	p := strings.Trim(parentPath, "/")
	if p != "" {
		parts := strings.Split(p, "/")
		encoded := make([]string, len(parts))
		for i, seg := range parts {
			encoded[i] = url.PathEscape(seg)
		}
		base += strings.Join(encoded, "/") + "/"
	}
	return base + "?name=" + url.QueryEscape(name) + "&kind=folder"
}

func truncateName(s string, max int) string {
	// Show only the last component for cleaner display
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		s = s[idx+1:]
	}
	if len(s) > max {
		return "…" + s[len(s)-max+1:]
	}
	return s
}
