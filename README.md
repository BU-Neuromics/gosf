# gosf

[![CI](https://github.com/BU-Neuromics/gosf/actions/workflows/ci.yml/badge.svg)](https://github.com/BU-Neuromics/gosf/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/BU-Neuromics/gosf)](https://github.com/BU-Neuromics/gosf/releases)

`gosf` is a fast, single-binary command-line tool for pushing and pulling files
to and from the [Open Science Framework](https://osf.io) (OSF). It is a
maintained replacement for the Python `osfclient`, distributed as a static
binary with no runtime dependencies.

```console
$ gosf pull abc12:/data/results.csv
$ gosf push ./figures/ abc12:/manuscript/figures/
$ gosf ls abc12:/data
```

## Features

- **Single binary** — no Python, no virtualenv; drop it on an HPC node and go.
- **Push, pull, list, remove** files in OSF Storage, plus project metadata.
- **Token auth** stored securely in your OS keychain (with a plaintext fallback
  for headless systems).
- **Scriptable** — `--output=json` on every command.
- **Safe** — `--dry-run` on push/pull/rm, conflict handling on push, a rich
  confirmation before bulk pushes, and state-based gates that refuse to silently
  clobber diverged files.
- **Idempotent** — pushing or pulling a file that already matches is a no-op; no
  redundant versions, no needless downloads.
- **Progress bars** on transfers and **colorized output** (both auto-off when not
  a TTY or under `--quiet`/`--output=json`; controllable with `--color`).
- **Ctrl-C aware** — cancels in-flight transfers cleanly and never leaves
  half-downloaded files behind.
- **Sync manifest** — declare files in `.gosf/gosf.toml` and keep them in sync
  with `gosf sync`; CI-friendly status with `gosf status`.

## For coding agents

`gosf` ships a [skills.sh](https://skills.sh) agent skill so AI coding agents can
understand and invoke it without hand-written instructions.

Install the skill in your project (supported by Claude Code, GitHub Copilot,
Codex, and 38+ other agents):

```console
npx skills add BU-Neuromics/gosf
```

The skill covers installation, authentication, path syntax, the
`.gosf/gosf.toml` manifest, every command, and common workflows. The source
lives in [`skills/gosf/SKILL.md`](./skills/gosf/SKILL.md).

## Installation

### Script (recommended)

**Linux / macOS**

```bash
curl -fsSL https://raw.githubusercontent.com/BU-Neuromics/gosf/main/install.sh | bash
```

Detects your OS and architecture, downloads the right binary, verifies the
SHA-256 checksum, and installs to `/usr/local/bin` (or `~/.local/bin` if
`/usr/local/bin` is not writable). Override the destination with
`GOSF_INSTALL_DIR`:

```bash
GOSF_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/BU-Neuromics/gosf/main/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/BU-Neuromics/gosf/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\Programs\gosf` and adds it to your user `PATH`.
Override with `$env:GOSF_INSTALL_DIR`.

### Pre-built binary (manual)

Download the archive for your platform from the
[releases page](https://github.com/BU-Neuromics/gosf/releases), verify the
checksum against `checksums.txt`, extract, and put `gosf` on your `PATH`:

```console
# Linux x86_64 example
tar -xzf gosf_*_linux_amd64.tar.gz
sudo mv gosf /usr/local/bin/
```

### With Go

```console
go install github.com/BU-Neuromics/gosf@latest
```

### From source

```console
git clone https://github.com/BU-Neuromics/gosf
cd gosf
go build -o gosf .
```

## Authentication

Most public projects can be read without authentication. To access private
projects or to upload, you need a personal access token.

1. Create a token at <https://osf.io/settings/tokens/> (grant the scopes you
   need, e.g. `osf.full_write`).
2. Log in:

   ```console
   $ gosf auth login
   Enter your OSF personal access token: ********
   Logged in as Ada Lovelace (a1b2c)
   ```

3. Check status anytime:

   ```console
   $ gosf auth status
   Logged in as: Ada Lovelace (a1b2c)
   Token from:   OS keychain
   ```

### Where the token comes from

`gosf` resolves a token from the first source that has one:

| Priority | Source |
|----------|--------|
| 1 | `--token` flag |
| 2 | `OSF_TOKEN` environment variable |
| 3 | Token file (`~/.config/gosf/token`) |
| 4 | OS keychain |

If none are set, `gosf` runs unauthenticated (public projects only).

### Headless / HPC systems

If there's no OS keychain available, store the token in a local token file instead:

```console
gosf auth login --no-keychain
```

The token is written to `~/.config/gosf/token` with mode `0600`. This file is
separate from `config.toml` so the config file remains safe to commit to version
control.

Or skip persistent storage entirely and use the environment variable:

```console
export OSF_TOKEN=your-token-here
gosf ls abc12
```

The token is never printed in logs or error output.

### Logging out

```console
gosf auth logout
```

Removes the token file and (best-effort) the OS keychain entry. If the keychain
is locked or unavailable, logout still succeeds and prints a warning — the token
file, which gosf controls directly, is always removed.

## Path syntax

OSF locations are written as `<guid>[:<path>]`:

```
abc12                         # the project/component with GUID abc12
abc12:/data/results/file.csv  # a file inside OSF Storage
abc12:/data                   # a folder inside OSF Storage
abc12/xyz34:/path             # path inside component xyz34 of project abc12
```

- A **GUID** is the 5-character identifier from an OSF URL
  (`https://osf.io/abc12/` → `abc12`).
- The part after `:` is a path within the project's **OSF Storage** provider.
- Components (sub-projects) are addressed as `parent/child:/path`.

## Commands

### `gosf onboard`

Guided, interactive setup — the easiest way to start. It detects your current
state and resumes at the right step, so it's safe to re-run:

1. **Authenticate** (offered if you're not logged in).
2. **Attach a project** — type a GUID or pick from your project list.
3. **Select files to push** — a collapsible file-tree of the things git doesn't
   track (data, models, artifacts); check individual files or whole directories.

It records your picks in `.gosf/gosf.toml` as `direction=push` entries and stops
there; run `gosf sync` to upload. Requires an interactive terminal.

```console
$ gosf onboard
$ gosf onboard --project abc12 --remote-base /inputs   # skip the prompts
```

### `gosf ls <project>[:<path>]`

List files and folders.

```console
$ gosf ls abc12:/data
NAME          SIZE      MODIFIED
results/      —         2024-02-02 12:00
notes.txt     1.2 KB    2024-01-15 09:30
```

### `gosf pull <project>[:<path>] [dest]`

Download a file or an entire folder tree.

```console
$ gosf pull abc12:/data/results.csv            # → ./results.csv
$ gosf pull abc12:/data/results.csv out.csv    # → ./out.csv
$ gosf pull abc12:/data/ ./local-copy          # download the folder tree
$ gosf pull abc12: --dry-run                   # preview a whole-project pull
$ gosf pull abc12:/data/counts.h5 --version=2  # download a specific version
```

A pull is **idempotent**: if the local file already matches the remote (same
MD5), the download is skipped. With no path argument, `gosf pull` pulls the
pull-eligible entries from `.gosf/gosf.toml`.

Flags:
- `--version=<n>` — download a specific historical version instead of the latest.
  Only valid for single-file targets; errors for directory pulls.
- `--force` — overwrite a locally-modified file with the tracked version.
- `--resolve=theirs` — resolve a diverged file (changed both locally and
  remotely) by taking the remote copy.
- `--dry-run` — list what would be downloaded without writing any files.

### `gosf push <src> <project>:<path>`

Upload a file or directory. With a trailing slash, the destination is treated
as a folder and the source filename is kept.

```console
$ gosf push results.csv abc12:/data/results.csv
$ gosf push ./figures/  abc12:/manuscript/figures/
$ gosf push data.csv    abc12:/data/data.csv --conflict=overwrite
```

A push is **idempotent**: uploading a file whose bytes already match the remote
is skipped rather than minting a redundant version.

Conflict handling for an *explicit* push (`--conflict`, default `skip`):

| Mode | Behaviour when a file already exists |
|------|--------------------------------------|
| `skip` | Leave the existing file untouched (default) |
| `overwrite` | Replace it with the local file (new version) |
| `rename` | Upload as `name_1.ext`, `name_2.ext`, … |

With no arguments, `gosf push` pushes the push-eligible entries from
`.gosf/gosf.toml`. Because that writes remote data, it prints a per-file plan
(project title + visibility, the action per file, sizes and MD5s) and asks for
confirmation:

- `--yes` — skip the prompt for a safe push (new files, real updates).
- `--force` — also authorize a *rollback* (overwriting a newer remote version).
- `--resolve=ours` — resolve a diverged file by taking the local copy.
- In `--output=json` mode, `--force` is required (there is no prompt).

### `gosf rm <project>:<path>`

Delete a file or folder. Prompts for confirmation unless `--yes` is given.

```console
$ gosf rm abc12:/data/old.csv
Delete /data/old.csv from project abc12? [y/N]: y
Deleted /data/old.csv

$ gosf rm abc12:/scratch/ --yes
```

### `gosf projects`

List the projects and components you can access (requires auth).

### `gosf info <project>`

Show project/component metadata.

### `gosf open <project>[:<path>]`

Open the project or file in your web browser.

### `gosf versions <project>:<path>`

List all versions of a file, newest first.

```console
$ gosf versions abc12:/data/counts.h5
VERSION  DATE                  SIZE     CONTRIBUTOR
3        2024-03-01 09:15 UTC  14.2 MB  ada@example.com
2        2024-02-10 14:30 UTC  13.8 MB  ada@example.com
1        2024-01-20 08:00 UTC  12.1 MB  ada@example.com
```

### `gosf init <project-id>`

Create or update `.gosf/gosf.toml` in the current directory, setting the default
project GUID. Existing `[[files]]` entries are preserved.

```console
$ gosf init abc12
```

### `gosf mkdir <project>:<path>`

Create a folder in OSF Storage (the parent folder must already exist).

### `gosf mv <src> <dest>` / `gosf cp <src> <dest>`

Move/rename or copy a file or folder within or across projects
(`--conflict=keep|replace|warn`).

### `gosf set <project>`

Update a project's title, description, category, and/or tags (only the flags you
pass are changed).

### `gosf add <local-path> [<project>:]<remote-path>`

Stage a file (or directory, recursively) in the `.gosf/gosf.toml` manifest for
**push** (creates the manifest if absent). To record a file for *pull* instead,
use `gosf pull`, which tracks what it downloads. If the remote path is omitted it
mirrors the local path.

```console
$ gosf add results/model.pkl abc12:/results/model.pkl
$ gosf add data/raw/ abc12:/data/raw/        # add a directory, one entry per file
$ gosf add notes.md                          # remote mirrors the local path
```

If the file already exists on OSF its current version and MD5 are recorded in
the manifest automatically. Files larger than 50 MB get a `.gitignore` tip.

### `gosf status`

Show the sync state of every file in `.gosf/gosf.toml`.

```console
$ gosf status
DIR   STATUS    LOCAL PATH             VER   DETAIL
pull  ✓         data/counts.h5         v3
push  AHEAD     results/model.pkl      v1    unpushed changes
pull  BEHIND    shared/ref.csv         v2    remote is v3
pull  ≡         inputs/ref.csv         —     identical to remote v2, unpinned — run sync
push  DIVERGED  notes/summary.md       v1    local and remote both changed since v1
push  ·         outputs/report.pdf     —     never pushed
```

Status is read-only — it reports, it never mutates the manifest. It also
content-compares **unpinned** (`version = 0`) entries against the remote, so an
already-identical file shows `≡`/`PIN_ONLY` rather than a blanket "never pushed".

Exit code 0 when everything is `IN_SYNC`; exit code 1 otherwise — useful in CI.

Flags:
- `--no-check-remote` — skip remote version lookups (faster; cannot detect
  `BEHIND` or `REMOTE_NEWER`).

### `gosf sync`

Reconcile files with OSF according to the manifest. Push-eligible entries
(`direction=push` or `both`) are pushed; pull-only entries (`direction=pull`) are
pulled. `sync` is non-interactive and fails hard (before transferring anything)
on a diverged file.

```console
$ gosf sync                       # push push/both entries, pull pull entries
$ gosf sync --dry-run             # preview without making changes
$ gosf sync --force               # authorize rollbacks / overwrite local edits
$ gosf sync --resolve=theirs      # resolve diverged files by taking remote
```

Actions are chosen from the file's state, comparing **L**ocal, the pinned
**B**aseline, and the **R**emote latest:

| State | Action |
|-------|--------|
| `IN_SYNC` | ✓ skip |
| `PIN_ONLY` (local already matches remote) | record the pin, no transfer |
| `AHEAD_OF_MANIFEST` (only local changed) | push new version |
| `REMOTE_NEWER` (only remote changed) | pull: fast-forward; push: refuse unless `--force` |
| `BEHIND` | pull: restore; push: refuse unless `--force` |
| `NOT_PUSHED` (file exists) | push new file |
| `MISSING` / `NOT_PUSHED` (missing) | pull to restore / skip |
| `DIVERGED` (both changed) | **fail hard** — needs `--resolve=ours\|theirs` |

Flags:
- `--force` — authorize a remote-newer/behind rollback, and overwrite a
  locally-modified file on pull. Does **not** cover divergence.
- `--resolve=ours\|theirs` — resolve diverged entries (`ours` keeps local,
  `theirs` keeps remote).
- `--dry-run` — show what would happen without making any changes.
- `--no-check-remote` — skip remote version lookups (faster; cannot detect
  `BEHIND` or `REMOTE_NEWER`).

## Sync manifest (`.gosf/gosf.toml`)

The manifest lives at `.gosf/gosf.toml` in your repository root (gosf walks up
from the current directory to find it). Create it with `gosf init <project-id>`,
or let `gosf add` / `gosf pull` create it for you. It declares which OSF files
belong to the project:

```toml
[project]
id = "abc12"          # default OSF project GUID

[[files]]
local     = "data/counts.h5"       # path relative to repo root
remote    = "/data/counts.h5"      # path within OSF Storage
direction = "pull"                 # "push", "pull", or "both"
version   = 3                      # pinned OSF version; 0 = not yet pushed
md5       = "d41d8cd98f00b204e..."  # MD5 of the pinned version

[[files]]
local     = "results/model.pkl"
remote    = "/results/model.pkl"
direction = "push"
version   = 1
md5       = "0cc175b9c0f1b6..."

[[files]]
local     = "data/ref.csv"
remote    = "/data/ref.csv"
direction = "both"
version   = 0        # not yet pushed — md5 left blank
md5       = ""
project   = "xyz89"  # per-entry project override
```

The manifest is updated automatically when you run `gosf sync` or
`gosf push` — you rarely need to edit it by hand.

## Scripting with JSON

Every command accepts `--output=json`, writing structured JSON to stdout
(progress bars are suppressed automatically):

```console
$ gosf ls abc12:/data --output=json
[{"id":"...","attributes":{"name":"results","kind":"folder", ...}}, ...]

$ gosf push data.csv abc12:/data/data.csv --output=json
{"uploaded": [{"path": "/data/data.csv", "action": "upload"}], "dry_run": false}

$ gosf status --output=json
[{"path":"data/counts.h5","direction":"pull","state":"IN_SYNC","declared_version":3}]

$ gosf sync --output=json
[{"path":"results/model.pkl","state":"AHEAD_OF_MANIFEST","declared_version":1,"action_taken":"push"}]

$ gosf versions abc12:/data/counts.h5 --output=json
{"versions": [{"version":3,"date_created":"2024-03-01T09:15:00","size":14900000,"contributor":"ada@example.com"}]}

$ gosf add data/new.csv abc12:/data/new.csv --output=json
{"entries":[{"local":"data/new.csv","remote":"/data/new.csv","project":"abc12","version":0,"md5":""}],"manifest_created":false}
```

In JSON mode, `gosf rm` requires `--yes` (there is no interactive prompt).

## Global flags

| Flag | Description |
|------|-------------|
| `--token <token>` | Use this OSF token (overrides env/config/keychain) |
| `--output text\|json` | Output format (default `text`) |
| `--color auto\|always\|never` | Colorize output (default `auto`: on only at a TTY, off under `--output=json`/`--quiet`/`NO_COLOR`) |
| `--quiet`, `-q` | Suppress progress and non-error output |
| `--version` | Print the version |

## Exit codes

`gosf` exits non-zero on any error, so it composes cleanly in scripts and CI.

## Development

```console
go build -o gosf .     # build
go test ./...          # run tests
go test -race ./...    # run tests with the race detector
go vet ./...           # static checks
gofmt -l .             # formatting check (should print nothing)
```

This project follows **test-driven development**: write a failing test first,
then the code that makes it pass; every bug fix starts with a regression test.
See [`CLAUDE.md`](./CLAUDE.md) for the full development guide, architecture, and
the OSF API notes.

## License

See [LICENSE](./LICENSE).
