package client

import "testing"

func TestBuildUploadURL(t *testing.T) {
	cases := []struct {
		nodeID     string
		parentPath string
		filename   string
		want       string
	}{
		{
			"abc12", "/", "file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/?name=file.csv&kind=file",
		},
		{
			"abc12", "", "file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/?name=file.csv&kind=file",
		},
		{
			"abc12", "/data", "result.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/data/?name=result.csv&kind=file",
		},
		{
			"abc12", "/data/results", "out.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/data/results/?name=out.csv&kind=file",
		},
		{
			"abc12", "/path with spaces", "my file.csv",
			"https://files.osf.io/v1/resources/abc12/providers/osfstorage/path%20with%20spaces/?name=my+file.csv&kind=file",
		},
	}

	for _, tc := range cases {
		got := BuildUploadURL(tc.nodeID, tc.parentPath, tc.filename)
		if got != tc.want {
			t.Errorf("BuildUploadURL(%q, %q, %q)\n  got  %s\n  want %s",
				tc.nodeID, tc.parentPath, tc.filename, got, tc.want)
		}
	}
}
