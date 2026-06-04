package client

import (
	"context"
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
	return &WaterbutlerClient{
		token: token,
		// No timeout — callers stream large files.
		http: &http.Client{
			CheckRedirect: stripAuthOnCrossHost,
		},
	}
}

// stripAuthOnCrossHost removes the Authorization header when a redirect
// crosses to a different host (e.g. Waterbutler → S3).
func stripAuthOnCrossHost(req *http.Request, via []*http.Request) error {
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		req.Header.Del("Authorization")
	}
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	return nil
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
		f.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// Upload sends a local file to a Waterbutler upload URL, showing a progress
// bar unless quiet is true. Use BuildUploadURL for new files.
func (c *WaterbutlerClient) Upload(ctx context.Context, srcPath, uploadURL string, quiet bool) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", srcPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
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
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
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
func BuildUploadURL(nodeID, parentPath, filename string) string {
	base := wbBase + "/v1/resources/" + nodeID + "/providers/osfstorage/"

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
