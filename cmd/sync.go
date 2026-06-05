package cmd

import (
	"context"
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

var (
	syncForce         bool
	syncDryRun        bool
	syncNoCheckRemote bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local files with OSF according to gosf.toml",
	Long: `Sync files declared in gosf.toml between local storage and OSF.

Default behavior: push all push-eligible entries (direction=push or direction=both)
and pull all pull-eligible entries (direction=pull or direction=both) that are
missing or behind.

Examples:
  gosf sync                         # push and pull all tracked files
  gosf sync --force                 # overwrite locally modified pull files
  gosf sync --dry-run               # show what would happen
  gosf sync --no-check-remote       # faster, but skips BEHIND/REMOTE_NEWER detection`,
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

		if m.Project.ID == "" {
			return fmt.Errorf("no project configured — run: gosf init <project-id>")
		}

		token := config.LoadToken(flagToken)
		if token == "" {
			return fmt.Errorf("sync requires authentication — run 'gosf auth login' or set OSF_TOKEN")
		}

		osfClient := client.New(token)
		wb := client.NewWaterbutler(token)
		res := resolver.New(osfClient)

		jsonMode := flagOutput == "json"
		quiet := flagQuiet || jsonMode

		jsonResults := make([]output.SyncItem, 0)
		manifestChanged := false

		for i := range m.Files {
			entry := &m.Files[i]
			proj := entry.ResolveProject(m.Project.ID)
			localAbs := filepath.Join(repoRoot, entry.Local)

			localMD5, err := computeLocalMD5(localAbs)
			if err != nil {
				return fmt.Errorf("computing MD5 for %s: %w", entry.Local, err)
			}

			isPushEligible := entry.Direction == "push" || entry.Direction == "both"
			isPullEligible := entry.Direction == "pull" || entry.Direction == "both"

			var remoteVersions []manifest.RemoteVersion
			var resolvedItem *client.FileItem
			// Always resolve pull-eligible entries so processPullEntry has a download URL.
			// Skip the resolve for push-only entries when --no-check-remote is set.
			if entry.Version > 0 && (!syncNoCheckRemote || isPullEligible) {
				item, resolveErr := res.Resolve(cmd.Context(), proj, entry.Remote)
				if resolveErr == nil {
					resolvedItem = &item
					if !syncNoCheckRemote {
						fvs, fetchErr := osfClient.GetFileVersions(cmd.Context(), item.ID)
						if fetchErr == nil {
							remoteVersions = fileVersionsToRemote(fvs)
						}
					}
				}
			}

			state := manifest.ClassifyFile(*entry, localMD5, remoteVersions, syncNoCheckRemote)

			if isPushEligible {
				action, changed, err := processPushEntry(cmd.Context(), entry, proj, localAbs, state, res, wb, osfClient, quiet, jsonMode, syncDryRun)
				if err != nil {
					return err
				}
				if changed {
					manifestChanged = true
				}
				if jsonMode {
					jsonResults = append(jsonResults, makeSyncItem(entry, state, action, remoteVersions))
				}
				continue
			}

			if isPullEligible {
				action, changed, err := processPullEntry(cmd.Context(), entry, proj, localAbs, state, resolvedItem, wb, osfClient, repoRoot, quiet, jsonMode, syncDryRun, syncForce)
				if err != nil {
					return err
				}
				if changed {
					manifestChanged = true
				}
				if jsonMode {
					jsonResults = append(jsonResults, makeSyncItem(entry, state, action, remoteVersions))
				}
			}
		}

		if manifestChanged && !syncDryRun {
			if err := manifest.Save(m, manifestPath); err != nil {
				return fmt.Errorf("saving manifest: %w", err)
			}
		}

		if jsonMode {
			return output.PrintJSON(os.Stdout, jsonResults)
		}

		return nil
	},
}

