# Bardic Torch: implementation plan

**Status:** approved (2026-10-31). Every default in §2 is confirmed, and both repos are public. This plan is the input every build pipeline reads.

**Research basis:** a full read-only crawl of https://bardictorch.miraheze.org (381 pages, as of 2026-10-30), held in the demesne job output `fe3d5dd2-f6f6-4a8c-b355-9dc525e718b1`.
- Report: `report.md`
- Raw dump: `dump/`
- Parsed entities: `analysis/`
- Crawl and parse scripts: `scripts/`

`R-n` references point at that report's requirements list (§7). `§n` references point at report sections.

## 1. Product summary

Bardic Torch is a public, community-edited **index** of SCA bardic pieces (songs, poems, stories, dance & music) and the bards who wrote or perform them. It helps people *find* pieces and *preserve* them. It replaces the MediaWiki site, which stays where it is on Miraheze.

Fixed decisions:
- **No lyrics or text are hosted**, ever. Pieces carry only links, metadata and sourced notes.
- **Moderators are John and Fabian.** Everyone else's edits wait for moderator approval: the last approved revision is the live one.
- **Login:** Auth0 social login (Google, Facebook).
- **Hosting:** the Go API runs on John's dokku instance (MySQL through the dokku mysql plugin). The Remix FE runs on Cloudflare Pages, following the alignment-feed pattern.
- **Domain:** the site is bardictorch.org, with the API at api.bardictorch.org.
- **No redirects** from old wiki URLs. The old wiki is imported once, at launch.
- **Look:** the site should feel like a wiki (so editing feels allowed), with full dark mode support and a mobile-first faceted browse (R-3).

Non-goals for v1:
- lyrics or text display
- file, audio or video uploads
- verified-bard accounts whose word counts as a source
- automatic enumeration of collection tables of contents
- old-URL redirects
- recommendations, RSS, chat

## 2. Decisions taken on the research's open questions (§10)

Every decision below was confirmed by John on 2026-10-31.

1. **Kind:** a primary `kind` (song, poem, story, dance_music, instrumental, comedy, extensible) plus `secondary_kinds[]`. Song + Instrumental and Story + Poem are both valid.
2. **Origin** (the wiki's "Source Category"):
   - multi-valued, with one value marked primary
   - an optional `part` scope (words, tune, verses, chorus)
   - century categories become child terms of Origin
   - topic centuries stay under Topic
3. **Conservation status:**
   - Stored as an enum: unassessed, low_risk, endangered, critically_endangered, restricted.
   - Every change is recorded with a reason.
   - The app *suggests* a status from a count of distinct live "places" among the links (each link carries a place type); a moderator confirms it.
   - **Exactly three places counts as Endangered** (the wiki leaves this case undefined).
   - Restricted overrides everything and hides the "help preserve this" prompts.
4. **Pseudo-bards:** "Traditional & Folk", Unknown, Anonymous, Various and Disputed become an `attribution` enum on the credit, plus a `certainty` field. Bands and historical figures become person records with `kind = group | historical`.
5. **Red-link people and missing works** are imported as **public stub pages**, marked "stub — help complete this", in line with the wiki's "any contribution counts".
6. **Legal and mundane names** in bard aliases are imported **hidden** (`is_public = false`) until a moderator reviews each one. Other aliases (SCA names, stage names, nicknames) stay public.
7. **Bard self-verification** is out of scope for v1. Every licensing and etiquette claim needs a source URL, as on the wiki (R-12, R-15).
8. **"Prefers no page"** is a person status:
   - The page becomes a stub that shows the cited statement.
   - The person's links and credits are hidden from public view.
   - Creating a duplicate person with a matching name or alias is blocked.
9. **Ambiguous records** are imported as the importer's best guess and listed in its review report for a human decision (Fight for the Midrealm's title, "My Thing Is My Own", Dunstan, Marian/Marion of Heatherdale, Samantha Moore / Ambra Michelli).
10. **Unparented categories** get these parents:
    - Seasons, Fire, Hope → Topics
    - Archery → Topics › War
    - Short Pieces → Other
    - Jewish, Sephardic, Portuguese → Cultures, with Sephardic under Jewish
