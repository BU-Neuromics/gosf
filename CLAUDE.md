# gosf — Go CLI for Open Science Framework

## Project overview

`gosf` is a Go CLI replacing the stale Python `osfclient` package. Single-binary,
distributed to researchers. CLI-only (no SDK/library scope).

**Module path:** `github.com/BU-Neuromics/gosf`
**Binary name:** `gosf`
**CLI framework:** Cobra + Viper

## Command structure

```
gosf ls       <project>[:<path>]
gosf pull     <project>[:<path>] [dest]
gosf push     <src> <project>:<path>
gosf rm       <project>:<path>
gosf versions <project>:<path>
gosf projects
gosf info     <project>
gosf auth login
gosf auth status
gosf auth logout
gosf open     <project>[:<path>]
gosf add      <local-path> <project>:<remote-path>
gosf status
gosf sync
```

Path convention: `abc12:/data/results/file.csv`
- 5-char alphanumeric before the colon = OSF project/component GUID
- After the colon = path within OSF Storage
- Components (sub-projects) addressable as `abc12/xyz34:/path`

## Auth design

Priority order: `--token` flag > `OSF_TOKEN` env var > config file > OS keychain

- Absent all: unauthenticated mode (public projects only), same code paths
- Never echo token in logs or error output
- Store via go-keyring; plaintext fallback in config for headless/HPC
- Config file: `~/.config/gosf/config.toml`

## OSF API — two-tier architecture

### Tier 1 — Metadata REST API (JSON:API spec)

Base: `https://api.osf.io/v2`
Auth header: `Authorization: Bearer <token>`

Key endpoints:
- `GET /nodes/{id}/` — project metadata
- `GET /nodes/{id}/files/osfstorage/` — list files at root
- `GET /nodes/{id}/files/osfstorage/?path=/subdir/` — list files in subdir
- `GET /files/{file_id}/` — file metadata (includes download link)
- `GET /files/{file_id}/versions/` — all versions, newest-first (no `embed=user`: the OSF versions endpoint has no embeddable user relationship and returns 400 if one is requested)

### Tier 2 — Waterbutler (actual file bytes)

Base: `https://files.osf.io`

- Upload new file: `PUT https://files.osf.io/v1/resources/{node_id}/providers/osfstorage/?name={filename}`
- Upload existing: PUT to the file's `upload` link from metadata API (creates a new version)
- Download: follow the `download` link from file metadata response
- Download specific version: append `?revision={n}` to the download URL (`client.RevisionURL`)

Path resolution: walk Tier 1 tree to resolve a path string to a Waterbutler URL.
This is the core complexity — isolated in `internal/resolver/path.go`.

## Project structure

```
gosf/
├── cmd/
│   ├── root.go              # root command, global flags, version
│   ├── ls.go
│   ├── pull.go              # --version=<n> flag for specific version download
│   ├── push.go              # manifest direction enforcement + manifest update on push
│   ├── rm.go
│   ├── versions.go          # gosf versions <project>:<path>
│   ├── projects.go
│   ├── info.go
│   ├── auth.go
│   ├── open.go
│   ├── add.go               # gosf add — add entry to .gosf/gosf.toml
│   ├── status.go            # gosf status — show manifest sync status
│   ├── sync.go              # gosf sync — push/pull; processPushEntry/processPullEntry gates
│   ├── gate.go              # state-based safety: divergenceError, entryPlan, push-plan helpers
│   ├── prompt.go            # printPushPlan, confirmation/TTY helpers
│   ├── auth_helpers.go      # friendlyAuthError (401/403 → auth hint)
│   └── manifest_helpers.go  # computeLocalMD5, localFileMatches, fileVersionsToRemote, latestRemoteVersion
├── internal/
│   ├── client/
│   │   ├── osf.go          # JSON:API metadata client
│   │   └── waterbutler.go  # file transfer client; Upload returns UploadResult
│   ├── resolver/
│   │   └── path.go         # path string → Waterbutler URLs
│   ├── manifest/
│   │   ├── manifest.go     # Load, Save, FindManifest, Entry, Manifest types
│   │   └── status.go       # ClassifyFile, FileState, RemoteVersion
│   ├── config/
│   │   └── config.go       # config file + keychain + env
│   └── output/
│       ├── format.go       # human-readable vs --output=json
│       ├── style.go        # color init + Green/Red/Yellow/Cyan/Bold/Dim helpers
│       ├── table.go        # ANSI-safe aligned table renderer (Cell, RenderTable)
│       └── spinner.go      # indeterminate-wait spinner (no-op off a TTY)
├── go.mod
├── .goreleaser.yaml
├── CLAUDE.md
└── main.go
```

