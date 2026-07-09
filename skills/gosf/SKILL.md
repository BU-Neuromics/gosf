---
name: gosf
description: "Use when working with the Open Science Framework (OSF) for research data management. Invoke when: the project contains a .gosf/gosf.toml manifest; the user mentions OSF, osf.io, or osfclient; the task involves syncing, pushing, or pulling research data files with an OSF project; or you need to inspect, manage, or automate files stored in OSF Storage. Covers the full gosf CLI: manifest management (gosf init / add / status / sync), file transfer (gosf pull / push / rm), storage management (gosf mkdir / mv / cp), project navigation (gosf ls / info / projects / versions / open / set), and authentication (gosf auth)."
metadata:
  version: "1.1.0"
---

# gosf — Open Science Framework CLI

`gosf` is a single-binary CLI for pushing, pulling, and syncing files with
the [Open Science Framework](https://osf.io) (OSF). It replaces the
unmaintained Python `osfclient`.

## Installation

If `gosf` is not already on `PATH` (`gosf --version` to check), install it:

```bash
# Linux / macOS — downloads the right binary, verifies the checksum
curl -fsSL https://raw.githubusercontent.com/BU-Neuromics/gosf/main/install.sh | bash

# With Go
go install github.com/BU-Neuromics/gosf@latest
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/BU-Neuromics/gosf/main/install.ps1 | iex
```

Or download a prebuilt archive from
<https://github.com/BU-Neuromics/gosf/releases> and put `gosf` on `PATH`. It is a
static binary with no runtime dependencies — suitable for HPC nodes.

## Authentication

Public projects are readable without auth. A token is needed for private
projects or any write. Tokens are resolved in priority order:
1. `--token` flag
2. `OSF_TOKEN` environment variable
3. `~/.config/gosf/token` (written by `gosf auth login --no-keychain`)
4. OS keychain (written by `gosf auth login`)

Check status: `gosf auth status`  ·  Log in: `gosf auth login`  ·  Log out: `gosf auth logout`

On HPC/headless systems without a keychain, use `OSF_TOKEN` or `--no-keychain`.
`gosf auth logout` is best-effort on the keychain: it always removes the token
file and only warns if the keychain is unavailable.

## Path syntax

```
abc12                         # project/component GUID (5 chars)
abc12:/data/results.csv       # file inside OSF Storage
abc12:/data/                  # folder inside OSF Storage
abc12/xyz34:/path             # path inside component xyz34 of project abc12
```

GUIDs appear in OSF URLs: `https://osf.io/abc12/` → `abc12`.

## .gosf/gosf.toml manifest

The manifest declares which files belong to the project and how they flow. It
lives at `.gosf/gosf.toml`; gosf walks up from the current directory to find it.
Create it with `gosf init <project-id>`, or let `gosf add`/`gosf pull` create it.

```toml
[project]
id = "abc12"          # default project GUID for all entries

[[files]]
local     = "data/counts.h5"       # path relative to repo root
remote    = "/data/counts.h5"      # path in OSF Storage
direction = "pull"                 # "push", "pull", or "both" (REQUIRED)
version   = 3                      # pinned OSF version; 0 = not yet pushed
md5       = "d41d8cd98f00b204..."  # MD5 of pinned version; "" if version=0
project   = "xyz89"                # optional: override [project].id for this entry
```

**Direction is a default intent, not a hard lock.** It sets what `gosf sync` does
by default; safety comes from state-based gates at the moment of a destructive
action (see below), not from blanket refusals.
- `pull` — sync pulls this file
- `push` — sync pushes this file
- `both` — sync pushes it (push takes precedence for `both`)

**File states** (from `gosf status`), comparing Local / pinned Baseline / Remote:

| State | Meaning |
|-------|---------|
| `IN_SYNC` | Local matches the pinned version and the remote |
| `PIN_ONLY` | Local already matches the remote, but the pin is stale/absent — record it, no transfer |
| `MISSING` | Local file does not exist |
| `BEHIND` | Local matches an older remote version |
| `AHEAD_OF_MANIFEST` | Only local changed since the baseline |
| `REMOTE_NEWER` | Only the remote changed since the baseline |
| `DIVERGED` | Both local and remote changed — unsafe to transfer automatically |
| `NOT_PUSHED` | version = 0 and nothing on the remote to compare |

`gosf status` is read-only and exits 0 only if all files are `IN_SYNC`, 1
otherwise — safe to use in CI. It content-compares unpinned (`version=0`) entries
against the remote instead of blindly reporting "never pushed".

## Command reference

### Manifest commands

```bash
gosf onboard [--project <guid>] [--remote-base <path>]  # interactive guided setup (TTY only)
gosf init <project-id>                              # create/update .gosf/gosf.toml
gosf add <local-path> [<project>:]<remote-path>     # stage file(s) for push (dir = recursive)
gosf status [--no-check-remote] [--output=json]     # show sync state of all entries
gosf sync [--force] [--resolve=ours|theirs] [--dry-run] [--no-check-remote] [--output=json]
```

`gosf onboard` is a resumable, interactive wizard (auth → attach a project → pick
git-untracked files to push via a tree checkbox UI). It writes `direction=push`
entries and stops; run `gosf sync` to upload. TTY only — for scripting/agents,
use `init` + `add` + `sync` directly.

`gosf add` always stages for **push**; to record a file for pull, use `gosf pull`
(it tracks what it downloads). If the remote path is omitted it mirrors the local
path.

`gosf sync` reconciles per state (pushes push/both entries, pulls pull entries):

| State | Action |
|-------|--------|
| `IN_SYNC` | Skip |
| `PIN_ONLY` | Record the pin (version+MD5), no transfer |
| `AHEAD_OF_MANIFEST` | Push a new version, update manifest |
| `NOT_PUSHED` (file exists) | Push, set manifest version+MD5 |
| `NOT_PUSHED` (file missing) / `MISSING` | Skip / pull to restore |
| `REMOTE_NEWER` | pull: fast-forward + re-pin; push: refuse unless `--force` |
| `BEHIND` | pull: restore baseline; push: refuse unless `--force` |
| `DIVERGED` | **Fail hard** — requires `--resolve=ours\|theirs` |

- `--force` authorizes a remote-newer/behind **rollback** and overwriting a
  locally-modified file on pull. It does **not** cover divergence.
- `--resolve=ours` keeps local; `--resolve=theirs` keeps remote.

### File transfer

```bash
gosf pull <project>[:<path>] [dest] [--version=N] [--force] [--resolve=theirs] [--dry-run]
gosf push <src> <project>:<path> [--conflict=skip|overwrite|rename] [--dry-run]
gosf push [--yes|--force] [--resolve=ours]          # no args: push manifest entries
gosf rm <project>:<path> [--yes] [--dry-run]
```

- Both `pull` and `push` are **idempotent**: a transfer whose content already
  matches is skipped (no redundant version, no needless download).
- Explicit `push <src> <dest>` uses `--conflict` (default `skip`; `overwrite`
  creates a new version; `rename` → `name_1.ext`).
- Bare `gosf push` (manifest-driven) prints a per-file plan and prompts before
  writing remote data. `--yes` skips the prompt for safe pushes; `--force` also
  authorizes a rollback. **In `--output=json`, `--force` is required** (no prompt).
- `pull --version=N` fetches a historical version (single-file targets only).

### Storage management

```bash
gosf mkdir <project>:<path>          # create a folder (parent must exist)
gosf mv <src> <dest>                 # move/rename within or across projects
gosf cp <src> <dest>                 # copy within or across projects
```

`mv`/`cp` accept `--conflict=keep|replace|warn`.

### Project navigation

```bash
gosf ls <project>[:<path>] [--output=json]     # list files/folders
gosf info <project> [--output=json]            # project metadata
gosf projects [--output=json]                  # list accessible projects (needs auth)
gosf versions <project>:<path> [--output=json] # list file versions (files only)
gosf open <project>[:<path>] [--output=json]   # open in browser (or print URL)
gosf set <project> [--title ...] [--description ...] [--category ...] [--tags ...]
```

## Common workflows

### Set up a new project and pull inputs

```bash
gosf init abc12                                  # 1. create .gosf/gosf.toml
gosf pull abc12:/data/ ml/data/                  # 2. pull + track (direction=pull)
gosf status                                      # 3. verify → all ✓ IN_SYNC
```

Pulling files that are already present locally and identical is a no-op that just
records the pin — no redundant downloads.

### Push locally modified outputs

```bash
gosf add results/model.pkl abc12:/results/model.pkl   # stage for push (once)
gosf sync --dry-run                                   # preview
gosf sync --yes                                       # push (skip the prompt)
```

### Resolve a divergence

```bash
# gosf sync failed hard: notes.md changed both locally and on OSF.
gosf pull abc12:/notes.md --resolve=theirs   # take remote (discard local), or
gosf push notes.md --resolve=ours            # take local (discard remote)
```

### Check whether everything is in sync (CI)

```bash
gosf status --no-check-remote   # fast: no remote API calls
gosf status                     # full: checks BEHIND / REMOTE_NEWER / DIVERGED
# exits 0 if all IN_SYNC, 1 otherwise
```

## Global flags

| Flag | Description |
|------|-------------|
| `--token <tok>` | OSF token (overrides env/file/keychain) |
| `--output json` | JSON to stdout; progress/color suppressed |
| `--color auto\|always\|never` | Colorize output (default `auto`) |
| `--quiet` / `-q` | Suppress progress bars and informational output |
| `--version` | Print gosf version |

## JSON output

Every command supports `--output=json` (stdout; errors/progress on stderr). JSON
is never colorized. In `--output=json` mode, `gosf rm` and a bytes-writing
`gosf push` both require an explicit flag (`--yes` / `--force`) — there is no
prompt.

## Constraints to respect

- A `DIVERGED` file (changed both locally and remotely) blocks `sync`/`push`/`pull`
  until you pass `--resolve=ours|theirs`; plain `--force` will not override it.
- `--force` authorizes a rollback (overwriting a newer remote version) or
  overwriting local edits on pull — a deliberate, one-sided discard.
- Bare `gosf push` and `gosf rm` require `--yes`/`--force` in `--output=json` mode.
- `gosf versions` works on files, not folders.
- New files/folders upload into the parent folder resolved from OSF (osfstorage
  addresses folders by ID); the parent must exist (`gosf mkdir` first if needed).
- The manifest is updated atomically after every successful push, pull, or sync.