11. **Rude** stays a mood. A separate content-advisory flag can come later.
12. **Kingdoms:** one shared `affiliation` term tree (SCA › Kingdom › group). Bards are linked to it as affiliations. Patriotic songs keep their `Patriotic (Kingdom)` topic terms, and each of those terms links to its kingdom term.
13. **Licence:** imported prose stays CC BY-SA 4.0, with wiki contributors credited in each page's history and footer. New contributions are also CC BY-SA 4.0, and the edit form says so.
14. **Performances** are first-class records attached to a link: performers, event, year and uploader. The original description text is always kept as written.
15. **Collections:** a directory entity. Links can point at a collection with a locator ("p. 63"). Table-of-contents enumeration is post-v1 (for MCP-assisted seeding).

## 3. Architecture

Two new repos, both copied from the alignment-feed skeletons (layering, idioms, CLAUDE.md quality checks, CI):

- **`bardic-torch`** (Go API)
  - Packages: transport/web (gorilla/mux) → command → domain → datasources/mysql (sqlc), plus migrations (golang-migrate).
  - Three entrypoints:
    - `cmd/app`: the API server
    - `cmd/import-mediawiki`: the one-shot importer
    - `cmd/check-links`: link liveness checks, run by dokku cron
  - `cmd/mcp` (an MCP server) comes later.
  - The OpenAPI spec (`openapi/api.yaml`) is **the contract** with the FE.
- **`bardic-torch-fe`** (Remix on Cloudflare Pages)
  - Reuses the feed's auth (remix-auth cookie session with refresh) and its server-side proxy routes (`/api/*`).
  - Types are generated from the BE's OpenAPI spec (openapi-typescript) and validated at runtime with zod.
  - Tailwind design tokens support light and dark themes.

**Removed from the feed skeleton:** Pinecone, VoyageAI, recommendations and clustering, RSS, chat, D1 and MiniMax.

**Kept and reused:**
- the multi-validator auth middleware (Auth0 JWT plus `user_api|` tokens), for future MCP and bulk entry
- the null-driver pattern
- the API token endpoints

## 4. Data model (MySQL 8)

**Editable entities** (each has a `current_revision_id`, plus `legacy_wiki_pageid` and `legacy_title` columns used for import):
- **`pieces`**:
  - identity: `slug`, `title`, `sort_title` (articles ignored)
  - type and status: `kind`, `conservation_status`, `is_stub`
  - Markdown fields: `notes_md`, `version_notes_md`, `licensing_notes_md`
  - licensing: `licensing_status` (known / unknown), `licence` (pd, cc-by…, arr, other, null)
  - history: `historical_source_md` and `period_start`/`period_end`, for documentable works (R-23)
  - import audit: `legacy_had_inline_lyrics`
  - Child tables: `piece_secondary_kinds`, `piece_aliases(text, kind)`, `piece_terms(term_id, is_primary, part, position)`, `piece_relations(to_piece, type: variant_of | contrafactum_of | setting_of | same_text_other_form)`, `conservation_history(status, reason, revision_id, at)`.
  - `piece_credits(person_id NULL, role: lyrics | tune | author | choreography | music | arrangement | translation, attribution: named | traditional | unknown | anonymous | various, certainty: certain | probable | possible | disputed, part, note, position)`
- **`persons`**:
  - identity: `slug`, `name`, `sort_name`, `kind` (community_bard / historical / group)
  - status: `status` (active / deceased / prefers_no_page), `status_date`, `status_source_url`
  - details: `pronouns`, `notes_md`
  - licensing: `licensing_notes_md`, `licensing_status`
  - flags: `is_stub`, `claimed_by_user_id` (unused in v1)
  - Child tables: `person_aliases(name, kind: legal | sca | stage | nickname | former | redirect, is_public)`, `person_roles`, `person_affiliations(term_id)`, `person_relations(to_person, type: heir_of | member_of | same_as)`.
  - `person_preferences(dimension, value_json, source_url)`, one row per Bard Preference Checklist dimension (R-8). Piece-level overrides go in `piece_preferences` in the same shape.