## Key UX requirements

- Progress bars on pull/push (`schollz/progressbar`)
- `--dry-run` on push, pull, rm
- `--output=json` on all commands for scripting
- `--conflict=skip|overwrite|rename` on push (default: skip)
- `--quiet` suppresses progress/non-error output
- Proper non-zero exit codes on errors
- Colorized output (`fatih/color`) + spinners (`briandowns/spinner`) for
  indeterminate waits. Color is resolved once in `root.go`'s `PersistentPreRunE`
  via `output.InitColor`: on only when stdout is a TTY, forced off under
  `--output=json` (hard invariant — machine output is never colored), `--quiet`,
  and `NO_COLOR`. Global `--color=auto|always|never` overrides. All styling flows
  through the `internal/output` helpers, so it degrades to plain automatically.

### Colorized tables

`text/tabwriter` measures width in bytes and misaligns once ANSI codes are
present, so colored tables (`ls`, `status`, `versions`, `projects`) use
`output.RenderTable(header, [][]output.Cell)` instead: each `Cell{Text, Style}`
is padded on its *plain* text, then the `Style` func colors the padded cell, so
columns line up identically with color on or off. The final column is never
padded (no trailing whitespace). `output.NewTabWriter`/`PrintHeader` were removed.

### `--output=json` contract

Every command supports `--output=json`. Result types live in
`internal/output/result.go` so the contract is explicit and unit-tested.
JSON goes to stdout; progress bars are suppressed in JSON mode.

| Command | JSON shape |
|---------|-----------|
| `ls` | array of file objects (`[]` when empty, never `null`); each file object carries its content hashes under `attributes.extra.hashes.{md5,sha256}` (empty hashes omitted, so folders carry none) |
| `info` | the node object |
| `projects` | array of node objects |
| `open` | `{"url": "..."}` (does not launch a browser) |
| `pull` | `{"downloaded": [{"path","size"}], "dry_run": bool}` |
| `push` | `{"uploaded": [{"path","action"}], "dry_run": bool}` where action ∈ upload\|overwrite\|rename\|skip |
| `rm` | `{"node","path","kind","dry_run"}` — requires `--yes` (no interactive prompt in JSON mode) |
| `versions` | `{"versions": [{"version","date_created","size","contributor"}]}` — `[]` when empty |

### Cancellation

`Execute` installs a `signal.NotifyContext` (SIGINT/SIGTERM) and runs via
`ExecuteContext`. Commands use `cmd.Context()`, so Ctrl-C cancels in-flight
HTTP requests and aborts transfers. A failed download removes its partial file.

## OSF API notes

### JSON:API response shapes

Files list (`/nodes/{id}/files/osfstorage/`):
```json
{
  "data": [
    {
      "id": "...",
      "attributes": {
        "name": "filename.csv",
        "kind": "file",          // or "folder"
        "size": 12345,
        "date_modified": "...",
        "materialized_path": "/data/results/file.csv"
      },
      "links": {
        "download": "https://files.osf.io/...",
        "upload": "https://files.osf.io/...",
        "delete": "https://files.osf.io/..."
      },
      "relationships": {
        "files": { "links": { "related": { "href": "..." } } }
      }
    }
  ],
  "links": { "next": "..." }
}
```

Node metadata (`/nodes/{id}/`):
```json
{
  "data": {
    "id": "abc12",
    "attributes": {
      "title": "My Project",
      "description": "...",
      "date_created": "...",
      "date_modified": "...",
      "public": true
    }
  }
}
```

### Pagination

All list endpoints paginate. Check `links.next` and follow until null.

### Component addressing

`abc12/xyz34:/path` — `abc12` is the parent project GUID, `xyz34` is the
component (child node) GUID. The path is resolved under `xyz34`.

## Sync manifest (.gosf/gosf.toml)

### Schema

