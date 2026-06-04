package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var (
	pushDryRun   bool
	pushConflict string
)

var pushCmd = &cobra.Command{
	Use:   "push <src> <project>:<path>",
	Short: "Upload a file or directory to an OSF project",
	Long: `Upload a local file or directory to an OSF project.

<path> must include a filename when uploading a single file.
When <src> is a directory, all files in it are uploaded under <path>,
preserving the relative directory structure.

Conflict behaviour (--conflict):
  skip      (default) Skip files that already exist at the destination.
  overwrite           Replace existing files.
  rename              Append _1, _2, … to find a free name.

Examples:
  gosf push results.csv abc12:/data/results.csv
  gosf push ./results/  abc12:/data/
  gosf push data.csv    abc12:/data/data.csv --conflict=overwrite`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		src := args[0]
		target, err := resolver.ParseTarget(args[1])
		if err != nil {
			return err
		}

		switch pushConflict {
		case "skip", "overwrite", "rename":
		default:
			return fmt.Errorf("--conflict must be skip, overwrite, or rename; got %q", pushConflict)
		}

		token := config.LoadToken(flagToken)
		if token == "" {
			return fmt.Errorf("push requires authentication — run 'gosf auth login' or set OSF_TOKEN")
		}

		osfClient := client.New(token)

		srcInfo, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}

		jsonMode := flagOutput == "json"
		s := &pushSession{
			ctx:      cmd.Context(),
			res:      resolver.New(osfClient),
			wb:       client.NewWaterbutler(token),
			result:   output.NewPushResult(pushDryRun),
			jsonMode: jsonMode,
			quiet:    flagQuiet || jsonMode,
			dryRun:   pushDryRun,
			conflict: pushConflict,
		}

		if srcInfo.IsDir() {
			err = s.dir(src, target.NodeID, target.Path)
		} else {
			err = s.file(src, target.NodeID, target.Path)
		}
		if err != nil {
			return err
		}

		if jsonMode {
			return output.PrintJSON(os.Stdout, s.result)
		}
		if len(s.result.Uploaded) == 0 && !flagQuiet {
			fmt.Fprintln(os.Stderr, "Nothing to upload (no files at source).")
		}
		return nil
	},
}

// pushSession carries the shared state for one push invocation.
type pushSession struct {
	ctx      context.Context
	res      *resolver.Resolver
	wb       *client.WaterbutlerClient
	result   *output.PushResult
	jsonMode bool
	quiet    bool
	dryRun   bool
	conflict string
}

// file uploads a single local file to an OSF destination path.
func (s *pushSession) file(srcPath, nodeID, destPath string) error {
	parentDir, filename := deriveUploadTarget(destPath, srcPath)

	existing, err := s.res.ListDir(s.ctx, nodeID, parentDir)
	if err != nil {
		return fmt.Errorf("destination folder %s is not accessible (does it exist?): %w", parentDir, err)
	}

	var existingItem *client.FileItem
	for i := range existing {
		if existing[i].Attributes.Name == filename && existing[i].Attributes.Kind == "file" {
			existingItem = &existing[i]
			break
		}
	}

	plan, err := planUpload(s.conflict, existingItem, nodeID, parentDir, filename, existing)
	if err != nil {
		return err
	}
	destFull := strings.TrimRight(parentDir, "/") + "/" + plan.name

	switch {
	case plan.action == "skip":
		s.result.Add(destFull, "skip")
		if !s.jsonMode && !s.quiet {
			fmt.Fprintf(os.Stderr, "skip  %s (already exists)\n", destFull)
		}
		return nil
	case s.dryRun:
		s.result.Add(destFull, plan.action)
		if !s.jsonMode {
			fmt.Printf("[dry-run] would %s → %s\n", plan.action, destFull)
		}
		return nil
	}

	if !s.quiet {
		fmt.Fprintf(os.Stderr, "%s %s → %s\n", plan.action, srcPath, destFull)
	}
	if err := s.wb.Upload(s.ctx, srcPath, plan.url, s.quiet); err != nil {
		return err
	}
	s.result.Add(destFull, plan.action)
	return nil
}

// dir walks a local directory and uploads all files under destPath,
// preserving the relative directory structure.
func (s *pushSession) dir(srcDir, nodeID, destPath string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		osfDest := filepath.ToSlash(filepath.Join(destPath, rel))
		return s.file(path, nodeID, osfDest)
	})
}

// uploadPlan describes how a single file upload should be carried out.
type uploadPlan struct {
	url    string // Waterbutler URL to PUT to; empty when action is "skip"
	name   string // final filename at the destination
	action string // "upload", "overwrite", "rename", or "skip"
}

// planUpload decides how to handle a file given the conflict mode and whether
// a file of the same name already exists at the destination. It is pure: all
// inputs are explicit, so it is fully unit-testable.
func planUpload(
	conflict string,
	existing *client.FileItem,
	nodeID, parentDir, filename string,
	siblings []client.FileItem,
) (uploadPlan, error) {
	if existing == nil {
		return uploadPlan{
			url:    client.BuildUploadURL(nodeID, parentDir, filename),
			name:   filename,
			action: "upload",
		}, nil
	}

	switch conflict {
	case "skip":
		return uploadPlan{name: filename, action: "skip"}, nil
	case "overwrite":
		return uploadPlan{url: existing.Links.Upload, name: filename, action: "overwrite"}, nil
	case "rename":
		name := findFreeName(filename, siblings)
		return uploadPlan{
			url:    client.BuildUploadURL(nodeID, parentDir, name),
			name:   name,
			action: "rename",
		}, nil
	}
	return uploadPlan{}, fmt.Errorf("unknown conflict mode: %s", conflict)
}

// deriveUploadTarget splits an OSF destination path into the parent directory
// and the filename to upload as.
//
//   - A trailing slash means the destination is a directory: the source file
//     keeps its own name (e.g. "/data/" + "in.csv" → "/data", "in.csv").
//   - Otherwise the last path segment is the explicit target filename
//     (e.g. "/data/out.csv" → "/data", "out.csv").
//
// An empty destination is treated as the storage root.
func deriveUploadTarget(destPath, srcPath string) (parentDir, filename string) {
	if destPath == "" {
		destPath = "/"
	}
	isDir := strings.HasSuffix(destPath, "/")
	destPath = "/" + strings.Trim(destPath, "/")

	if isDir {
		// Destination is a directory; keep the source's basename.
		return cleanParent(destPath), filepath.Base(srcPath)
	}

	parent := filepath.Dir(destPath)
	name := filepath.Base(destPath)
	if name == "." || name == "/" || name == "" {
		// No explicit filename — fall back to the source basename.
		return cleanParent(destPath), filepath.Base(srcPath)
	}
	return cleanParent(parent), name
}

// cleanParent normalises a parent directory path to start with "/" and have no
// trailing slash (except the root itself, which stays "/").
func cleanParent(p string) string {
	return "/" + strings.Trim(p, "/")
}

// findFreeName appends _1, _2, … to base until no sibling has that name.
func findFreeName(filename string, siblings []client.FileItem) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	existing := make(map[string]bool, len(siblings))
	for _, s := range siblings {
		existing[s.Attributes.Name] = true
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !existing[candidate] {
			return candidate
		}
	}
}

func init() {
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Show what would be uploaded without uploading")
	pushCmd.Flags().StringVar(&pushConflict, "conflict", "skip", "Conflict resolution: skip, overwrite, or rename")
	rootCmd.AddCommand(pushCmd)
}
