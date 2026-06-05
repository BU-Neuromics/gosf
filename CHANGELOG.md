# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-06-05

Initial public release.

### Added

**Commands**
- `gosf ls` — list files and folders in an OSF project or subfolder
- `gosf pull` — download a file or entire folder tree; `--version=N` for historical versions
- `gosf push` — upload a file or directory; `--conflict=skip|overwrite|rename`
- `gosf rm` — delete a file or folder with confirmation prompt; `--yes` to skip
- `gosf projects` — list all accessible OSF projects (requires auth)
- `gosf info` — show project/component metadata
- `gosf open` — construct the OSF web URL and open it in a browser
- `gosf versions` — list all versions of a file, newest first
- `gosf add` — add a file to the `gosf.toml` sync manifest
- `gosf status` — show manifest sync state for each tracked file (exit 1 if anything is out of sync)
- `gosf sync` — push/pull tracked files according to manifest directions; `--pull-new` to download missing files
- `gosf auth login / status / logout` — store and manage OSF personal access tokens

**Core features**
- Two-tier OSF API integration: JSON:API metadata (`api.osf.io`) + Waterbutler file transfer (`files.osf.io`)
- `gosf.toml` sync manifest with per-file `direction` (`push`, `pull`, or `both`) and pinned version/MD5
- Six file states in `gosf status`: `IN_SYNC`, `MISSING`, `BEHIND`, `AHEAD_OF_MANIFEST`, `REMOTE_NEWER`, `NOT_PUSHED`
- Manifest updated automatically after successful `gosf push` and `gosf sync`
- `--output=json` on every command for scripting; JSON goes to stdout, progress to stderr
- `--dry-run` on `push`, `pull`, `rm`, `sync`
- `--quiet` / `-q` to suppress progress bars and informational output
- Progress bars on all file transfers via `schollz/progressbar`
- Signal-aware context: Ctrl-C cancels in-flight HTTP requests and removes partial download files
- Token priority chain: `--token` flag → `OSF_TOKEN` env var → token file → OS keychain
- Plaintext token fallback in `~/.config/gosf/token` (mode `0600`) for headless/HPC systems (via `--no-keychain`); kept separate from `config.toml` so the config file is safe to commit to version control
- Token never printed in logs or error output

**Distribution and CI**
- Static single-binary releases for Linux, macOS, Windows on amd64 and arm64
- GoReleaser pipeline publishing cross-platform archives + checksums to GitHub Releases
- GitHub Actions CI: `gofmt`, `go vet`, race-detector test suite, `golangci-lint v2`, cross-compile matrix
- End-to-end integration test suite with an in-process fake OSF + Waterbutler server (36 tests)
- One-liner installers: `curl … | bash` for Linux/macOS and `irm … | iex` for Windows; verifies SHA-256 checksums and supports `GOSF_INSTALL_DIR` override
- [skills.sh](https://skills.sh) agent skill (`skills/gosf/SKILL.md`) for AI coding agents; install with `npx skills add BU-Neuromics/gosf`