```toml
[project]
id = "abc12"          # default project GUID for entries that omit project field

[[files]]
local     = "data/raw/counts.h5"    # path relative to repo root
remote    = "/data/raw/counts.h5"   # path within OSF Storage
direction = "pull"                  # REQUIRED: "push", "pull", or "both"
version   = 3                       # pinned OSF version number; 0 = not yet pushed
md5       = "d41d8cd98f00b204e9800998ecf8427e"  # MD5 of pinned version; "" if version=0
project   = "xyz89"                 # optional per-entry override of [project].id
```

Validation on load:
- `direction` must be present on every entry — missing = load error.
- No duplicate `local` paths.
- No duplicate `(project, remote)` pairs.
- Every entry must resolve a project (own field or `[project].id`).

### File states

`ClassifyFile(entry, localMD5, remoteVersions, noCheckRemote)` in `internal/manifest/status.go`
compares three values — **L** = local, **B** = pinned baseline (`version`+`md5`),
**R** = remote latest — and reports how many sides diverged from the baseline:

| State              | Meaning |
|--------------------|---------|
| `IN_SYNC`          | L = B, R = B |
| `MISSING`          | Local file does not exist |
| `BEHIND`           | Local MD5 matches an older remote version (safe to pull) |
| `AHEAD_OF_MANIFEST`| L ≠ B, R = B — only local moved (a real local update) |
| `REMOTE_NEWER`     | L = B, R ≠ B — only remote moved (safe fast-forward for pull) |
| `PIN_ONLY`         | Local content already equals remote latest but the pin is stale/absent → record the pin, **no transfer** |
| `DIVERGED`         | L ≠ B **and** R ≠ B and local matches no remote version → unsafe, fail hard |
| `NOT_PUSHED`       | version = 0 **and** the remote path does not exist |

Unpinned entries (version = 0) are **content-compared** against the remote when it
exists, so an already-identical file classifies as `PIN_ONLY` (not `NOT_PUSHED`).
`NOT_PUSHED` now means only "version = 0 and nothing on the remote to compare".

When `--no-check-remote`: only IN_SYNC, MISSING, AHEAD_OF_MANIFEST, NOT_PUSHED are
possible (the remote-comparing states need network).

### State-based safety (gate matrix)

`direction` is a **default intent** (what `sync` does by default), not a lock.
Safety comes from state-based gates at the moment of a destructive action, keyed
on how many sides diverged from the baseline:

| L vs B | R vs B | `push` | `pull` |
|--------|--------|--------|--------|
| L=B | R=B | no-op | no-op |
| unpinned, L=R | — | pin, no transfer (`PIN_ONLY`) | pin, no transfer (`PIN_ONLY`) |
| L≠B | R=B | real update (confirm) | would clobber local → skip unless `--force` |
| L=B | R≠B | rollback → refuse unless `--force` | fast-forward (safe), re-pin |
| L≠B | R≠B | `DIVERGED` → fail hard, `--resolve=ours` | `DIVERGED` → fail hard, `--resolve=theirs` |

- **Idempotent transfers**: explicit `pull`/`push` skip the byte transfer when the
  local file already matches the remote MD5 (`localFileMatches`,
  `redundantOverwrite` in `cmd/`). Manifest-driven transfers pin without
  transferring in the `PIN_ONLY` state (`pinEntry` in `cmd/sync.go`).
- **`--force`** authorizes a remote-newer/behind rollback (a deliberate, unilateral
  overwrite of a newer remote version). It does **not** cover divergence.
- **`--yes`** bypasses the push confirmation prompt for *safe* actions (new file,
  real update) without authorizing a rollback.
- **`--resolve=ours|theirs`** is the only way through a `DIVERGED` entry: `ours`
  takes local (push a new version), `theirs` takes remote (download + re-pin).
  Divergence is detected in a pre-flight pass before any bytes move, so a bulk
  `sync`/`push` never applies a partial, half-resolved state.
- Helpers live in `cmd/gate.go` (`divergenceError`, `latestRemoteVersionInfo`,
  `pushActionLabel`, `needsPushConfirmation`, `summarizePush`, `validateResolve`,
  `entryPlan`) and `cmd/prompt.go` (`printPushPlan`, `isInteractive`).

### MD5 sourcing

- **Local**: stream file through `crypto/md5` (never read fully into memory).
- **Remote**: `data.attributes.extra.hashes.md5` from the versions list API response, and from Waterbutler upload response (`UploadResult.MD5`).
- Compare local MD5 against **all** remote version MD5s (not just declared version) to distinguish BEHIND from AHEAD_OF_MANIFEST.

