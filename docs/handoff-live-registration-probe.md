# Handoff: live OSF registration probe

**For:** an agent session in an environment with OSF API access.
**Working doc, not a permanent artifact.** Delete it in the commit that lands the
live findings.

---

## Mission

[`docs/osf-registration-findings.md`](./osf-registration-findings.md) answers the
registration feasibility question from **OSF's source code**, because the session
that wrote it had no OSF credentials and no network route to `api.osf.io`. Your job
is to run the empirical probe that session could not, convert its *inconclusive*
labels into verdicts, and commit real fixtures.

You are **confirming or refuting an existing analysis**, not starting from zero.
Read the findings doc first. If something there is wrong, say so plainly and fix
it — it was written from code, not observation, and the whole point of your run is
to catch where real OSF diverges.

## Read this before you touch anything

Three things make this probe unlike other work in this repo.

**1. Registrations are permanent.** They cannot be deleted — only *withdrawn*, and
a withdrawn registration leaves a public tombstone (title, contributors,
withdrawal justification) forever. A public one also mints a **real DOI**, which is
a permanent global identifier.

> Use a **throwaway project under a test account**. Never register a real project.
> Never register anything you would mind existing in perpetuity.

**2. The probe cannot finish in one sitting, and part of it needs a human.**
Making a registration public requires either an admin clicking an emailed approval
link or a ≥48-hour wait for the nightly auto-approval job. There is no API path
(this is the central finding of the desk study). So:

- Everything in **Phase A–C** below works on a *pending, private* registration and
  needs no approval. Do that first; it is the bulk of the value.
- **Phase D** needs approval. Ask the human to click the emailed link, or park the
  session and resume after the window. Do not block Phase A–C on it.

**3. Do not commit secrets.** Fixtures are captured from authenticated responses.
Scrub tokens, email addresses, and real names before committing. `gosf` never
prints tokens, but raw `curl` output can echo request context.

Also: OSF throttles (~10k requests/day authenticated). The probe is small, but
don't loop.

## What is already settled — do not re-derive

Confirmed from `CenterForOpenScience/osf.io@8ec432a16a33ccb121fe05efa334fcf1e4a09549`
(develop, 2026-08-02). Spot-check these against live behaviour as you go, but don't
spend the session re-reading source:

| Established | Evidence |
|---|---|
| Registration **is** API-creatable: draft → `POST /v2/registrations/` | `api/registrations/views.py:134,224` |
| Approval is **not** API-reachable; emailed web token or nightly job after 48h | `osf/models/sanctions.py:867,876`; `scripts/approve_registrations.py` |
| `osf.full_write` is sufficient — no special scope | `framework/auth/oauth_scopes.py:299,346,376` |
| Wiki content **and full version history** are cloned | `addons/wiki/models.py:358,525`; `osf/models/node.py:1412` |
| Embedded image URLs are copied **verbatim**, no rewriting | `addons/wiki/static/dragNDrop.js`; `clone_version` at `addons/wiki/models.py:211` |
| Omitting `included_node_ids` registers **all** children recursively | `osf/models/node.py:1470` |
| Components can register independently (no root-only constraint) | `osf/models/registrations.py:1049`; `osf/models/node.py:1409` |
| `/v2/nodes/{id}/…` **404s** for a registration | `api/nodes/views.py:203` |
| Registration wiki writes → **405** | `api/wikis/serializers.py:164` |
| Withdrawal hides content, keeps metadata | `api/wikis/permissions.py:28,38`; `api/registrations/serializers.py:259` |
| Embargo range: **2 days – 4 years** | `website/settings/defaults.py:75` |
| Archive cap: **5 GiB** | `website/settings/defaults.py:390` |
| Open-ended schema: name `Open-Ended Registration`, v3, only `summary` required | `website/project/metadata/osf-open-ended-3.json` |

## Open questions, ranked

Ranked by how much a wrong answer would change the product. Each says what to do
and what the outcome means.

### Q1 — Do registered wiki images resolve to the live project? (**highest value**)

