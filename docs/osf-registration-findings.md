# OSF registration feasibility probe

Status of this document: **desk study, not a live probe.** See
[Evidence basis and its limits](#evidence-basis-and-its-limits) before relying on
anything here. Every claim below is sourced to COS's own code or documentation and
labelled *confirmed (source)*, *confirmed (docs)*, or *inconclusive*. No claim is
carried over from third-party training material.

## Feasibility verdict

**Partial — creation is API-driven, publication is not.**

A registration can be created end-to-end through the v2 API: create a draft, then
`POST /v2/registrations/` with the draft's id. That much is a first-class,
documented, write-scoped API operation.

What cannot be done through the API is the **final approval** that makes the
registration public and mints its DOI. OSF routes approval through an emailed
token link handled by the web app, and the API exposes the pending state as a
read-only field. So an API-created registration lands in a pending state and
becomes public either when an admin clicks the emailed link or when a nightly job
auto-approves it after the 48-hour window elapses.

Consequence for a future `gosf register`/`gosf freeze`: the command can *initiate*
a registration unattended, but it cannot *complete* one. It must return a pending
registration and tell the user what remains. See
[Recommended scope](#recommended-scope-for-gosf-register--gosf-freeze).

## Evidence basis and its limits

The empirical probe described in the task could not be run in this session:

- No OSF credentials are available here (`OSF_TEST_TOKEN` unset, no
  `~/.config/gosf/config.toml`).
- `api.osf.io`, `files.osf.io`, `osf.io`, `developer.osf.io`, and `help.osf.io`
  are **all blocked by this session's egress policy** — the proxy answers 403 to
  CONNECT. Server-side fetches of `api.osf.io` and `developer.osf.io` returned
  403 as well.

So no project was created, no wiki was pushed, nothing was registered, and the
live image-re-upload test was not performed.

What replaced it: OSF is developed in the open, so the **implementation itself**
was read at a pinned commit. Unless noted otherwise, code citations are to
`CenterForOpenScience/osf.io` at commit `8ec432a16a33ccb121fe05efa334fcf1e4a09549`
(branch `develop`, 2026-08-02), and frontend citations to
`CenterForOpenScience/ember-osf-web` at its `develop` HEAD on the same date.
Documentation citations are to `CenterForOpenScience/OSFDocs`.

This is strong evidence — it is the code that serves the API — but it is not the
same as an observed response. Two specific gaps matter and are called out inline:

1. Schema and object **ids** are database-assigned and cannot be read from source.
2. **Rendered** output (what a browser shows for a registered wiki) depends on the
   deployed frontend and on Waterbutler, which is a separate service whose source
   was not consulted.

Everything marked *inconclusive* below needs the live probe. The reproduction
recipe in [Capturing the fixtures](#capturing-the-fixtures) is written so the live
run is mechanical.

## The gating question: creating a registration through the API

### Flow

Two steps, both write operations.

**1. Create a draft registration.** Either route works:

```
POST /v2/nodes/{node_id}/draft_registrations/
POST /v2/draft_registrations/
```

Routes: `api/nodes/urls.py:27`, `api/draft_registrations/urls.py`. The views are
`NodeDraftRegistrationsList` (`api/nodes/views.py:667`) and
`DraftRegistrationList` (`api/draft_registrations/views.py:52`), both
`generics.ListCreateAPIView` — POST is a real, supported method.

The draft's `branched_from` is the node it was created against
(`osf/models/registrations.py:1049`, `:1363`).

**2. Register the draft.**

```
POST /v2/registrations/
```

`RegistrationList` (`api/registrations/views.py:134`) is a
`ListCreateAPIView` with `DraftMixin`. `perform_create`
(`api/registrations/views.py:224`) reads `draft_registration` (older API
versions) or `draft_registration_id` (newer) from the request body, requires the
caller to hold **ADMIN** on the draft, and calls `draft.register(...)`:

```python
draft_id = self.request.data.get('draft_registration', None) or self.request.data.get('draft_registration_id', None)
draft = self.get_draft(draft_id)
...
if draft.has_permission(user, ADMIN):
    serializer.save(draft=draft)
else:
    raise PermissionDenied(
        'You must be an admin contributor on the draft registration to create a registration.',
    )
```

*Confirmed (source).*

### Accepted create fields

From `RegistrationCreateSerializer` (`api/registrations/serializers.py:715`).
The field names are **API-version dependent** — there is a cutover version
(`CREATE_REGISTRATION_FIELD_CHANGE_VERSION`) and passing the wrong generation's
field raises an explicit "Deprecated in version …" error rather than being
ignored:

| Newer versions | Older versions | Meaning |
|---|---|---|
| `draft_registration_id` | `draft_registration` | which draft to register (required) |
| `embargo_end_date` | `lift_embargo` | embargo lift date; presence implies embargo |
| `included_node_ids` | `children` | which child components to include |
| — | `registration_choice` | `immediate`\|`embargo`; rejected on newer versions |

`manual_guid` and `manual_doi` exist but are gated behind a
`MANUAL_DOI_AND_GUID` feature flag and will be rejected for ordinary users
(`api/registrations/serializers.py:830`).

Pin an explicit API version in the request rather than relying on the default —
this is the one place where a wrong guess produces a hard 400 instead of a
default. *Confirmed (source); the exact cutover version string is
**inconclusive** and must be read from the deployed API.*

### Schema identification

Registration schemas are `RegistrationSchema` rows with database-assigned `_id`s,
served at `GET /v2/schemas/registrations/`. **The id cannot be determined from
source** and must be looked up at runtime.

The open-ended template intended for archival snapshots rather than hypothesis
preregistration is named **"Open-Ended Registration"**, current version **3**
(`website/project/metadata/osf-open-ended-3.json`). Look it up with:

```
GET /v2/schemas/registrations/?filter[name]=Open-Ended%20Registration
```

Its form is minimal — one required question:

| qid | required | type |
|---|---|---|
| `summary` | **yes** | string |
| `uploader` | no | osf-upload |

So the draft's `registration_responses` need only carry a `summary` string. This
is the right schema for a supplement snapshot: it imposes no preregistration
semantics. *Confirmed (source) for name/version/fields; schema `_id`
**inconclusive**.*

### Embargo

Settable at create time. Validated range, from `website/settings/defaults.py:75`:

```python
EMBARGO_END_DATE_MIN = datetime.timedelta(days=2)
EMBARGO_END_DATE_MAX = datetime.timedelta(days=1460)  # Four years
```

Enforced in `osf/models/node.py:762`. So: **at least 2 days out, at most 4 years
out**, measured from now. Out-of-range dates are a validation error. COS's user
docs agree ("Embargo periods keep registrations private up to four years… At the
end of the embargo period the registration is automatically made public").
*Confirmed (source + docs).*

An embargoed registration is not public and therefore has no DOI until the
embargo lifts or is terminated early — see [DOI minting](#doi-minting).

### The approval window is not bypassable via the API

For a non-embargoed registration, `create` ends with:

```python
registration.require_approval(auth.user)
```

(`api/registrations/serializers.py:834`.) That creates a `RegistrationApproval`
sanction (`osf/models/sanctions.py:867`) with:

```python
@property
def auto_approval_time(self):
    return self.initiation_date + osf_settings.REGISTRATION_APPROVAL_TIME
```

and `REGISTRATION_APPROVAL_TIME = datetime.timedelta(days=2)`
(`website/settings/defaults.py:72`) — the 48-hour window, confirmed.

Two ways out of the pending state, **neither of which is a JSON:API call**:

- **Admin acts.** The approve/reject URLs are web-app token links:
  `APPROVE_URL_TEMPLATE = osf_settings.DOMAIN + 'token_action/{node_id}/?token={token}'`
  (`osf/models/sanctions.py:876`). They arrive by email.
- **Auto-approval.** `scripts/approve_registrations.py` — "Run nightly, this
  script will approve any pending registrations that have elapsed the pending
  approval time" — flips them and makes the registration public.

Note the practical consequence of it being a *nightly* job: the wait is "at least
48 hours, then until the next nightly run," not "exactly 48 hours."

The API surface is read-only here: `pending_registration_approval` is
`HideIfWithdrawal(ser.BooleanField(source='is_pending_registration', read_only=True))`
(`api/registrations/serializers.py:134`). There is no approve action endpoint.

**Confirmed (source): submission still triggers the approval window, and it is
not bypassable for API-created registrations.** Also note
`registered.is_public = False` at registration time (`osf/models/node.py:1433`) —
registrations start private regardless of the source project's visibility.

### Scopes and rate limits

`RegistrationList` requires `CoreScopes.NODE_REGISTRATIONS_WRITE`
(`api/registrations/views.py:143`); the draft views require
`NODE_DRAFT_REGISTRATIONS_WRITE`. Both are members of `NODE_ACCESS_WRITE` ⊂
`NODE_ALL_WRITE` ⊂ `FULL_WRITE`, which is what the public scope `osf.full_write`
grants (`framework/auth/oauth_scopes.py:299`, `:346`, `:376`).

**No scope requirement beyond `osf.full_write`.** *Confirmed (source).*

No registration-specific rate limit was found; registration creation is subject to
the same throttling `gosf` already handles (see the rate-limiting notes in
`CLAUDE.md`). Registration also kicks off an **archiving** job, so the
registration is not immediately readable — `archiving` is an exposed boolean
(`api/registrations/serializers.py:140`) and a `register` command would need to
poll it. *Confirmed (source) that the field exists; archive duration
**inconclusive**.*

## Content questions

### 1. Is wiki content included in the snapshot? — **confirmed**

Yes. The wiki addon's `after_register` hook (`addons/wiki/models.py:525`):

```python
def after_register(self, node, registration, user, save=True):
    """Copy wiki settings and wiki pages to registrations."""
    WikiPage.clone_wiki_pages(node, registration, user, save)
```

invoked from `register_node`'s post-register addon loop
(`osf/models/node.py:1461`). COS's own docs agree: *"The content and version
history of Wiki and OSF Storage will be copied to the registration."*
*Confirmed (source + docs).*

### 2. Is wiki version history carried over? — **confirmed: yes, in full**

Not a flattened copy. `clone_wiki_page` iterates every version
(`addons/wiki/models.py:358`):

```python
def clone_wiki_page(self, copy, user, save=True):
    new_wiki_page = self.clone()
    new_wiki_page.node = copy
    ...
    for version in self.versions.all().order_by('created'):
        new_version = version.clone_version(new_wiki_page, user)
```

and `register_node` carries an explicit source comment: *"Cloning a node will
clone each WikiPage on the node and all the related WikiVersions"*
(`osf/models/node.py:1412`).

`clone_version` (`addons/wiki/models.py:211`) is a plain `self.clone()` with the
`wiki_page` FK and `user` reattached; `content` is a `TextField`
(`addons/wiki/models.py:122`) copied verbatim.

So a registration is a **true archive** of the supplement's history, not a
snapshot of current text. Deleted pages are excluded — `clone_wiki_pages`
filters `node.wikis.filter(deleted__isnull=True)`. *Confirmed (source + docs).*

Caveat: version *identifiers* restart in the clone's own sequence and the cloning
loop reassigns `user`, so version numbers and attributed authorship in the
registration should not be assumed to match the live project's. Verify in the live
probe.

### 3. How do embedded images resolve? — **confirmed, and this is the problem**

**A registered wiki's images are served from the live project's storage, not from
the registration's archived copy.** The frozen-supplement guarantee does not
extend to figures.

Three findings compose to this:

**(a) Where wiki images live.** The wiki editor's drag-and-drop handler uploads
into a folder literally named `Wiki images` in the *node's* osfstorage
(`addons/wiki/static/dragNDrop.js`):

```javascript
var imageFolder = 'Wiki images';
```

`getOrCreateWikiImagesFolder` looks it up via
`nodes/{node.id}/files/osfstorage/?filter[kind]=folder&filter[name]=Wiki images`
and creates it via Waterbutler if absent.

**(b) What gets written into the markdown.** The Waterbutler download link for the
uploaded file, plus a render flag:

```javascript
urls.splice(i, 0, response.data.links.download + '?mode=render');
```

That link is of the form
`https://files.osf.io/v1/resources/<LIVE_NODE_ID>/providers/osfstorage/<file_id>?mode=render`
— it hard-codes the **live node's** GUID and the **live file's** id, and carries
no `revision` parameter.

**(c) Nothing rewrites it.** Registration clones `content` byte-for-byte
(finding 2). A search for URL rewriting in the wiki addon
(`replace.*osf.io`, `rewrite`, `re.sub` across `addons/wiki/models.py` and
`addons/wiki/utils.py`) returns **nothing**. The registration's archived image
exists as a separate file with a different id under "Archive of OSF Storage", and
no wiki content is repointed at it.

Therefore, for a figure referenced this way:

- Re-uploading the image at the same path creates a **new version of the same
  file id**. An unparameterised `?mode=render` link serves the latest version, so
  **the registered wiki starts rendering the new figure.**
- Deleting the file, or making the live project private, breaks the image in the
  registered wiki.

This must be stated plainly in the docs and in the paper. *Confirmed (source) for
the mechanism.* The observable end state — what a browser actually renders after
a re-upload — is **inconclusive** and is exactly what step 6 of the live probe
tests. Waterbutler's own source was not consulted, so the claim that a
revision-less `?mode=render` serves latest is an inference from the absence of a
`revision` parameter, consistent with `gosf`'s existing `RevisionURL` helper.

**Mitigation available to `gosf` users**, because `gosf` authors markdown in the
repo and therefore controls the URL: reference figures with an explicit
`&revision=N` pin. OSF file versions are immutable, so a pinned revision cannot
silently change content. It still depends on the live project continuing to exist
and stay public — a pinned revision fixes *silent substitution*, not *link rot*.
This is worth verifying live before recommending it in the README.

### 4. Do markdown tables survive rendering? — **inconclusive, and not obviously fine**

This one should not be assumed. The server-side wiki renderer enables only three
Python-Markdown extensions (`addons/wiki/models.py:67`):

```python
def build_html_output(content, node):
    return markdown.markdown(
        content,
        extensions=[
            wikilinks.WikiLinkExtension(...),
            fenced_code.FencedCodeExtension(),
            codehilite.CodeHiliteExtension(css_class='highlight')
        ]
    )
```

Python-Markdown does **not** support pipe tables without the `tables` extension,
and `tables` is absent. On this path, a markdown table would render as a
paragraph of literal `|` characters.

Two reasons this is not yet a verdict:

- The sanitizer *does* permit table markup — `WIKI_WHITELIST`
  (`website/settings/defaults.py:232`) allows `table`, `thead`, `tbody`, `tr`,
  `th`, `td`, and also `img` with `src`/`alt`/`width`/`height`. So tables are not
  stripped; the question is only whether the markdown is converted.
- The wiki is still served by the legacy addon (`addons/wiki/templates`,
  `pagedown-ace`), not by `ember-osf-web` — a search of the frontend repo found
  no wiki route and no wiki use of its Showdown dependency. But the deployed
  rendering path was not observed, and the editor preview is client-side and may
  differ from the stored render.

**Inconclusive.** The live probe must render a table in both a project wiki and a
registered wiki. If tables do not convert, the workaround is raw HTML `<table>`
markup, which the whitelist permits — and that would be a real constraint on
`gosf`-authored supplements worth documenting.

### 5. Component scoping — **confirmed**

**A component can be registered independently.** `DraftRegistration.branched_from`
is a FK to `AbstractNode` (`osf/models/registrations.py:1049`) with no root-only
constraint, and `register_node` rejects only collections/folders
(`osf/models/node.py:1409`). Creating a draft against a component's GUID and
registering it produces a registration whose `registered_from` is that component,
with its own GUID — and therefore its own DOI once public.

**Registering a parent pulls in child components' wikis, by default.** The
recursion (`osf/models/node.py:1470`):

```python
for node_relation in original.node_relations.filter(child__is_deleted=False):
    node_contained = node_relation.child
    ...
    if child_ids and node_contained._id not in child_ids:
        ...
        continue
    node_contained.register_node(..., parent=registered, child_ids=child_ids)
```

The guard is `if child_ids and …` — falsy when the list is empty. So **omitting
`included_node_ids` registers every child recursively**; supplying it restricts to
the listed ones. Each child runs its own `after_register`, so each child's wikis
are cloned too.

One constraint: you cannot include a child while excluding its parent —
*"The parents of all child nodes being registered must be registered."*

This supports the per-paper-component pattern: a component carries its own wiki,
registers on its own, and gets its own DOI. *Confirmed (source).* COS's docs add
that withdrawal is all-or-nothing across a registration tree: *"Only the entirety
of a registration (a registered project and its registered components) can be
withdrawn—individual components cannot be withdrawn."*

### 6. Read access to a registration's wiki — **confirmed**

Registered wiki pages are ordinary `WikiPage` rows with their own ids, so
**`gosf`'s existing wiki read client works against registrations essentially
unchanged.** Only the list endpoint differs:

| Purpose | Endpoint |
|---|---|
| list a registration's wikis | `GET /v2/registrations/{reg_id}/wikis/` |
| page metadata | `GET /v2/wikis/{wiki_id}/` |
| latest content (plain text) | `GET /v2/wikis/{wiki_id}/content/` |
| version list | `GET /v2/wikis/{wiki_id}/versions/` |
| version content (plain text) | `GET /v2/wikis/{wiki_id}/versions/{n}/content/` |

The list route is `api/registrations/urls.py` → `RegistrationWikiList`
(`api/registrations/views.py:705`), which subclasses `NodeWikiList` with
`RegistrationWikiSerializer`. The `/v2/wikis/…` routes (`api/wikis/urls.py`) are
node-agnostic and serve registered pages by id.

Two implementation notes for `gosf`:

- **`/v2/nodes/{id}/…` returns 404 for a registration.** `NodeMixin.get_node`
  raises `NotFound` when `node.is_registration`
  (`api/nodes/views.py:203`). A `register`/`verify` command must branch to the
  `/registrations/` prefix — `gosf`'s current `ListWikis` builds a
  `/nodes/{id}/wikis/` URL and would 404.
- **Writes are refused, as expected.** `RegistrationWikiSerializer.create` raises
  `MethodNotAllowed` (`api/wikis/serializers.py:164`), and
  `ExcludeWithdrawals`/`ExcludeWithdrawalsWikiVersion`
  (`api/wikis/permissions.py:28`, `:38`) additionally deny access for retracted
  nodes. This corroborates the existing note in `CLAUDE.md` that registrations are
  read-only via this API.

*Confirmed (source).* Response *shapes* were not observed — capture them live.

### 7. Withdrawal behavior — **confirmed**

A reader following the DOI after withdrawal gets **metadata, not content**. The
DOI keeps resolving; the page becomes a tombstone.

Content is gated at the permission layer (`api/wikis/permissions.py`):

```python
class ExcludeWithdrawals(permissions.BasePermission):
    def has_object_permission(self, request, view, obj):
        node = obj.node
        if node and node.is_retracted:
            return False
        return True
```

with the same for `WikiVersion`. In the API serializer, the `wikis` relationship
is wrapped in `HideIfWithdrawalOrWikiDisabled`
(`api/registrations/serializers.py:259`), and essentially every content-bearing
field — `tags`, `node_license`, `children`, `files`, `comments`,
`registered_meta`, `registration_responses` — is wrapped in `HideIfWithdrawal`.

COS's docs describe the same from the reader's side: *"Withdrawing a registration
will remove its content from the OSF, but leave basic metadata behind. The title
of a withdrawn registration and its contributor list will remain, as will
justification or explanation of the withdrawal."* Withdrawn registrations show a
red "Withdrawn" tag and a non-dismissable notice.

*Confirmed (source + docs).* Registrations cannot be deleted, only withdrawn.

### 8. Size ceilings — **partially confirmed**

`website/settings/defaults.py:390`:

```python
ARCHIVE_PROVIDER = 'osfstorage'
MAX_ARCHIVE_SIZE = 5 * 1024 ** 3  # == math.pow(1024, 3) == 1 GB
```

**5 GiB.** (The trailing comment is stale and contradicts its own expression —
`5 * 1024**3` is 5 GiB, not 1 GB. Trust the code.) This confirms the ~5 GB figure
in the task description. *Confirmed (source).*

The **lower cap for content replicated from connected add-ons** was not located in
this pass, and COS's docs note only that *"draft figshare files cannot be copied
and will be excluded"*. **Inconclusive** — treat the add-on-specific number as
unverified.

Implication, unchanged by the missing number: **large data should not live inside
the registration.** Keep it in the live project or an external repository and have
the registered wiki link out. Which loops back to finding 3 — a link out of a
frozen supplement is a link to something mutable, and that is a property of the
design, not a bug to be fixed. Say so in the paper.

## Correction needed to Part 2's persistence claims

One claim in the docs plan needs qualification. Everything else holds.

- ✅ "A wiki page has a persistent URL but no DOI of its own." Holds — DOIs are
  minted per node/registration, never per wiki page.
- ✅ "Project and component DOIs resolve to current state." Holds.
- ⚠️ **"A registration is frozen and separately DOI'd, which is how a supplement
  gets pinned at submission time."** True for wiki *text* and its *version
  history* (findings 1–2), and the DOI is separate and automatic (below). **But
  not true for embedded figures** (finding 3): images referenced the way OSF's own
  editor writes them are served from the live project and can change after
  registration. Any sentence promising a frozen supplement must say that figures
  are pinned only if referenced by an explicit immutable revision — and even then
  only as long as the live project survives.
- ⚠️ **Markdown tables.** Do not claim tables render in a registered wiki until
  finding 4 is settled live. The evidence currently points the *unfavourable* way.
- ➕ Worth adding: a registration starts **private and pending**, and its DOI
  appears only when it goes public. "Pinned at submission time" requires either an
  admin click or a ≥48-hour wait. A supplement cannot be frozen at the instant of
  submission without planning for that lag.

### DOI minting

Relevant to the recommended pattern, and it differs between projects and
registrations.

**Registrations get a DOI automatically on becoming public.** In
`AbstractNode.set_privacy` (`osf/models/node.py:1223`), inside an
`if self.is_registration:` branch:

```python
if not self.get_identifier_value('doi'):
    try:
        doi = self.request_identifier('doi')['doi']
        self.set_identifier_value('doi', doi)
```

with the failure path logging *"Registration cannot be made public without a
DOI."* No explicit "Create DOI" action is needed.

**Plain projects and components do not.** That DOI block is registration-only, so
a public project's DOI comes from the explicit action. *Confirmed (source) for the
registration path; the project path is inferred from the absence of an equivalent
branch and is **inconclusive** — verify in the live probe (method step 3).*

COS's docs add the eligibility rule and format: *"Public, meaning non-embargoed,
registrations can be given DOIs and ARKs"*, formatted
`10.17605/OSF.IO/GUID`.

## Recommended scope for `gosf register` / `gosf freeze`

The probe says the command is possible. It also says what it must not promise.

### What it can do

1. **Resolve the schema.** Look up "Open-Ended Registration" by name filter; do
   not hard-code an `_id`.
2. **Create the draft** against a project or component GUID, with
   `registration_responses.summary` — from a flag, or generated from the manifest.
3. **Register it**, optionally with `embargo_end_date` (2 days–4 years) and
   `included_node_ids`. Pin the API version explicitly.
4. **Report the pending state and stop.** Print the registration GUID, that it is
   private and pending approval, the auto-approval time, and that a DOI appears
   only once public. This is the honest terminal state for an unattended command.
5. **Verify a registration** — a genuinely useful second mode, and cheap given
   finding 6: list the registration's wikis, fetch content, compare canonical MD5s
   against the manifest's `[[wikis]]` entries using the existing
   `CanonicalizeWikiContent` path. Answers "does this DOI show the supplement I
   think it does?"
6. **Pre-flight lint the supplement** — the highest-value piece, and it needs no
   new API surface. Scan tracked wiki markdown for image references that will not
   survive freezing: `files.osf.io` links without a `revision` parameter, links
   pointing at a different node, plain relative paths. Warn before registering,
   when it is still fixable.

### What it cannot promise

- **It cannot make a registration public.** No API approval path (gating
  question). `--wait` would mean polling for up to 48 hours plus a nightly job;
  not worth building.
- **It cannot guarantee frozen figures.** Finding 3. Best available is the lint in
  (6) plus a documented `revision`-pinning convention.
- **It cannot be undone.** Registrations are withdrawable, not deletable, and
  withdrawal is not exposed as a create-time-reversible API operation. This makes
  `register` the most destructive command `gosf` would have — it must require
  explicit confirmation, respect `--dry-run`, and require `--yes` under
  `--output=json`, matching the `rm` rule.
- **It cannot register just a wiki.** Registration is node-scoped. A supplement is
  frozen together with everything else in the project or component — which is the
  real argument for the per-paper-component pattern, not merely a stylistic
  preference.
- **It should not be relied on for large data.** 5 GiB archive cap (finding 8).

### Suggested naming

Prefer **`gosf register`**. It is OSF's own term, it maps to the API resource, and
`freeze` implies a completed, reversible-sounding local operation — which is
precisely what this is not.

## Capturing the fixtures

`integration/fixtures/registrations/` does not yet contain fixtures: this session
could not reach the API, and fabricating plausible JSON:API responses would be
worse than having none — they would encode guesses as ground truth and the
hermetic tier would then assert the guesses. The directory carries a
[README](../integration/fixtures/registrations/README.md) listing the exact
responses to capture.

Run the live probe on a machine with OSF access. Prerequisites: a throwaway
project with one child component, several wiki pages with real version history,
at least one embedded image, at least one markdown table, and a token with
`osf.full_write`.

```bash
export T="$OSF_TEST_TOKEN" PROJ=xxxxx COMP=yyyyy
H=(-H "Authorization: Bearer $T" -H "Accept: application/vnd.api+json")
OUT=integration/fixtures/registrations
mkdir -p "$OUT"

# --- schema id (never hard-code this) ---
curl -s "${H[@]}" \
  'https://api.osf.io/v2/schemas/registrations/?filter[name]=Open-Ended%20Registration' \
  > "$OUT/schemas-open-ended.json"
SCHEMA=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"][0]["id"])' < "$OUT/schemas-open-ended.json")

# --- baseline: the live project's wikis, for the registration diff ---
curl -s "${H[@]}" "https://api.osf.io/v2/nodes/$PROJ/wikis/" > "$OUT/project-wikis.json"

# --- 1. create the draft ---
curl -s "${H[@]}" -H 'Content-Type: application/vnd.api+json' \
  -X POST "https://api.osf.io/v2/nodes/$PROJ/draft_registrations/" \
  -d "{\"data\":{\"type\":\"draft_registrations\",\"attributes\":{
        \"registration_supplement\":\"$SCHEMA\",
        \"registration_responses\":{\"summary\":\"gosf registration probe\"}}}}" \
  > "$OUT/draft-create.json"
DRAFT=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["id"])' < "$OUT/draft-create.json")

# --- 2. register it (omit included_node_ids to pull in all children) ---
curl -s "${H[@]}" -H 'Content-Type: application/vnd.api+json' \
  -X POST 'https://api.osf.io/v2/registrations/' \
  -d "{\"data\":{\"type\":\"registrations\",\"attributes\":{\"draft_registration\":\"$DRAFT\"}}}" \
  > "$OUT/registration-create.json"
REG=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"]["id"])' < "$OUT/registration-create.json")

# --- 3. read side: does gosf's wiki client work against it? ---
curl -s "${H[@]}" "https://api.osf.io/v2/registrations/$REG/wikis/"   > "$OUT/registration-wikis.json"
curl -s "${H[@]}" "https://api.osf.io/v2/registrations/$REG/children/" > "$OUT/registration-children.json"
curl -s "${H[@]}" "https://api.osf.io/v2/registrations/$REG/"          > "$OUT/registration-detail.json"
# confirm the 404 that forces the /registrations/ prefix
curl -s -o "$OUT/registration-via-nodes-404.json" -w 'nodes/{reg} => %{http_code}\n' \
  "${H[@]}" "https://api.osf.io/v2/nodes/$REG/wikis/"

# per-page content + full version history (proves finding 2)
WID=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["data"][0]["id"])' < "$OUT/registration-wikis.json")
curl -s "${H[@]}" -H 'Accept: text/markdown, */*' \
  "https://api.osf.io/v2/wikis/$WID/content/"  > "$OUT/registration-wiki-content.md"
curl -s "${H[@]}" "https://api.osf.io/v2/wikis/$WID/versions/" > "$OUT/registration-wiki-versions.json"

# --- 4. confirm writes are refused (expect 405) ---
curl -s -o "$OUT/registration-wiki-write-405.json" -w 'POST reg wikis => %{http_code}\n' \
  "${H[@]}" -H 'Content-Type: application/vnd.api+json' \
  -X POST "https://api.osf.io/v2/registrations/$REG/wikis/" \
  -d '{"data":{"type":"wikis","attributes":{"name":"probe","content":"x"}}}'
```

Then, by hand — the parts a script cannot settle:

1. **Q3, the decisive test.** Note the image URL inside
   `registration-wiki-content.md`. Re-upload a *different* image at the same path
   in the **live** project (`gosf push`), then reload the registration's wiki in a
   browser. If the new figure appears, finding 3 is confirmed end-to-end. Repeat
   with an `&revision=N`-pinned URL to confirm the mitigation.
2. **Q4.** Compare the rendered table in the project wiki and the registration
   wiki. Screenshot both.
3. **Q3/Q8.** Check whether an "Archive of OSF Storage" folder appears and whether
   `Wiki images` is inside it — that tells you a frozen copy *exists* even though
   the markdown does not point at it, which is what a future rewrite-on-register
   feature request to COS would rest on.
4. **DOI.** Record whether the *project* got a DOI automatically on going public
   (closing the gap in [DOI minting](#doi-minting)), and confirm the registration's
   DOI appears only after approval.
5. **Withdrawal.** Withdraw the throwaway registration and capture
   `registration-detail.json` again plus a screenshot of the tombstone.

Scrub tokens and personal data from captured fixtures before committing.
