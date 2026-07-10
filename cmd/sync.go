package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/config"
	"github.com/BU-Neuromics/gosf/internal/log"
	"github.com/BU-Neuromics/gosf/internal/manifest"
	"github.com/BU-Neuromics/gosf/internal/output"
	"github.com/BU-Neuromics/gosf/internal/resolver"
)

var (
	syncForce         bool
	syncDryRun        bool
	syncNoCheckRemote bool
	syncResolve       string
	syncJobs          int
)

// defaultScanJobs bounds how many manifest entries are classified against the
// remote concurrently during a scan (sync/status pass 1).
const defaultScanJobs = 8

// scanConcurrency clamps a requested job count to a sane minimum of 1.
func scanConcurrency(jobs int) int {
	if jobs < 1 {
		return 1
	}
	return jobs
}

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
			return fmt.Errorf("no .gosf/gosf.toml found — run 'gosf init <project-id>' to start tracking this repo, then 'gosf add' / 'gosf pull' to register files")
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
		// Wrap the lister in a per-run cache so the many files that share
		// directories don't each re-walk (and re-list) the same folders.
		res := resolver.New(resolver.NewCachingLister(osfClient))

		jsonMode := flagOutput == "json"
		showBar := progressBarEnabled()

		jsonResults := make([]output.SyncItem, 0)
		manifestChanged := false

		// Pass 1: classify every entry (no transfers yet), concurrently. This is
		// the phase that used to run silently and serially; a bounded worker pool
		// overlaps the OSF round-trips, and per-entry progress is logged so a
		// large manifest visibly makes headway instead of appearing stalled.
		total := len(m.Files)
		plans := make([]entryPlan, total)
		var scanned int64
		g, gctx := errgroup.WithContext(cmd.Context())
		g.SetLimit(scanConcurrency(syncJobs))
		for i := range m.Files {
			i := i
			g.Go(func() error {
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
				contactsRemote := !syncNoCheckRemote || isPullEligible
				if contactsRemote {
					resolvedItem, remoteVersions = fetchRemoteState(gctx, res, osfClient, proj, *entry, localMD5, !syncNoCheckRemote)
					log.Infof("scanned remote %d/%d  %s", atomic.AddInt64(&scanned, 1), total, entry.Local)
				} else {
					log.Debugf("classifying %d/%d  %s (no remote check)", atomic.AddInt64(&scanned, 1), total, entry.Local)
				}

				state := manifest.ClassifyFile(*entry, localMD5, remoteVersions, syncNoCheckRemote)
				plans[i] = entryPlan{
					entry: entry, proj: proj, localAbs: localAbs, localMD5: localMD5,
					state: state, resolvedItem: resolvedItem, remoteVersions: remoteVersions,
					treatAsPush: isPushEligible, treatAsPull: !isPushEligible && isPullEligible,
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
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
				action, changed, perr = processPushEntry(cmd.Context(), p.entry, p.proj, p.localAbs, p.localMD5, p.state, res, wb, osfClient, showBar, syncDryRun, syncForce, syncResolve, p.remoteVersions)
			case p.treatAsPull:
				action, changed, perr = processPullEntry(cmd.Context(), p.entry, p.proj, p.localAbs, p.localMD5, p.state, p.resolvedItem, wb, osfClient, repoRoot, showBar, syncDryRun, syncForce, syncResolve, p.remoteVersions)
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
// the local copy. showBar toggles the live transfer progress bar.
func processPushEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	res *resolver.Resolver,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	showBar, dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		log.Debugf("in sync: %s (v%d)", entry.Local, entry.Version)
		return "in_sync", false, nil

	case manifest.StateMissing:
		log.Warnf("%s: missing locally, skipping", entry.Local)
		return "skipped_missing", false, nil

	case manifest.StateNotPushed:
		if _, statErr := os.Stat(localAbs); os.IsNotExist(statErr) {
			log.Debugf("%s: not found locally, skipping", entry.Local)
			return "skipped_not_found", false, nil
		}
		return pushFile(ctx, entry, proj, localAbs, res, wb, showBar, dryRun)

	case manifest.StatePinOnly:
		// Local content already equals the remote — record the pin, no upload.
		return pinEntry(entry, remoteVersions, dryRun)

	case manifest.StateAheadOfManifest:
		// Only local moved (L≠B, R=B) — a real update. Safe to push.
		return pushFile(ctx, entry, proj, localAbs, res, wb, showBar, dryRun)

	case manifest.StateRemoteNewer, manifest.StateBehind:
		// Pushing here buries a newer/different remote version — a rollback.
		if !force {
			latest := latestRemoteVersionInfo(remoteVersions)
			return "", false, fmt.Errorf(
				"push refused: %s would roll the remote back over v%d (pinned v%d); "+
					"pass --force to overwrite the remote deliberately",
				entry.Local, latest.Version, entry.Version)
		}
		return pushFile(ctx, entry, proj, localAbs, res, wb, showBar, dryRun)

	case manifest.StateDivergent:
		if resolve == "ours" {
			return pushFile(ctx, entry, proj, localAbs, res, wb, showBar, dryRun)
		}
		return "", false, divergenceError(*entry, proj, localMD5, remoteVersions)
	}
	return "noop", false, nil
}

// pinEntry records the latest remote version+MD5 into the manifest entry without
// transferring any bytes (the PIN_ONLY state: local content already matches the
// remote). Returns changed=true unless in dry-run mode.
func pinEntry(entry *manifest.Entry, remoteVersions []manifest.RemoteVersion, dryRun bool) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	if dryRun {
		log.Infof("[dry-run] %s: identical to remote v%d, would pin", entry.Local, latest.Version)
		return "pin", false, nil
	}
	entry.Version = latest.Version
	entry.MD5 = latest.MD5
	log.Infof("≡ %s: identical to remote v%d, pinned (no transfer)", entry.Local, latest.Version)
	return "pinned", true, nil
}

// pushFile uploads the local file and updates the entry. It chooses between
// creating a new file and PUTting a new version by whether the remote file
// actually exists — not by entry.Version. A version=0 entry whose remote file
// already exists must update the existing file (create would 409); this is the
// fix for #62. The (cached) resolver makes the existence check cheap: the scan
// already listed the parent directory.
func pushFile(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs string,
	res *resolver.Resolver,
	wb *client.WaterbutlerClient,
	showBar, dryRun bool,
) (action string, changed bool, err error) {
	oldVer := entry.Version

	uploadURL, remoteExists, err := uploadTarget(ctx, entry, proj, res)
	if err != nil {
		return "", false, fmt.Errorf("resolving upload URL for %s: %w", entry.Local, err)
	}

	if dryRun {
		logPush(true, entry.Local, remoteExists, oldVer, oldVer+1)
		return "push", false, nil
	}

	log.Debugf("uploading %s → %s", entry.Local, uploadURL)
	result, uploadErr := wb.Upload(ctx, localAbs, uploadURL, showBar)
	if uploadErr != nil {
		return "", false, fmt.Errorf("uploading %s: %w", entry.Local, uploadErr)
	}

	newVer := result.Version
	if newVer == 0 {
		newVer = oldVer + 1
	}
	logPush(false, entry.Local, remoteExists, oldVer, newVer)

	entry.Version = newVer
	entry.MD5 = result.MD5
	return "push", true, nil
}

// logPush emits the per-file push activity line, distinguishing a first push
// (no remote file) from a new version of an existing file, and noting when the
// prior local pin was unknown (oldVer == 0 but the remote already existed).
func logPush(dryRun bool, local string, remoteExists bool, oldVer, newVer int) {
	prefix, verb := "", "pushed"
	if dryRun {
		prefix, verb = "[dry-run] ", "would push"
	}
	switch {
	case !remoteExists:
		log.Infof("%s↑ %s %s (first push → v%d)", prefix, verb, local, newVer)
	case oldVer > 0:
		log.Infof("%s↑ %s %s v%d → v%d", prefix, verb, local, oldVer, newVer)
	default:
		log.Infof("%s↑ %s %s (new version → v%d)", prefix, verb, local, newVer)
	}
}

// uploadTarget returns the Waterbutler upload URL for an entry and whether the
// remote file already exists. An existing file's own upload link creates a new
// version on PUT; a new file uses the parent folder's ID-based create URL.
func uploadTarget(ctx context.Context, entry *manifest.Entry, proj string, res *resolver.Resolver) (url string, remoteExists bool, err error) {
	if item, rerr := res.Resolve(ctx, proj, entry.Remote); rerr == nil && item.Attributes.Kind == "file" {
		return item.Links.Upload, true, nil
	}
	// New file — resolve the parent folder's ID-based upload base (or root).
	parentDir := filepath.Dir(entry.Remote)
	filename := filepath.Base(entry.Remote)
	base, err := folderUploadBase(ctx, res, proj, parentDir)
	if err != nil {
		return "", false, err
	}
	return client.AppendUploadName(base, filename), false, nil
}

// processPullEntry handles a pull-eligible entry. force authorizes overwriting
// locally-modified files; resolve="theirs" resolves a divergence by taking the
// remote copy. showBar toggles the live transfer progress bar.
func processPullEntry(
	ctx context.Context,
	entry *manifest.Entry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	resolvedItem *client.FileItem,
	wb *client.WaterbutlerClient,
	osfClient *client.OSFClient,
	repoRoot string,
	showBar, dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		log.Debugf("in sync: %s (v%d)", entry.Local, entry.Version)
		return "in_sync", false, nil

	case manifest.StatePinOnly:
		// Local content already equals the remote — record the pin, no download.
		return pinEntry(entry, remoteVersions, dryRun)

	case manifest.StateMissing, manifest.StateBehind:
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		// Restore the pinned baseline for pinned entries; for unpinned entries
		// (no baseline) fetch the latest and pin to it.
		if entry.Version > 0 {
			return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, entry.Version, false, remoteVersions, showBar, dryRun, "pull")
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, showBar, dryRun, "pull")

	case manifest.StateRemoteNewer:
		// L == B and remote moved ahead — a safe fast-forward. Advance and re-pin.
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, showBar, dryRun, "fast_forward")

	case manifest.StateAheadOfManifest:
		if !force {
			log.Warnf("%s: locally modified, skipping (use --force to overwrite with the pinned version)", entry.Local)
			return "skipped_modified", false, nil
		}
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		log.Warnf("overwriting locally modified file: %s", entry.Local)
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, entry.Version, false, remoteVersions, showBar, dryRun, "pull_force")

	case manifest.StateDivergent:
		if resolve != "theirs" {
			return "", false, divergenceError(*entry, proj, localMD5, remoteVersions)
		}
		if resolvedItem == nil {
			return "skipped_unresolved", false, nil
		}
		log.Warnf("resolving divergence by taking remote: %s", entry.Local)
		return downloadAndPin(ctx, entry, resolvedItem, wb, localAbs, 0, true, remoteVersions, showBar, dryRun, "pull_theirs")

	case manifest.StateNotPushed:
		log.Debugf("%s: not yet pushed", entry.Local)
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
	showBar, dryRun bool,
	action string,
) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	label := fmt.Sprintf("v%d", entry.Version)
	if toLatest {
		label = fmt.Sprintf("→ v%d", latest.Version)
	}
	if dryRun {
		log.Infof("[dry-run] ↓ %s (%s)", entry.Local, label)
		return action, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(localAbs), 0755); err != nil {
		return "", false, err
	}
	dlURL := item.Links.Download
	if !toLatest && revision > 0 {
		dlURL = client.RevisionURL(dlURL, revision)
	}
	log.Debugf("downloading %s (%s)", entry.Local, label)
	if err := wb.Download(ctx, dlURL, localAbs, -1, showBar); err != nil {
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
	log.Infof("↓ pulled %s (%s)", entry.Local, label)
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
	syncCmd.Flags().IntVarP(&syncJobs, "jobs", "j", defaultScanJobs, "Number of files to scan against the remote concurrently")
	rootCmd.AddCommand(syncCmd)
}