// processPushEntry handles a push-eligible entry. Returns the action taken,
// whether the manifest was mutated, and any error.
func processPushEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs string,
	state manifest.FileState,
	res *resolver.Resolver,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	quiet, jsonMode, dryRun bool,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		if !quiet {
			fmt.Printf("✓  %s (v%d)\n", entry.Local, entry.Version)
		}
		return "in_sync", false, nil

	case manifest.StateMissing:
		if !quiet {
			fmt.Printf("✗  %s  missing locally, skipping\n", entry.Local)
		}
		return "skipped_missing", false, nil

	case manifest.StateNotPushed:
		if _, statErr := os.Stat(localAbs); os.IsNotExist(statErr) {
			if !quiet {
				fmt.Printf("·  %s  not found locally, skipping\n", entry.Local)
			}
			return "skipped_not_found", false, nil
		}
		return pushFile(ctx, entry, proj, localAbs, 0, res, wb, quiet, jsonMode, dryRun)

	case manifest.StateAheadOfManifest, manifest.StateBehind, manifest.StateRemoteNewer:
		return pushFile(ctx, entry, proj, localAbs, entry.Version, res, wb, quiet, jsonMode, dryRun)
	}
	return "noop", false, nil
}

// pushFile resolves the upload URL and uploads the file, updating the entry.
func pushFile(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs string,
	currentVersion int,
	res *resolver.Resolver,
	wb *client.WaterbutlerClient,
	quiet, jsonMode, dryRun bool,
) (action string, changed bool, err error) {
	oldVer := entry.Version

	if dryRun {
		if currentVersion == 0 {
			if !jsonMode {
				fmt.Printf("[dry-run] ↑  %s  (first push → v1)\n", entry.Local)
			}
		} else {
			if !jsonMode {
				fmt.Printf("[dry-run] ↑  %s  v%d → v%d\n", entry.Local, oldVer, oldVer+1)
			}
		}
		return "push", false, nil
	}

	// Resolve the upload URL.
	uploadURL, uploadErr := resolveUploadURL(ctx, entry, proj, res)
	if uploadErr != nil {
		return "", false, fmt.Errorf("resolving upload URL for %s: %w", entry.Local, uploadErr)
	}

	result, uploadErr := wb.Upload(ctx, localAbs, uploadURL, quiet)
	if uploadErr != nil {
		return "", false, fmt.Errorf("uploading %s: %w", entry.Local, uploadErr)
	}

	newVer := result.Version
	if newVer == 0 {
		newVer = oldVer + 1
	}

	if currentVersion == 0 {
		if !quiet {
			fmt.Printf("↑  %s  (first push → v%d)\n", entry.Local, newVer)
		}
	} else {
		if !quiet {
			fmt.Printf("↑  %s  v%d → v%d\n", entry.Local, oldVer, newVer)
		}
	}

	entry.Version = newVer
	entry.MD5 = result.MD5
	return "push", true, nil
}

// resolveUploadURL determines the correct Waterbutler upload URL for an entry.
// For existing files it uses the file's own upload link; for new files it builds one.
func resolveUploadURL(ctx context.Context, entry *manifest.Entry, proj string, res *resolver.Resolver) (string, error) {
	if entry.Version > 0 {
		// File exists on OSF — use its upload link for overwrite (creates new version).
		item, err := res.Resolve(ctx, proj, entry.Remote)
		if err == nil {
			return item.Links.Upload, nil
		}
	}
	// New file — build upload URL.
	parentDir := filepath.Dir(entry.Remote)
	filename := filepath.Base(entry.Remote)
	return client.BuildUploadURL(proj, parentDir, filename), nil
}