### `internal/manifest/` package

- `manifest.go`: `Load`, `Save` (atomic temp+rename), `FindManifest` (walks up from cwd), `Entry.ResolveProject`.
- `status.go`: `FileState`, `RemoteVersion`, `ClassifyFile`.
- `IsNotFound(err)` checks for `NotFoundError` from `FindManifest`.

### `internal/client/` changes

- `FileVersionAttributes` now includes `Extra.Hashes.MD5` from `attributes.extra.hashes.md5`.
- `WaterbutlerClient.Upload` returns `(UploadResult, error)` — `UploadResult` carries `Version int` and `MD5 string` from the Waterbutler response.

### `gosf add` (`cmd/add.go`)

```
gosf add <local-path> <project>:<remote-path> [--direction=push|pull|both]
```
- Default direction: push.
- Creates .gosf/gosf.toml if absent.
- Errors if local path already in manifest.
- Fetches remote version+MD5 if file exists; writes version=0, md5="" otherwise.
- Prints .gitignore tip for local files >50 MB.

### `gosf status` (`cmd/status.go`)

- Computes local MD5 for each entry.
- Fetches remote versions unless `--no-check-remote` — **including for unpinned
  (version = 0) entries**, so an already-identical file reports `PIN_ONLY` instead
  of a blanket "never pushed". Status is **read-only**: it reports, never mutates.
- Tabular output: DIR / STATUS / LOCAL PATH / VER / DETAIL.
- Exit code 0 only if all entries are `IN_SYNC` (`statusIsInSync`); exit code 1
  otherwise (CI-friendly). `PIN_ONLY` and `DIVERGED` count as not-in-sync.
- `--output=json` emits array of `{path, direction, state, declared_version, remote_latest_version}`.

### `gosf sync` (`cmd/sync.go`)

Non-interactive by default: push-eligible entries (direction=push or both) push;
pull-only entries (direction=pull) pull. Two passes — classify all, then a
divergence/rollback **pre-flight** (fail hard before any transfer), then execute.

| State              | Push action | Pull action |
|--------------------|-------------|-------------|
| IN_SYNC            | ✓ skip | ✓ skip |
| PIN_ONLY           | pin, no transfer | pin, no transfer |
| AHEAD_OF_MANIFEST  | push new version | skip unless `--force` (would clobber local) |
| REMOTE_NEWER       | refuse unless `--force` (rollback) | fast-forward + re-pin |
| BEHIND             | refuse unless `--force` (rollback) | download baseline / advance |
| NOT_PUSHED (exists)| push, set manifest | · skip |
| MISSING            | ✗ skip | download + pin |
| DIVERGED           | fail hard, `--resolve=ours` | fail hard, `--resolve=theirs` |

Flags: `--force`, `--resolve=ours|theirs`, `--dry-run`, `--no-check-remote`.

### `gosf push` manifest integration

- **Bare `gosf push`** (manifest-driven) runs classify → pre-flight → confirm →
  execute. A push that writes remote bytes (new file / new version) prints a rich
  per-file plan (header with project title + PUBLIC/PRIVATE and a loud warning when
  public, per-file `local → remote` + action + size + MD5, and a summary line) and
  prompts for confirmation on a TTY. `--yes`/`--force` bypass the prompt; in
  `--output=json` mode `--force` is **mandatory** (same rule as `gosf rm`), and a
  non-TTY run without `--yes`/`--force` refuses rather than hang.
- **Explicit `gosf push <src> <project>:<path>`** keeps the `--conflict`
  behavior; it additionally skips an overwrite that would merely re-mint identical
  bytes, and still refuses when the tracked entry has `direction=pull`.
- After a successful push, if the entry has `direction=push` or `both` and
  `UploadResult.Version > 0` → update manifest atomically.

### Anonymous reads

`pull`/`ls`/`info`/`status`/`versions` attempt the fetch unauthenticated (empty
token is a valid client) and only need a token for private data. A raw 401/403 on
a read is wrapped by `friendlyAuthError` (`cmd/auth_helpers.go`) into an
actionable "run 'gosf auth login' or set OSF_TOKEN" message. `push`/`sync`/
`projects` still require a token up front.

