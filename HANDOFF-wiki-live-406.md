# Handoff — fix live wiki 406 (content-endpoint Accept header)

**Branch:** `claude/wiki-live-406-fix` (cut fresh from `origin/dev`; PR #74 for the
original `gosf wiki` feature is already **merged**, so this is a new branch + new PR —
do not reuse merged history).

**Status when this doc was written:** the fix is committed and pushed to this branch,
and it passes locally: `go build`, `gofmt -l` (clean), `go vet`, `go test -race ./...`,
`go test -tags integration ./integration/ -count=1`, and `golangci-lint run ./...`
(0 issues). What remains is **verifying against real OSF** (the live tier) and opening
the PR — see "What's left" below. The reason this was handed off: the origin
environment hit a prolonged tool-safety-classifier outage that blocked `git push` and
workflow dispatch at the finish line.

---

## The bug (what the live suite caught)

After `gosf wiki` merged to `dev`, the live workflow (`.github/workflows/live.yml`,
runs on push to `dev` + `workflow_dispatch`) ran against the real private OSF test
project and **3 wiki write tests failed** (all pre-existing file tests passed):

- `TestLive_WikiRoundTripFidelity` → content came back empty (`got ""`)
- `TestLive_WikiCreateVersionRenameDelete` → `OSF API 406: Not Acceptable`
- `TestLive_WikiIdempotentPush` → `OSF API 406: Not Acceptable`

**Root cause (single bug, not three).** OSF serves wiki *content* endpoints
(`GET /wikis/{id}/content/` and `GET /wikis/{id}/versions/{n}/content/`) with
`PlainTextRenderer`, whose media type is **`text/markdown`** (verified in OSF source
`api/base/renderers.py`). The gosf client fetched content through the shared
`doGet`, which hardcodes `Accept: application/vnd.api+json`. Real OSF's DRF content
negotiation can't satisfy that Accept for a `text/markdown`-only endpoint, so it
returns **406 Not Acceptable**. Every failing test hits a content GET before any
version POST — the empty round-trip was just the read failing 40× and leaving
`got == ""`.

The write endpoints (`POST /nodes/{id}/wikis/`, `POST /wikis/{id}/versions/`,
`PATCH`, `DELETE`) use the default JSON:API renderer and were always fine — the
`application/vnd.api+json` Accept is correct there. **Only the two content GETs were
wrong.**

**Why the fake didn't catch it:** `internal/testutil/fakeosf` served content
regardless of the `Accept` header (no content negotiation), so it encoded the wrong
assumption. That's the classic fake-vs-real gap the live tier exists to surface.

## The fix (in this branch)

1. `internal/client/wiki.go` — `getText` now builds its own request with
   `Accept: text/markdown, */*` (and the bearer token when present) instead of
   routing through `doGet`'s JSON:API Accept. This is the actual bug fix; it covers
   both `GetWikiContent` and `GetWikiVersionContent`.
2. `internal/testutil/fakeosf/wiki.go` — added `acceptsMarkdown(r)`; the two content
   handlers now return **406** when the request's `Accept` won't take `text/markdown`,
   mirroring real OSF. This makes the bug reproducible in the hermetic tiers so it
   can't regress.
3. `internal/client/wiki_test.go` — `TestGetWikiContent_ByteExact` now asserts the
   content request's `Accept` accepts `text/markdown` (and the test server 406s
   otherwise), i.e. the unit-level red test for the fix. (`acceptsText` helper +
   `strings` import added.)

No production behavior changed beyond the content-fetch Accept header; the write
path, manifest sync, and gate matrix are untouched.

## What's left (for the receiving agent)

1. **Verify against real OSF** — the whole point. Two options:
   - Preferred: trigger the live workflow against this branch and confirm the 3 wiki
     tests now pass:
     ```
     gh workflow run live.yml --ref claude/wiki-live-406-fix
     # then watch the run; it needs secrets.OSF_TEST_TOKEN + vars.OSF_TEST_PROJECT,
     # which are configured on the repo (OSF_TEST_PROJECT=mzwck, COMPONENT=qm5tk).
     ```
     (Via MCP: `actions_run_trigger` method `run_workflow`, `workflow_id: live.yml`,
     `ref: claude/wiki-live-406-fix`. `workflow_dispatch` on a same-repo branch gets
     the secrets; the `live-osf` concurrency group serializes it.)
   - Or run locally with a token:
     ```
     OSF_TEST_TOKEN=… OSF_TEST_PROJECT=mzwck OSF_TEST_COMPONENT=qm5tk \
       go test -tags live -count=1 -v ./integration/live/... -run TestLive_Wiki
     ```
2. **If the live wiki tests pass** → open a PR from `claude/wiki-live-406-fix` into
   `dev` (repo default). Suggested title: `fix(wiki): content endpoints need
   Accept: text/markdown (live 406)`. Reference that this fixes the live-suite
   regression from the `gosf wiki` epic (#66 / merged PR #74). Fill in the repo PR
   template (`.github/PULL_REQUEST_TEMPLATE.md`); the live-suite check is the load-
   bearing box.
3. **If a live wiki test still fails**, the next most likely follow-on (lower
   probability, but worth knowing): confirm that `POST /nodes/{id}/wikis/` with
   `attributes.content` actually persists initial content on real OSF. OSF's
   `NodeWikiSerializer.create` passes `content` to `WikiPage.objects.create_for_node`,
   so it should — but if a freshly created page reads back empty even with the Accept
   fix, change `wiki push`/`CreateWiki` to create the page and then `CreateWikiVersion`
   the content in a second call. Add a live regression test if so. (The content field
   is `write_only=True, required=False, allow_blank=False` — never POST empty content.)

## Verification already done in the origin environment (all green)

```
go build ./...                                   # ok
gofmt -l .                                       # clean
go vet ./...                                     # ok
go test -race ./...                              # ok
go test -tags integration ./integration/ -count=1  # ok
golangci-lint run ./...                          # 0 issues
```

## Pointers

- Live workflow: `.github/workflows/live.yml`
- Live wiki tests: `integration/live/wiki_live_test.go`
- Client under test: `internal/client/wiki.go` (`getText`, `GetWikiContent`,
  `GetWikiVersionContent`)
- Fake: `internal/testutil/fakeosf/wiki.go` (`acceptsMarkdown`)
- The failing live run for reference: workflow run `29412150460` on `dev`.

Delete this file (or leave it) before merging — it's a handoff note, not part of the
feature.
