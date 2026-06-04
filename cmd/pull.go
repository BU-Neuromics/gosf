package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var pullDryRun bool
var pullVersion int

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
		res := resolver.New(osfClient)

		items, err := res.ListDir(cmd.Context(), target.NodeID, target.Path)
		if err != nil {
			return err
		}

		// Validate --version before doing any transfer work.
		if pullVersion > 0 {
			if len(items) != 1 || items[0].Attributes.Kind != "file" {
				return fmt.Errorf("--version requires a single file target, not a directory")
			}
			versions, err := osfClient.GetFileVersions(cmd.Context(), items[0].ID)
			if err != nil {
				return fmt.Errorf("fetching versions: %w", err)
			}
			if err := validateVersion(versions, pullVersion); err != nil {
				return err
			}
		}

		jsonMode := flagOutput == "json"
		s := &pullSession{
			ctx:      cmd.Context(),
			osf:      osfClient,
			wb:       client.NewWaterbutler(token),
			result:   output.NewPullResult(pullDryRun),
			jsonMode: jsonMode,
			quiet:    flagQuiet || jsonMode, // don't interleave progress bars with JSON
			dryRun:   pullDryRun,
			version:  pullVersion,
		}

		// Single file?
		if len(items) == 1 && items[0].Attributes.Kind == "file" {
			item := items[0]
			destPath := dest
			if info, err := os.Stat(dest); err == nil && info.IsDir() {
				destPath = filepath.Join(dest, item.Attributes.Name)
			} else if dest == "." {
				destPath = item.Attributes.Name
			}
			if err := s.file(item, destPath); err != nil {
				return err
			}
		} else {
			if err := s.tree(items, dest); err != nil {
				return err
			}
		}

		if jsonMode {
			return output.PrintJSON(os.Stdout, s.result)
		}
		if len(s.result.Downloaded) == 0 && !flagQuiet {
			fmt.Fprintln(os.Stderr, "Nothing to download (no files at that path).")
		}
		return nil
	},
}

// pullSession carries the shared state for one pull invocation.
type pullSession struct {
	ctx      context.Context
	osf      *client.OSFClient
	wb       *client.WaterbutlerClient
	result   *output.PullResult
	jsonMode bool
	quiet    bool
	dryRun   bool
	version  int // 0 = latest; >0 = specific version number
}

// file downloads a single file item to destPath, recording it in the result.
func (s *pullSession) file(item client.FileItem, destPath string) error {
	if s.dryRun {
		s.result.Add(destPath, item.Attributes.Size)
		if !s.jsonMode {
			fmt.Printf("[dry-run] would download → %s\n", destPath)
		}
		return nil
	}

	if item.Links.Download == "" {
		return fmt.Errorf("no download URL for %q", item.Attributes.Name)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}
	dlURL := item.Links.Download
	if s.version > 0 {
		dlURL = client.RevisionURL(dlURL, s.version)
	}
	if err := s.wb.Download(s.ctx, dlURL, destPath, item.Attributes.Size, s.quiet); err != nil {
		return err
	}
	s.result.Add(destPath, item.Attributes.Size)
	return nil
}

// tree recursively downloads a slice of items into destDir.
func (s *pullSession) tree(items []client.FileItem, destDir string) error {
	for _, item := range items {
		localPath := filepath.Join(destDir, item.Attributes.Name)

		if item.Attributes.Kind == "folder" {
			children, err := s.osf.ListFilesFromURL(s.ctx, item.Relationships.Files.Links.Related.Href)
			if err != nil {
				return fmt.Errorf("listing %s: %w", item.Attributes.Name, err)
			}
			if err := s.tree(children, localPath); err != nil {
				return err
			}
			continue
		}

		if err := s.file(item, localPath); err != nil {
			return fmt.Errorf("downloading %s: %w", item.Attributes.Name, err)
		}
	}
	return nil
}

// validateVersion checks that version n exists in the list returned by GetFileVersions.
func validateVersion(versions []client.FileVersion, n int) error {
	for _, v := range versions {
		if v.Attributes.Version == n {
			return nil
		}
	}
	if len(versions) == 0 {
		return fmt.Errorf("no versions found for this file")
	}
	return fmt.Errorf("version %d not found; run 'gosf versions <path>' to see available versions", n)
}

func init() {
	pullCmd.Flags().BoolVar(&pullDryRun, "dry-run", false, "Show what would be downloaded without downloading")
	pullCmd.Flags().IntVar(&pullVersion, "version", 0, "Download a specific version number (0 = latest)")
	rootCmd.AddCommand(pullCmd)
}