- **`terms`**: `facet` (kind, mood, topic, culture, language, legendaria, form, origin, other, affiliation), `name`, `display_name` (without the "(Topic)"/"(Language)" disambiguation suffix), `slug`, `parent_id` (depth ≤ 3), `description_md`, `status` (active / pending_suggestion).
  - Child tables: `term_aliases`, and a `term_ancestors` closure table rebuilt transactionally when a term revision is approved.
- **`collections`**: `name`, `sort_name`, `url`, `kind`, `description_md`, `region_term_id`, `parent_id`, `is_bardic_resource`, `enumerable`.
- **`links`**: `url`, `normalized_url` (unique; tracking parameters stripped), `title`, `description`, `platform`, `place_type`, `collection_id`, `locator`, `liveness` (unknown / ok / dead / blocked), `last_checked_at`, `archived_url`, `is_hidden` (moderator takedown, R-11).
  - `link_attachments(link_id, piece_id | person_id, role, position)`. The roles are official, lyrics, text, sheet_music, dance_instructions, text_source, translation, performance, licensing_source, website, external_profile, other. One link can carry several roles (R-21, R-14).
  - `performances(link_id, piece_id, event_id, year, uploader, description_verbatim)`, plus `performance_performers(person_id)`.
- **`events`**: name, kind (venue / event), description. Seeded with Green Dragon and with events parsed from performance descriptions.

**Workflow tables:**
- **`revisions`**: `id`, `entity_type`, `entity_id`, `base_revision_id`, `author_user_id`, `source` (user / import / mcp), `status` (draft / pending / approved / rejected / withdrawn), `snapshot_json`, `summary`, `reviewer_user_id`, `reviewed_at`, `review_note`, `created_at`.
  - The snapshot is the full document, including owned child rows. Links appear inline by normalized URL.
- **`users`**: Auth0 user ID, `display_name`, `role` (contributor / moderator), `created_at`, `blocked_at`. Moderators are seeded by migration.
- **`repertoire`**: user_id, piece_id, notes, visibility (private / public), added_at.
- **`link_reports`**: for removal requests (R-11).
- **`contact_requests`**: kind (takedown / opt_out / correction / general), name, email, message, url, status. These need no account and are protected by Cloudflare Turnstile (R-13).
- **`api_tokens`**: kept from the feed.

## 5. Revision engine and moderation rules (the core)

**Commands:** `SaveDraft`, `SubmitRevision`, `ApproveRevision`, `RejectRevision`, `WithdrawRevision`, `RevertTo`.

**Approval** is one transaction:
1. Check that `base_revision_id == current_revision_id`. If not, the revision is **stale**.
2. Validate the snapshot against the domain rules.
3. Write the snapshot into the entity and its child tables: upsert links by `normalized_url`, rebuild `term_ancestors` if a term changed, append to `conservation_history` if the status changed.
4. Point `current_revision_id` at the approved revision.

**Stale revisions** are rebased automatically onto the current version when the two edits touched different fields. Otherwise the moderator sees a three-way view and resolves it.

**Edit rules:**
- **Contributors:** edits go to `pending`. Contributors see their own pending version across the site, with a banner. They may have at most 30 open pending revisions.
- **Moderators:** edits are approved as they are saved, but still recorded as revisions.
- **Sourced fields** (licensing notes, licence, preferences, person status, conservation status) are rejected at validation without a source URL. The rule applies to moderators too (R-12).
- **New entities**, whether created by hand or as term suggestions, exist with `current_revision_id = NULL` and are invisible to the public until approved. When a moderator reviews a piece, any pending terms it references are shown and approved alongside it.
- **Duplicate check on create:** normalised titles, names and aliases are compared, with apostrophes and case folded, Unicode NFC applied and accents stripped for matching only (R-17).

