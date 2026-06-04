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
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var (
	pushDryRun  bool
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
		wbClient := client.NewWaterbutler(token)

		srcInfo, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}

		if srcInfo.IsDir() {
			return pushDir(cmd.Context(), osfClient, wbClient, src, target.NodeID, target.Path)
		}
		return pushFile(cmd.Context(), osfClient, wbClient, src, target.NodeID, target.Path)
	},
}

// pushFile uploads a single local file to an exact OSF destination path.
func pushFile(
	ctx context.Context,
	osfClient *client.OSFClient,
	wb *client.WaterbutlerClient,
	srcPath, nodeID, destPath string,
) error {
	// Split destination into parent dir and filename.
	parentDir, filename := deriveUploadTarget(destPath, srcPath)

	// List parent dir to detect conflicts.
	res := resolver.New(osfClient)
	existing, err := res.ListDir(ctx, nodeID, parentDir)
	if err != nil {
		return fmt.Errorf("listing destination directory: %w", err)
	}

	// Check for an existing file with the same name.
	var existingItem *client.FileItem
	for i := range existing {
		if existing[i].Attributes.Name == filename && existing[i].Attributes.Kind == "file" {
			existingItem = &existing[i]
			break
		}
	}

	uploadURL, resolvedName, err := resolveUploadURL(ctx, osfClient, existingItem, nodeID, parentDir, filename, existing)
	if err != nil {
		return err
	}

	if pushDryRun {
		if existingItem != nil {
			fmt.Printf("[dry-run] would overwrite /%s/%s\n", strings.TrimPrefix(parentDir, "/"), resolvedName)
		} else {
			fmt.Printf("[dry-run] would upload → /%s/%s\n", strings.TrimPrefix(parentDir, "/"), resolvedName)
		}
		return nil
	}

	if !flagQuiet {
		action := "uploading"
		if existingItem != nil && pushConflict == "overwrite" {
			action = "overwriting"
		}
		fmt.Fprintf(os.Stderr, "%s %s → %s/%s\n", action, srcPath, parentDir, resolvedName)
	}

	return wb.Upload(ctx, srcPath, uploadURL, flagQuiet)
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
	p = "/" + strings.Trim(p, "/")
	return p
}

// resolveUploadURL decides which Waterbutler URL to use based on conflict mode,
// and returns (uploadURL, resolvedFilename, error).
func resolveUploadURL(
	ctx context.Context,
	_ *client.OSFClient,
	existingItem *client.FileItem,
	nodeID, parentDir, filename string,
	siblings []client.FileItem,
) (string, string, error) {
	if existingItem == nil {
		// New file.
		return client.BuildUploadURL(nodeID, parentDir, filename), filename, nil
	}

	switch pushConflict {
	case "skip":
		return "", filename, &skipError{name: filename}
	case "overwrite":
		return existingItem.Links.Upload, filename, nil
	case "rename":
		name := findFreeName(filename, siblings)
		return client.BuildUploadURL(nodeID, parentDir, name), name, nil
	}
	return "", "", fmt.Errorf("unknown conflict mode: %s", pushConflict)
}

// skipError signals that a file was intentionally skipped.
type skipError struct{ name string }

func (e *skipError) Error() string { return fmt.Sprintf("skipped %s (already exists)", e.name) }

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

// pushDir walks a local directory and uploads all files under destPath.
func pushDir(
	ctx context.Context,
	osfClient *client.OSFClient,
	wb *client.WaterbutlerClient,
	srcDir, nodeID, destPath string,
) error {
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

		osfDest := filepath.Join(destPath, rel)
		// Use forward slashes for OSF paths.
		osfDest = filepath.ToSlash(osfDest)

		if pushErr := pushFile(ctx, osfClient, wb, path, nodeID, osfDest); pushErr != nil {
			if _, ok := pushErr.(*skipError); ok {
				if !flagQuiet {
					fmt.Fprintf(os.Stderr, "skip  %s (already exists)\n", osfDest)
				}
				return nil
			}
			return pushErr
		}
		return nil
	})
}

func init() {
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Show what would be uploaded without uploading")
	pushCmd.Flags().StringVar(&pushConflict, "conflict", "skip", "Conflict resolution: skip, overwrite, or rename")
	rootCmd.AddCommand(pushCmd)
}
