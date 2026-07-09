package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var statusNoCheckRemote bool

var statusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show sync status of all manifest entries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		manifestPath, repoRoot, err := manifest.FindManifest()
		if manifest.IsNotFound(err) {
			return fmt.Errorf("no .gosf/gosf.toml found — run 'gosf init <project-id>' to start tracking this repo, then 'gosf add' / 'gosf pull' to register files")
		}
		if err != nil {
			return err
		}

		m, err := manifest.Load(manifestPath)
		if err != nil {
			return err
		}

		token := config.LoadToken(flagToken)
		osfClient := client.New(token)
		res := resolver.New(osfClient)

		jsonMode := flagOutput == "json"
		allInSync := true

		jsonItems := make([]output.StatusItem, 0)
		var rows [][]output.Cell

		var sp *output.Spinner
		if !statusNoCheckRemote && !jsonMode {
			sp = output.NewSpinner("Checking remote…")
		}

		for _, entry := range m.Files {
			localAbs := filepath.Join(repoRoot, entry.Local)
			localMD5, err := computeLocalMD5(localAbs)
			if err != nil {
				return fmt.Errorf("computing MD5 for %s: %w", entry.Local, err)
			}

			// Fetch remote versions even for unpinned (version=0) entries so an
			// entry that is already byte-identical to the remote is reported as
			// content-in-sync rather than a blanket "never pushed". Status is
			// read-only: it reports, it never mutates the manifest.
			var remoteVersions []manifest.RemoteVersion
			if !statusNoCheckRemote {
				proj := entry.ResolveProject(m.Project.ID)
				item, resolveErr := res.Resolve(cmd.Context(), proj, entry.Remote)
				if resolveErr == nil {
					fvs, fetchErr := osfClient.GetFileVersions(cmd.Context(), item.ID)
					if fetchErr == nil {
						remoteVersions = fileVersionsToRemote(fvs)
					}
				}
			}

			state := manifest.ClassifyFile(entry, localMD5, remoteVersions, statusNoCheckRemote)
			if !statusIsInSync(state) {
				allInSync = false
			}

			if jsonMode {
				jsonItems = append(jsonItems, buildStatusItem(entry, state, remoteVersions))
			} else {
				statusStr, detail := stateDisplay(state, entry, remoteVersions)
				rows = append(rows, []output.Cell{
					{Text: entry.Direction},
					{Text: statusStr, Style: stateStyle(state)},
					{Text: entry.Local},
					{Text: verLabel(entry.Version)},
					{Text: detail, Style: output.Dim},
				})
			}
		}

		if sp != nil {
			sp.Stop()
		}

		if jsonMode {
			if err := output.PrintJSON(os.Stdout, jsonItems); err != nil {
				return err
			}
		} else {
			output.RenderTable(os.Stdout, []string{"DIR", "STATUS", "LOCAL PATH", "VER", "DETAIL"}, rows)
		}

		if !allInSync {
			return &exitCodeError{code: 1}
		}
		return nil
	},
}

// stateStyle maps a file state to the color applied to its status glyph.
// Returns nil (no styling) for states without a distinct color.
func stateStyle(state manifest.FileState) func(string) string {
	switch state {
	case manifest.StateInSync:
		return output.Green
	case manifest.StateMissing:
		return output.Red
	case manifest.StateDivergent:
		return output.RedBold
	case manifest.StateBehind, manifest.StateAheadOfManifest:
		return output.Yellow
	case manifest.StateRemoteNewer:
		return output.Cyan
	case manifest.StatePinOnly, manifest.StateNotPushed:
		return output.Dim
	default:
		return nil
	}
}

// stateDisplay returns the display string and detail for a file state.
func stateDisplay(state manifest.FileState, entry manifest.Entry, remoteVersions []manifest.RemoteVersion) (status, detail string) {
	switch state {
	case manifest.StateInSync:
		return "✓", ""
	case manifest.StateMissing:
		return "✗", "missing locally"
	case manifest.StateBehind:
		latest := latestRemoteVersion(remoteVersions)
		return "BEHIND", fmt.Sprintf("remote is v%d", latest)
	case manifest.StateAheadOfManifest:
		if entry.Direction == "pull" {
			return "~", "locally modified"
		}
		return "AHEAD", "unpushed changes"
	case manifest.StateRemoteNewer:
		latest := latestRemoteVersion(remoteVersions)
		return "↑", fmt.Sprintf("remote has v%d", latest)
	case manifest.StateNotPushed:
		return "·", "never pushed"
	case manifest.StatePinOnly:
		latest := latestRemoteVersion(remoteVersions)
		return "≡", fmt.Sprintf("identical to remote v%d, unpinned — run sync to pin", latest)
	case manifest.StateDivergent:
		return "DIVERGED", fmt.Sprintf("local and remote both changed since v%d — resolve with --resolve", entry.Version)
	default:
		return "?", ""
	}
}

// statusIsInSync reports whether a state should count as fully in sync for the
// CI-friendly exit code. Only IN_SYNC is fully in sync; PIN_ONLY (content
// matches but the manifest pin is stale) and everything else signal work to do.
func statusIsInSync(state manifest.FileState) bool {
	return state == manifest.StateInSync
}

func verLabel(version int) string {
	if version == 0 {
		return "—"
	}
	return fmt.Sprintf("v%d", version)
}

func init() {
	statusCmd.Flags().BoolVar(&statusNoCheckRemote, "no-check-remote", false, "Skip remote version lookups (faster, but cannot detect BEHIND or REMOTE_NEWER)")
	rootCmd.AddCommand(statusCmd)
}

func buildStatusItem(entry manifest.Entry, state manifest.FileState, remoteVersions []manifest.RemoteVersion) output.StatusItem {
	item := output.StatusItem{
		Path:            entry.Local,
		Direction:       entry.Direction,
		State:           state.String(),
		DeclaredVersion: entry.Version,
	}
	if len(remoteVersions) > 0 {
		item.RemoteLatestVersion = latestRemoteVersion(remoteVersions)
	}
	return item
}
