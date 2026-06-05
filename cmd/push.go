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
	pushDryRun        bool
	pushConflict      string
	pushNoTrack       bool
	pushNoCheckRemote bool
)

var pushCmd = &cobra.Command{
	Use:   "push [<src> <project>:<path>]",
	Short: "Upload a file or directory to an OSF project",
	Long: `Upload a local file or directory to an OSF project.

With no arguments, pushes all tracked files with direction=push or direction=both
from gosf.toml. Requires gosf.toml with [project].id set (run 'gosf init').

With arguments, uploads the specified local file or directory and records it
in gosf.toml (unless --no-track is set).

Conflict behaviour (--conflict):
  skip      (default) Skip files that already exist at the destination.
  overwrite           Replace existing files.
  rename              Append _1, _2, … to find a free name.

Examples:
  gosf push                               # push all manifest push-eligible files
  gosf push results.csv abc12:/data/results.csv
  gosf push ./results/  abc12:/data/
  gosf push data.csv    abc12:/data/data.csv --conflict=overwrite`,
	Args:         cobra.RangeArgs(0, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runBarePush(cmd)
		}
		if len(args) == 1 {
			return fmt.Errorf("usage: gosf push <src> <project>:<path>")
		}

		src := args[0]
		src = strings.TrimRight(src, "/"+string(filepath.Separator))

		target, err := resolver.ParseTarget(args[1])
		if err != nil {
			return err
		}

		switch pushConflict {
		case "skip", "overwrite", "rename":
		default:
			return fmt.Errorf("--conflict must be skip, overwrite, or rename; got %q", pushConflict)
		}

		token := config.LoadToken(flagToken)
		if token == "" {
			return fmt.Errorf("push requires authentication — run 'gosf auth login' or set OSF_TOKEN")
		}

		osfClient := client.New(token)

		srcInfo, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}

		// Load existing gosf.toml; if absent and tracking enabled, prepare to create one.
		var mf *manifest.Manifest
		var mfPath string
		mfPathFound, _, findErr := manifest.FindManifest()
		if findErr == nil {
			if loaded, loadErr := manifest.Load(mfPathFound); loadErr == nil {
				mf = loaded
				mfPath = mfPathFound
			}
		} else if manifest.IsNotFound(findErr) && !pushNoTrack {
			mfPath = "gosf.toml"
			mf = &manifest.Manifest{Project: manifest.ProjectConfig{ID: target.NodeID}}
		}

		jsonMode := flagOutput == "json"
		s := &pushSession{
			ctx:          cmd.Context(),
			res:          resolver.New(osfClient),
			wb:           client.NewWaterbutler(token),
			result:       output.NewPushResult(pushDryRun),
			jsonMode:     jsonMode,
			quiet:        flagQuiet || jsonMode,
			dryRun:       pushDryRun,
			conflict:     pushConflict,
			manifest:     mf,
			manifestPath: mfPath,
			track:        !pushNoTrack && mf != nil,
			nodeID:       target.NodeID,
		}

		if srcInfo.IsDir() {
			err = s.dir(src, target.NodeID, target.Path)
		} else {
			err = s.file(src, target.NodeID, target.Path)
		}
		if err != nil {
			return err
		}

		if s.manifestDirty && !s.dryRun {
			if err := manifest.Save(s.manifest, s.manifestPath); err != nil {
				return fmt.Errorf("updating gosf.toml: %w", err)
			}
		}

		if jsonMode {
			return output.PrintJSON(os.Stdout, s.result)
		}
		if len(s.result.Uploaded) == 0 && !flagQuiet {
			fmt.Fprintln(os.Stderr, "Nothing to upload (no files at source).")
		}
		return nil
	},
}

