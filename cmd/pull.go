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
	pullDryRun  bool
	pullVersion int
	pullNoTrack bool
	pullForce   bool
	pullResolve string
)

var pullCmd = &cobra.Command{
	Use:   "pull [<project>:<path>] [dest]",
	Short: "Download files from an OSF project",
	Long: `Download files from an OSF project to a local destination.

With no arguments, pulls all tracked files with direction=pull or direction=both
from .gosf/gosf.toml. Requires .gosf/gosf.toml with [project].id set (run 'gosf init').

With a path argument, downloads the specified file or folder and records it
in .gosf/gosf.toml (unless --no-track is set).

Path rules follow scp conventions:
  gosf pull abc12:/data/file.csv             → ./data/file.csv
  gosf pull abc12:/data/file.csv out.csv     → ./out.csv
  gosf pull abc12:/data/dir/ local/          → ./local/<files>
  gosf pull abc12:/data/dir  local/          → ./local/dir/<files>
  gosf pull abc12:                           → download entire project`,
	Args:         cobra.RangeArgs(0, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateResolve(pullResolve); err != nil {
			return err
		}
		token := config.LoadToken(flagToken)
		osfClient := client.New(token)
		wb := client.NewWaterbutler(token)

		if len(args) == 0 {
			return runBarePull(cmd.Context(), osfClient, wb)
		}
		return runExplicitPull(cmd, args, osfClient, wb)
	},
}