### Exit code handling

`exitCodeError` in `cmd/status.go` carries a numeric exit code without printing an error message.
`Execute()` in `cmd/root.go` handles it: `errors.As(err, &exitErr)` → `os.Exit(exitErr.code)`.
All other errors are printed to stderr and exit 1. `rootCmd.SilenceErrors = true` prevents Cobra's double-printing.

## Development notes

- Build: `go build -o gosf .`
- Test: `go test ./...` (and `go test -race ./...`)
- Format check: `gofmt -l .` (must print nothing)
- Vet: `go vet ./...`
- The OSF API requires no auth for public projects; token elevates to private

### Test tiers

1. **Unit** — pure functions and HTTP clients against `httptest` (`go test ./...`).
2. **Integration** (`-tags integration`, `integration/`) — the built binary driven
   against the in-process `fakeosf` server. Fast, hermetic, runs in CI.
3. **Live** (`-tags live`, `integration/live/`) — the built binary against a **real**
   private OSF project. Compiled only under `-tags live`; each test skips unless
   `OSF_TEST_TOKEN` + `OSF_TEST_PROJECT` (+ optional `OSF_TEST_COMPONENT`) are set.
   Tests write under a unique `/gosf-ci-<nano>-<pid>/` folder and delete it on
   cleanup, so they are repeatable and leave no residue. Run privately:
   ```
   OSF_TEST_TOKEN=… OSF_TEST_PROJECT=… OSF_TEST_COMPONENT=… \
     go test -tags live -count=1 -v ./integration/live/...
   ```
   The `fakeosf` server encodes our *assumptions*; the live tier catches where real
   OSF/Waterbutler diverges (e.g. the cross-project new-version push 404, captured
   by the skipped `TestLive_ComponentPushNewVersion` regression test).

### Coverage

`make cover` reports **merged unit + integration** coverage. Integration/live tests
drive the compiled binary as a subprocess, so plain `go test -cover` misses them
(and undercounts `cmd`). The harness builds the binary with `-cover` and points it
at `GOCOVERDIR` when `GOSF_COVERDIR` is set; `go tool covdata` then merges the
subprocess profiles with the unit `-coverprofile` into one number and
`coverage/coverage.txt`. Real baseline: `cmd` ~75%, `internal/*` 84–98%.

### Branch model

`dev` is the integration branch and the repo default: **PRs target `dev`** and get
the full non-live CI. `main` is the release branch; promote `dev` → `main` once the
live suite is green.

### CI / Release

- `.github/workflows/ci.yml` runs on every push to `main`/`dev` and every PR:
  gofmt check, `go vet`, `go test -race`, integration tests, a build, and a
  cross-compile matrix over linux/darwin/windows × amd64/arm64. The Go version is
  read from `go.mod` via `go-version-file`, so bumping the toolchain is a one-line
  change.
- `.github/workflows/live.yml` runs the live suite on **push to `dev`** and manual
  `workflow_dispatch` (never on PRs). Serialized via a `live-osf` concurrency group.
  Requires repo secret `OSF_TEST_TOKEN` and repo variables `OSF_TEST_PROJECT` /
  `OSF_TEST_COMPONENT`; skips gracefully if the token is absent.
- `.github/workflows/release.yml` runs on a `v*` tag push and invokes
  GoReleaser (`release --clean`) using `.goreleaser.yaml` to build and publish
  the cross-platform archives + checksums to a GitHub Release.
- To cut a release: tag `vX.Y.Z` and push the tag. The version is injected into
  the binary via `-ldflags -X .../cmd.version=<version>`.
- Validate the release config locally with `goreleaser check` and
  `goreleaser build --snapshot --clean --single-target`.
- `golangci-lint` is **not** yet wired into CI: the default linter set flags
  ~30 `errcheck` findings (mostly `fmt.Fprint*` to stdout/stderr and deferred
  `Close()`). Adding it requires either addressing those or committing a tuned
  `.golangci.yml` first — tracked as a follow-up.

---

## Development strategy

### Test-driven development (REQUIRED)

This project follows test-driven development. The rule is non-negotiable:

> **Write the failing test first. Watch it fail. Then write the code that makes
> it pass.**

Concretely, for every change — a new command, a bug fix, a new helper:

1. **Red** — Write a test that encodes the desired behaviour. Run it; confirm it
   fails for the *expected* reason (not a compile error in the test itself).