// runBarePush pushes all push-eligible manifest entries.
func runBarePush(cmd *cobra.Command) error {
	manifestPath, repoRoot, err := manifest.FindManifest()
	if manifest.IsNotFound(err) {
		return fmt.Errorf("no gosf.toml found — run: gosf init <project-id>")
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
		return fmt.Errorf("push requires authentication — run 'gosf auth login' or set OSF_TOKEN")
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
		if entry.Direction != "push" && entry.Direction != "both" {
			continue
		}
		proj := entry.ResolveProject(m.Project.ID)
		localAbs := filepath.Join(repoRoot, entry.Local)

		localMD5, err := computeLocalMD5(localAbs)
		if err != nil {
			return fmt.Errorf("computing MD5 for %s: %w", entry.Local, err)
		}

		var remoteVersions []manifest.RemoteVersion
		if !pushNoCheckRemote && entry.Version > 0 {
			if item, resolveErr := res.Resolve(cmd.Context(), proj, entry.Remote); resolveErr == nil {
				if fvs, fetchErr := osfClient.GetFileVersions(cmd.Context(), item.ID); fetchErr == nil {
					remoteVersions = fileVersionsToRemote(fvs)
				}
			}
		}

		state := manifest.ClassifyFile(*entry, localMD5, remoteVersions, pushNoCheckRemote)
		action, changed, err := processPushEntry(cmd.Context(), entry, proj, localAbs, state, res, wb, osfClient, quiet, jsonMode, pushDryRun)
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

	if manifestChanged && !pushDryRun {
		if err := manifest.Save(m, manifestPath); err != nil {
			return fmt.Errorf("saving manifest: %w", err)
		}
	}
	if jsonMode {
		return output.PrintJSON(os.Stdout, jsonResults)
	}
	return nil
}

// pushSession carries the shared state for one push invocation.
type pushSession struct {
	ctx           context.Context
	res           *resolver.Resolver
	wb            *client.WaterbutlerClient
	result        *output.PushResult
	jsonMode      bool
	quiet         bool
	dryRun        bool
	conflict      string
	manifest      *manifest.Manifest
	manifestPath  string
	track         bool
	nodeID        string
	manifestDirty bool
}

// file uploads a single local file to an OSF destination path.
func (s *pushSession) file(srcPath, nodeID, destPath string) error {
	// Enforce manifest direction before doing any network work.
	if s.manifest != nil {
		if idx := findEntryByLocal(s.manifest, srcPath); idx >= 0 {
			entry := s.manifest.Files[idx]
			if entry.Direction == "pull" {
				return fmt.Errorf("push refused: %q has direction=pull in gosf.toml; "+
					"edit gosf.toml to change direction to push or both first", srcPath)
			}
		}
	}

	parentDir, filename := deriveUploadTarget(destPath, srcPath)

	existing, err := s.res.ListDir(s.ctx, nodeID, parentDir)
	if err != nil {
		return fmt.Errorf("destination folder %s is not accessible (does it exist?): %w", parentDir, err)
	}

	var existingItem *client.FileItem
	for i := range existing {
		if existing[i].Attributes.Name == filename && existing[i].Attributes.Kind == "file" {
			existingItem = &existing[i]
			break
		}
	}

	plan, err := planUpload(s.conflict, existingItem, nodeID, parentDir, filename, existing)
	if err != nil {
		return err
	}
	destFull := strings.TrimRight(parentDir, "/") + "/" + plan.name

	// Refuse if a different local path already tracks this remote destination.
	if s.track && s.manifest != nil && plan.action != "skip" {
		if idx := findEntryByRemote(s.manifest, nodeID, destFull); idx >= 0 {
			if s.manifest.Files[idx].Local != srcPath {
				return fmt.Errorf("push refused: %s:%s is already tracked to %q — edit gosf.toml to change",
					nodeID, destFull, s.manifest.Files[idx].Local)
			}
		}
	}

	switch {
	case plan.action == "skip":
		s.result.Add(destFull, "skip")
		if !s.jsonMode && !s.quiet {
			fmt.Fprintf(os.Stderr, "skip  %s (already exists)\n", destFull)
		}
		return nil
	case s.dryRun:
		s.result.Add(destFull, plan.action)
		if !s.jsonMode {
			fmt.Printf("[dry-run] would %s → %s\n", plan.action, destFull)
		}
		return nil
	}

	if !s.quiet {
		if plan.action == "overwrite" {
			fmt.Fprintf(os.Stderr, "overwrite %s → %s (new version created)\n", srcPath, destFull)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s → %s\n", plan.action, srcPath, destFull)
		}
	}
	uploadResult, err := s.wb.Upload(s.ctx, srcPath, plan.url, s.quiet)
	if err != nil {
		return err
	}
	s.result.Add(destFull, plan.action)

	// Auto-track: create or update the manifest entry.
	if s.track && s.manifest != nil && uploadResult.Version > 0 {
		if idx := findEntryByLocal(s.manifest, srcPath); idx >= 0 {
			e := &s.manifest.Files[idx]
			e.Version = uploadResult.Version
			e.MD5 = uploadResult.MD5
			s.manifestDirty = true
		} else if idx := findEntryByRemote(s.manifest, nodeID, destFull); idx >= 0 {
			e := &s.manifest.Files[idx]
			e.Version = uploadResult.Version
			e.MD5 = uploadResult.MD5
			s.manifestDirty = true
		} else {
			entryProject := ""
			if nodeID != s.manifest.Project.ID {
				entryProject = nodeID
			}
			s.manifest.Files = append(s.manifest.Files, manifest.Entry{
				Local:     srcPath,
				Remote:    destFull,
				Direction: "push",
				Version:   uploadResult.Version,
				MD5:       uploadResult.MD5,
				Project:   entryProject,
			})
			s.manifestDirty = true
		}
	}
	return nil
}

// dir walks a local directory and uploads all files under destPath,
// preserving the relative directory structure.
func (s *pushSession) dir(srcDir, nodeID, destPath string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		osfDest := filepath.ToSlash(filepath.Join(destPath, rel))
		return s.file(path, nodeID, osfDest)
	})
}

