# gosf

[![CI](https://github.com/BU-Neuromics/gosf/actions/workflows/ci.yml/badge.svg)](https://github.com/BU-Neuromics/gosf/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/BU-Neuromics/gosf)](https://github.com/BU-Neuromics/gosf/releases)

The [Open Science Framework](https://osf.io) — free and open, from the Center for
Open Science — has become one of the research community's most valuable pieces of
open infrastructure for managing, archiving, and citing the material behind a
paper.

`gosf` configures and synchronizes the *content* of an OSF project: files,
folders, metadata, and wiki pages, driven from a repository. You declare what
belongs in the project in a manifest, keep that manifest under version control
next to the code that produces the results, and reconcile the two with one
command.

The case it was built for is authoring a paper's supplemental materials as
markdown in the repository — prose, figures, tables, generated results — and
keeping them reconciled with a public, citable OSF project or component. The
supplement is written where the analysis lives, reviewed alongside the code that
produced it, and published by running `gosf sync`. What a reader cites and what
the repository contains stay the same thing.

```console
$ gosf sync                                        # reconcile everything the manifest tracks
$ gosf status                                      # what differs, before you touch anything
$ gosf wiki push docs/supplement.md abc12:Supplement
$ gosf pull abc12:/data/results.csv
```

## Features

- **Project wikis** — read, write, and sync a project's versioned markdown wiki
  pages (`gosf wiki`), including manifest-driven sync of local `.md` files. This is
  how a supplement written in the repository becomes a page on OSF.
- **Sync manifest** — declare files and wiki pages in `.gosf/gosf.toml` and
  reconcile them with `gosf sync`; CI-friendly reporting with `gosf status`.
- **Push, pull, list, remove** files in OSF Storage, plus project and component
  metadata.
- **Safe** — `--dry-run` on push/pull/rm, conflict handling on push, a rich
  confirmation before bulk pushes, and state-based gates that refuse to silently
  clobber diverged files.
- **Idempotent** — pushing or pulling something that already matches is a no-op;
  no redundant versions, no needless downloads.
- **Scriptable** — `--output=json` on every command.
- **Token auth** stored securely in your OS keychain (with a plaintext fallback
  for headless systems).
- **Single binary** — no Python, no virtualenv; drop it on an HPC node and go.
- **Progress bars** on transfers and **colorized output** (both auto-off when not
  a TTY or under `--quiet`/`--output=json`; controllable with `--color`).
- **Ctrl-C aware** — cancels in-flight transfers cleanly and never leaves
  half-downloaded files behind.

## Scope

`gosf` configures the content surface of an OSF project or component as a whole:
the files and folders in OSF Storage, the project's and components' metadata, and
the wiki pages — not file transfer alone. Its scope is bounded by what OSF is, and
a manifest describes a project the way you want it to look rather than a sequence
of transfers to perform. `gosf` is not a general-purpose data management or
pipeline tool and is not meant to be compared to one; it configures OSF project
content. It began as an effort to build on
[`osfclient`](https://github.com/osfclient/osfclient), the Python library and CLI
for OSF file transfer, and grew into a tool covering project content more broadly.

## Identifiers and persistence

What you can cite when you manage a supplement with `gosf`, and what each
identifier actually promises:

- **A wiki page has a persistent URL, but no DOI of its own.** The citable
  identifier is the enclosing project's or component's. There is no per-page DOI to
  put in a reference list.
- **Project and component DOIs resolve to current state.** Pushing a wiki revision
  changes what that DOI shows. A citation to a live project DOI pins the *object*,
  not the version of the supplement a reader or reviewer saw.
- **A registration is frozen and separately DOI'd**, which is how a supplement gets
  pinned at submission time. Registering copies wiki content *and its full version
  history* into a read-only snapshot with its own identifier. Two caveats worth
  knowing before you rely on it, both detailed in
  [`docs/osf-registration-findings.md`](./docs/osf-registration-findings.md):
  - **Embedded figures are not necessarily frozen with the text.** OSF's wiki
    editor references dropped images by a storage link into the *live* project that
    names no particular file version, and registration copies wiki content
    verbatim without rewriting those links. Re-uploading a figure can therefore
    change what an already-registered wiki renders. If a frozen figure matters,
    reference it by a URL that pins an explicit file revision.
  - **A registration is not public the instant you create it.** It starts private
    and pending, and its DOI appears when it becomes public — after an admin
    approves it or the 48-hour approval window elapses. Plan for that lag rather
    than registering on the day of submission.
- **Recommended pattern: one public component per paper**, with its own DOI and its
  own wiki. A component registers independently of its parent and carries its own
  identifier, which makes it the natural unit for a single paper's supplement.

`gosf` does not create registrations today; the findings document works out what
such a command could and could not promise. Claims here are sourced to OSF's own
documentation and implementation, and the findings document marks which are
verified and which still need a live check.

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

It records your picks in `.gosf/gosf.toml` and stops there; run `gosf sync` to
upload. Requires an interactive terminal.

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
MD5), the download is skipped. With no path argument, `gosf pull` downloads every
tracked entry that is missing locally or behind the remote; locally modified
files are reported and left alone unless `--force` is given.

Flags:
- `--version=<n>` — download a specific historical version instead of the latest.
  Only valid for single-file targets; errors for directory pulls.
- `--force` — overwrite a locally-modified file with the tracked version.
- `--track-only` — record the matched files in `.gosf/gosf.toml` without
  transferring any bytes, so a large remote can be adopted and reviewed before it
  is downloaded. The entries land as `MISSING`; a plain `gosf sync` then fetches
  them. (`sync` only ever visits entries in the manifest, so this is how remote
  files that nothing tracks become visible to it.)
- `--resolve=theirs` — resolve a diverged file (changed both locally and
  remotely) by taking the remote copy.
- `--dry-run` — list what would be downloaded without writing any files.

```console
$ gosf pull abc12:/data/ data/ --track-only   # register 900 files, download none
$ gosf status                                 # review what you just adopted
$ gosf sync                                   # now fetch them
```

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

With no arguments, `gosf push` publishes every tracked file that holds local work
the remote does not have — files modified since they were last synced, and files
never pushed. Because that writes remote data, it prints a per-file plan
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

Track a file (or directory, recursively) in the `.gosf/gosf.toml` manifest
(creates the manifest if absent). `gosf pull` does the same for files it
downloads. Neither fixes which way the file will move later — that is decided per
transfer, from the file's state. If the remote path is omitted it mirrors the
local path.

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
STATUS    LOCAL PATH             VER   DETAIL
✓         data/counts.h5         v3
AHEAD     results/model.pkl      v1    locally modified — push to publish, pull --force to discard
BEHIND    shared/ref.csv         v2    remote is v3
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

Reconcile files with OSF according to the manifest. `sync` takes the one correct
action for each file's state and reports the states that do not have one. It is
non-interactive and fails hard (before transferring anything) on a diverged file.

```console
$ gosf sync                       # reconcile everything with a clear answer
$ gosf sync --dry-run             # preview without making changes
$ gosf sync --force               # discard local edits, restoring from OSF
$ gosf sync --resolve=theirs      # resolve diverged files by taking remote
```

Actions are chosen from the file's state, comparing **L**ocal, the pinned
**B**aseline, and the **R**emote latest:

| State | Action |
|-------|--------|
| `IN_SYNC` | ✓ skip |
| `PIN_ONLY` (local already matches remote) | record the pin, no transfer |
| `MISSING` | download it, pin — no flag needed; nothing local is at risk |
| `BEHIND` | fast-forward to the remote's latest, re-pin |
| `REMOTE_NEWER` (only remote changed) | fast-forward to the remote's latest, re-pin |
| `NOT_PUSHED` (file exists) | upload it, pin |
| `NOT_PUSHED` (missing) | skip — nothing local, nothing remote |
| `AHEAD_OF_MANIFEST` (only local changed) | **report only**; `sync` exits non-zero |
| `DIVERGED` (both changed) | **fail hard** — needs `--resolve=ours\|theirs` |

`AHEAD_OF_MANIFEST` is the one state `sync` will not guess at: the same
difference means "publish this" for a generated output and "throw this away" for
an edited input, and no hash comparison distinguishes them. Say which you meant
with the verb — `gosf push` publishes it, `gosf pull --force` (or
`gosf sync --force`) discards it.

Flags:
- `--force` — discard local modifications, restoring the tracked version from
  OSF. Does **not** cover divergence.
- `--resolve=ours\|theirs` — resolve diverged entries (`ours` keeps local,
  `theirs` keeps remote).
- `--dry-run` — show what would happen without making any changes.
- `--no-check-remote` — skip remote version lookups (faster; cannot detect
  `BEHIND` or `REMOTE_NEWER`).

### `gosf wiki`

Manage a project's wiki — the versioned markdown pages attached to it. Pages are
addressed as `<project>:<page>`; the page name is a flat namespace (not a path),
may contain spaces, and defaults to `home` where optional.

```console
$ gosf wiki ls abc12                          # list pages
$ gosf wiki get abc12 | less                  # print the home page
$ gosf wiki get abc12:protocol protocol.md    # write a page to a file
$ gosf wiki push docs/home.md abc12:home      # create or update a page
$ gosf wiki versions abc12:home               # version history
$ gosf wiki mv abc12:draft "Final Protocol"   # rename
$ gosf wiki rm abc12:scratch --yes            # delete
$ gosf wiki open abc12:home                   # open in the browser
```

`gosf wiki push` creates the page if it does not exist, otherwise mints a new
version; an identical re-push is skipped (no redundant version). The `home` page
cannot be renamed or deleted.

**Syncing wiki pages with local markdown.** Track a markdown file as a wiki page
and it syncs like any other file:

```console
$ gosf wiki add docs/home.md abc12:home
$ gosf status        # shows the wiki row alongside files
$ gosf sync          # reconciles the page from its state, like any other entry
```

Wiki entries live under `[[wikis]]` in the manifest and reconcile through the
same pinned-baseline safety model as files (`PIN_ONLY`, `REMOTE_NEWER`,
`DIVERGED`, `--force`, `--resolve=ours|theirs`). Note that OSF normalizes wiki
content on save (CRLF line endings become LF and surrounding whitespace is
trimmed), so gosf compares a canonical form rather than raw bytes — a local file
that differs from the wiki only in line endings or a trailing newline still counts
as in sync, and pushing it again is a no-op.

## Sync manifest (`.gosf/gosf.toml`)

The manifest lives at `.gosf/gosf.toml` in your repository root (gosf walks up
from the current directory to find it). Create it with `gosf init <project-id>`,
or let `gosf add` / `gosf pull` create it for you. It declares which OSF files
belong to the project:

```toml
[project]
id = "abc12"          # default OSF project GUID

[[files]]
local   = "data/counts.h5"        # path relative to repo root
remote  = "/data/counts.h5"       # path within OSF Storage
version = 3                       # pinned OSF version; 0 = not yet pushed
md5     = "d41d8cd98f00b204e..."  # MD5 of the pinned version

[[files]]
local   = "results/model.pkl"
remote  = "/results/model.pkl"
version = 1
md5     = "0cc175b9c0f1b6..."

[[files]]
local   = "data/ref.csv"
remote  = "/data/ref.csv"
version = 0          # not yet pushed — md5 left blank
md5     = ""
project = "xyz89"    # per-entry project override

[[wikis]]
local   = "docs/home.md"   # markdown file, relative to repo root
page    = "home"           # wiki page name on OSF
version = 3                # pinned wiki version; 0 = not yet pushed
md5     = "…"              # MD5 of the pinned version's content
```

`[[wikis]]` entries track wiki pages the same way `[[files]]` track storage
files (a `local` path may appear in only one of the two). The manifest is
updated automatically when you run `gosf sync` or `gosf push` — you rarely need
to edit it by hand.

There is **no per-entry direction**. An entry says *what* is tracked; how it
moves is decided per transfer from the file's state. Manifests written by
gosf ≤ 1.9 carry a `direction` key on every entry: it is ignored with a warning
on load and dropped the next time gosf writes the file. No migration is needed.

## Scripting with JSON

Every command accepts `--output=json`, writing structured JSON to stdout
(progress bars are suppressed automatically):

```console
$ gosf ls abc12:/data --output=json
[{"id":"...","attributes":{"name":"results","kind":"folder", ...}}, ...]

$ gosf push data.csv abc12:/data/data.csv --output=json
{"uploaded": [{"path": "/data/data.csv", "action": "upload"}], "dry_run": false}

$ gosf status --output=json
[{"path":"data/counts.h5","kind":"file","state":"IN_SYNC","declared_version":3}]

$ gosf sync --output=json
[{"path":"results/model.pkl","state":"AHEAD_OF_MANIFEST","declared_version":1,"action_taken":"push"}]

$ gosf versions abc12:/data/counts.h5 --output=json
{"versions": [{"version":3,"date_created":"2024-03-01T09:15:00","size":14900000,"contributor":"ada@example.com"}]}

$ gosf add data/new.csv abc12:/data/new.csv --output=json
{"entries":[{"local":"data/new.csv","remote":"/data/new.csv","project":"abc12","version":0,"md5":""}],"manifest_created":false}

$ gosf wiki ls abc12 --output=json
[{"id":"...","name":"home","version":3,"size":128,"date_modified":"2024-03-01T09:15:00"}]

$ gosf wiki get abc12:home --output=json
{"project":"abc12","page":"home","version":3,"size":128,"content":"# Home\n..."}

$ gosf wiki push docs/home.md abc12:home --output=json
{"project":"abc12","page":"home","action":"update","version":4,"dry_run":false}
```

The wiki commands follow the same JSON conventions: `wiki push` reports
`"action"` ∈ `create|update|skip`, `wiki versions` matches `gosf versions`, and
`wiki mv`/`wiki add` emit `{node,from,to,dry_run}` / `{entries,manifest_created}`.
In `gosf status` / `gosf sync` output, each item carries `"kind":"file"|"wiki"`
so mixed manifests are unambiguous.

In JSON mode, `gosf rm` and `gosf wiki rm` require `--yes` (there is no
interactive prompt).

## Global flags

| Flag | Description |
|------|-------------|
| `--token <token>` | Use this OSF token (overrides env/config/keychain) |
| `--output text\|json` | Output format (default `text`) |
| `--color auto\|always\|never` | Colorize output (default `auto`: on only at a TTY, off under `--output=json`/`--quiet`/`NO_COLOR`) |
| `--verbose`, `-v` | Increase log verbosity (repeatable: `-v` debug, `-vv` HTTP traces + timestamps, `-vvv` max) |
| `--progress-bar`, `-p` | Show live progress bars for transfers (default: log lines) |
| `--jobs`, `-j` | Files to scan against the remote concurrently on `sync`/`status` (default 8) |
| `--quiet`, `-q` | Suppress progress and non-error output (logs drop to errors only; conflicts with `-v`) |
| `--version` | Print the version |

## Logging and output streams

`gosf` writes **results to stdout and activity to stderr**, so the two compose
cleanly:

- **stdout** — the machine/result surface: `ls`/`status`/`versions`/`projects`
  tables, `info`/`set` fields, and every `--output=json` payload.
- **stderr** — a leveled activity log (what the tool is doing): the remote-scan
  phase, per-file transfers, skips, and mutation confirmations. Colorized on a
  TTY; capture it separately with `2>run.log`.

By default you see high-level activity (`INFO`); add `-v`/`-vv`/`-vvv` for more
detail, or `--quiet` for errors only. Transfers show a one-line summary by
default — pass `-p` for the classic live progress bar. In `--output=json` mode
stdout stays pure JSON and activity logging is silenced unless you pass `-v`.

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