**Revert** creates a new revision copying an older snapshot.

**History:** every entity has a public history list, and each revision a diff view. Wiki contributors are credited on imported revisions.

## 6. API surface (all specified in OpenAPI; the FE never calls the API directly from the browser)

- **Public reads:**
  - `GET /v1/pieces`: faceted search. Facet terms are combined with AND across facets and OR within one; descendants count via the closure table. Also takes `q` (FULLTEXT over title, aliases and notes), `letter`, `sort`, and `status`/`kind` filters, and returns counts for each facet value.
  - `GET /v1/pieces/{slug}`, `GET /v1/persons`, `GET /v1/persons/{slug}`
  - `GET /v1/terms?facet=…`: the term tree with counts
  - `GET /v1/terms/{slug}`, with its pieces and persons
  - `GET /v1/collections`, `GET /v1/collections/{id}`, `GET /v1/events/{slug}`
  - `GET /v1/{entity}/{id}/history`, `GET /v1/revisions/{id}`: the snapshot plus a structured diff against its base
  - `GET /v1/needs-work`: stubs, unassessed conservation status, missing licensing information, dead links (R-18)
- **Authenticated:**
  - `GET /v1/me`
  - drafts: `PUT /v1/drafts/{entity}/{id|new}`, and `POST /v1/revisions` to submit
  - `POST /v1/revisions/{id}/withdraw`
  - `GET /v1/me/revisions`
  - `GET|PUT|DELETE /v1/me/repertoire/{piece}`
  - `GET /v1/users/{id}/repertoire`: public lists
  - `POST /v1/links/{id}/report`
  - suggestions for new terms are submitted as term revisions
- **Moderator:**
  - `GET /v1/moderation/queue` (filters by type, author and age)
  - `POST /v1/revisions/{id}/approve`, `…/reject`, `…/rebase`
  - `GET|POST /v1/moderation/link-reports`, `GET|POST /v1/moderation/contact-requests`
  - `PUT /v1/links/{id}/hidden`
- **No account needed:** `POST /v1/contact`, verified with Turnstile.
- **Kept from the feed:** `/v1/tokens`.

## 7. Importer (`cmd/import-mediawiki`)

**Input:** the research dump format (`pages.jsonl` and `categories.json`), or a live crawl. The research `scripts/` define the expected behaviour and are ported to Go. A frozen copy of the dump is committed under `testdata/`, so tests are deterministic and need no network.

**Steps** (report §8):
1. Type the pages: Pieces / Bards, skipping meta pages, redirects and the test page.
2. Parse sections with a heading normaliser, and drop boilerplate hints.
3. Take facets from the real categories, classified by `taxonomy.json` rules, and report infobox drift in both directions.
4. Parse credits, including attribution and certainty, and split on `&`, `,`, `;`, `/` and "and", but not inside links.
5. Union the three bard signals: infobox credit, bard-name category, works list.
6. Extract links, with role taken from the section, platform from the host, collection match by host plus path prefix, and the locator. Deduplicate by normalised URL, keeping multiple roles.
7. Parse performance descriptions; the verbatim text is always kept.
8. Turn redirects into aliases and red links into stubs.
9. **Drop inline lyrics** but set `legacy_had_inline_lyrics`; keep version commentary as `version_notes`.
10. Write all imported data as approved revisions with `source = import`, crediting wiki contributors.
11. Write `import-review.md` for the moderators, listing every flagged ambiguity, drift item and fuzzy-matched person.

**Modes:** `--dry-run` (report only), and `--apply`, which only runs against an empty catalogue.

**Tests assert:**
- the research counts: 142 pieces (135/4/2/1 by kind), 48 bards, 62 collection entries, 11 inline-lyrics pages flagged
- the dropped inline lyrics are absent from every public field
- golden snapshots for a hand-picked set of 15 or more tricky pages (Three Words, My Thing is My Own, Fight for the Midrealm, Band of Brothers, Black Alman, The Burning of Tara, Samantha Moore, Lasair inghen ui h’Airt, Auld Lang Syne…)

