package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

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
			return fmt.Errorf("no gosf.toml found — run 'gosf add' to create one")
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
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if !jsonMode {
			fmt.Fprintln(w, "DIR\tSTATUS\tLOCAL PATH\tVER\tDETAIL")
		}

		var jsonItems []statusJSONItem

		for _, entry := range m.Files {
			localAbs := filepath.Join(repoRoot, entry.Local)
			localMD5, err := computeLocalMD5(localAbs)
			if err != nil {
				return fmt.Errorf("computing MD5 for %s: %w", entry.Local, err)
			}

			var remoteVersions []manifest.RemoteVersion
			if !statusNoCheckRemote && entry.Version > 0 {
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
			if state != manifest.StateInSync {
				allInSync = false
			}

			if jsonMode {
				jsonItems = append(jsonItems, buildStatusJSON(entry, state, remoteVersions))
			} else {
				statusStr, detail := stateDisplay(state, entry, remoteVersions)
				verStr := verLabel(entry.Version)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					entry.Direction, statusStr, entry.Local, verStr, detail)
			}
		}

		if jsonMode {
			if err := output.PrintJSON(os.Stdout, jsonItems); err != nil {
				return err
			}
		} else {
			w.Flush()
		}

		if !allInSync {
			return &exitCodeError{code: 1}
		}
		return nil
	},
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
	default:
		return "?", ""
	}
}

func verLabel(version int) string {
	if version == 0 {
		return "—"
	}
	return fmt.Sprintf("v%d", version)
}

// exitCodeError is an error that carries a specific exit code.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("exit code %d", e.code)
}

func init() {
	statusCmd.Flags().BoolVar(&statusNoCheckRemote, "no-check-remote", false, "Skip remote version lookups (faster, but cannot detect BEHIND or REMOTE_NEWER)")
	rootCmd.AddCommand(statusCmd)
}

// JSON output for status (one object per file).
type statusJSONItem struct {
	Path                string `json:"path"`
	Direction           string `json:"direction"`
	State               string `json:"state"`
	DeclaredVersion     int    `json:"declared_version"`
	RemoteLatestVersion int    `json:"remote_latest_version,omitempty"`
}

func buildStatusJSON(entry manifest.Entry, state manifest.FileState, remoteVersions []manifest.RemoteVersion) statusJSONItem {
	item := statusJSONItem{
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
