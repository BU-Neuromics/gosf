---
name: gosf
description: "Use when working with the Open Science Framework (OSF) for research data management. Invoke when: the project contains a gosf.toml manifest; the user mentions OSF, osf.io, or osfclient; the task involves syncing, pushing, or pulling research data files with an OSF project; or you need to inspect, manage, or automate files stored in OSF Storage. Covers the full gosf CLI: manifest management (gosf add / status / sync), file transfer (gosf pull / push / rm), project navigation (gosf ls / info / projects / versions / open), and authentication (gosf auth)."
metadata:
  version: "1.0.0"
---

# gosf — Open Science Framework CLI

`gosf` is a single-binary CLI for pushing, pulling, and syncing files with
the [Open Science Framework](https://osf.io) (OSF). It replaces the
unmaintained Python `osfclient`.

## Authentication

Tokens are resolved in priority order:
1. `--token` flag
2. `OSF_TOKEN` environment variable
3. `~/.config/gosf/token` (written by `gosf auth login --no-keychain`)
4. OS keychain (written by `gosf auth login`)

Check auth status: `gosf auth status`  
Log in interactively: `gosf auth login`  
Log out: `gosf auth logout`

On HPC/headless systems without a keychain, use `OSF_TOKEN` or `--no-keychain`.

## Path syntax

```
abc12                         # project/component GUID (5 chars)
abc12:/data/results.csv       # file inside OSF Storage
abc12:/data/                  # folder inside OSF Storage
abc12/xyz34:/path             # path inside component xyz34 of project abc12
```

GUIDs appear in OSF URLs: `https://osf.io/abc12/` → `abc12`.

## gosf.toml manifest

`gosf.toml` declares which files belong to the project and how they flow.
It lives at the repository root (or any parent directory).

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

**Direction semantics:**
- `pull` — agent/CI pulls this file; refuse to push it
- `push` — agent/CI pushes this file; safe to overwrite remote
- `both` — flows in either direction depending on state

**File states** (from `gosf status`):

| State | Meaning |
|-------|---------|
| `IN_SYNC` | Local MD5 matches pinned version |
| `MISSING` | Local file does not exist |
| `BEHIND` | Local MD5 matches an older remote version |
| `AHEAD_OF_MANIFEST` | Local file differs from any known remote version |
| `REMOTE_NEWER` | In sync with manifest but remote has newer versions |
| `NOT_PUSHED` | version = 0; file has never been pushed |

`gosf status` exits 0 if all files are IN_SYNC, 1 otherwise — safe to use in CI.

## Command reference

### Manifest commands

```bash
# Add a file to gosf.toml (creates it if absent)
gosf add <local-path> <project>:<remote-path> [--direction=push|pull|both]

# Show sync state of all manifest entries
gosf status [--no-check-remote] [--output=json]

# Sync files according to manifest (push eligible entries by default)
gosf sync [--pull-new] [--force] [--dry-run] [--no-check-remote] [--output=json]
```

`gosf sync` push actions by state:

| State | Action |
|-------|--------|
| `IN_SYNC` | Skip |
| `AHEAD_OF_MANIFEST` | Push, update manifest version+MD5 |
| `NOT_PUSHED` (file exists) | Push, set manifest version+MD5 |
| `NOT_PUSHED` (file missing) | Skip |
| `MISSING` | Skip |
| `BEHIND` / `REMOTE_NEWER` | Push as new version, update manifest |

With `--pull-new`: also pulls MISSING/BEHIND entries with `direction=pull` or `both`.

### File transfer

```bash
# Download a file or folder tree
gosf pull <project>[:<path>] [dest] [--version=N] [--dry-run] [--quiet]

# Upload a file or directory
gosf push <src> <project>:<path> [--conflict=skip|overwrite|rename] [--dry-run] [--quiet]

# Delete a file or folder (prompts unless --yes)
gosf rm <project>:<path> [--yes] [--dry-run]
```

`push --conflict` modes (default: `skip`):
- `skip` — leave existing file untouched
- `overwrite` — replace with new version
- `rename` — upload as `name_1.ext`, `name_2.ext`, …

### Project navigation

```bash
gosf ls <project>[:<path>] [--output=json]          # list files/folders
gosf info <project> [--output=json]                  # project metadata
gosf projects [--output=json]                        # list accessible projects
gosf versions <project>:<path> [--output=json]       # list file versions
gosf open <project>[:<path>] [--output=json]         # open in browser (or print URL)
```

## Common workflows

### Set up a new project

```bash
# 1. Create gosf.toml with your first file
gosf add data/counts.h5 abc12:/data/counts.h5 --direction=pull

# 2. Pull the file
gosf sync --pull-new

# 3. Check status
gosf status
```

### Push locally modified files

```bash
gosf sync                  # pushes all AHEAD_OF_MANIFEST and NOT_PUSHED entries
gosf sync --dry-run        # preview first
```

### Pull updated remote files

```bash
gosf sync --pull-new       # pulls MISSING and BEHIND pull-eligible entries
gosf sync --pull-new --force  # overwrite locally modified files
```

### Download a specific historical version

```bash
gosf pull abc12:/data/counts.h5 --version=2
```

### Check whether everything is in sync (CI)

```bash
gosf status --no-check-remote   # fast: no remote API calls
gosf status                     # full: checks for BEHIND / REMOTE_NEWER
# exits 0 if all IN_SYNC, 1 otherwise
```

## Global flags

| Flag | Description |
|------|-------------|
| `--token <tok>` | OSF token (overrides env/file/keychain) |
| `--output json` | JSON to stdout; progress suppressed |
| `--quiet` / `-q` | Suppress progress bars and informational output |
| `--version` | Print gosf version |

## JSON output

Every command supports `--output=json`. Output goes to stdout; errors and
progress bars go to stderr. In JSON mode, `gosf rm` requires `--yes`.

## Constraints to respect

- Never push a file whose `direction=pull`; gosf will refuse with an error.
- `gosf rm` requires `--yes` in `--output=json` mode.
- `gosf versions` only works on files, not folders.
- `gosf sync --force` only affects pull-eligible entries; it does not force-push.
- The manifest is updated atomically after every successful push or sync.
