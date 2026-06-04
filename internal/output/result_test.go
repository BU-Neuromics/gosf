package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

// roundTrip marshals v via PrintJSON and unmarshals into a generic map so we
// can assert on the exact JSON field names and values scripting clients see.
func roundTrip(t *testing.T, v any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := PrintJSON(&buf, v); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\njson was: %s", err, buf.String())
	}
	return out
}

func TestOpenResultJSON(t *testing.T) {
	m := roundTrip(t, OpenResult{URL: "https://osf.io/abc12/"})
	if m["url"] != "https://osf.io/abc12/" {
		t.Errorf("url field = %v", m["url"])
	}
}

func TestRemoveResultJSON(t *testing.T) {
	m := roundTrip(t, RemoveResult{Node: "abc12", Path: "/data/x.csv", Kind: "file", DryRun: true})
	if m["node"] != "abc12" || m["path"] != "/data/x.csv" || m["kind"] != "file" {
		t.Errorf("unexpected fields: %+v", m)
	}
	if m["dry_run"] != true {
		t.Errorf("dry_run = %v, want true", m["dry_run"])
	}
}

func TestPullResultJSON(t *testing.T) {
	r := PullResult{
		Downloaded: []TransferItem{{Path: "out.csv", Size: 10}},
		DryRun:     false,
	}
	m := roundTrip(t, r)
	if m["dry_run"] != false {
		t.Errorf("dry_run = %v", m["dry_run"])
	}
	dl, ok := m["downloaded"].([]any)
	if !ok || len(dl) != 1 {
		t.Fatalf("downloaded = %v", m["downloaded"])
	}
	first := dl[0].(map[string]any)
	if first["path"] != "out.csv" || first["size"].(float64) != 10 {
		t.Errorf("transfer item = %+v", first)
	}
}

func TestPushResultJSON(t *testing.T) {
	r := PushResult{
		Uploaded: []TransferItem{{Path: "/data/x.csv", Action: "upload"}},
		DryRun:   false,
	}
	m := roundTrip(t, r)
	up, ok := m["uploaded"].([]any)
	if !ok || len(up) != 1 {
		t.Fatalf("uploaded = %v", m["uploaded"])
	}
	first := up[0].(map[string]any)
	if first["path"] != "/data/x.csv" || first["action"] != "upload" {
		t.Errorf("transfer item = %+v", first)
	}
}

// TestEmptyTransferSlicesAreArraysNotNull guards scripting consumers: an empty
// result must serialise as [] rather than null.
func TestEmptyTransferSlicesAreArraysNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSON(&buf, NewPullResult(false)); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}
	if got := buf.String(); !bytes.Contains([]byte(got), []byte(`"downloaded": []`)) {
		t.Errorf("empty downloaded should serialise as [], got: %s", got)
	}
}