// uploadPlan describes how a single file upload should be carried out.
type uploadPlan struct {
	url    string // Waterbutler URL to PUT to; empty when action is "skip"
	name   string // final filename at the destination
	action string // "upload", "overwrite", "rename", or "skip"
}

// planUpload decides how to handle a file given the conflict mode and whether
// a file of the same name already exists at the destination. It is pure: all
// inputs are explicit, so it is fully unit-testable.
func planUpload(
	conflict string,
	existing *client.FileItem,
	nodeID, parentDir, filename string,
	siblings []client.FileItem,
) (uploadPlan, error) {
	if existing == nil {
		return uploadPlan{
			url:    client.BuildUploadURL(nodeID, parentDir, filename),
			name:   filename,
			action: "upload",
		}, nil
	}

	switch conflict {
	case "skip":
		return uploadPlan{name: filename, action: "skip"}, nil
	case "overwrite":
		return uploadPlan{url: existing.Links.Upload, name: filename, action: "overwrite"}, nil
	case "rename":
		name := findFreeName(filename, siblings)
		return uploadPlan{
			url:    client.BuildUploadURL(nodeID, parentDir, name),
			name:   name,
			action: "rename",
		}, nil
	}
	return uploadPlan{}, fmt.Errorf("unknown conflict mode: %s", conflict)
}

// deriveUploadTarget splits an OSF destination path into the parent directory
// and the filename to upload as.
//
//   - A trailing slash means the destination is a directory: the source file
//     keeps its own name (e.g. "/data/" + "in.csv" → "/data", "in.csv").
//   - Otherwise the last path segment is the explicit target filename
//     (e.g. "/data/out.csv" → "/data", "out.csv").
//
// An empty destination is treated as the storage root.
func deriveUploadTarget(destPath, srcPath string) (parentDir, filename string) {
	if destPath == "" {
		destPath = "/"
	}
	isDir := strings.HasSuffix(destPath, "/")
	destPath = "/" + strings.Trim(destPath, "/")

	if isDir {
		// Destination is a directory; keep the source's basename.
		return cleanParent(destPath), filepath.Base(srcPath)
	}

	parent := filepath.Dir(destPath)
	name := filepath.Base(destPath)
	if name == "." || name == "/" || name == "" {
		// No explicit filename — fall back to the source basename.
		return cleanParent(destPath), filepath.Base(srcPath)
	}
	return cleanParent(parent), name
}

// cleanParent normalises a parent directory path to start with "/" and have no
// trailing slash (except the root itself, which stays "/").
func cleanParent(p string) string {
	return "/" + strings.Trim(p, "/")
}

// findFreeName appends _1, _2, … to base until no sibling has that name.
func findFreeName(filename string, siblings []client.FileItem) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	existing := make(map[string]bool, len(siblings))
	for _, s := range siblings {
		existing[s.Attributes.Name] = true
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if !existing[candidate] {
			return candidate
		}
	}
}

func init() {
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Show what would be uploaded without uploading")
	pushCmd.Flags().StringVar(&pushConflict, "conflict", "skip", "Conflict resolution: skip, overwrite, or rename")
	pushCmd.Flags().BoolVar(&pushNoTrack, "no-track", false, "Upload without recording in gosf.toml")
	pushCmd.Flags().BoolVar(&pushNoCheckRemote, "no-check-remote", false, "Skip remote version lookups for bare push")
	rootCmd.AddCommand(pushCmd)
}
