# gosf — Go CLI for Open Science Framework

## Project overview

`gosf` is a Go CLI replacing the stale Python `osfclient` package. Single-binary,
distributed to researchers. CLI-only (no SDK/library scope).

**Module path:** `github.com/BU-Neuromics/gosf`
**Binary name:** `gosf`
**CLI framework:** Cobra + Viper

## Command structure

```
gosf ls   <project>[:<path>]
gosf pull <project>[:<path>] [dest]
gosf push <src> <project>:<path>
gosf rm   <project>:<path>
gosf projects
gosf info <project>
gosf auth login
gosf auth status
gosf auth logout
gosf open <project>[:<path>]
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

### Tier 2 — Waterbutler (actual file bytes)

Base: `https://files.osf.io`

- Upload new file: `PUT https://files.osf.io/v1/resources/{node_id}/providers/osfstorage/?name={filename}`
- Upload existing: PUT to the file's `upload` link from metadata API
- Download: follow the `download` link from file metadata response

Path resolution: walk Tier 1 tree to resolve a path string to a Waterbutler URL.
This is the core complexity — isolated in `internal/resolver/path.go`.

## Project structure

```
gosf/
├── cmd/
│   ├── root.go         # root command, global flags, version
│   ├── ls.go
│   ├── pull.go
│   ├── push.go
│   ├── rm.go
│   ├── projects.go
│   ├── info.go
│   ├── auth.go
│   └── open.go
├── internal/
│   ├── client/
│   │   ├── osf.go          # JSON:API metadata client
│   │   └── waterbutler.go  # file transfer client
│   ├── resolver/
│   │   └── path.go         # path string → Waterbutler URLs
│   ├── config/
│   │   └── config.go       # config file + keychain + env
│   └── output/
│       └── format.go       # human-readable vs --output=json
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

### `--output=json` contract

Every command supports `--output=json`. Result types live in
`internal/output/result.go` so the contract is explicit and unit-tested.
JSON goes to stdout; progress bars are suppressed in JSON mode.

| Command | JSON shape |
|---------|-----------|
| `ls` | array of file objects (`[]` when empty, never `null`) |
| `info` | the node object |
| `projects` | array of node objects |
| `open` | `{"url": "..."}` (does not launch a browser) |
| `pull` | `{"downloaded": [{"path","size"}], "dry_run": bool}` |
| `push` | `{"uploaded": [{"path","action"}], "dry_run": bool}` where action ∈ upload\|overwrite\|rename\|skip |
| `rm` | `{"node","path","kind","dry_run"}` — requires `--yes` (no interactive prompt in JSON mode) |

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

## Development notes

- Build: `go build -o gosf .`
- Test: `go test ./...`
- Lint: `golangci-lint run`
- The OSF API requires no auth for public projects; token elevates to private

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

- **Pure functions** (`ParseTarget`, `BuildUploadURL`, `FormatSize`,
  `findFreeName`, `splitPath`, `buildOSFWebURL`): test directly with table tests.
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

```
https://files.osf.io/v1/resources/{nodeID}/providers/osfstorage/{parentPath}?name={filename}&kind=file
```

- Root: `parentPath` = empty (URL ends in `osfstorage/`)
- Subdir: `parentPath` = URL-path-encoded path with trailing slash, no leading slash

For *overwriting* an existing file, PUT directly to `FileLinks.Upload` (already
contains the correct versioned URL from the metadata API).

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

### Adding a new OSF API endpoint to `osf.go`

1. Add response struct(s) near the top of the file alongside related types
2. Add the method to `*OSFClient`
3. Use `c.getJSON(ctx, url, &result)` for single-item GETs
4. Use `c.listXFromURL(ctx, url)` pattern (with a typed page struct) for paginated lists

### Adding new Waterbutler operations

Add methods to `*WaterbutlerClient` in `internal/client/waterbutler.go`.
Keep auth header stripping on cross-host redirects in place for all GET requests.
