package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var pullDryRun bool

var pullCmd = &cobra.Command{
	Use:   "pull <project>[:<path>] [dest]",
	Short: "Download files from an OSF project",
	Long: `Download files from an OSF project to a local destination.

If <path> resolves to a single file, it is written to dest (default: the
filename in the current directory). If <path> resolves to a folder (or is
omitted), the entire tree is downloaded into dest (default: current directory).

Examples:
  gosf pull abc12:/data/results/file.csv          # → ./file.csv
  gosf pull abc12:/data/results/file.csv out.csv  # → ./out.csv
  gosf pull abc12:/data/                          # → ./data/... (all files)
  gosf pull abc12:/data/ ./local-copy             # → ./local-copy/...
  gosf pull abc12:                                # download entire project`,
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := resolver.ParseTarget(args[0])
		if err != nil {
			return err
		}

		dest := "."
		if len(args) == 2 {
			dest = args[1]
		}

		token := config.LoadToken(flagToken)
		osfClient := client.New(token)
		wbClient := client.NewWaterbutler(token)
		res := resolver.New(osfClient)

		items, err := res.ListDir(cmd.Context(), target.NodeID, target.Path)
		if err != nil {
			return err
		}

		// Single file?
		if len(items) == 1 && items[0].Attributes.Kind == "file" {
			item := items[0]
			destPath := dest
			// If dest looks like a directory (exists or ends in /), use filename.
			if info, err := os.Stat(dest); err == nil && info.IsDir() {
				destPath = filepath.Join(dest, item.Attributes.Name)
			} else if dest == "." {
				destPath = item.Attributes.Name
			}
			return pullFile(cmd.Context(), wbClient, item, destPath)
		}

		// Directory / multi-file: download tree into dest.
		return pullTree(cmd.Context(), osfClient, wbClient, res, items, target.NodeID, dest)
	},
}

// pullFile downloads a single file item to destPath.
func pullFile(ctx context.Context, wb *client.WaterbutlerClient, item client.FileItem, destPath string) error {
	if pullDryRun {
		fmt.Printf("[dry-run] would download → %s\n", destPath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	return wb.Download(ctx, item.Links.Download, destPath, item.Attributes.Size, flagQuiet)
}

// pullTree recursively downloads a slice of items into destDir.
func pullTree(
	ctx context.Context,
	osfClient *client.OSFClient,
	wb *client.WaterbutlerClient,
	res *resolver.Resolver,
	items []client.FileItem,
	nodeID, destDir string,
) error {
	for _, item := range items {
		localPath := filepath.Join(destDir, item.Attributes.Name)

		if item.Attributes.Kind == "folder" {
			children, err := osfClient.ListFilesFromURL(ctx, item.Relationships.Files.Links.Related.Href)
			if err != nil {
				return fmt.Errorf("listing %s: %w", item.Attributes.Name, err)
			}
			if err := pullTree(ctx, osfClient, wb, res, children, nodeID, localPath); err != nil {
				return err
			}
			continue
		}

		if err := pullFile(ctx, wb, item, localPath); err != nil {
			return fmt.Errorf("downloading %s: %w", item.Attributes.Name, err)
		}
	}
	return nil
}

func init() {
	pullCmd.Flags().BoolVar(&pullDryRun, "dry-run", false, "Show what would be downloaded without downloading")
	rootCmd.AddCommand(pullCmd)
}
