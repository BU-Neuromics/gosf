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
- **Safe** — `--dry-run` on push/pull/rm, conflict handling on push, and
  confirmation prompts before deletion.
- **Progress bars** on transfers, suppressible with `--quiet`.
- **Ctrl-C aware** — cancels in-flight transfers cleanly and never leaves
  half-downloaded files behind.

## Installation

### Pre-built binary (recommended)

Download the archive for your platform from the
[releases page](https://github.com/BU-Neuromics/gosf/releases), extract it, and
put `gosf` on your `PATH`:

```console
# Example: Linux x86_64
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
| 3 | Config file (`~/.config/gosf/config.toml`) |
| 4 | OS keychain |

If none are set, `gosf` runs unauthenticated (public projects only).

### Headless / HPC systems

If there's no OS keychain available, store the token in the config file instead:

```console
gosf auth login --no-keychain
```

Or skip persistent storage entirely and use the environment variable:

```console
export OSF_TOKEN=your-token-here
gosf ls abc12
```

The token is never printed in logs or error output.

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
```

### `gosf push <src> <project>:<path>`

Upload a file or directory. With a trailing slash, the destination is treated
as a folder and the source filename is kept.

```console
$ gosf push results.csv abc12:/data/results.csv
$ gosf push ./figures/  abc12:/manuscript/figures/
$ gosf push data.csv    abc12:/data/data.csv --conflict=overwrite
```

Conflict handling (`--conflict`, default `skip`):

| Mode | Behaviour when a file already exists |
|------|--------------------------------------|
| `skip` | Leave the existing file untouched (default) |
| `overwrite` | Replace it with the local file |
| `rename` | Upload as `name_1.ext`, `name_2.ext`, … |

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

## Scripting with JSON

Every command accepts `--output=json`, writing structured JSON to stdout
(progress bars are suppressed automatically):

```console
$ gosf ls abc12:/data --output=json
[
  {"id":"...","attributes":{"name":"results","kind":"folder", ...}},
  ...
]

$ gosf push data.csv abc12:/data/data.csv --output=json
{
  "uploaded": [{"path": "/data/data.csv", "action": "upload"}],
  "dry_run": false
}
```

In JSON mode, `gosf rm` requires `--yes` (there is no interactive prompt).

## Global flags

| Flag | Description |
|------|-------------|
| `--token <token>` | Use this OSF token (overrides env/config/keychain) |
| `--output text\|json` | Output format (default `text`) |
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