// runBarePull pulls all pull-eligible tracked manifest entries.
func runBarePull(ctx context.Context, osfClient *client.OSFClient, wb *client.WaterbutlerClient) error {
	manifestPath, repoRoot, err := manifest.FindManifest()
	if manifest.IsNotFound(err) {
		return fmt.Errorf("no .gosf/gosf.toml found — run: gosf init <project-id>")
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

	res := resolver.New(osfClient)
	jsonMode := flagOutput == "json"
	jsonResults := make([]output.SyncItem, 0)
	manifestChanged := false

	for i := range m.Files {
		entry := &m.Files[i]
		if entry.Direction != "pull" && entry.Direction != "both" {
			continue
		}
		proj := entry.ResolveProject(m.Project.ID)
		localAbs := filepath.Join(repoRoot, entry.Local)

		localMD5, _ := computeLocalMD5(localAbs)

		var remoteVersions []manifest.RemoteVersion
		var resolvedItem *client.FileItem
		// Resolve every pull entry (including unpinned ones) so content can be
		// compared and a download URL is available.
		item, resolveErr := res.Resolve(ctx, proj, entry.Remote)
		if resolveErr == nil {
			resolvedItem = &item
			fvs, fetchErr := osfClient.GetFileVersions(ctx, item.ID)
			if fetchErr == nil {
				remoteVersions = fileVersionsToRemote(fvs)
			}
		}

		state := manifest.ClassifyFile(*entry, localMD5, remoteVersions, false)
		action, changed, err := processPullEntry(ctx, entry, proj, localAbs, localMD5, state, resolvedItem, wb, osfClient, repoRoot, progressBarEnabled(), pullDryRun, pullForce, pullResolve, remoteVersions)
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

	if manifestChanged && !pullDryRun {
		if err := manifest.Save(m, manifestPath); err != nil {
			return fmt.Errorf("saving manifest: %w", err)
		}
	}
	if jsonMode {
		return output.PrintJSON(os.Stdout, jsonResults)
	}
	return nil
}

// runExplicitPull pulls a specific remote path.
func runExplicitPull(cmd *cobra.Command, args []string, osfClient *client.OSFClient, wb *client.WaterbutlerClient) error {
	target, err := resolver.ParseTarget(args[0])
	if err != nil {
		return err
	}

	dest := "."
	if len(args) == 2 {
		dest = args[1]
	}

	res := resolver.New(osfClient)
	sp := output.NewSpinner("Resolving…")
	items, err := res.ListDir(cmd.Context(), target.NodeID, target.Path)
	sp.Stop()
	if err != nil {
		return friendlyAuthError(err)
	}

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

	// Load .gosf/gosf.toml if present for duplicate-tracking check and auto-tracking.
	var m *manifest.Manifest
	var manifestPath string
	manifestCreated := false
	mfPathFound, _, findErr := manifest.FindManifest()
	if findErr == nil {
		if loaded, loadErr := manifest.Load(mfPathFound); loadErr == nil {
			m = loaded
			manifestPath = mfPathFound
		}
	} else if !pullNoTrack && !manifest.IsNotFound(findErr) {
		return findErr
	}

	// If no .gosf/gosf.toml and tracking enabled, we'll create one.
	if m == nil && !pullNoTrack {
		if target.NodeID == "" {
			return fmt.Errorf("no project configured — run: gosf init <project-id>")
		}
		manifestPath = filepath.Join(".gosf", "gosf.toml")
		m = &manifest.Manifest{Project: manifest.ProjectConfig{ID: target.NodeID}}
		manifestCreated = true
	}

	jsonMode := flagOutput == "json"
	s := &pullSession{
		ctx:          cmd.Context(),
		osf:          osfClient,
		wb:           wb,
		result:       output.NewPullResult(pullDryRun),
		jsonMode:     jsonMode,
		quiet:        flagQuiet || jsonMode,
		dryRun:       pullDryRun,
		version:      pullVersion,
		track:        !pullNoTrack,
		manifest:     m,
		manifestPath: manifestPath,
		nodeID:       target.NodeID,
		remotePath:   target.Path,
	}

	if len(items) == 1 && items[0].Attributes.Kind == "file" {
		item := items[0]
		var destPath string
		switch {
		case dest == ".":
			// Mirror remote path locally (strip leading /).
			destPath = filepath.FromSlash(strings.TrimLeft(target.Path, "/"))
		case func() bool { info, err := os.Stat(dest); return err == nil && info.IsDir() }():
			destPath = filepath.Join(dest, item.Attributes.Name)
		default:
			destPath = dest
		}
		if err := s.file(item, destPath); err != nil {
			return err
		}
	} else {
		if err := s.tree(items, dest, target.Path); err != nil {
			return err
		}
	}

	if s.track && !s.dryRun && len(s.tracked) > 0 {
		for _, te := range s.tracked {
			if findEntryByLocal(s.manifest, te.local) < 0 {
				s.manifest.Files = append(s.manifest.Files, te.entry)
			} else {
				idx := findEntryByLocal(s.manifest, te.local)
				s.manifest.Files[idx] = te.entry
			}
		}
		if err := manifest.Save(s.manifest, s.manifestPath); err != nil {
			return fmt.Errorf("updating .gosf/gosf.toml: %w", err)
		}
		_ = manifestCreated
	}

	if jsonMode {
		return output.PrintJSON(os.Stdout, s.result)
	}
	if len(s.result.Downloaded) == 0 && !flagQuiet {
		fmt.Fprintln(os.Stderr, "Nothing to download (no files at that path).")
	}
	return nil
}

// trackedEntry pairs a local path with the manifest entry to write.
type trackedEntry struct {
	local string
	entry manifest.Entry
}

// pullSession carries the shared state for one pull invocation.
type pullSession struct {
	ctx          context.Context
	osf          *client.OSFClient
	wb           *client.WaterbutlerClient
	result       *output.PullResult
	jsonMode     bool
	quiet        bool
	dryRun       bool
	version      int
	track        bool
	manifest     *manifest.Manifest
	manifestPath string
	nodeID       string
	remotePath   string
	tracked      []trackedEntry
}

// trackedRemoteConflict reports the local path a remote is already tracked
// under when that differs from destPath, or "" when there is no conflict
// (remote untracked, tracked at the same destination, or tracked in a
// different project). A non-empty result means the caller asked to download to
// an explicit alternate destination, so the file should be fetched but not
// (re-)tracked — downloading to a path and tracking that path are distinct.
func trackedRemoteConflict(m *manifest.Manifest, nodeID, remotePath, destPath string) string {
	if m == nil {
		return ""
	}
	idx := findEntryByRemote(m, nodeID, remotePath)
	if idx < 0 {
		return ""
	}
	existing := m.Files[idx].Local
	if existing == destPath || existing == filepath.ToSlash(destPath) {
		return ""
	}
	return existing
}

// file downloads a single file item to destPath, recording it in the result.
func (s *pullSession) file(item client.FileItem, destPath string) error {
	remotePath := item.Attributes.MaterializedPath
	if remotePath == "" {
		remotePath = s.remotePath
	}

	// Decide whether to record this download in the manifest. Downloading to an
	// explicit alternate destination (e.g. a scratch/verification copy) is a
	// plain download: fetch the bytes but leave the existing tracked entry
	// untouched rather than aborting the whole pull.
	trackThis := s.track && s.manifest != nil
	if trackThis {
		if conflict := trackedRemoteConflict(s.manifest, s.nodeID, remotePath, destPath); conflict != "" {
			trackThis = false
			if !s.quiet && !s.jsonMode {
				fmt.Fprintf(os.Stderr, "note: %q is tracked as %q; downloading to %q without tracking\n",
					remotePath, conflict, destPath)
			}
		}
	}

	if s.dryRun {
		s.result.Add(destPath, item.Attributes.Size)
		if !s.jsonMode {
			fmt.Printf("[dry-run] would download → %s\n", destPath)
		}
		return nil
	}

	// Idempotent skip: if the destination already holds byte-identical content
	// there is nothing to transfer. Only applies when fetching the latest
	// version (item hashes describe the latest, not a requested revision).
	identical := s.version == 0 && localFileMatches(destPath, item.Attributes.Extra.Hashes.MD5)
	if identical {
		if !s.quiet && !s.jsonMode {
			fmt.Fprintf(os.Stderr, "%s  %s (identical, skipped)\n", output.Dim("≡"), destPath)
		}
		s.result.Add(destPath, item.Attributes.Size)
	} else {
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
	}

	// Auto-track.
	if trackThis {
		ver := s.version
		md5 := ""
		if ver == 0 {
			if versions, err := s.osf.GetFileVersions(s.ctx, item.ID); err == nil && len(versions) > 0 {
				ver = versions[0].Number()
				md5 = versions[0].Attributes.Extra.Hashes.MD5
			}
		} else {
			// Specific version: compute MD5 from downloaded file.
			md5, _ = computeLocalMD5(destPath)
		}
		entryProject := ""
		if s.nodeID != s.manifest.Project.ID {
			entryProject = s.nodeID
		}
		localPath := filepath.ToSlash(destPath)
		s.tracked = append(s.tracked, trackedEntry{
			local: localPath,
			entry: manifest.Entry{
				Local:     localPath,
				Remote:    remotePath,
				Direction: "pull",
				Version:   ver,
				MD5:       md5,
				Project:   entryProject,
			},
		})
	}
	return nil
}

// tree recursively downloads a slice of items into destDir.
func (s *pullSession) tree(items []client.FileItem, destDir, remoteBase string) error {
	for _, item := range items {
		localPath := filepath.Join(destDir, item.Attributes.Name)

		if item.Attributes.Kind == "folder" {
			children, err := s.osf.ListFilesFromURL(s.ctx, item.Relationships.Files.Links.Related.Href)
			if err != nil {
				return fmt.Errorf("listing %s: %w", item.Attributes.Name, err)
			}
			childRemote := remoteBase + "/" + item.Attributes.Name
			if err := s.tree(children, localPath, childRemote); err != nil {
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
		if v.Number() == n {
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
	pullCmd.Flags().BoolVar(&pullNoTrack, "no-track", false, "Download without recording in .gosf/gosf.toml")
	pullCmd.Flags().BoolVar(&pullForce, "force", false, "Overwrite locally-modified files with the pinned version")
	pullCmd.Flags().StringVar(&pullResolve, "resolve", "", "Resolve divergence by taking remote: 'theirs'")
	rootCmd.AddCommand(pullCmd)
}