The desk study says **yes, they do** — which voids the frozen-supplement guarantee
for figures. Mechanism: OSF's editor writes
`https://files.osf.io/v1/resources/<LIVE_NODE>/providers/osfstorage/<file_id>?mode=render`
with **no `revision` parameter**, and registration copies wiki content byte-for-byte.

This is an inference from code. Waterbutler's own source was **not** consulted, so
the claim that a revision-less `?mode=render` serves *latest* is unverified.

**Test.** Register a project whose wiki embeds an image. Then, in the **live**
project, replace that image with a visibly different one at the same path
(`gosf push --conflict=overwrite`). Reload the **registration's** wiki in a browser.

- New figure appears → **confirmed.** The README caveat and the paper both stand as
  written. Capture a before/after screenshot; this becomes the paper's figure.
- Old figure persists → **the desk study is wrong.** Find out why (does Waterbutler
  pin a revision server-side? does the registration archive rewrite something?) and
  correct both the findings doc and the README's persistence section. This would be
  good news and a significant change.

**Then test the mitigation:** repeat with an image referenced by a URL carrying an
explicit `&revision=N`. Confirm it does *not* change. If pinning works, the README's
advice is sound; if it doesn't, that advice must be removed.

Also check whether an `Archive of OSF Storage` folder appears in the registration
containing a frozen copy of `Wiki images`. If a frozen copy exists but the markdown
doesn't point at it, that's the basis for a feature request to COS — worth noting.

### Q2 — Do markdown tables render in a wiki at all?

Desk study says **probably not**, contradicting the original expectation. The
renderer enables only `wikilinks`, `fenced_code`, `codehilite`
(`addons/wiki/models.py:67`) — Python-Markdown needs the `tables` extension for
pipe tables, and it is absent. But the sanitizer *does* permit `<table>` markup
(`website/settings/defaults.py:232`), and the deployed render path wasn't observed.

**Test.** Push a page containing a pipe table with `gosf wiki push`. View it in a
browser, in both the live project and the registration. Also try a raw HTML
`<table>`.

- Pipe tables render → good, note it and move on.
- They don't → this is a **real constraint on `gosf`-authored supplements** and
  needs to go in the README's wiki section, not just the findings doc. Confirm the
  raw-HTML fallback works and document that instead.

Note the editor's client-side *preview* may differ from the stored render — judge
by the saved page, not the preview.

### Q3 — Does `gosf`'s wiki client actually break on a registration GUID?

Concrete and code-level. `client.ListWikis` builds
`fmt.Sprintf("%s/nodes/%s/wikis/", …)` (`internal/client/wiki.go:116`), which per
the desk study 404s for a registration.

**Test.** `gosf wiki ls <registration_guid>`. Expect a 404-derived error.

Confirm it, capture the exact error text, and note whether the message is
actionable or cryptic — a future `gosf register --verify` needs the
`/registrations/{id}/wikis/` prefix, and this is the gap it has to close. Do **not**
fix it in this session (see Non-goals).

### Q4 — API version cutover, schema id, archiving lag

Three unknowns that a future `gosf register` needs and that only the live API can
answer:

1. **The `CREATE_REGISTRATION_FIELD_CHANGE_VERSION` string.** Field names differ by
   API version (`draft_registration_id`/`embargo_end_date`/`included_node_ids` vs
   `draft_registration`/`lift_embargo`/`children`), and passing the wrong
   generation's field is a hard 400 with a "Deprecated in version …" message —
   **not** a silent default. Provoke that error deliberately and record the version
   string it names. This is the single most useful string for implementing the
   command.
2. **The `Open-Ended Registration` schema `_id`.** Database-assigned. Capture it,
   and confirm the `?filter[name]=` lookup works so the command never hard-codes it.
3. **Archiving lag.** Registration kicks off an archive job and exposes an
   `archiving` boolean. Time how long it stays true — it determines whether
   `register` must poll.

### Q5 — DOI minting: projects vs registrations

