# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.4.0] - 2026-07-08

### Added

- **State-based sync safety** (#38): `direction` is now a *default intent* rather than a hard lock. Safety comes from state-based gates at the moment of a destructive action, comparing local (L), the pinned baseline (B), and the remote latest (R).
- Idempotent transfers: `gosf pull` and `gosf push` skip the byte transfer when the local file already matches the remote MD5. Manifest entries that are already byte-identical to the remote are pinned without any transfer (the new `PIN_ONLY` state), so registering already-present inputs no longer mints redundant remote versions or requires hand-editing `.gosf/gosf.toml`.
- `gosf status` now content-compares unpinned (`version = 0`) entries against the remote and reports `PIN_ONLY` / `BEHIND` / `AHEAD` instead of a blanket "never pushed"; it remains read-only. New `DIVERGED` state for entries where both sides changed since the baseline.
- Rich `gosf push` confirmation: bare push prints a per-file plan (header with project title and PUBLIC/PRIVATE visibility, a loud warning when public, `local → remote` action, size, and MD5) and a summary, then prompts for confirmation. `--yes` bypasses the prompt for safe pushes; `--force` also authorizes a remote-newer rollback. In `--output=json` mode `--force` is mandatory (same rule as `gosf rm`).
- `--resolve=ours|theirs` on `push` / `pull` / `sync` to resolve a `DIVERGED` entry explicitly (`ours` keeps local, `theirs` keeps remote). Divergence otherwise fails hard with a diagnostic naming the three states and the exact resolution commands.
- Anonymous reads for public projects (#38): `pull` / `ls` / `info` / `status` / `versions` attempt the fetch unauthenticated and only need a token for private data; a 401/403 is turned into an actionable "run 'gosf auth login' or set OSF_TOKEN" message.
- `gosf ls --output=json` now includes each file's content hashes under `attributes.extra.hashes.{md5,sha256}` (#35).

### Changed

- `gosf sync` runs a divergence/rollback pre-flight before any transfer, so a bulk run never applies a partial, half-resolved state. On a remote-newer pull entry it now fast-forwards and re-pins to the latest version.
- Bare `gosf push` now requires confirmation before writing remote bytes; non-interactive callers must pass `--yes` or `--force` (breaking change for scripts that relied on silent bare push).

### Fixed

- `gosf versions` no longer sends `embed=user`, which the OSF v2 versions endpoint rejects with a 400 (#34).
- `gosf pull` to an explicit alternate destination is a plain download and no longer refuses or re-tracks the existing entry (#36).
- Fresh-manifest creation on `pull`/`push` wrote to `.gosf/.gosf/gosf.toml`; it now correctly writes `.gosf/gosf.toml`.

## [1.3.0] - 2026-06-10

### Changed

- The sync manifest moved from `gosf.toml` to `.gosf/gosf.toml`, grouping gosf's project-level files under a dedicated `.gosf/` directory (following the convention of `.github/`, `.vscode/`, etc.). `gosf init` now creates the `.gosf/` directory automatically, and `FindManifest` walks up looking for `.gosf/gosf.toml`.

  **Migration:** there is no fallback to the old location. Existing users must move their manifest: `mkdir -p .gosf && git mv gosf.toml .gosf/gosf.toml` (or `mv` if not tracked).

## [1.2.0] - 2026-06-05

### Added

- `gosf init <project-id>` — create or update `gosf.toml` with a default project GUID; idempotent
- `gosf pull` bare form (no arguments) — download all `direction=pull|both` manifest entries that are missing or behind
- `gosf pull` auto-tracking — after a successful download, writes/updates the entry in `gosf.toml` with `direction=pull`, version, and MD5; opt out with `--no-track`
- `gosf push` bare form (no arguments) — upload all `direction=push|both` manifest entries that are ahead of manifest or not yet pushed
- `gosf push` auto-tracking — after a successful upload, writes/updates the entry in `gosf.toml` with `direction=push`, version, and MD5; opt out with `--no-track`
- scp-style path semantics on `gosf pull`, `gosf push`, and `gosf add`: trailing slash on source copies contents only; no trailing slash copies the directory itself; omitted dest mirrors the source path
- `gosf add` directory recursion — `gosf add data/dir abc12:/remote/` walks the directory and creates one manifest entry per file
- `gosf add` no-dest form — `gosf add local/path/file.txt` mirrors to remote `/local/path/file.txt`

### Changed

- `gosf sync` now processes **both** push-eligible and pull-eligible entries by default; the `--pull-new` flag has been removed
- `gosf add` direction is always `push`; the `--direction` flag has been removed
- `gosf push` and `gosf pull` refuse to operate on entries whose manifest `direction` conflicts (e.g. pushing a `direction=pull` file) with a clear error message
- Duplicate remote-tracking guard: pushing or pulling to a remote path already tracked under a different local path is a hard error

### Fixed

- `gosf sync` now requires `[project].id` to be set in `gosf.toml`; an empty project ID produces an actionable error pointing to `gosf init`
- `gosf pull` with default dest `"."` now correctly mirrors the remote path instead of treating `.` as a directory name

## [1.1.0] - 2026-06-05

### Added

- `gosf mv <src> <dest>` — rename or move a file/folder within or across OSF projects; uses Waterbutler rename action when src/dest share the same folder, move action otherwise; `--conflict=warn|replace|keep`
- `gosf cp <src> <dest>` — copy a file/folder within or across OSF projects; `--conflict=keep|replace|warn` (default: keep)
- `gosf mkdir <project>:<path>` — create a folder in OSF Storage; `--dry-run`
- `gosf set <project>` — update node title, description, category, and/or tags via `PATCH /v2/nodes/{id}/`; only supplied flags are sent; `--output=json` returns the updated node

## [1.0.1] - 2026-06-05

### Fixed

- `gosf pull` returned 403 on files whose `links.download` URL is at `osf.io` (e.g. `https://osf.io/download/…/`) rather than directly at `files.osf.io`. The `CheckRedirect` policy stripped the `Authorization` header on any host change, including OSF-internal redirects (`osf.io` → `files.osf.io`). The policy now only strips auth when redirecting outside OSF infrastructure (`*.osf.io`), preserving credentials across OSF-internal hops while still preventing token leakage to third-party storage backends (S3, GCS).

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
