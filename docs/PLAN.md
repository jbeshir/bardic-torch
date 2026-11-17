# Bardic Torch: implementation plan

**Status:** approved 2026-10-31; rewritten 2026-11-10 to a simpler scope (the commit that rewrote it lists what
changed); revised 2026-11-12 (monthly link check, link reports as contact requests, takedowns by link ID, sources
for licensing only, no email withholding in the importer); revised 2026-11-13 (trimmed the data model to what the
wiki fills); revised 2026-11-15 (content fidelity: §2 and the help pages follow the original wiki's policy and
wording, except for the three decisions in §2.0; four importer guesses removed, §7); revised 2026-11-15 (the
owners' decisions on the last open questions: §2.1, §2.3, §4.1); revised 2026-11-15 (text provenance: the
importer writes no sentence of its own into notes, §7).
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
- **No lyrics are stored or shown; moderators remove any that are submitted.** Pieces carry only links, metadata
  and sourced notes.
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

Confirmed by John on 2026-10-31; 2.3, 2.6, 2.7 and 2.15 amended on 2026-11-10; 2.1–2.3, 2.6, 2.8 and 2.14 amended
on 2026-11-15 to follow the wiki; 2.1 and 2.3 settled by the owners on 2026-11-15.

0. **The original wiki's policy and wording win.** Where this section and the wiki disagree, the wiki is right
   and this section is corrected; a rule here stands only where it fills a gap the wiki left. Only three decisions
   override the wiki, because the owners made them explicitly: **links only, never lyrics** (the wiki allowed public
   domain, Creative Commons and permitted lyrics); **edits go live after moderator review** (the wiki was open);
   and **"prefers no page"** works as 2.8 says. The help pages carry the wiki's own text, changed only for
   formatting, the new site's editing mechanics and these three decisions.

