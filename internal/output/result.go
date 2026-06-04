package output

// This file defines the structured result types emitted by commands when
// --output=json is used. Keeping them in one place makes the JSON contract
// that scripting clients depend on explicit and testable.

// OpenResult is emitted by `gosf open --output=json`.
type OpenResult struct {
	URL string `json:"url"`
}

// RemoveResult is emitted by `gosf rm --output=json`.
type RemoveResult struct {
	Node   string `json:"node"`
	Path   string `json:"path"`
	Kind   string `json:"kind"` // "file" or "folder"
	DryRun bool   `json:"dry_run"`
}

// TransferItem describes one file moved by pull or push.
type TransferItem struct {
	Path   string `json:"path"`
	Size   int64  `json:"size,omitempty"`
	Action string `json:"action,omitempty"` // push: upload|overwrite|rename|skip
}

// PullResult is emitted by `gosf pull --output=json`.
type PullResult struct {
	Downloaded []TransferItem `json:"downloaded"`
	DryRun     bool           `json:"dry_run"`
}

// NewPullResult returns a PullResult with a non-nil slice so it serialises as
// [] rather than null when empty.
func NewPullResult(dryRun bool) *PullResult {
	return &PullResult{Downloaded: []TransferItem{}, DryRun: dryRun}
}

// Add appends a downloaded file to the result.
func (r *PullResult) Add(path string, size int64) {
	r.Downloaded = append(r.Downloaded, TransferItem{Path: path, Size: size})
}

// PushResult is emitted by `gosf push --output=json`.
type PushResult struct {
	Uploaded []TransferItem `json:"uploaded"`
	DryRun   bool           `json:"dry_run"`
}

// NewPushResult returns a PushResult with a non-nil slice so it serialises as
// [] rather than null when empty.
func NewPushResult(dryRun bool) *PushResult {
	return &PushResult{Uploaded: []TransferItem{}, DryRun: dryRun}
}

// Add appends an uploaded file (with the action taken) to the result.
func (r *PushResult) Add(path, action string) {
	r.Uploaded = append(r.Uploaded, TransferItem{Path: path, Action: action})
}

// VersionItem describes one version in the versions list.
type VersionItem struct {
	Version     int    `json:"version"`
	DateCreated string `json:"date_created"`
	Size        int64  `json:"size"`
	Contributor string `json:"contributor"`
}

// VersionsResult is emitted by `gosf versions --output=json`.
type VersionsResult struct {
	Versions []VersionItem `json:"versions"`
}

// NewVersionsResult returns a VersionsResult with a non-nil slice so it
// serialises as [] rather than null when empty.
func NewVersionsResult() *VersionsResult {
	return &VersionsResult{Versions: []VersionItem{}}
}