## 8. Frontend

**Pages:**
- home (mission, non-affiliation disclaimer R-1, entry points)
- A–Z index
- faceted browse: a sidebar on desktop and a drawer on mobile (R-2, R-3)
- search results
- piece page:
  - an infobox with kind, credits, facets, origin and conservation badge
  - links grouped by role
  - performances, with performer, event and year
  - an etiquette card that combines the bard's profile with piece-level overrides (R-9)
  - notes and related pieces
- person page: aliases, affiliations, roles, etiquette card, works, performances, external profiles
- term pages, showing the tree, subcategories and members
- collections directory, sorted ignoring articles (R-24)
- event and venue pages
- history and diff pages
- edit forms for each entity type:
  - structured pickers
  - a credit editor with attribution and certainty
  - a link editor with role, place type, collection and locator, plus performance fields
  - a term picker with a "suggest new term" option that requires a parent and validates the name (R-20)
  - a sourced-field widget that requires a URL
  - live preview using the same components as the read view
  - autosave of drafts
  - a duplicate warning
- "my contributions" and the pending-version banner
- moderation queue, diff and three-way review, approve/reject with a note
- link-report and contact-request inboxes
- repertoire: add/remove on piece pages, and my and public lists
- needs-work dashboard
- contact / takedown / opt-out form, with Turnstile and no account needed
- static help pages adapted from the wiki's meta pages: contributing guide, FAQ for Bards, the Bard Preference Checklist explainer, the conservation guide, and a licence page

**Standards:**
- WCAG 2.2 AA, fully keyboard operable
- every style works in light and dark themes
- works at 360px width
- no layout shift on load
- server-rendered read pages for SEO, with page metadata

## 9. Testing strategy (heavy, at both layers)

**BE:**
- Unit tests for domain and commands, using mockery mocks. The revision engine gets table-driven tests for every state transition, stale/rebase case and sourced-field rule.
- Repository tests against **real MySQL 8**, run in the sandbox if pipeline BE1's spike succeeds (§10). The host always runs them with `make test-integration` (podman).
- HTTP tests for every route, checking authentication, authorisation by role (anonymous, contributor, moderator, API token) and status codes, against the OpenAPI spec using kin-openapi response validation.
- Importer golden tests (§7).
- `make validate` covers lint (golangci pinned via `go tool`), vacuum OpenAPI lint, unit tests and build.

**FE:**
- vitest and testing-library component tests for every component, including both theme classes.
- Route loader and action tests against an **API mock server** driven by OpenAPI-validated fixtures (msw for unit tests; a small fixture server for e2e).
- **Playwright e2e** in demesne's `browser` image (offline) against the built app and the fixture server. It covers every user journey: browse by facets on desktop and mobile, search, read a piece and a bard, log in (stubbed), edit and preview, the duplicate warning, submit, the pending banner, moderator approve, reject and three-way rebase, term suggestion, repertoire, report a link, the contact form, history and revert.
- Each journey is checked with **axe-core** in both light and dark themes, and takes screenshots at 360, 768 and 1280px in both themes. Those screenshots feed the UX review step.
- A contract test generates FE types from the BE's `openapi/api.yaml` and fails if the fixtures don't match the spec.
- `npm run validate` covers format, lint, typecheck, vitest and the Playwright suite.

**Full stack (host, at the end):**
- podman MySQL, BE, FE dev server and Playwright run against the **real imported data**, followed by John and Fabian clicking through it themselves.

## 10. Build sequence

The work runs as **separate pipelines coordinated by the host**, one milestone and one repo each, not as one giant pipeline:
- The two repos only share the OpenAPI contract, so their pipelines can run side by side.
- Landing each milestone on the host gives a checkpoint where integration tests, screenshots and a human look happen before the next layer builds on it.
- A failure stays contained to one milestone.

