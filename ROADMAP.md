# Roadmap

This roadmap covers planned content-management features for agents. The scope
is deliberately limited to _content_ operations (files, metadata, wiki). User
management, permissions, and project administration are out of scope.

## v1.1 — File operations and node metadata

Builds on infrastructure already in place (Waterbutler client, OSF metadata
client) with minimal new API surface.

| Command | Description |
|---------|-------------|
| `gosf mv <src> <dest>` | Rename or move a file or folder within OSF Storage |
| `gosf cp <src> <dest>` | Copy a file or folder (across projects supported) |
| `gosf mkdir <project>:<path>` | Create a folder in OSF Storage |
| `gosf set <project> [flags]` | Update node title, description, category, or tags |

`gosf mv` updates `gosf.toml` automatically if the moved path has a manifest
entry.

`gosf set` flags: `--title`, `--description`, `--category`, `--tags`.

## v1.2 — Wiki and components

New API surface (node write path, wiki endpoints); deserves its own release
and test coverage.

| Command | Description |
|---------|-------------|
| `gosf wiki ls <project>` | List wiki pages |
| `gosf wiki get <project> <page>` | Print wiki page content |
| `gosf wiki set <project> <page>` | Create or update a wiki page (`--file` or `--message`) |
| `gosf mkproject <parent> --title <t>` | Create a sub-component under an existing project |

## Later / under consideration

- CEDAR / custom file metadata (`/cedar_metadata_records/`)
- Comments (`POST /nodes/{id}/comments/`)
- `gosf status --remote-newer` CI mode (fail only on REMOTE_NEWER)
