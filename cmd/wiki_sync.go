package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BU-Neuromics/gosf/internal/client"
	"github.com/BU-Neuromics/gosf/internal/log"
	"github.com/BU-Neuromics/gosf/internal/manifest"
)

// wikiEntryPlan is the classified, not-yet-executed reconciliation of one wiki
// manifest entry — the wiki analogue of entryPlan.
type wikiEntryPlan struct {
	entry          *manifest.WikiEntry
	proj           string
	localAbs       string
	localMD5       string
	state          manifest.FileState
	page           *client.Wiki // resolved remote page, nil when it does not exist
	remoteVersions []manifest.RemoteVersion
	treatAsPush    bool
	treatAsPull    bool
}

// wikiDivergenceError builds the hard-failure diagnostic for a diverged wiki
// entry, mirroring divergenceError for files.
func wikiDivergenceError(entry manifest.WikiEntry, proj, localMD5 string, remoteVersions []manifest.RemoteVersion) error {
	latest := latestRemoteVersionInfo(remoteVersions)
	return fmt.Errorf(
		"divergence on %s (wiki %q) — both local and remote changed since the pinned baseline\n"+
			"  baseline: v%d  md5 %s   (last synced)\n"+
			"  local:        md5 %s   (changed)\n"+
			"  remote:   v%d  md5 %s   (changed)\n"+
			"Both sides have unreconciled changes; refusing to overwrite either automatically.\n"+
			"Resolve explicitly:\n"+
			"  gosf pull  --resolve=theirs   # take remote (discards local)\n"+
			"  gosf push  --resolve=ours     # take local  (discards remote v%d)",
		entry.Local, entry.Page,
		entry.Version, shortMD5(entry.MD5),
		shortMD5(localMD5),
		latest.Version, shortMD5(latest.MD5),
		latest.Version)
}

// processWikiPushEntry handles a push-eligible wiki entry through the same gate
// matrix as files. Returns the action taken, whether the manifest changed, and
// any error.
func processWikiPushEntry(
	ctx context.Context,
	c *client.OSFClient,
	entry *manifest.WikiEntry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	page *client.Wiki,
	dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		log.Debugf("wiki in sync: %s (v%d)", entry.Local, entry.Version)
		return "in_sync", false, nil

	case manifest.StateMissing:
		log.Warnf("wiki %s: missing locally, skipping", entry.Local)
		return "skipped_missing", false, nil

	case manifest.StatePinOnly:
		return pinWikiEntry(entry, remoteVersions, dryRun)

	case manifest.StateNotPushed, manifest.StateAheadOfManifest:
		return pushWikiFile(ctx, c, entry, proj, localAbs, localMD5, page, dryRun)

	case manifest.StateRemoteNewer, manifest.StateBehind:
		if !force {
			latest := latestRemoteVersionInfo(remoteVersions)
			return "", false, fmt.Errorf(
				"push refused: %s (wiki %q) would roll the remote back over v%d (pinned v%d); "+
					"pass --force to overwrite the remote deliberately",
				entry.Local, entry.Page, latest.Version, entry.Version)
		}
		return pushWikiFile(ctx, c, entry, proj, localAbs, localMD5, page, dryRun)

	case manifest.StateDivergent:
		if resolve == "ours" {
			return pushWikiFile(ctx, c, entry, proj, localAbs, localMD5, page, dryRun)
		}
		return "", false, wikiDivergenceError(*entry, proj, localMD5, remoteVersions)
	}
	return "noop", false, nil
}

// processWikiPullEntry handles a pull-eligible wiki entry through the gate matrix.
func processWikiPullEntry(
	ctx context.Context,
	c *client.OSFClient,
	entry *manifest.WikiEntry,
	proj, localAbs, localMD5 string,
	state manifest.FileState,
	page *client.Wiki,
	dryRun, force bool,
	resolve string,
	remoteVersions []manifest.RemoteVersion,
) (action string, changed bool, err error) {
	switch state {
	case manifest.StateInSync:
		return "in_sync", false, nil

	case manifest.StatePinOnly:
		return pinWikiEntry(entry, remoteVersions, dryRun)

	case manifest.StateMissing, manifest.StateBehind:
		if page == nil {
			return "skipped_unresolved", false, nil
		}
		if entry.Version > 0 {
			return downloadWikiVersion(ctx, c, entry, page, localAbs, entry.Version, false, remoteVersions, dryRun, "pull")
		}
		return downloadWikiVersion(ctx, c, entry, page, localAbs, 0, true, remoteVersions, dryRun, "pull")

	case manifest.StateRemoteNewer:
		if page == nil {
			return "skipped_unresolved", false, nil
		}
		return downloadWikiVersion(ctx, c, entry, page, localAbs, 0, true, remoteVersions, dryRun, "fast_forward")

	case manifest.StateAheadOfManifest:
		if !force {
			log.Warnf("wiki %s: locally modified, skipping (use --force to overwrite with the pinned version)", entry.Local)
			return "skipped_modified", false, nil
		}
		if page == nil {
			return "skipped_unresolved", false, nil
		}
		return downloadWikiVersion(ctx, c, entry, page, localAbs, entry.Version, false, remoteVersions, dryRun, "pull_force")

	case manifest.StateDivergent:
		if resolve != "theirs" {
			return "", false, wikiDivergenceError(*entry, proj, localMD5, remoteVersions)
		}
		if page == nil {
			return "skipped_unresolved", false, nil
		}
		return downloadWikiVersion(ctx, c, entry, page, localAbs, 0, true, remoteVersions, dryRun, "pull_theirs")

	case manifest.StateNotPushed:
		return "skipped_not_pushed", false, nil
	}
	return "noop", false, nil
}