Registrations get a DOI automatically on becoming public
(`osf/models/node.py:1223`). The desk study *infers* that plain projects do **not**
(the DOI block is inside `if self.is_registration:`) but marks this inconclusive.

**Test.** Make the throwaway project public. Check `GET /v2/nodes/{proj}/identifiers/`
without taking any explicit action. Does a DOI exist?

This matters because the README's recommended pattern is "one public component per
paper, with its own DOI" — if a component DOI needs an explicit *Create DOI* click,
that instruction is incomplete and should say so.

### Q6 — Component-scoped registration, end to end

Confirm a draft can be created against a **component** GUID and registered on its
own, producing a registration with its own GUID whose `registered_from` is the
component. This is the mechanism the README's recommended pattern rests on, so it
should be observed rather than inferred.

Also confirm the converse: register the **parent** with `included_node_ids` omitted
and verify the child's wikis appear in the child registration.

### Q7 — Version identifiers and authorship in the clone

Flagged in the findings doc as a caveat but never tested. `clone_wiki_page`
reassigns `user` on each cloned version and the clone gets its own identifier
sequence. So version numbers and attributed authorship in a registration may not
match the live project's.

**Test.** Diff `GET /v2/wikis/{live_wiki}/versions/` against
`GET /v2/wikis/{registered_wiki}/versions/`. Compare identifiers, `date_created`,
and the embedded user.

If numbering diverges, a future `--verify` mode cannot match versions by number and
must compare canonical content hashes — which is what `gosf` already does for
wikis (`client.CanonicalizeWikiContent`). Worth knowing before designing it.

### Q8 — Add-on size cap, and withdrawal as a reader sees it

Low priority, finish if time allows.

- The findings doc could not locate the **lower cap for content replicated from
  connected add-ons** and marks the number unverified. Look for it in OSF's user
  docs and either confirm or leave it explicitly unverified. Do not guess.
- Withdraw the throwaway registration at the end of the probe and capture what a
  reader following the DOI sees. Screenshot the tombstone.

## Suggested sequence

### Phase A — setup (no registration yet)

1. Confirm credentials and reachability: `gosf auth status`, then
   `curl -sS -o /dev/null -w '%{http_code}\n' https://api.osf.io/v2/`.
2. Build `gosf` from this branch: `go build -o gosf .`
3. Create a **throwaway** project and one child component under the test account.
4. Use **`gosf` itself** to build the wiki — that exercises the tool and the probe
   together:
   - several pages, pushed repeatedly so there is real version history
     (`gosf wiki push` twice with different content mints a second version);
   - at least one page embedding an image (upload the image with `gosf push`, then
     reference its Waterbutler download URL);
   - at least one page with a pipe-table **and** a raw-HTML table (Q2);
   - one page embedding a `&revision=N`-pinned image URL (Q1 mitigation).
   - Put a page on the **component** too (Q6).
5. Make the project public. **Record whether a DOI appeared on its own** (Q5).
6. Capture the pre-registration baseline: `GET /v2/nodes/{proj}/wikis/`.

### Phase B — create the registration