2. **Green** — Write the minimum production code to make the test pass.
3. **Refactor** — Clean up with the test as a safety net.

**Bug fixes start with a regression test.** Before fixing any bug, write a test
that reproduces it and fails. The fix is done when that test goes green. This is
how we prevent the same class of bug twice. If a bug shipped, it means a test
was missing — add it.

**No production logic lands without a test that exercises it.** The only
exceptions are thin glue that cannot fail in isolation (e.g. a Cobra `RunE` that
only wires flags to an already-tested function, or a `main()`). Push the real
logic *out* of those glue layers and into tested functions.

#### What this means in practice

- **Pure functions** (`ParseTarget`, `RootUploadURL`, `AppendUploadName`,
  `FormatSize`, `findFreeName`, `splitPath`, `buildOSFWebURL`): test directly with
  table tests.
- **HTTP clients** (`internal/client`): test against `httptest.Server`. The
  client base URLs are injectable fields so tests point them at the test server.
  Cover happy path, pagination, and every error status the command maps.
- **Path resolution** (`internal/resolver`): the `Resolver` depends on a
  `FileLister` interface, not the concrete client. Test `ListDir` with a mock
  that returns canned `FileItem` trees — no network.
- **Config** (`internal/config`): use `keyring.MockInit()` and a temp
  `XDG_CONFIG_HOME`/`os.UserConfigDir` so tests never touch the real keychain or
  the developer's config. Cover the full token priority chain.
- **Command glue** (`cmd`): keep `RunE` bodies thin. Extract decision logic into
  functions in `internal/` and test those. Filesystem-touching behaviour
  (download cleanup, dest path resolution) is tested with `t.TempDir()`.

#### Testability rules for new code

- Any new dependency on the network, filesystem, clock, or keychain must be
  reachable behind an interface or an injectable field so it can be faked.
- Never call `os.Exit` outside `cmd.Execute`; return errors so they're testable.
- Prefer returning values over printing; a function that builds a string is
  testable, one that writes to `os.Stdout` is not (without capture).

### Branching model

All work happens on feature branches cut from `main`. One branch per logical
group of commands. Branch naming: `claude/<slug>`. Open a PR, get it merged,
delete the branch, pull main, repeat.

### Definition of done (per command)

A command is considered done when:
1. **A test was written first and failed before the implementation existed**
2. It compiles cleanly (`go build`)
3. It passes `go test ./...`, and the new logic is covered by tests
4. It produces correct output against the live OSF API (verified manually or
   with a recorded HTTP fixture)
5. `--output=json` emits valid, parseable JSON
6. Non-zero exit code on all error paths
7. Help text (`--help`) is complete

Stub commands (`return fmt.Errorf("not yet implemented")`) do **not** land in
`main`. Each PR must take stubs to done.

### Implementation order and rationale

Commands are ordered by dependency and complexity. Each tier must be stable
before the next.

#### Tier 1 — metadata-only (no Waterbutler)

These commands only talk to `api.osf.io`. They can be implemented and tested
without touching the file-transfer layer.

| Command | Key work |
|---------|----------|
| `info` | `GET /nodes/{id}/`, format metadata display |
| `projects` | `GET /users/me/nodes/` with pagination, tabular output |
| `open` | Construct OSF web URL, open in OS browser |

#### Tier 2 — file transfer (Waterbutler)

Requires a working `internal/client/waterbutler.go`. Implement Waterbutler
client first, then commands in dependency order.

| Command | Key work |
|---------|----------|
| `pull` | Waterbutler download, streaming to disk, progress bar, recursive folder walk |
| `push` | Waterbutler upload (new + overwrite paths), `--conflict` logic, directory walk |
| `rm` | Waterbutler DELETE, confirmation prompt, `--dry-run` |

### Waterbutler client design

`internal/client/waterbutler.go` exposes three operations:

```
Download(ctx, downloadURL, destPath string, size int64, quiet bool) error
Upload(ctx, srcPath, uploadURL string, quiet bool) error
Delete(ctx, deleteURL string) error
```

**Upload URL construction** (used by `push` to upload new files):

`osfstorage` addresses folders by **opaque object ID, not by name**, so upload
URLs must never be built from a folder-name path (doing so 404s on real OSF for
any non-root subfolder — the bug the live tier caught). Instead:

