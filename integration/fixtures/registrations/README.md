# Registration API fixtures

**Empty by design, for now.** The registration probe
([`docs/osf-registration-findings.md`](../../../docs/osf-registration-findings.md))
was carried out as a desk study against COS's own source, because the session that
ran it had no OSF credentials and no network route to `api.osf.io`. Committing
hand-written JSON:API responses would encode guesses as ground truth, and the
hermetic `fakeosf` tier would then assert the guesses — worse than having nothing.

Capture these from a real OSF account using the script in the findings doc
("Capturing the fixtures"), scrub tokens and personal data, then commit them here.

| File | Source | What it pins down |
|---|---|---|
| `schemas-open-ended.json` | `GET /v2/schemas/registrations/?filter[name]=Open-Ended Registration` | the schema `_id`, which is database-assigned and cannot be hard-coded |
| `project-wikis.json` | `GET /v2/nodes/{proj}/wikis/` | pre-registration baseline for the wiki diff |
| `draft-create.json` | `POST /v2/nodes/{proj}/draft_registrations/` | accepted draft payload + response shape |
| `registration-create.json` | `POST /v2/registrations/` | the register call; `archiving`/pending state on creation |
| `registration-detail.json` | `GET /v2/registrations/{reg}/` | `public`, `embargoed`, `pending_registration_approval`, identifiers |
| `registration-wikis.json` | `GET /v2/registrations/{reg}/wikis/` | that wikis are cloned, and the list shape |
| `registration-wiki-content.md` | `GET /v2/wikis/{wiki}/content/` | cloned content verbatim, incl. image URLs (finding 3) |
| `registration-wiki-versions.json` | `GET /v2/wikis/{wiki}/versions/` | full version history carried over (finding 2) |
| `registration-children.json` | `GET /v2/registrations/{reg}/children/` | child components registered recursively (finding 5) |
| `registration-via-nodes-404.json` | `GET /v2/nodes/{reg}/wikis/` | the 404 that forces the `/registrations/` prefix |
| `registration-wiki-write-405.json` | `POST /v2/registrations/{reg}/wikis/` | writes refused (finding 6) |

Once captured, these should drive `fakeosf` registration endpoints so the
behaviour is asserted rather than assumed — the same split `CLAUDE.md` describes
for the wiki tiers.