// processPullEntry handles a pull-eligible entry when --pull-new is set.
func processPullEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs string,
	state manifest.FileState,
	resolvedItem *client.FileItem,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	repoRoot string,
	quiet, jsonMode, dryRun, force bool,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		if !quiet {
			fmt.Printf("✓  %s (v%d)\n", entry.Local, entry.Version)
		}
		return "in_sync", false, nil

	case manifest.StateMissing, manifest.StateBehind:
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		label := "v" + fmt.Sprint(entry.Version)
		if state == manifest.StateBehind {
			label = fmt.Sprintf("v? → v%d", entry.Version)
		}
		if dryRun {
			if !jsonMode {
				fmt.Printf("[dry-run] ↓  %s  (%s)\n", entry.Local, label)
			}
			return "pull", false, nil
		}
		if err := os.MkdirAll(filepath.Dir(localAbs), 0755); err != nil {
			return "", false, err
		}
		dlURL := client.RevisionURL(resolvedItem.Links.Download, entry.Version)
		if err := wb.Download(ctx, dlURL, localAbs, -1, quiet); err != nil {
			return "", false, fmt.Errorf("downloading %s: %w", entry.Local, err)
		}
		// Verify MD5 after download.
		gotMD5, _ := computeLocalMD5(localAbs)
		if entry.MD5 != "" && gotMD5 != entry.MD5 {
			os.Remove(localAbs)
			return "", false, fmt.Errorf("MD5 mismatch after downloading %s: expected %s, got %s", entry.Local, entry.MD5, gotMD5)
		}
		if !quiet {
			fmt.Printf("↓  %s  (%s)\n", entry.Local, label)
		}
		return "pull", false, nil

	case manifest.StateRemoteNewer:
		latest := 0
		if resolvedItem != nil {
			if fvs, err := osfClient.GetFileVersions(ctx, resolvedItem.ID); err == nil {
				latest = latestRemoteVersion(fileVersionsToRemote(fvs))
			}
		}
		if !quiet {
			fmt.Printf("  ↑  %s  remote has v%d (manifest pins v%d).\n     Bump version in gosf.toml then run --pull-new to update.\n",
				entry.Local, latest, entry.Version)
		}
		return "skipped_remote_newer", false, nil

	case manifest.StateAheadOfManifest:
		if !force {
			if !quiet {
				fmt.Printf("  ~  %s  locally modified, skipping.\n     Use --force to overwrite.\n", entry.Local)
			}
			return "skipped_modified", false, nil
		}
		// --force: overwrite.
		if !quiet {
			fmt.Fprintf(os.Stderr, "  !  Overwriting locally modified file: %s\n", entry.Local)
		}
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		if dryRun {
			if !jsonMode {
				fmt.Printf("[dry-run] ↓  %s  (force, v%d)\n", entry.Local, entry.Version)
			}
			return "pull_force", false, nil
		}
		dlURL := client.RevisionURL(resolvedItem.Links.Download, entry.Version)
		if err := wb.Download(ctx, dlURL, localAbs, -1, quiet); err != nil {
			return "", false, fmt.Errorf("downloading %s: %w", entry.Local, err)
		}
		if !quiet {
			fmt.Printf("↓  %s  (force, v%d)\n", entry.Local, entry.Version)
		}
		return "pull_force", false, nil

	case manifest.StateNotPushed:
		if !quiet {
			fmt.Printf("·  %s  not yet pushed\n", entry.Local)
		}
		return "skipped_not_pushed", false, nil
	}
	return "noop", false, nil
}

func makeSyncItem(entry *manifest.Entry, state manifest.FileState, action string, remoteVersions []manifest.RemoteVersion) output.SyncItem {
	item := output.SyncItem{
		Path:            entry.Local,
		State:           state.String(),
		DeclaredVersion: entry.Version,
		ActionTaken:     action,
	}
	if len(remoteVersions) > 0 {
		item.RemoteLatestVersion = latestRemoteVersion(remoteVersions)
	}
	return item
}

func init() {
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Overwrite locally modified pull files")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would happen without making changes")
	syncCmd.Flags().BoolVar(&syncNoCheckRemote, "no-check-remote", false, "Skip remote version lookups (faster, cannot detect BEHIND/REMOTE_NEWER)")
	rootCmd.AddCommand(syncCmd)
}