Follow the capture script in the findings doc
([Capturing the fixtures](./osf-registration-findings.md#capturing-the-fixtures)).
It is written to be run as-is and to drop fixtures straight into
`integration/fixtures/registrations/`.

Deliberately provoke the wrong-API-version error while you're here (Q4.1).

### Phase C — diff project against registration (no approval needed)

Wikis, wiki content, wiki version lists, files, components. Then Q1's live
image-substitution test, Q2's render check, Q3's `gosf wiki ls` failure, and Q7's
version diff. **This is the core of the probe** — do it all before touching
approval.

### Phase D — approval-gated (needs a human, or a ≥48h wait)

Ask the human to click the emailed approval link. Then confirm the registration is
public, that a DOI was minted automatically, and finally withdraw it and capture the
tombstone (Q8).

If the human isn't available, stop after Phase C, commit, and say exactly what
remains. **A partial probe with honest labels is a good outcome**; a probe that
guesses at Phase D is not.

## Deliverables

1. **Real fixtures** in `integration/fixtures/registrations/`, matching the table in
   that directory's README. Delete that README's "empty by design" framing once
   populated. Scrub secrets.
2. **`docs/osf-registration-findings.md` updated in place.** Convert every
   *inconclusive* label to a verdict or state why it's still open. Where live
   behaviour contradicts the source reading, **say so explicitly** — keep the
   original claim visible with a correction next to it, so the next reader can see
   that code-reading was insufficient and why. Add the API version string, the
   schema `_id` lookup result, and the archiving lag.
3. **README corrections, if the probe demands them.** The persistence section's
   figure caveat and the recommended per-component pattern are both written to be
   revisable. If Q1 refutes the figure finding, or Q5 shows component DOIs need an
   explicit action, fix the README in the same commit and note it in the message.
   If Q2 confirms tables don't render, add that to the README's `gosf wiki` section.
4. **Screenshots** for the paper: before/after figure substitution (Q1), table
   rendering (Q2), withdrawal tombstone (Q8). Put them under `docs/` only if you
   intend to keep them; otherwise hand them back and don't commit binaries.
5. **A recommendation on `gosf register` scope**, revised against what you observed.
   The findings doc has a draft; sharpen or overturn it.
6. **Delete this handoff** in the commit that lands the findings.

## Non-goals

Same boundary as the session that wrote the findings doc:

- **No `gosf register` / `gosf freeze` implementation.** This is still a knowledge
  session. Q3 identifies a real bug in `ListWikis` for registration GUIDs — record
  it, don't fix it.
- **No changes to `internal/`.** Including the `ListWikis` URL.
- **No new dependencies.**
- Don't open a PR unless the human asks.

## Repo conventions you'll need

- **Branch:** work on `claude/gosf-registration-docs-ok4j6v` (already pushed) or a
  fresh branch cut from `dev`. **PRs target `dev`**, not `main`.
- **Commit the fixtures separately from any doc reframe** — that's the split the
  previous session used (`236d31f` probe, `3bace71` docs).
- Before committing: `gofmt -l .` must print nothing; `go vet ./...`;
  `go test -race ./...`; `golangci-lint run ./...` must report 0 issues.
- `cmd/skill_doc_test.go` asserts `skills/gosf/SKILL.md` covers every command and
  flag. Docs-only changes can still break it — run it.
- Live suite convention, if you add tests later: `-tags live`, gated on
  `OSF_TEST_TOKEN` + `OSF_TEST_PROJECT` (+ optional `OSF_TEST_COMPONENT`), writing
  under a unique `/gosf-ci-<nano>-<pid>/` folder and cleaning up. See
  `integration/live/main_test.go`. **Registrations can't be cleaned up** — which is
  why the probe uses a throwaway project rather than the shared live-CI project.

## Still outstanding, unrelated to the probe

The previous session could not set the GitHub repo description and topics — the
proxy refused repository-settings writes (`"Repository settings writes are not
permitted through this proxy"`; the topics endpoint returned 403). Both are still
empty. If your environment permits it:

- **Description:** `Configure and synchronize the content of an OSF project — wiki pages, files, folders, and metadata — from a repository.`
- **Topics:** `osf`, `open-science`, `research-data-management`, `reproducibility`, `supplemental-materials`, `cli`, `go`

## Context: why any of this matters

`gosf` is being prepared for a short software paper (likely *Journal of Open
Research Software*). Its distinguishing capability is managing OSF wikis as
versioned supplemental materials. The persistence story is the paper's weakest
point: OSF DOIs resolve to *current* state and OSF does not version DOIs, so a
citation to a project DOI does not pin the supplement a reviewer actually saw.
Registrations are the only frozen, automatically-DOI'd object on the platform, so
whether they faithfully capture a wiki — **figures included** — decides whether the
paper can claim a citable, pinned supplement at all.

Q1 is therefore the question that most changes what the paper can say. If figures
aren't frozen, that limitation goes in the paper explicitly rather than being
discovered by a reviewer.
