package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var addDirection string

var addCmd = &cobra.Command{
	Use:   "add <local-path> <project>:<remote-path>",
	Short: "Add a file to the gosf.toml sync manifest",
	Long: `Add a local file entry to gosf.toml.

The direction controls whether the file is pushed, pulled, or both.
Defaults to push if --direction is not specified.

Examples:
  gosf add data/raw/counts.h5 abc12:/data/raw/counts.h5
  gosf add results/model.pkl  abc12:/results/model.pkl  --direction=pull`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		localPath := args[0]
		target, err := resolver.ParseTarget(args[1])
		if err != nil {
			return err
		}

		switch addDirection {
		case "push", "pull", "both":
		default:
			return fmt.Errorf("--direction must be push, pull, or both; got %q", addDirection)
		}

		jsonMode := flagOutput == "json"

		// Find or create gosf.toml.
		manifestPath, repoRoot, err := manifest.FindManifest()
		var m *manifest.Manifest
		manifestCreated := false
		if manifest.IsNotFound(err) {
			manifestPath = "gosf.toml"
			m = &manifest.Manifest{}
			manifestCreated = true
			if !jsonMode {
				fmt.Fprintln(os.Stdout, "Created gosf.toml. Set [project].id before running sync.")
			}
		} else if err != nil {
			return err
		} else {
			m, err = manifest.Load(manifestPath)
			if err != nil {
				return err
			}
			_ = repoRoot
		}

		// Check for duplicate local path.
		if findEntryByLocal(m, localPath) >= 0 {
			return fmt.Errorf("entry with local path %q already exists in gosf.toml", localPath)
		}

		// Resolve project for this entry.
		proj := target.NodeID
		defaultProj := m.Project.ID
		entryProject := ""
		if proj != defaultProj && defaultProj != "" {
			entryProject = proj
		}
		if defaultProj == "" {
			entryProject = proj
		}

		// Check remote for existing file.
		token := config.LoadToken(flagToken)
		c := client.New(token)
		res := resolver.New(c)

		entry := manifest.Entry{
			Local:     localPath,
			Remote:    target.Path,
			Direction: addDirection,
			Project:   entryProject,
		}

		existingItem, resolveErr := res.Resolve(cmd.Context(), proj, target.Path)
		if resolveErr == nil {
			versions, fetchErr := c.GetFileVersions(cmd.Context(), existingItem.ID)
			if fetchErr == nil && len(versions) > 0 {
				latest := versions[0] // newest-first
				entry.Version = latest.Attributes.Version
				entry.MD5 = latest.Attributes.Extra.Hashes.MD5
			}
		}

		m.Files = append(m.Files, entry)

		if jsonMode {
			result := output.AddResult{
				Local:           localPath,
				Remote:          target.Path,
				Project:         proj,
				Direction:       addDirection,
				Version:         entry.Version,
				MD5:             entry.MD5,
				ManifestCreated: manifestCreated,
			}
			if err := manifest.Save(m, manifestPath); err != nil {
				return err
			}
			return output.PrintJSON(os.Stdout, result)
		}

		// Text output.
		if entry.Version > 0 {
			fmt.Fprintf(os.Stdout, "Added %s → %s:%s  (direction=%s, v%d)\n",
				localPath, proj, target.Path, addDirection, entry.Version)
		} else {
			fmt.Fprintf(os.Stdout, "Added %s → %s:%s  (direction=%s, not yet pushed)\n",
				localPath, proj, target.Path, addDirection)
		}

		if info, statErr := os.Stat(localPath); statErr == nil {
			if info.Size() > 50*1024*1024 {
				fmt.Fprintf(os.Stdout, "Tip: consider adding %s to .gitignore (large file, %s)\n",
					localPath, formatSizeMB(info.Size()))
			}
		} else if addDirection == "push" || addDirection == "both" {
			fmt.Fprintf(os.Stdout, "Note: local file not found. Create it before running gosf sync.\n")
		}

		return manifest.Save(m, manifestPath)
	},
}

// findEntryByLocal returns the index of the first entry with the given local path,
// or -1 if not found.
func findEntryByLocal(m *manifest.Manifest, local string) int {
	for i, e := range m.Files {
		if e.Local == local {
			return i
		}
	}
	return -1
}

func formatSizeMB(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/1024/1024)
}

func init() {
	addCmd.Flags().StringVar(&addDirection, "direction", "push", "Sync direction: push, pull, or both")
	rootCmd.AddCommand(addCmd)
}
