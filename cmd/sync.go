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
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var (
	syncForce         bool
	syncDryRun        bool
	syncNoCheckRemote bool
	syncResolve       string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local files with OSF according to .gosf/gosf.toml",
	Long: `Sync files declared in .gosf/gosf.toml between local storage and OSF.

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
		if err := validateResolve(syncResolve); err != nil {
			return err
		}
		manifestPath, repoRoot, err := manifest.FindManifest()
		if manifest.IsNotFound(err) {
			return fmt.Errorf("no .gosf/gosf.toml found — run 'gosf add' to create one")
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

		// Pass 1: classify every entry (no transfers yet).
		plans := make([]entryPlan, 0, len(m.Files))
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
			// Resolve for content comparison (all entries when checking the
			// remote) and to obtain a download URL (pull-eligible entries).
			if !syncNoCheckRemote || isPullEligible {
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
			plans = append(plans, entryPlan{
				entry: entry, proj: proj, localAbs: localAbs, localMD5: localMD5,
				state: state, resolvedItem: resolvedItem, remoteVersions: remoteVersions,
				treatAsPush: isPushEligible, treatAsPull: !isPushEligible && isPullEligible,
			})
		}

		// Pre-flight: refuse to transfer anything if any entry has diverged
		// (and no applicable --resolve). Fail hard before touching bytes so a
		// bulk sync never applies a partial, half-resolved state.
		var blocked []string
		for _, p := range plans {
			if p.state != manifest.StateDivergent {
				continue
			}
			resolved := (p.treatAsPush && syncResolve == "ours") || (p.treatAsPull && syncResolve == "theirs")
			if !resolved {
				blocked = append(blocked, divergenceError(*p.entry, p.proj, p.localMD5, p.remoteVersions).Error())
			}
		}
		if len(blocked) > 0 {
			return fmt.Errorf("%s", strings.Join(blocked, "\n\n"))
		}

		// Pass 2: execute.
		for _, p := range plans {
			var action string
			var changed bool
			var perr error
			switch {
			case p.treatAsPush:
				action, changed, perr = processPushEntry(cmd.Context(), p.entry, p.proj, p.localAbs, p.localMD5, p.state, res, wb, osfClient, quiet, jsonMode, syncDryRun, syncForce, syncResolve, p.remoteVersions)
			case p.treatAsPull:
				action, changed, perr = processPullEntry(cmd.Context(), p.entry, p.proj, p.localAbs, p.localMD5, p.state, p.resolvedItem, wb, osfClient, repoRoot, quiet, jsonMode, syncDryRun, syncForce, syncResolve, p.remoteVersions)
			default:
				continue
			}
			if perr != nil {
				return perr
			}
			if changed {
				manifestChanged = true
			}
			if jsonMode {
				jsonResults = append(jsonResults, makeSyncItem(p.entry, p.state, action, p.remoteVersions))
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
// whether the manifest was mutated, and any error. force authorizes a
// remote-newer/behind rollback; resolve="ours" resolves a divergence by taking
// the local copy.
func processPushEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	res *resolver.Resolver,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	quiet, jsonMode, dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		if !quiet {
			fmt.Printf("%s  %s (v%d)\n", output.Green("✓"), entry.Local, entry.Version)
		}
		return "in_sync", false, nil

	case manifest.StateMissing:
		if !quiet {
			fmt.Printf("%s  %s  missing locally, skipping\n", output.Red("✗"), entry.Local)
		}
		return "skipped_missing", false, nil

	case manifest.StateNotPushed:
		if _, statErr := os.Stat(localAbs); os.IsNotExist(statErr) {
			if !quiet {
				fmt.Printf("%s  %s  not found locally, skipping\n", output.Dim("·"), entry.Local)
			}
			return "skipped_not_found", false, nil
		}
		return pushFile(ctx, entry, proj, localAbs, 0, res, wb, quiet, jsonMode, dryRun)

	case manifest.StatePinOnly:
		// Local content already equals the remote — record the pin, no upload.
		return pinEntry(entry, remoteVersions, quiet, jsonMode, dryRun)

	case manifest.StateAheadOfManifest:
		// Only local moved (L≠B, R=B) — a real update. Safe to push.
		return pushFile(ctx, entry, proj, localAbs, entry.Version, res, wb, quiet, jsonMode, dryRun)

	case manifest.StateRemoteNewer, manifest.StateBehind:
		// Pushing here buries a newer/different remote version — a rollback.
		if !force {
			latest := latestRemoteVersionInfo(remoteVersions)
			return "", false, fmt.Errorf(
				"push refused: %s would roll the remote back over v%d (pinned v%d); "+
					"pass --force to overwrite the remote deliberately",
				entry.Local, latest.Version, entry.Version)
		}
		return pushFile(ctx, entry, proj, localAbs, entry.Version, res, wb, quiet, jsonMode, dryRun)

	case manifest.StateDivergent:
		if resolve == "ours" {
			return pushFile(ctx, entry, proj, localAbs, entry.Version, res, wb, quiet, jsonMode, dryRun)
		}
		return "", false, divergenceError(*entry, proj, localMD5, remoteVersions)
	}
	return "noop", false, nil
}

// pinEntry records the latest remote version+MD5 into the manifest entry without
// transferring any bytes (the PIN_ONLY state: local content already matches the
// remote). Returns changed=true unless in dry-run mode.
func pinEntry(entry *manifest.Entry, remoteVersions []manifest.RemoteVersion, quiet, jsonMode, dryRun bool) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	if dryRun {
		if !jsonMode {
			fmt.Printf("[dry-run] %s  %s  identical to remote v%d, would pin\n", output.Dim("≡"), entry.Local, latest.Version)
		}
		return "pin", false, nil
	}
	entry.Version = latest.Version
	entry.MD5 = latest.MD5
	if !quiet {
		fmt.Printf("%s  %s  identical to remote v%d, pinned (no transfer)\n", output.Dim("≡"), entry.Local, latest.Version)
	}
	return "pinned", true, nil
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
				fmt.Printf("[dry-run] %s  %s  (first push → v1)\n", output.Cyan("↑"), entry.Local)
			}
		} else {
			if !jsonMode {
				fmt.Printf("[dry-run] %s  %s  v%d → v%d\n", output.Cyan("↑"), entry.Local, oldVer, oldVer+1)
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
			fmt.Printf("%s  %s  (first push → v%d)\n", output.Cyan("↑"), entry.Local, newVer)
		}
	} else {
		if !quiet {
			fmt.Printf("%s  %s  v%d → v%d\n", output.Cyan("↑"), entry.Local, oldVer, newVer)
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

// processPullEntry handles a pull-eligible entry. force authorizes overwriting
// locally-modified files; resolve="theirs" resolves a divergence by taking the
// remote copy.
func processPullEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	resolvedItem *client.FileItem,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	repoRoot string,
	quiet, jsonMode, dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		if !quiet {
			fmt.Printf("%s  %s (v%d)\n", output.Green("✓"), entry.Local, entry.Version)
		}
		return "in_sync", false, nil

	case manifest.StatePinOnly:
		// Local content already equals the remote — record the pin, no download.
		return pinEntry(entry, remoteVersions, quiet, jsonMode, dryRun)

	case manifest.StateMissing, manifest.StateBehind:
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		// Restore the pinned baseline for pinned entries; for unpinned entries
		// (no baseline) fetch the latest and pin to it.
		if entry.Version > 0 {
			return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, entry.Version, false, remoteVersions, quiet, jsonMode, dryRun, "pull")
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, quiet, jsonMode, dryRun, "pull")

	case manifest.StateRemoteNewer:
		// L == B and remote moved ahead — a safe fast-forward. Advance and re-pin.
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, quiet, jsonMode, dryRun, "fast_forward")

	case manifest.StateAheadOfManifest:
		if !force {
			if !quiet {
				fmt.Printf("  %s  %s  locally modified, skipping.\n     Use --force to overwrite with the pinned version.\n", output.Yellow("~"), entry.Local)
			}
			return "skipped_modified", false, nil
		}
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "  %s  Overwriting locally modified file: %s\n", output.Yellow("!"), entry.Local)
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, entry.Version, false, remoteVersions, quiet, jsonMode, dryRun, "pull_force")

	case manifest.StateDivergent:
		if resolve != "theirs" {
			return "", false, divergenceError(*entry, proj, localMD5, remoteVersions)
		}
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "  %s  Resolving divergence by taking remote: %s\n", output.Yellow("!"), entry.Local)
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, quiet, jsonMode, dryRun, "pull_theirs")

	case manifest.StateNotPushed:
		if !quiet {
			fmt.Printf("%s  %s  not yet pushed\n", output.Dim("·"), entry.Local)
		}
		return "skipped_not_pushed", false, nil
	}
	return "noop", false, nil
}

// downloadAndPin downloads a file version to localAbs and optionally updates the
// manifest pin. When toLatest is true it fetches the latest version (no revision
// query) and re-pins the entry to the latest version+MD5; otherwise it fetches
// the given revision and leaves the pin unchanged (baseline restore), verifying
// the downloaded MD5 against the pin.
func downloadAndPin(
	ctx context.Context,
	entry *manifest.Entry,
	item *client.FileItem,
	wb *client.WaterbutlerClient,
	localAbs string,
	revision int,
	toLatest bool,
	remoteVersions []manifest.RemoteVersion,
	quiet, jsonMode, dryRun bool,
	action string,
) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	label := fmt.Sprintf("v%d", entry.Version)
	if toLatest {
		label = fmt.Sprintf("→ v%d", latest.Version)
	}
	if dryRun {
		if !jsonMode {
			fmt.Printf("[dry-run] %s  %s  (%s)\n", output.Cyan("↓"), entry.Local, label)
		}
		return action, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(localAbs), 0755); err != nil {
		return "", false, err
	}
	dlURL := item.Links.Download
	if !toLatest && revision > 0 {
		dlURL = client.RevisionURL(dlURL, revision)
	}
	if err := wb.Download(ctx, dlURL, localAbs, -1, quiet); err != nil {
		return "", false, fmt.Errorf("downloading %s: %w", entry.Local, err)
	}

	gotMD5, _ := computeLocalMD5(localAbs)
	if toLatest {
		entry.Version = latest.Version
		if latest.MD5 != "" {
			entry.MD5 = latest.MD5
		} else {
			entry.MD5 = gotMD5
		}
	} else if entry.MD5 != "" && gotMD5 != entry.MD5 {
		os.Remove(localAbs)
		return "", false, fmt.Errorf("MD5 mismatch after downloading %s: expected %s, got %s", entry.Local, entry.MD5, gotMD5)
	}
	if !quiet {
		fmt.Printf("%s  %s  (%s)\n", output.Cyan("↓"), entry.Local, label)
	}
	return action, true, nil
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
	syncCmd.Flags().StringVar(&syncResolve, "resolve", "", "Resolve divergence: 'ours' (keep local) or 'theirs' (keep remote)")
	rootCmd.AddCommand(syncCmd)
}