1. **Kind** (the wiki's Genre): one `kind` per piece (song, poem, story, dance_music, instrumental; extensible). A
   musical piece can be "tagged as both 'Songs' and 'Instrumental' if it can be performed by both singers and
   instrumentalists", as the wiki says, through the **Instrumental** term (facet Other): the piece keeps one kind,
   the importer gives the term to every piece in the wiki's Instrumental category, and editors pick it like any
   other term. The wiki's Comedy genre (fooling, jesting) has no kind: comedic pieces take the Comedic mood. The
   wiki's poem Form category has no facet. (The owners decided all three on 2026-11-15.)
2. **Origin** (the wiki's "Source Category"): generally one, as the wiki says; a contrafactum or setting piece may
   also carry the pre-1600, traditional, SCA, SF&F or Borrowings categories the wiki allows, after the main one.
   Stored multi-valued and ordered, like every facet; century categories become child terms of Origin; topic
   centuries stay under Topic.
3. **Conservation status:** an enum (unassessed, low_risk, endangered, critically_endangered, restricted) with the
   wiki's definitions, from its Conservation Status page and status categories:
   - **Low Risk:** the work exists in at least three places (Category:Low Risk; the Conservation Status page's
     "at least four" is superseded, so exactly three places is Low Risk).
   - **Endangered:** it exists in less than three known places, and is not Restricted.
   - **Critically Endangered:** contributors are unable to find the work, or only part of it, and it is not
     Restricted.
   - **Restricted:** the creator does not allow the work to be recorded, archived or shared. It is never marked
     Endangered or Critically Endangered, because those exist to help works be preserved or found, and these
     should not be. A piece has one status, so a Restricted piece shows no other. Its page shows none of its links
     or recordings, only its licensing sources (where the creator says so), and has no prompts to find, add or
     archive copies (decided 2026-11-15). Pages that list its performances or citations name it without a link.
   - **unassessed** is a piece in none of the wiki's conservation categories (a gap the wiki left).

   Anyone may change a status in an edit, by the conservation guide, and a moderator reviews it like any edit. Its
   history is the piece's revision history, with each revision's summary (which may be empty) as the reason for a
   change. The creator's statement, with a link to where they made it, goes in the piece's notes, which a
   Restricted page shows like any other.
4. **Pseudo-bards:** "Traditional & Folk", Unknown, Anonymous and Various become an `attribution` on the credit,
   with a separate `certainty` (Disputed is a certainty). Bands and historical figures are persons with
   `kind = group | historical`.
5. **Red-link people and missing works** are imported as public stub pages, marked "stub — help complete this".
6. **Legal and mundane names** in bard aliases are imported as **ordinary public aliases**, as the wiki shows
   them; as on the wiki, only what bards publish on their public-facing pages is added. There is no hidden-alias concept. Someone who wants a name removed asks, and a moderator deletes it.
7. **Bard self-verification** is out of scope for v1. Every licensing claim needs a source URL, as on the wiki
   (R-12).
8. **"Prefers no page"** is a person status, as on the old wiki: their page shows only their identity (name, kind
   and status) and their notes, where their statement goes with a link to where they made it. As the wiki says,
   that statement must be public, permanent and on a page where the person is identifiable, and a request to take
   a page down is met as best we can. Creating a person
   with a matching name or alias is blocked. Elsewhere they are named like anyone else, as plain text without a
   link to their page (decided 2026-11-12; nothing else is hidden).
9. **Ambiguous records** (the five cases the research report names) are imported as the importer's best guess.
10. **Unparented categories:** Seasons, Fire, Hope → Topics; Archery → Topics › War; Short Pieces → Other;
    Jewish, Sephardic, Portuguese → Cultures, with Sephardic under Jewish.
11. **Rude** stays a mood. A content-advisory flag can come later.
12. **Kingdoms:** one `affiliation` term tree (SCA › Kingdom › group); bards link to it as affiliations, and each
    `Patriotic (Kingdom)` topic term links to its kingdom.
13. **Licence:** imported prose stays CC BY-SA 4.0, with wiki contributors credited in each page's history and
    footer. New contributions are CC BY-SA 4.0, and the edit form says so.
14. **Performances** are records attached to a link: performers, event, year and uploader. The original
    description text is kept as written. The importer leaves the uploader empty: the wiki's wording ("X on YouTube
    by Y") does not tell the uploader from the artist or singer, and the description already says it. Contributors
    can set it.
15. **Collections** are a directory entity. Links can point at a collection with a locator ("p. 63"). Contents
    enumeration is post-v1.

## 3. Architecture

Two repos, both from the alignment-feed skeletons (layering, idioms, CLAUDE.md quality checks, CI).

- **`bardic-torch`** (Go API): transport/web (net/http `ServeMux`) → command → domain → datasources/mysql
  (sqlc), with golang-migrate.
  The OpenAPI spec (`openapi/api.yaml`)
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

- **`pieces`**: title, slug and sort keys; `kind`; `conservation_status`; `is_stub`; notes and version notes
  (Markdown); licensing (`licensing_status`, `licensing_notes_md`); `legacy_had_inline_lyrics`. Children: aliases
  (`alt_title`, `subtitle`, `redirect`), terms (ordered by `position`) and credits (person, role: lyrics, tune,
  lyrics_and_tune ("Words and tune": one person's words and tune together, counting as both) or author,
  attribution, certainty; a person exactly when `named`).
- **`persons`**: name, slug and sort keys; `kind` (community_bard / historical / group); `status` (active /
  deceased / prefers_no_page); notes, licensing; `is_stub`. Children: aliases (`legal`, `sca`, all public), roles
  and affiliations.
- **`terms`**: facet, name, `display_name` (without the wiki's "(Topic)" suffix), slug, parent (depth ≤ 3),
  `related_affiliation_id` (§2.12), description, position, and the `term_ancestors` closure.
- **`collections`** (addressed by ID: name, URL, kind, description, region) and **`events`** (a name).
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
  promotes (and unblocks) the IDs in `BOOTSTRAP_MODERATOR_USER_IDS`; it never demotes (that is done in SQL). A
  moderator bootstrapped before signing in has an empty `display_name` until their first request.
- **`contact_requests`**: `kind` (takedown / opt_out / correction / link_report / general), name, email, message,
  URL (for a `link_report`, the link's), status, resolver, note.

### 4.1 Catalogue rules (built in BE2)

- **Visibility is approval.** An entity is public when it has an approved revision (a term also needs its
  ancestors approved), defined once in the views `public_terms`, `listed_persons` and `public_link_attachments`.
  References to non-public entities are left out of every read, and an unknown or non-public slug is one 404.
- **"Prefers no page" persons** have a page of their identity and notes only: name, kind, status and notes.
  Credits, performers and citations name them like anyone else, without a link (`slug` null). The lists of persons
  (`listed_persons`) list them as stubs, find them by name only, and leave them out of role and affiliation
  filters and needs-work.
- **Term counts and member lists leave out stubs** (no-page persons among them), as the piece and bard lists do
  unless asked, so every count on the site (home, categories, lists) counts the same pages.
- **Hidden links** are left out of every read by their link ID. Current pages join on the link's row. A revision
  view leaves out a snapshot URL whose row (matched by normalised URL, as approval upserts it) is hidden, and shows
  every other URL as the snapshot wrote it.
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
by their author and moderators. A snapshot holds only editor-owned fields, never IDs, sort
keys, `legacy_*` or link system state, so approving an old snapshot never un-hides a link or undoes a link check.
Links are inline by URL; other entities are referenced by ID in storage and by slug in the editor format: a
piece's terms are one ordered list, `term_ids` (`term_slugs` in the editor format), beside its flat
`conservation_status`. An added field needs no `schema_version` bump; any other change of shape does. **Revision
views** apply the §4.1 rules to the stored document, and compute `changed_fields` on what they
show. Authors appear by display name; imports list the wiki contributors.

## 5. Revisions and moderation

- **Blocking:** moderators block and unblock contributors; moderators cannot be blocked. A blocked user gets 403 on
  every authenticated endpoint; their pending revisions stay for a moderator to reject.
- **Display names** come from Auth0: a post-login Action puts the user's name from Google or Facebook in the
  access token's `https://bardictorch.org/name` claim (DEPLOY.md), unless the name is empty or is the user's
  email address. The API cleans it (characters names may not hold, such as zero-width joiners and direction
  marks, dropped; NFC, trimmed, cut to 64 characters), rejects a token without one that is still valid with 401, creates users with it, and replaces a stored name
  that differs, so a name changed at the provider shows from the next sign-in or token refresh, on past edits
  too. Users cannot set their own. Moderators are badged.

## 6. API surface

The OpenAPI spec is the exact contract.

- **Public reads** (BE2): `GET /v1/pieces` (filters `term`, `kind`, `conservation_status`, `stub`, `q`, `letter`,
  `sort`, paging; facet and letter counts), `/v1/pieces/{slug}`, `/v1/persons` (`kind`, `role`, `stub`,
  `affiliation`, `q`, `letter`, `sort`), `/v1/persons/{slug}`, `/v1/terms?facet=…`, `/v1/terms/{slug}`,
  `/v1/collections`, `/v1/collections/{collection_id}`, `/v1/events`, `/v1/events/{slug}`, each entity's `…/history`,
  `/v1/revisions/{revision_id}` and `/v1/needs-work`.
- **Signed in:** `GET /v1/me`;
- **Moderator:**
  `PUT /v1/users/{id}/blocked`.
- **Health:** `/healthz`, `/readyz`.

## 8. Frontend

**Read pages (FE2, built):** home (mission, non-affiliation disclaimer R-1); A–Z; faceted browse with a desktop
sidebar and mobile drawer (R-2, R-3), the piece and bard lists leaving out stubs unless one link asks for
them, so that their totals match the home page's and the categories' (search still finds stubs); search; piece
page (infobox, links by role, performances, licensing card, notes); person, term, collection and event pages; history and diffs; help pages
(contributing, FAQ for bards, the Bard Preference Checklist, the conservation guide, the licence),
carrying the wiki's own text (§2.0).

**Editing (FE3):** an edit form per entity type with structured pickers; a credit editor (attribution,
certainty); a link editor (role, place type, collection, locator, performances); a term picker with "suggest a
new term" (needs a parent, R-20); a licensing-source prompt (R-12); preview with the read components; the
duplicate warning; the pending banner; "my contributions"; withdraw; revert (an old version loaded into the
editor and submitted); the CC BY-SA notice. "Suggest a new term" submits a term revision; the piece can use the
term once it is approved.

**Moderation and contact (FE4):** the queue; the review page (diffs, the stale warning, approve or reject with a
note); the contact inbox (link reports included); takedown and blocking; the needs-work page; the
contact / takedown / opt-out form with Turnstile.

**Standards:** server-rendered with page metadata; the browser never calls the API; WCAG 2.2 AA and keyboard
operable; light and dark themes; works at 360px; no layout shift; plain, friendly copy.

## 9. Testing

Test the behaviour that matters at the cheapest level that proves it: one test per rule, not one per status code
or combination. A test earns its place by catching a bug someone could plausibly write.

**BE:**
- Repository tests on real MySQL 8.4 (sandbox, host and CI) for the queries
  and the cases that matter.
- HTTP tests: one table of who may call each write route (anonymous, contributor, moderator, blocked) and a happy
  path per route, checked against the spec. Controllers are tested through the HTTP tests or one hand-written fake
  repository, not generated mocks.
- No tests of test helpers, of generated SQL strings, or of config files beyond "every env var is in `.env.dist`".
- One small leak test: a hidden link, seeded with credits, performances and citations, appears in no public read.
- `make validate` and `make vulncheck` in CI.

**FE:**
- Component tests for components with logic (forms, editors, pickers, the facet drawer); loader and action tests
  against msw with spec-checked fixtures.
- A short Playwright suite, one journey per main flow: browse and search, read a piece and a bard, sign in
  (stubbed), edit and submit with the duplicate warning, approve and reject (one stale), contact (including
  reporting a link). axe on each page type in the default theme (the token contrast check covers dark mode);
  screenshots at 360 and 1280px for the UX review.
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
| BE4 | revisions and moderation | BE | BE-S | §5 and the §6 write routes; migrations for the revision columns, `contact_requests`, performance owners and the link-check columns; `updated_at` and `credit_names`; the monthly `check-links` cron. |
| FE3 | editing | FE | FE-S, BE4 spec | §8 "Editing". |
| FE4 | moderation and contact | FE | FE3 | §8 "Moderation and contact". |
| H1 | launch | host | all | The Cloudflare rate-limiting rule; full-stack e2e on the real import; staging; the real import; the click-through; production. |

**BE-S removed** hidden aliases (`person_aliases.is_public` by migration, the score-0 alias search, the snapshot
name rules and their tests); the `draft` status and `mcp` source values; the importer's name withholding and every
other rule that treated names as private; the import review file (`import-review.md`, `--review`); the hostile-input
CPU-time tests; the unused `claimed_by_user_id` column and preference tables; API tokens (`api_tokens` by
migration, `/v1/tokens*`, the token validator in the auth middleware) and the `auth0|` credential prefix (the API
takes `Authorization: Bearer <jwt>`); `cmd/scrub-dump` and the scrubber; the importer's lyric heuristics beyond
the rule in §7; and the docs about them. It also brought the tests to §9:
- deleted the research-oracle tests, the tests of test helpers, and the generated-SQL string tests (keeping the
  search integration tests and a few builder unit tests);
- replaced the mockery mocks and one-method interfaces with the HTTP tests or one hand-written fake;
- merged the auth-matrix and routing tests into one table that also checks route parity with the spec;
- cut the leak matrix to the one small leak test and the Auth0 validator tests to one per rule;
- dropped the schema-mirroring import validator test, and trimmed `deploy_files_test.go` to the `.env.dist` check;
- replaced hidden-link snapshot redaction with a deny-set of hidden normalised URLs (since reduced to the §4.1
  rule by link ID).

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
breakpoints, voice, accessibility). Event pages stay.

**BE4 reuses** from the closed `milestone/be4-moderation` and `milestone/be5-extras` branches where they fit: the
`contact_requests` DDL and code, the Turnstile and Wayback clients, the link-check guard and
prober, the entity writer shared with the importer, the document `Validate` functions and `DuplicateKey` (without
hidden-data handling), the bounded connection pools and the 503 for a refused connection (BE4's connection
fix), and the `updated_at` and `credit_names` read changes. It reuses none of the merge,
withhold/restore, drafts, placeholder rows, pending-reference approval, flags, revert endpoint, rate limiting,
display-name controls, repertoire, preferences or MCP code; the revision write path is rewritten smaller, using
the old tests as a source of cases worth keeping, not as a list to reproduce.

**Order:** BE-S ∥ FE-S → BE4 → FE3 → FE4 → H1. FE3 may start on BE4's spec while BE4 is built; at most two
pipelines run at once, one per repo.
