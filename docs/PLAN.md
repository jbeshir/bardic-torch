# Bardic Torch: implementation plan

**Status:** approved 2026-10-31; rewritten 2026-11-10 to a simpler scope (the commit that rewrote it lists what
changed).
This plan is the input every build pipeline reads.

**Proportion is the main quality bar.** The site holds about 142 pieces and 48 bards, with two moderators (John
and Fabian) and a handful of contributors who are not technical. Build each feature in the plainest way that does
the job, and test what matters at the cheapest level that proves it. If a rule needs a page of edge cases to be
safe, question the rule before writing the page.

**Research basis:** a crawl of https://bardictorch.miraheze.org (381 pages, 2026-10-30), demesne job
`fe3d5dd2-f6f6-4a8c-b355-9dc525e718b1`. `R-n` refers to its requirements list.

## 1. Product summary

Bardic Torch is a public, community-edited **index** of SCA bardic pieces (songs, poems, stories, dance and music)
and the bards who wrote or perform them. It helps people *find* pieces and *preserve* them. It replaces the
MediaWiki site, which stays where it is on Miraheze.

Fixed decisions:
- **No lyrics or text are hosted**, ever. Pieces carry only links, metadata and sourced notes.
- **Moderators are John and Fabian.** Everyone else's edits wait for approval; the last approved revision is live.
- **Login:** Auth0 social login (Google, Facebook).
- **Hosting:** the Go API on John's dokku instance (dokku MySQL plugin); the React Router 8 FE on Cloudflare
  Workers, deployed by Workers Builds.
- **Domain:** bardictorch.org, with the API at api.bardictorch.org.
- **No redirects** from old wiki URLs. The wiki is imported once, at launch.
- **Look:** like a wiki (so editing feels allowed), with full dark mode and a mobile-first faceted browse (R-3).

Non-goals for v1: lyrics or text display; uploads; verified-bard accounts; automatic enumeration of collection
contents; old-URL redirects; recommendations, RSS, chat; MCP. Repertoire lists and structured bard preference
profiles are on the backlog.

## 2. Decisions on the research's open questions

Confirmed by John on 2026-10-31; 2.3, 2.6, 2.7 and 2.15 amended on 2026-11-10.

1. **Kind:** a primary `kind` (song, poem, story, dance_music, instrumental, comedy, extensible) plus
   `secondary_kinds[]`. Song + Instrumental and Story + Poem are both valid.
2. **Origin** (the wiki's "Source Category"): multi-valued with one primary; an optional `part` scope (words,
   tune, verses, chorus); century categories become child terms of Origin; topic centuries stay under Topic.
3. **Conservation status:** an enum (unassessed, low_risk, endangered, critically_endangered, restricted). Every
   change is recorded with a reason. A moderator sets it by the conservation guide, in which exactly three places
   counts as Endangered. Restricted overrides everything and hides the "help preserve this" prompts.
4. **Pseudo-bards:** "Traditional & Folk", Unknown, Anonymous and Various become an `attribution` on the credit,
   with a separate `certainty` (Disputed is a certainty). Bands and historical figures are persons with
   `kind = group | historical`.
5. **Red-link people and missing works** are imported as public stub pages, marked "stub — help complete this".
6. **Legal and mundane names** in bard aliases are imported as **ordinary public aliases**, as the wiki shows
   them. There is no hidden-alias concept. Someone who wants a name removed asks, and a moderator deletes it.
7. **Bard self-verification** is out of scope for v1. Every licensing claim needs a source URL, as on the wiki
   (R-12).
8. **"Prefers no page"** is a person status: the page is a stub showing the cited statement, the person's links
   and credits are hidden from public view, and creating a person with a matching name or alias is blocked.
9. **Ambiguous records** (the five cases the research report names) are imported as the importer's best guess.
10. **Unparented categories:** Seasons, Fire, Hope → Topics; Archery → Topics › War; Short Pieces → Other;
    Jewish, Sephardic, Portuguese → Cultures, with Sephardic under Jewish.