- **Root:** `client.RootUploadURL(nodeID)` → `…/providers/osfstorage/`, then
  `client.AppendUploadName(base, filename)`.
- **Subfolder:** resolve the parent folder via the metadata API and use its
  `FileLinks.Upload` (already an ID-based Waterbutler URL), then
  `AppendUploadName`. `cmd.folderUploadBase` encapsulates root-vs-subfolder.

For *overwriting* an existing file, PUT directly to the file's `FileLinks.Upload`
(the correct versioned, ID-based URL from the metadata API).

> Note: `mkdir` into a subfolder still builds a name-based folder URL and has the
> same ID bug — a scoped follow-up.

**Redirect handling**: Waterbutler redirects downloads to S3 (or other backend).
Strip the `Authorization` header when following redirects to a different host.

### Edge cases to handle per command

**`info`**
- Invalid GUID → APIError 404, friendly message
- Private project without auth → APIError 403

**`projects`**
- No token → 401, tell user to run `gosf auth login`
- Paginate all pages before printing

**`open`**
- Path `/` → `https://osf.io/{nodeID}/`
- File or folder → `https://osf.io/{nodeID}/files/osfstorage{path}`
- Print URL with `--quiet` on headless systems (fallback if browser fails)

**`pull`**
- File target → download single file
- Folder target (or `/`) → walk full tree, preserve relative structure under `dest`
- `dest` defaults to `.` (current dir); `dest` for a single file defaults to `./filename`
- Skip existing files by default (no overwrite flag needed for pull)
- `--dry-run` lists what would be downloaded
- `--version=<n>` downloads a specific historical version; validates version exists before transferring
- `--version` is invalid for directory/tree targets (errors early)

**`push`**
- Split dest path into `parentDir` + `filename`; fail if parent doesn't exist in OSF
- Check for existing file in parent dir before uploading
- `--conflict=skip` (default): print notice and skip if exists
- `--conflict=overwrite`: PUT to existing file's `links.upload`
- `--conflict=rename`: append `_1`, `_2`, … until name is free
- If `src` is a directory, walk it and upload all files (maintaining relative paths)
- `--dry-run` shows what would be uploaded without uploading

**`rm`**
- Resolve path → `FileLinks.Delete`
- Print what will be deleted; require `--yes` or interactive confirmation unless `--dry-run`
- `--dry-run` prints path without deleting

**`versions`**
- Requires a specific file path (not a folder or project root)
- Folder target → error: "versions only applies to files"
- Missing path → error: "versions requires a specific file path"
- Resolves via `resolver.Resolve` to get the file's OSF ID, then calls `GetFileVersions`
- Contributor resolution (best-effort): `email_primary` > `full_name` > user GUID
- `--output=json` emits `{"versions": [{"version","date_created","size","contributor"}]}`

### OSF file versioning

Every PUT to an existing file's `links.upload` URL creates a new numbered version.
Versions are immutable once created.

`GetFileVersions(fileID)` calls `GET /v2/files/{id}/versions/` and returns
`[]FileVersion` sorted newest-first. The OSF versions endpoint does not expose a
user relationship (requesting `embed=user` returns a 400), so contributor info is
generally unavailable from this endpoint; `FileVersion.Contributor()` resolves to
`email_primary` → `full_name` → GUID when embedded user data happens to be present,
and returns an empty string otherwise.

`RevisionURL(downloadURL, n)` appends `?revision=n` to a Waterbutler download URL,
fetching a specific historical version. Used by `pull --version=<n>`.

Two distinct upload paths in `push`:
- **New file**: PUT to the parent folder's ID-based upload URL
  (`folderUploadBase` + `AppendUploadName`) → 201 Created
- **Update (overwrite)**: PUT to `existing.Links.Upload` → 200 OK, creates new version

### Adding a new OSF API endpoint to `osf.go`

1. Add response struct(s) near the top of the file alongside related types
2. Add the method to `*OSFClient`
3. Use `c.getJSON(ctx, url, &result)` for single-item GETs
4. Use `c.listXFromURL(ctx, url)` pattern (with a typed page struct) for paginated lists

### Adding new Waterbutler operations

Add methods to `*WaterbutlerClient` in `internal/client/waterbutler.go`.
Keep auth header stripping on cross-host redirects in place for all GET requests.