Each pipeline has the same internal shape:
1. **Explore and plan:** the orchestrator writes the milestone plan. It breaks the work into numbered **chunks**, and each chunk into **sub-chunks**, with a list of acceptance tests per requirement.
2. **Implement:** for each chunk, an implementer child builds each sub-chunk in order, with its tests. Opus handles the core logic: revision engine, importer, faceted query, moderation UI. Sonnet handles the mechanical parts.
3. **Chunk gate:** `make validate` / `npm run validate` must pass before the next chunk starts.
4. **Review:** parallel reviewers each check one aspect:
   - correctness
   - the CLAUDE.md manual quality checks
   - test coverage against the acceptance list
   - security (authentication, authorisation, input handling)
   - for FE milestones, a UX and accessibility review that reads the screenshots
5. **Fix, then a final validate.**
6. **Commit** on a milestone branch, and copy the repo into `/out/repo`, along with `CHANGES.md` and the review results.

**The host then:**
- fetches the branch and fast-forward merges it
- runs `make validate` once more on the host
- runs `make test-integration` for BE milestones
- looks at the screenshots for FE milestones
- reports to John and Fabian before starting the next pipeline

| # | Pipeline | Repo | Depends on | Contents |
|---|---|---|---|---|
| H0 | host setup | — | decisions in §2 | Create both GitHub repos and copy this plan into `bardic-torch/docs/`. Set up the dokku app, MySQL, letsencrypt and the api.bardictorch.org domain. Create the Cloudflare Pages project with bardictorch.org and www. Create the Auth0 application with Google and Facebook connections, plus a Turnstile site key. *The Auth0, Cloudflare and DNS dashboard steps are John's.* |
| BE1 | foundation | BE | H0 | Copy and strip the feed skeleton. Add the users table, roles and moderator seeding, the auth carried over from the feed, the test harness (with the **in-sandbox MySQL spike**), the OpenAPI skeleton, CI, and the dokku files (Dockerfile, `app.json` predeploy migrate step, cron entry). |
| FE1 | foundation | FE | H0 (runs alongside BE1) | Copy and strip the feed FE. Add the wiki-style design system and layout, dark mode, auth, the type-generation pipeline, the fixture server, and the Playwright, axe and screenshot harness. Add the static help pages. |
| BE2 | catalogue and read API | BE | BE1 | Every §4 catalogue table and migration, and every public read endpoint in §6, including facet counts, FULLTEXT search and A–Z. The revisions table exists but only stores import data. |
| BE3 | importer | BE | BE2 | Everything in §7. A dry-run report is produced for John and Fabian to read. |
| FE2 | read UI | FE | BE2 spec (runs alongside BE3) | Every read page in §8, the etiquette card and conservation badge, the mobile facet drawer, and SEO. |
| BE4 | revisions and moderation | BE | BE3 | Everything in §5, plus the moderation, link-report, contact (Turnstile) and needs-work endpoints. |
| FE3 | editing | FE | BE4 spec | Edit forms for every entity type, the credit, link, term and sourced-field editors, preview, drafts, the duplicate warning, the pending banner, and history, diff and revert. |
| BE5 | contributor extras | BE | BE4 | Repertoire, preference profiles and their validation, conservation suggestions, the `check-links` cron with Wayback lookup, API-token MCP tools, and rate limits. |
| FE4 | moderation and extras UI | FE | BE5 spec, FE3 | The moderation queue, diff and three-way review, the inboxes, repertoire, the preference-profile editor, the needs-work dashboard, and the contact form. |
| H1 | launch | host | all | Full-stack e2e against the real import, a staging deploy, the real import run with its review report worked through, the click-through by John and Fabian, then production. |

**Concurrency:** at most two pipelines run at once, one per repo.

**Order:** H0 → (BE1 ∥ FE1) → BE2 → (BE3 ∥ FE2) → BE4 → (BE5 ∥ FE3) → FE4 → H1.
