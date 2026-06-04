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