// pinWikiEntry records the latest remote version+MD5 without transferring bytes.
func pinWikiEntry(entry *manifest.WikiEntry, remoteVersions []manifest.RemoteVersion, dryRun bool) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	if dryRun {
		log.Infof("[dry-run] wiki %s: identical to remote v%d, would pin", entry.Local, latest.Version)
		return "pin", false, nil
	}
	entry.Version = latest.Version
	entry.MD5 = latest.MD5
	log.Infof("≡ wiki %s: identical to remote v%d, pinned (no transfer)", entry.Local, latest.Version)
	return "pinned", true, nil
}

// pushWikiFile creates the page or mints a new version from the local file and
// re-pins the entry. Whether to create vs update is decided by page existence.
func pushWikiFile(
	ctx context.Context,
	c *client.OSFClient,
	entry *manifest.WikiEntry,
	proj, localAbs, localMD5 string,
	page *client.Wiki,
	dryRun bool,
) (string, bool, error) {
	oldVer := entry.Version
	content, err := os.ReadFile(localAbs)
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", entry.Local, err)
	}

	if dryRun {
		if page == nil {
			log.Infof("[dry-run] ↑ would create wiki %q from %s", entry.Page, entry.Local)
		} else {
			log.Infof("[dry-run] ↑ would push wiki %q v%d → v%d", entry.Page, oldVer, oldVer+1)
		}
		return "push", false, nil
	}

	var newVer int
	if page == nil {
		w, cerr := c.CreateWiki(ctx, proj, entry.Page, string(content))
		if cerr != nil {
			return "", false, fmt.Errorf("creating wiki %q: %w", entry.Page, friendlyWikiError(cerr, proj))
		}
		newVer = w.Attributes.Extra.Version
		if newVer == 0 {
			newVer = 1
		}
		entry.Page = w.Attributes.Name
		log.Infof("↑ created wiki %q on %s (v%d)", entry.Page, proj, newVer)
	} else {
		v, cerr := c.CreateWikiVersion(ctx, page.ID, string(content))
		if cerr != nil {
			return "", false, fmt.Errorf("updating wiki %q: %w", entry.Page, friendlyWikiError(cerr, proj))
		}
		newVer = v.Number()
		if newVer == 0 {
			newVer = oldVer + 1
		}
		log.Infof("↑ pushed wiki %q v%d → v%d", entry.Page, oldVer, newVer)
	}

	entry.Version = newVer
	entry.MD5 = localMD5
	if entry.MD5 == "" {
		entry.MD5 = md5hex(client.CanonicalizeWikiContent(content))
	}
	return "push", true, nil
}

// downloadWikiVersion writes a wiki page's content to localAbs and optionally
// re-pins. When toLatest is true it fetches the latest content and pins to the
// latest version+MD5; otherwise it fetches the given historical version and
// leaves the pin unchanged (baseline restore), verifying the downloaded MD5.
func downloadWikiVersion(
	ctx context.Context,
	c *client.OSFClient,
	entry *manifest.WikiEntry,
	page *client.Wiki,
	localAbs string,
	revision int,
	toLatest bool,
	remoteVersions []manifest.RemoteVersion,
	dryRun bool,
	action string,
) (string, bool, error) {
	latest := latestRemoteVersionInfo(remoteVersions)
	label := fmt.Sprintf("v%d", entry.Version)
	if toLatest {
		label = fmt.Sprintf("→ v%d", latest.Version)
	}
	if dryRun {
		log.Infof("[dry-run] ↓ wiki %s (%s)", entry.Local, label)
		return action, false, nil
	}

	var content []byte
	var err error
	if toLatest {
		content, err = c.GetWikiContent(ctx, page.ID)
	} else {
		content, err = c.GetWikiVersionContent(ctx, page.ID, strconv.Itoa(revision))
	}
	if err != nil {
		return "", false, fmt.Errorf("downloading wiki %s: %w", entry.Local, err)
	}

	if err := os.MkdirAll(filepath.Dir(localAbs), 0755); err != nil {
		return "", false, err
	}
	if err := writeFileAtomic(localAbs, content); err != nil {
		return "", false, fmt.Errorf("writing %s: %w", entry.Local, err)
	}

	gotMD5 := md5hex(client.CanonicalizeWikiContent(content))
	if toLatest {
		entry.Version = latest.Version
		if latest.MD5 != "" {
			entry.MD5 = latest.MD5
		} else {
			entry.MD5 = gotMD5
		}
	} else if entry.MD5 != "" && gotMD5 != entry.MD5 {
		os.Remove(localAbs)
		return "", false, fmt.Errorf("MD5 mismatch after downloading wiki %s: expected %s, got %s", entry.Local, entry.MD5, gotMD5)
	}
	log.Infof("↓ pulled wiki %s (%s)", entry.Local, label)
	return action, true, nil
}

// writeFileAtomic writes content to path via a temp file + rename so a failed
// or interrupted write never leaves a partial/corrupt file in place.
func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gosf-wiki.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
