package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var (
	rmDryRun bool
	rmYes    bool
)

var rmCmd = &cobra.Command{
	Use:   "rm <project>:<path>",
	Short: "Delete a file or folder from an OSF project",
	Long: `Delete a file or folder from an OSF project's storage.

Deleting a folder removes it and all its contents. You will be prompted
for confirmation unless --yes is supplied.

Examples:
  gosf rm abc12:/data/old-results.csv
  gosf rm abc12:/scratch/ --yes
  gosf rm abc12:/data/file.csv --dry-run`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := resolver.ParseTarget(args[0])
		if err != nil {
			return err
		}
		if target.Path == "/" || target.Path == "" {
			return fmt.Errorf("cowardly refusing to delete the root of a project")
		}

		token := config.LoadToken(flagToken)
		if token == "" {
			return fmt.Errorf("rm requires authentication — run 'gosf auth login' or set OSF_TOKEN")
		}

		osfClient := client.New(token)
		res := resolver.New(osfClient)

		// Resolve path to the target item.
		items, err := res.ListDir(context.Background(), target.NodeID, target.Path)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return fmt.Errorf("path not found: %s", target.Path)
		}

		// If path resolved to a directory listing (multiple items), the path itself
		// IS the folder we want to delete — we need its delete link. Resolve
		// the parent dir to find it.
		item, err := resolveItemAtPath(context.Background(), osfClient, res, target.NodeID, target.Path)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", target.Path, err)
		}

		if item.Links.Delete == "" {
			return fmt.Errorf("item has no delete URL (may not support deletion via API)")
		}

		label := target.Path
		if item.Attributes.Kind == "folder" {
			label += " (and all contents)"
		}

		if rmDryRun {
			fmt.Fprintf(os.Stdout, "[dry-run] would delete %s from project %s\n", label, target.NodeID)
			return nil
		}

		if !rmYes {
			if !confirm(fmt.Sprintf("Delete %s from project %s?", label, target.NodeID)) {
				fmt.Fprintln(os.Stdout, "Aborted.")
				return nil
			}
		}

		wb := client.NewWaterbutler(token)
		if err := wb.Delete(context.Background(), item.Links.Delete); err != nil {
			return fmt.Errorf("deleting %s: %w", target.Path, err)
		}

		if !flagQuiet {
			fmt.Fprintf(os.Stdout, "Deleted %s\n", target.Path)
		}
		return nil
	},
}

// resolveItemAtPath returns the FileItem for the exact path (not its children).
// For a file path, ListDir already returns the single item. For a folder path,
// we need to look it up in the parent directory listing.
func resolveItemAtPath(
	ctx context.Context,
	osfClient *client.OSFClient,
	res *resolver.Resolver,
	nodeID, path string,
) (client.FileItem, error) {
	// Try as a file first (ListDir returns single item for files).
	items, err := res.ListDir(ctx, nodeID, path)
	if err != nil {
		return client.FileItem{}, err
	}
	if len(items) == 1 && items[0].Attributes.Kind == "file" {
		return items[0], nil
	}

	// It's a folder — find it in the parent listing.
	parentPath, folderName := splitPath(path)
	parentItems, err := res.ListDir(ctx, nodeID, parentPath)
	if err != nil {
		return client.FileItem{}, err
	}
	for _, item := range parentItems {
		if item.Attributes.Name == folderName {
			return item, nil
		}
	}
	return client.FileItem{}, fmt.Errorf("item not found at %s", path)
}

// splitPath returns (parent, name) for a path like "/data/results".
func splitPath(path string) (parent, name string) {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndexByte(path, '/'); idx >= 0 {
		return path[:idx+1], path[idx+1:]
	}
	return "/", path
}

// confirm prints a [y/N] prompt and returns true if the user types y/yes.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

func init() {
	rmCmd.Flags().BoolVar(&rmDryRun, "dry-run", false, "Show what would be deleted without deleting")
	rmCmd.Flags().BoolVarP(&rmYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(rmCmd)
}