11. **Rude** stays a mood. A content-advisory flag can come later.
12. **Kingdoms:** one `affiliation` term tree (SCA › Kingdom › group); bards link to it as affiliations, and each
    `Patriotic (Kingdom)` topic term links to its kingdom.
13. **Licence:** imported prose stays CC BY-SA 4.0, with wiki contributors credited in each page's history and
    footer. New contributions are CC BY-SA 4.0, and the edit form says so.
14. **Performances** are records attached to a link: performers, event, year and uploader. The original
    description text is kept as written.
15. **Collections** are a directory entity. Links can point at a collection with a locator ("p. 63"). Contents
    enumeration is post-v1.

## 3. Architecture

Two repos, both from the alignment-feed skeletons (layering, idioms, CLAUDE.md quality checks, CI).

- **`bardic-torch`** (Go API): transport/web (net/http `ServeMux`) → command → domain → datasources/mysql
  (sqlc), with golang-migrate. Entrypoints: `cmd/app` (the API), `cmd/migrate` (dokku's predeploy step),
  `cmd/import-mediawiki` (§7) and `cmd/check-links` (§5.5, dokku cron). The OpenAPI spec (`openapi/api.yaml`)
  is the contract with the FE.
- **`bardic-torch-fe`** (React Router 8 on Cloudflare Workers): openid-client against Auth0 with a cookie session;
  server-side proxy routes, so the browser never calls the API; response types generated from the spec (zod only
  for form input and configuration, so a new API field never breaks the FE); Tailwind 4 tokens for light and dark
  themes.
- **Abuse control** is Cloudflare's: a rate-limiting rule on the zone, and Turnstile on the contact form. The API
  has no rate limiter of its own beyond the cap on pending revisions.
- Kept from the feed: the Auth0 JWT middleware and the null-driver pattern. There are no API tokens; every
  signed-in caller is an Auth0 session.

## 4. Data model (MySQL 8)

The migrations hold the exact DDL. Each **editable entity** has `current_revision_id` (NULL until first approval)
and the import columns `legacy_wiki_pageid` and `legacy_title`.

- **`pieces`**: title, slug and sort keys; kind and secondary kinds; `conservation_status` with
  `conservation_history` (status, reason, source, revision); `is_stub`; notes, version notes and historical source
  (Markdown), `period_start`/`period_end` (R-23); licensing (`licensing_status`, `licence`, `licence_version`
  beside a `cc_*` licence only, `licensing_notes_md`); `legacy_had_inline_lyrics`. Children: aliases, terms
  (`is_primary`, `part`), relations (variant_of, contrafactum_of, setting_of, same_text_other_form) and credits
  (person, role, attribution, certainty, part, note; a person exactly when `named`).
- **`persons`**: name, slug and sort keys; `kind` (community_bard / historical / group); `status` (active /
  deceased / prefers_no_page) with date and source; pronouns, notes, licensing; `is_stub`. Children: aliases
  (`legal`, `sca`, `stage`, `nickname`, `former`, `redirect`, all public), roles, affiliations and relations
  (heir_of, member_of, same_as).
- **`terms`**: facet, name, `display_name` (without the wiki's "(Topic)" suffix), slug, parent (depth ≤ 3),
  `related_affiliation_id` (§2.12), description, position, aliases, and the `term_ancestors` closure.
- **`collections`** (addressed by ID) and **`events`** (venue / event).
- **`links`**: `url`, unique `normalized_url`, title, description, platform, `place_type`, collection, locator,
  and system state: `liveness` (unknown / ok / dead / blocked), failure count, check and archive times,
  `archived_url`, and `is_hidden` (a moderator takedown, R-11). `link_attachments` give a link one or more roles
  on a piece or person. `performances` (piece, event, year, uploader, verbatim description, performers) hang off
  a link and belong to the document that records them.

**Workflow tables:**
- **`revisions`**: entity, base revision, author, `source` (user / import), `status` (pending / approved /
  rejected / withdrawn), `snapshot_json`, summary, reviewer, review time and note, `created_at`;
  `revision_contributors` credits wiki contributors on imports.
- **`users`**: Auth0 ID, `display_name`, `role` (contributor / moderator), `blocked_at`. At startup the API
  promotes (and unblocks) the IDs in `BOOTSTRAP_MODERATOR_USER_IDS`; it never demotes (that is done in SQL).
- **`link_reports`**: link, reporter, `reason` (broken / wrong_link / privacy / copyright / lyrics / other),
  message, `status` (open / resolved / dismissed), resolver and note.
- **`contact_requests`**: `kind` (takedown / opt_out / correction / general), name, email, message, URL, status,
  resolver, note, `purged_at`.

### 4.1 Catalogue rules (built in BE2)

- **Visibility is approval.** An entity is public when it has an approved revision (a term also needs its
  ancestors approved), defined once in the views `public_terms`, `listed_persons` and `public_link_attachments`.
  References to non-public entities are left out of every read, and an unknown or non-public slug is one 404.
- **"Prefers no page" persons** (the one privacy rule) show only name, kind, status, date and source. They are
  found by name only, and left out of every filter, count, credit, performer list, relation, citation and
  needs-work.
- **Hidden links** are left out of every read, revision views included: a snapshot drops a link whose URL
  normalises to a hidden link's, or does not normalise, and otherwise shows its own URL.
- **Kinds are an enum**, not terms. **Slugs and sort keys** are set by the writer (sort keys ignore a leading
  article).
- **Search** is InnoDB FULLTEXT on tokens the API makes itself, with a prefix fallback; reads are capped at 2 s.
  **Facets** OR within a facet and AND across facets, matching descendants, with disjunctive counts.
- **Lists** page with `limit` (≤ 100) and `offset`; nested lists show at most 200 entries and a total.
- **Public reads ignore credentials**, and **needs-work** (R-18) lists public stubs, unassessed pieces, unknown
  licensing and visible dead links (restricted pieces only as stubs).
- **Responses:** fields always present, objects closed; `legacy_*`, `normalized_url`, internal IDs and user
  IDs never exposed. Pages carry `updated_at` (when the current revision was approved), and piece summaries
  `credit_names` (up to five credited persons that a public read would show).

### 4.2 Documents and snapshots

A revision is public only if it is approved and its entity is public; pending and rejected revisions are seen only
by their author (`/v1/me/revisions`) and moderators. A snapshot holds only editor-owned fields, never IDs, sort
keys, `legacy_*`, `conservation_history` (appended at approval) or link system state, so approving an old snapshot
never un-hides a link or undoes a link check. Links are inline by URL; other entities are referenced by ID in
storage and by slug in the editor format. An added field needs no `schema_version` bump; any other change of shape
does. **Revision views** apply the §4.1 rules to the stored document, and compute `changed_fields` on what they
show. Authors appear by display name; imports list the wiki contributors.

## 5. Revisions and moderation

### 5.1 Contributor edits

- **Edit:** `GET /v1/{type}/{key}/editable` returns the document in editor format with its base revision, or the
  caller's own pending revision of the entity if they have one.
- **Submit** creates a `pending` revision; a contributor's next submit for the same entity withdraws their earlier
  one. At most 30 pending revisions per contributor. The author may **withdraw** a pending revision.
- **Revert** has no endpoint of its own: the FE loads an older approved snapshot into the editor and submits it
  through `POST /v1/revisions` on the current base, with a "Revert to revision N" summary.
- **Moderators' own edits** are approved as they are saved, and still recorded; on a stale base they
  get the same warning as in §5.3.
- **Pending banner:** an author sees, on the entity's page, a banner linking to their pending revision (the FE
  finds it in `/v1/me/revisions`, at most 30).
- **"Prefers no page":** a document whose editor would show such a person (their own, or one with them as a
  credit, performer or relation) is editable by moderators only, so the editor never shows a contributor what
  public reads hide. Hidden links are left out of editable documents, so an approved edit drops their attachment
  (restoring the link later does not bring it back); a re-added URL that normalises to a hidden link stays hidden.

### 5.2 Validation

One `Validate` per document type, shared with the importer (widths, NFC, control characters, caps, the credit
and facet rules, trees without cycles, unique term names in a facet), answering 422 with a path and code per
problem.

- **Sourced fields** (R-12), for moderators too: changing licence, licence version, licensing notes or a known
  licensing status needs a `licensing_source` link; a person status other than active needs its source; a
  conservation status other than unassessed needs its source, and a change needs a summary (the reason).
- **Duplicate warning on create** (R-17): titles, names and aliases folded by `domain.DuplicateKey` (case,
  apostrophes, accents) are compared with public entities. A match needs `acknowledge_duplicates` (409
  `possible_duplicate` otherwise) and is shown to the moderator. A match on a "prefers no page" person blocks a
  new person.

### 5.3 Approval

One transaction validates the snapshot, writes it into the entity and its children (links upserted by
`normalized_url`, the closure rebuilt if a term changed, conservation history appended), and points
`current_revision_id` at it. A rejection carries a note for the author.

- **Stale revisions:** if the base is no longer current, the moderator sees the diff against current and either
  approves anyway (`acknowledge_stale`), warned that changes since will be overwritten, or rejects asking the
  author to redo the edit on the current version. There is no merge or rebase.
- **New entities:** a new entity's revision has no entity row (`entity_id` NULL); the row, its slug and its sort
  keys are created at approval. There are no placeholder rows.
- **References are to public entities only.** A revision may reference only entities that are already public. A
  new term is suggested as its own term revision and approved on its own; the piece uses it once it is public.

### 5.4 Moderation, reports and contact

- **Queue:** pending revisions, oldest first. The moderator's revision view shows the full document and its diff
  against its base and, when stale, against current.
- **Blocking:** moderators block and unblock contributors; moderators cannot be blocked. A blocked user gets 403 on
  every authenticated endpoint; their pending revisions stay for a moderator to reject.
- **Display names** are set with `PATCH /v1/me` (non-blank, NFC, ≤ 255 characters). Moderators are badged.
- **Link reports** (R-11): a signed-in user reports a link by URL with a reason and optional message, one open
  report per user and link. Moderators resolve or dismiss each with a note, and may hide the link.
- **Takedown:** moderators find a link by URL (`GET /v1/moderation/links`) and hide or restore it everywhere
  (`PUT /v1/links/{id}/hidden`).
- **Contact form** (R-13, no account): kind, name, email, message, URL and a Turnstile token checked server-side
  (success, hostname, action `contact`). Moderators resolve or dismiss requests. The personal fields are never
  logged, and are nulled 180 days after resolution or dismissal (checked at startup and when the inbox is listed).

### 5.5 Link check (`cmd/check-links`)

Daily, from dokku cron:
- Checks up to 400 visible links unchecked for 7 days, politely (one request per host per second, 15 s and 5
  redirects per link); HEAD, then GET.
- 2xx is `ok`; the third 404, 410 or NXDOMAIN in a row makes a link `dead`; 401, 403, 429 and other refusals are
  `blocked`; timeouts, 5xx and connection errors change only `last_checked_at`.
- Asks the Wayback Machine's availability API for a copy of links without one (dead first), at most once a link
  per 30 days, and stores what it finds.
- **Internal-address guard:** http(s) on ports 80 and 443 only, no credentials, and only global unicast addresses
  outside the special-purpose ranges, checked on every redirect and on the address dialled. A refused link is
  `blocked`.
- Stops after 20 minutes and logs links by ID, never by URL.

## 6. API surface

The OpenAPI spec is the exact contract.

- **Public reads** (BE2): `GET /v1/pieces` (filters `term`, `kind`, `conservation_status`, `stub`, `q`, `letter`,
  `sort`, paging; facet and letter counts), `/v1/pieces/{slug}`, `/v1/persons` (`kind`, `role`, `affiliation`,
  `q`, `letter`, `sort`), `/v1/persons/{slug}`, `/v1/terms?facet=…`, `/v1/terms/{slug}`, `/v1/collections`,
  `/v1/collections/{collection_id}`, `/v1/events`, `/v1/events/{slug}`, each entity's `…/history`,
  `/v1/revisions/{revision_id}` and `/v1/needs-work`.
- **Signed in:** `GET|PATCH /v1/me`; `GET /v1/me/revisions[/{revision_id}]`; `GET /v1/{type}/{key}/editable`;
  `POST /v1/revisions`; `POST /v1/revisions/{id}/withdraw`; `POST /v1/link-reports`.
- **Moderator:** `GET /v1/moderation/queue`,
  `GET /v1/moderation/revisions/{id}`, `POST /v1/revisions/{id}/approve|reject`,
  `GET /v1/moderation/link-reports`, `POST /v1/moderation/link-reports/{id}`,
  `GET /v1/moderation/contact-requests`, `POST /v1/moderation/contact-requests/{id}`,
  `GET /v1/moderation/links?url=`, `PUT /v1/links/{id}/hidden`, `PUT /v1/users/{id}/blocked`. Moderation
  responses carry the author's user ID and link IDs that public reads never expose.
- **No account:** `POST /v1/contact`. **Health:** `/healthz`, `/readyz`.

Bodies are limited to 1 MiB for documents, 64 KiB for `/v1/contact` and 16 KiB otherwise.

## 7. Importer (`cmd/import-mediawiki`)

**Input:** the research dump (`pages.jsonl`, `categories.json`, `redirects.json`, `activity.json`); the research
`scripts/` define the expected behaviour. A copy with inline lyrics replaced by placeholder lines is committed
under `testdata/wiki-dump/`, so tests need no network; it is frozen and never regenerated. `denylist.txt` holds
salted hashes of the dropped lyrics' 8-word n-grams, and a test fails if any appears in the repository.

**Steps:** type the pages (pieces, bards) and parse their sections; take facets from the categories
(`taxonomy.json`); parse credits with attribution, certainty and part, uniting the bard
signals (infobox, bard category, works list) and matching plain-text names by folded name, then fuzzily; extract
links with role, platform, collection and locator, deduplicated by normalised URL; parse performances, keeping
the text verbatim; turn redirects into aliases and red links into stubs; **drop inline lyrics** (four or more
lines, and link-section prose that could be lyrics), setting `legacy_had_inline_lyrics`; and write everything as
approved `import` revisions crediting wiki contributors.

**Writing:** strings are NFC without control characters; wikitext becomes escaped Markdown with absolute http(s)
links free of tracking parameters; email addresses in free text become `[email withheld]`; summaries come from
fixed templates. `CatalogueImport.Validate` checks the import against the schema's constraints first, and it is
written in one transaction into an empty catalogue.

**Modes:** `--dump DIR (--dry-run | --apply)`; both print a short count summary, and `--dry-run` never opens a
database. The launch run (a dry run, then `--apply` once) is in DEPLOY.md.

**Tests:** the research counts (142 pieces, 135/4/2/1 by kind; 48 bards; 61 collections; 11 inline-lyrics pages);
no dropped lyric in any field (the n-gram scan); goldens of the tricky pages; the writer writes every row, rolls
back on failure and refuses a non-empty catalogue; the imported catalogue reads back valid by the spec.

## 8. Frontend

**Read pages (FE2, built):** home (mission, non-affiliation disclaimer R-1); A–Z; faceted browse with a desktop
sidebar and mobile drawer (R-2, R-3); search; piece page (infobox, links by role, performances, licensing card,
notes, related pieces); person, term, collection and event pages; history and diffs; help pages (contributing,
FAQ for bards, the Bard Preference Checklist as guidance, the conservation guide, the licence).

**Editing (FE3):** an edit form per entity type with structured pickers; a credit editor (attribution,
certainty); a link editor (role, place type, collection, locator, performances); a term picker with "suggest a
new term" (needs a parent, R-20); a sourced-field widget that needs a URL; preview with the read components; the
duplicate warning; the pending banner; "my contributions"; withdraw; revert (an old version loaded into the
editor and submitted); the CC BY-SA notice. "Suggest a new term" submits a term revision; the piece can use the
term once it is approved.

**Moderation and contact (FE4):** the queue; the review page (diffs, the stale warning, approve or reject with a
note); the link-report and contact inboxes; takedown and blocking; the needs-work page; the
contact / takedown / opt-out form with Turnstile.

**Standards:** server-rendered with page metadata; the browser never calls the API; WCAG 2.2 AA and keyboard
operable; light and dark themes; works at 360px; no layout shift; plain, friendly copy.

## 9. Testing

Test the behaviour that matters at the cheapest level that proves it: one test per rule, not one per status code
or combination. A test earns its place by catching a bug someone could plausibly write.

**BE:**
- Unit tests for domain rules: revision states, validation, sourced fields, the duplicate key, the stale check,
  URL normalisation, and the link-check guard and classification.
- Repository tests on real MySQL 8.4 (sandbox, host and CI) for the queries and the approval write: the main
  path and the cases that matter.
- HTTP tests: one table of who may call each write route (anonymous, contributor, moderator, blocked) and a happy
  path per route, checked against the spec. Controllers are tested through the HTTP tests or one hand-written fake
  repository, not generated mocks.
- No tests of test helpers, of generated SQL strings, or of config files beyond "every env var is in `.env.dist`".
- One small leak test: a "prefers no page" person and a hidden link, seeded with credits, performances and
  citations, appear in no public read.
- The importer tests (§7); `make validate` and `make vulncheck` in CI.

**FE:**
- Component tests for components with logic (forms, editors, pickers, the facet drawer); loader and action tests
  against msw with spec-checked fixtures.
- A short Playwright suite, one journey per main flow: browse and search, read a piece and a bard, sign in
  (stubbed), edit and submit with the duplicate warning, approve and reject (one stale), report a link, contact.
  axe on each page type in the default theme (the token contrast check covers dark mode); screenshots at 360 and
  1280px for the UX review.
- `npm run validate`, the `e2e` job and `npm run audit` in CI.

**Full stack (H1):** BE, FE and Playwright on the real import, then John and Fabian click through it.

## 10. Build sequence

Each milestone is one pipeline in one repo, coordinated by the host. A pipeline plans (chunks with acceptance
tests), implements (Opus for the revision engine and moderation UI, Sonnet for the mechanical parts), gates each
chunk on `make validate` / `npm run validate`, reviews (correctness, security, tests against the acceptance
list, UX and accessibility from screenshots, and **proportion**: code or tests out of scale with this plan are a
finding), fixes, and commits on a milestone branch with `CHANGES.md`. The host merges, re-checks, and reports to
John and Fabian.

Reviewers judge against this plan, not every conceivable edge case; a finding that would add a rule or mechanism
goes to the owners as a question.

| # | Pipeline | Repo | After | Contents |
|---|---|---|---|---|
| H0–BE3, FE1–FE2 | | | | *Done:* host setup, foundations, catalogue and read API, importer, read UI. |
| BE-S | simplify | BE | BE3 | *Done:* removed what this plan no longer needs (below). |
| FE-S | simplify | FE | FE2 | Remove what this plan no longer needs (below). |
| BE4 | revisions and moderation | BE | BE-S | §5 and the §6 write routes; migrations for the revision columns, `link_reports`, `contact_requests`, performance owners, the link-check columns and `licence_version`; `updated_at` and `credit_names`; the `check-links` cron. |
| FE3 | editing | FE | FE-S, BE4 spec | §8 "Editing". |
| FE4 | moderation and contact | FE | FE3 | §8 "Moderation and contact". |
| H1 | launch | host | all | The Cloudflare rate-limiting rule; full-stack e2e on the real import; staging; the real import; the click-through; production. |

**BE-S removed** hidden aliases (`person_aliases.is_public` by migration, the score-0 alias search, the snapshot
name rules and their tests); the `draft` status and `mcp` source values; the importer's name withholding and every
other rule that treated names as private; the import review file (`import-review.md`, `--review`); the hostile-input
CPU-time tests; the unused `claimed_by_user_id` column and preference tables; API tokens (`api_tokens` by
migration, `/v1/tokens*`, the token validator in the auth middleware) and the `auth0|` credential prefix (the API
takes `Authorization: Bearer <jwt>`); `cmd/scrub-dump` and the scrubber, keeping only the lyric n-gram scan of
`internal/scrub`; and the docs about them. It also brought the tests to §9:
- deleted the research-oracle tests, the tests of test helpers, and the generated-SQL string tests (keeping the
  search integration tests and a few builder unit tests);
- replaced the mockery mocks and one-method interfaces with the HTTP tests or one hand-written fake;
- merged the auth-matrix and routing tests into one table that also checks route parity with the spec;
- cut the leak matrix to the one small leak test and the Auth0 validator tests to one per rule;
- dropped the schema-mirroring import validator test, and trimmed `deploy_files_test.go` to the `.env.dist` check;
- replaced hidden-link snapshot redaction with the §4.1 deny-set.

TESTING.md, DEPLOY.md and CLAUDE.md were rewritten to this plan, short. Public responses kept their shape, apart
from what went with tokens.

**FE-S removes** API tokens (settings UI, proxy routes, tests), the styleguide route and the `auth0|` credential
prefix (the proxy sends `Authorization: Bearer <access_token>`), and brings the FE to §9 and §3:
- one table-driven server-render smoke test in place of the per-route SSR files;
- component tests only for components with behaviour;
- a short e2e suite (about eight read journeys; axe once per page template; screenshots on demand, not a gate;
  one 360px reflow sweep);
- repo-scan meta-tests cut to the raw-colour, worker-entry, exact-pin and no-secret checks;
- response types from the generated spec types instead of strict zod schemas;
- a fixture API serving canned responses instead of re-implementing BE rules;
- table-driven server, catalogue-helper and theme tests;
- one `sitemap.xml` and no JSON-LD; query-state without the offset caps and most redirect variants;
- a hand-written Tailwind `@theme` block in place of the theme generator (keeping `tokens.ts` and the contrast
  check);
- the audit workflow weekly and on pull requests to main; one knip pass.

DESIGN.md, TESTING.md and CLAUDE.md are rewritten to this plan, short (DESIGN about 300 lines: principles, tokens,
breakpoints, voice, accessibility). Event and venue pages stay.

**BE4 reuses** from the closed `milestone/be4-moderation` and `milestone/be5-extras` branches where they fit: the
`link_reports` and `contact_requests` DDL and code, the Turnstile and Wayback clients, the link-check guard and
prober, the entity writer shared with the importer, the document `Validate` functions and `DuplicateKey` (without
hidden-data handling), the bounded connection pools and the 503 for a refused connection (BE4's connection
fix), and the `licence_version`, `updated_at` and `credit_names` read changes. It reuses none of the merge,
withhold/restore, drafts, placeholder rows, pending-reference approval, flags, revert endpoint, rate limiting,
display-name controls, repertoire, preferences or MCP code; the revision write path is rewritten smaller, using
the old tests as a source of cases worth keeping, not as a list to reproduce.

**Order:** BE-S ∥ FE-S → BE4 → FE3 → FE4 → H1. FE3 may start on BE4's spec while BE4 is built; at most two
pipelines run at once, one per repo.
