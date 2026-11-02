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
  - Packages: transport/web (net/http `ServeMux`) → command → domain → datasources/mysql (sqlc), plus migrations (golang-migrate).
  - Four entrypoints:
    - `cmd/app`: the API server
    - `cmd/migrate`: applies the embedded migrations; dokku runs `migrate up` as the predeploy step
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
- **`terms`**: `facet` (mood, topic, culture, language, legendaria, form, origin, other, affiliation), `name`, `display_name` (without the "(Topic)"/"(Language)" disambiguation suffix), `slug`, `parent_id` (depth ≤ 3), `related_affiliation_id` (a `Patriotic (Kingdom)` topic's kingdom, §2.12), `description_md`, `position`. Kinds are not terms (§4.1).
  - Child tables: `term_aliases`, and a `term_ancestors` closure table rebuilt transactionally when a term revision is approved.
- **`collections`**: `name`, `sort_name`, `url`, `kind`, `description_md`, `region_term_id`, `parent_id`, `is_bardic_resource`, `enumerable`.
- **`links`**: `url`, `normalized_url` (unique; tracking parameters stripped), `title`, `description`, `platform`, `place_type`, `collection_id`, `locator`, `liveness` (unknown / ok / dead / blocked), `last_checked_at`, `archived_url`, `is_hidden` (moderator takedown, R-11).
  - `link_attachments(link_id, piece_id | person_id, role, position)`. The roles are official, lyrics, text, sheet_music, dance_instructions, text_source, translation, performance, licensing_source, website, external_profile, other. One link can carry several roles (R-21, R-14).
  - `performances(link_id, piece_id, event_id, year, uploader, description_verbatim)`, plus `performance_performers(person_id)`.
- **`events`**: name, kind (venue / event), description. Seeded with Green Dragon and with events parsed from performance descriptions.

**Workflow tables:**
- **`revisions`**: `id`, `entity_type`, `entity_id`, `base_revision_id`, `author_user_id`, `source` (user / import / mcp), `status` (draft / pending / approved / rejected / withdrawn), `snapshot_json`, `summary`, `reviewer_user_id`, `reviewed_at`, `review_note`, `created_at`.
  - The snapshot is the full document, including owned child rows. Links appear inline by URL as entered (the normalized URL is derived; §4.1).
- **`users`**: Auth0 user ID, `display_name`, `role` (contributor / moderator), `created_at`, `blocked_at`. Moderators are not seeded by migration, and no user IDs live in migrations. At startup the API promotes the users listed in `BOOTSTRAP_MODERATOR_USER_IDS`, creating any who have not signed in yet. Promotion also unblocks a blocked user: listing an ID trusts that user, and moderators cannot be blocked or unblocked. This is promote-only and idempotent: removing an ID never demotes the user, and demotion is done in SQL until a moderation endpoint exists.
- **`repertoire`**: user_id, piece_id, notes, visibility (private / public), added_at.
- **`link_reports`**: for removal requests (R-11).
- **`contact_requests`**: kind (takedown / opt_out / correction / general), name, email, message, url, status. These need no account and are protected by Cloudflare Turnstile (R-13).
- **`api_tokens`**: kept from the feed.

### 4.1 Catalogue decisions refined in BE2

BE2 built the catalogue schema (migrations 003–009) and the public read API. These decisions refine §4 and §6;
the DDL is in `migrations/003`–`009`.

- **Visibility is approval.** An entity is public exactly when `current_revision_id IS NOT NULL`; a term also needs
  every ancestor approved. The `terms.status` column was dropped as redundant: a pending suggestion is a term with
  no approved revision (BE4 deletes rejected new-entity rows, or adds a column). References from a public entity to
  a non-public one are omitted from every read, and an unknown slug and a non-public slug answer identically.
  The shared rules are defined once, as views (migration 009) that queries join instead of restating them:
  `public_terms` (approved, with every ancestor approved), `listed_persons` (public persons with a page) and
  `public_link_attachments` (attachments to a public piece or a public person with a page).
- **Kinds are an enum** (`pieces.kind` plus `piece_secondary_kinds`), not terms, so `terms.facet` has no `kind`. The
  `kind` filter matches the primary or a secondary kind. The list grows by appending ENUM values (an instant ALTER).
  **Enum growth rule:** a new value ships in code one release before any data uses it, so a rollback never reads a
  value it does not know.
- **Disputed is a certainty**, not an attribution (§2.4's wording). The database checks that a credit has a person
  exactly when `attribution = named`.
- **Hidden aliases** (`person_aliases.is_public = false`, the default, so a forgotten value fails closed) never
  appear in a public read, a search match or score, a snapshot or a diff. Alias search matches score 0, as the alias
  FULLTEXT index's statistics include hidden aliases. A snapshot's person `name` is shown only if it is the
  person's current name or one of their current public alias names (compared exactly), and is their current name
  otherwise, so a former name kept as a hidden alias never shows. BE2 cannot scrub free text, so the
  importer (BE3) hides `redirect` aliases equal to a hidden alias, withholds every hidden alias and other private
  name from the free text it writes (`[name withheld]`, §7), imports a link whose URL holds a legal name hidden,
  never writes a placeholder into a name or title, and lists each in `import-review.md`.
- **`prefers_no_page` persons** show only their identity (name, kind, status, date and source; `profile: null`).
  They are listed as stubs and found only by name. They are excluded from role/affiliation filters, every person
  count and member list, every credit (the credit is omitted), performer list, relation list (both directions),
  collection citation and needs-work.
- **Hidden links** (`links.is_hidden`) are excluded from every read, including snapshots and needs-work. In a
  snapshot, a link is shown only if its normalized URL (`domain.NormalizeURL`) is now the `normalized_url` of a
  link that is not hidden, and then under that link's `url`, not the document's spelling of it. Every link's `normalized_url`
  is `NormalizeURL(url)` (DEPLOY.md, "The normalized URL invariant"), so a link edited since to another spelling
  of the same URL (case of the host, a default port, tracking parameters) is still the same link: shown if it is
  visible, dropped if it was hidden. A link deleted since, or given a URL that normalizes otherwise, is dropped,
  and so is a document URL that does not normalize (fail closed). Normalized URLs are ASCII, so a non-ASCII
  document URL is compared by its percent-encoding and never reaches the ASCII column as it is (which MySQL
  refuses, error 3988); the column is binary (`ascii_bin`), so the comparison is exact.
- **Slugs** are ASCII `[a-z0-9]+(-[a-z0-9]+)*`, at most 191 characters and unique per table. Term slugs keep the
  wiki's disambiguation suffix (`12th-century` in origin, `12th-century-topic` in topic), while `display_name`
  drops it. The importer's `domain.Slugify` uses NFKD, strips marks, transliterates the letters NFKD leaves whole
  (`domain.FoldLetters`: ł→l, ø→o, đ/ð→d, æ→ae, œ→oe, ß→ss, þ→th, so `Astrið` is `astrid`; the same table gives
  `sort_letter`), lowercases and turns non-alphanumeric runs into `-`, cutting at 191 characters on a hyphen. An
  empty result becomes `<entity type>-<id>`, and collisions get `-2`, `-3`… Collections are addressed by ID.
- **Sort keys and letters are stored and written by the writer** (`sort_title`/`sort_name`, `sort_letter`), never
  derived in SQL.
  - The sort key skips leading punctuation and one leading "The", "A" or "An". R-24's "songbook/book" words are not
    stripped.
  - `sort_letter` is `A`–`Z` or `0`: the first letter of the sort key after NFKD and mark stripping, with Ł→L, Ø→O,
    Đ/Ð→D, Æ→A, Œ→O, ß→S, Þ→T and ﬁ→F. Anything else (digits, punctuation, other scripts) is `0`.
  - Persons sort by name as written.
- **Search** uses InnoDB FULLTEXT in boolean mode with the server's default stopwords. The API tokenises `q` itself:
  - it splits on characters other than letters, digits, underscores and combining marks (as InnoDB's parser
    does), removes combining marks, and drops tokens with no letter or digit, tokens shorter than 3 or longer
    than 84 characters (`innodb_ft_min_token_size`, `innodb_ft_max_token_size`) and InnoDB stopwords;
  - it sends `+token*` for at most 8 tokens, so user operators never reach MySQL;
  - if no token survives, it falls back to an accent-insensitive title/alias prefix match.

  FULLTEXT sees only committed rows, so BE4 searches after its approval transaction commits.

  Known ranking effect: relevance scores from the person name and the piece title, notes and alias indexes
  depend on index-wide statistics. Those indexes also hold entities that are not public (unapproved drafts), so
  draft words shift public results' scores and order, though never which results match. Scoring these indexes 0,
  as is done for person aliases, would remove relevance ranking entirely, and InnoDB cannot narrow a FULLTEXT
  index's statistics to some of its rows, so this is accepted. It reveals at most that words occur in drafts,
  not hidden names. Revisit it if BE4 creates drafts in volume.
- **Snapshots** hold only editor-owned fields. Left out:
  - IDs, `current_revision_id`, `legacy_*`, `claimed_by_user_id` and sort keys;
  - `conservation_history`, which is appended on approval from the document's `conservation {status,
    source_url}`;
  - link system and moderator state (`liveness`, `last_checked_at`, `archived_url`, `is_hidden`,
    `normalized_url`).

  So approving an old snapshot cannot un-hide a link or undo a liveness check. Links are inline by URL, documents
  own only their outgoing relations, and other entities are referenced by ID. A list's order is its rows'
  `position`. Piece and person documents already include their preference profiles (`dimension`, raw JSON
  `value`, `source_url`), so BE5 needs no schema-version bump; they stay unexposed until then. A coverage test fails
  when a column is added without being classified as document-owned or system-owned.
- **Public revision views** never serve stored documents raw:
  - references are resolved to public summaries, or omitted;
  - redaction applies the *current* alias, person and link visibility, so hiding an alias later also hides it in
    old revisions;
  - `changed_fields` (the diff) is computed on the redacted views.

  Only approved revisions are public until BE4. Authors show only a display name, and imports list the wiki
  contributors (`revision_contributors`).

  In detail (`domain.New*Snapshot`, given a `SnapshotVisibility` the repository loads for exactly what the
  snapshot and its base reference):
  - A revision is public if it is approved and its entity is public now; anything else is the same 404. History
    lists approved revisions newest first (`created_at`, then ID).
  - Views hold the document's editor-owned fields except preference profiles; links have no liveness or other
    system state. A link is shown only if its normalized URL is now that of a link that is not hidden ("Hidden
    links" above).
  - An alias is shown only if public in the document and now a public alias of the person, by name. A person's
    name is shown only if it is their current name or a current public alias name, and is their current name
    otherwise. Credits,
    performers and relations of persons not public with a page now are dropped (a named credit entirely), as is a
    person's performance of a piece that is not public.
  - A person who prefers no page now, or did in the document, is shown by identity only (`profile: null`), in the
    snapshot and the base.
  - The base is shown only if it is an approved revision of the same entity; otherwise `base_revision_id` and
    `base_snapshot` are both null. `changed_fields` lists the sorted top-level view fields whose encodings
    differ, or with no base those that are not empty (null, `[]`, `""`, false, 0); a person's profile is one field.
  - A snapshot that does not decode (unknown `schema_version`, wrong shape, a status date that is not a date, a
    vocabulary value this release does not know) is a 500, logged. Fields the document type does not have are
    ignored, so a rollback after a release adds a document field keeps serving the revisions written in between:
    a field is added without a new `schema_version`, and any other change to a document's shape needs one (see
    DEPLOY.md). A field that hides or redacts other document data (such as a per-link or per-alias privacy flag)
    is such a change, not an additive field: an older release would ignore it and show what it hides, so it needs
    a new `schema_version`, which older releases refuse.
- **Needs-work** (`GET /v1/needs-work`) lists public pieces and persons with a page that are stubs, unassessed
  (pieces), of unknown licensing (pieces, and community bards) or with a visible dead link; restricted pieces only
  for being stubs. Pieces come first, then persons, each by sort key. `counts` has every reason, whatever the
  `reason` filter.
- **Preference profiles** (`person_preferences`, `piece_preferences`) exist as tables but are not exposed until BE5
  defines and validates their `value_json` shapes. Until then the only etiquette data in the API is
  `licensing_status` and `licensing_notes_md` on pieces and person profiles, so FE2 builds a licensing card from
  those, and the full etiquette card (profile with piece-level overrides) waits for FE4 (§10).
- **Public reads ignore credentials.** The catalogue reads are served outside the authentication middleware, like
  the health checks: invalid, expired or uncheckable credentials and blocked users get the same answer as an
  anonymous request, and no user is looked up or created. A person's page lists the performances recorded by
  links attached to them, and those they perform in on links with a public attachment (as on an event's page);
  a performance of a piece that is not public is left out.
- **Terms, collections and events.** A term's `piece_count`/`person_count` count distinct public pieces, and
  public persons with a page, tagged with or affiliated with the term or any public descendant; a term under a
  pending parent is hidden with its subtree, from its ancestors' counts too. The term list is in tree order
  (facets in enum order, parents first, siblings by position and display name). A term page pages its members
  with `limit`/`offset`: pieces, or persons for an `affiliation` term. A collection's citations are one per
  visible link and public piece or person (not no-page) it is attached to, with that attachment's roles. An
  event's performances leave out those of non-public pieces, as on a person's page, and those whose link is
  attached to no public piece and no public person with a page, since only a hidden entity's document records
  them. Collection citations and event performances are capped at 200 with totals; collection children are not
  capped. Every query parameter is single-valued unless documented as repeatable: a repeated one, or a query
  string that is not validly percent-encoded, is a 400.
- **Faceted search and A–Z lists** (`GET /v1/pieces`, `/v1/persons`, `/v1/collections`).
  - Piece `term`s are grouped by facet: OR within a facet, AND across facets, each term matching its descendants.
    `kind` and `conservation_status` are two more facets. At most 20 terms; a repeated term counts once. A term
    that is unknown, unapproved, under an unapproved ancestor or an affiliation gives one 400, `unknown term:
    <slug>`, echoing only the client's slug. `affiliation` on persons works the same way (`unknown affiliation`).
  - Facet counts are disjunctive: a value's count ignores the selection in its own facet. Each piece counts once
    per term, even when tagged with the term and its descendant. Only counts above zero are listed. Letter counts
    ignore the `letter` filter, and the totals and counts are over every page.
  - The statements are built in Go from fixed fragments and `?` placeholders, since sqlc cannot express optional
    filters. A piece search makes at most three round trips (term resolution, the page, every count in one
    statement), and every statement carries `MAX_EXECUTION_TIME(2000)`. A search the server stops answers 500 and is
    logged as a warning.
  - Every connection also sets the session `max_execution_time` to 2000 (`ConfigFromURL`), so every `SELECT`, sqlc
    reads included, is stopped after 2 s; writes and DDL are not, and the migrator clears it for golang-migrate's
    `GET_LOCK` wait. The hint stays on builder statements, built from the same constant, so searches stay capped
    on a connection that lifts the session limit.
  - `q` is at most 200 code points of valid UTF-8 and must not be blank: only ASCII spaces, tabs and line breaks
    are blank, which the spec's pattern `[^ \t\n\f\r]` states exactly. Prefix matches have no score, so with
    `sort=relevance` they come in sort-key order. `relevance` is the default with `q` and a 400 without it.
- **Response conventions.**
  - Every scalar property is required, `type: [X, "null"]` when it can be empty, and arrays are required and
    possibly empty; every object is closed.
  - Never exposed: `legacy_*` columns, `claimed_by_user_id`, `normalized_url`, internal person, term and piece IDs
    (collections and revisions are addressed by ID by design), user IDs, and preference profiles until BE5.
  - Unknown path slugs and non-public slugs are one 404. A malformed slug is a 400.
  - An unknown ENUM value read from the database or in a stored document, a snapshot with an unsupported
    `schema_version` and a read the server stops after 2 s are 500s, logged, never panics.
  - Piece facet counts list only values above zero; needs-work `counts` always list all four reasons, zeros
    included.
- **Status sources:** "a status other than active needs a source" is domain validation (BE4), not a CHECK. The
  importer uses the legacy wiki page URL as the source. `conservation_history` has a `source_url`.

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

**Roles and blocking:** every user starts as a contributor. The moderator role is granted only through `BOOTSTRAP_MODERATOR_USER_IDS` (§4). Moderators block and unblock contributors, but not other moderators or themselves. A blocked user gets 403 on every authenticated endpoint.
- **Duplicate check on create:** normalised titles, names and aliases are compared, with apostrophes and case folded, Unicode NFC applied and accents stripped for matching only (R-17).

**Revert** creates a new revision copying an older snapshot.

**History:** every entity has a public history list, and each revision a diff view. Wiki contributors are credited on imported revisions.

## 6. API surface (all specified in OpenAPI; the FE never calls the API directly from the browser)

- **Public reads** (built in BE2; `openapi/api.yaml` is the exact contract):
  - `GET /v1/pieces`: faceted search.
    - Filters: `term` (repeatable slugs), `kind`, `conservation_status`, `stub`, `q`, `letter` (`A`–`Z` or `0`), `sort` (`title` / `relevance`), `limit` and `offset`.
    - Terms combine with OR within a facet and AND across facets, and a term matches its descendants (closure table). `kind` and `conservation_status` act as two more facets.
    - `q` is FULLTEXT over title, aliases and notes (§4.1).
    - Facet counts are disjunctive: each facet's counts ignore that facet's own selection. Letter counts are returned for the A–Z bar.
  - `GET /v1/pieces/{slug}`
  - `GET /v1/persons` (`kind`, `role`, `affiliation`, `q`, `letter`, `sort`) and `GET /v1/persons/{slug}`
  - `GET /v1/terms?facet=…`: the term tree with piece and person counts
  - `GET /v1/terms/{slug}`, with its pieces or persons (paginated, including descendants)
  - `GET /v1/collections`, `GET /v1/collections/{collection_id}`
  - `GET /v1/events` (added in BE2) and `GET /v1/events/{slug}`
  - history:
    - `GET /v1/{pieces|persons|terms|events}/{slug}/history` and `GET /v1/collections/{collection_id}/history`
    - `GET /v1/revisions/{revision_id}`: the redacted snapshot view, its base and the changed fields
  - `GET /v1/needs-work`: stubs, unassessed conservation status, missing licensing information, dead links (R-18). Restricted pieces appear only as stubs, and `prefers_no_page` persons never appear.
  - Lists page with `limit` (at most 100) and `offset` (at most 10000), and every ordering ends with the ID, so pages are stable. Nested lists that grow with data are capped at 200 entries, each with a `*_total` field.
- **Authenticated:**
  - `GET /v1/me`, and `PATCH /v1/me` to set the display name
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
  - `PUT /v1/users/{id}/blocked`: block or unblock a contributor. Until the BE4 moderation endpoints show author IDs, moderators take IDs from Auth0 or the database.
- **No account needed:** `POST /v1/contact`, verified with Turnstile.
- **Health checks** (no authentication): `GET /healthz` (liveness) and `GET /readyz` (the database answers; dokku's startup check).
- **Kept from the feed:** `/v1/tokens`.

## 7. Importer (`cmd/import-mediawiki`)

**Input:** the research dump format (`pages.jsonl`, `categories.json`, `redirects.json` and `activity.json`). Reading a live crawl of the wiki directly is deferred to a later milestone: the research `scripts/crawl.mjs` produces a dump, which is the input. The research `scripts/` define the expected behaviour and are ported to Go. A **scrubbed** copy of the dump is committed under `testdata/wiki-dump/` (regenerated with `cmd/scrub-dump`), so tests are deterministic and need no network: legal and mundane names (the aliases the importer classifies as legal, every other possible person name it cannot classify confidently as public, and their honorific forms; the candidates are H1 and "Name (Name)" parentheticals, the legal/mundane/real-name and "aka" markers, uploaders, and in prose and link titles the names after "by", "from", "credited to", "attributed to" or "copyright", either side of "/", "and" or "&", before a possessive such as "'s YouTube" or "'s blog" or a verb such as "singing", in an "also known as" list, and a link label its website's host spells; a name paired by "/" with a person's public name is a legal name), emails, phones and wiki user names are replaced with numbered placeholders, the same number everywhere a name occurs (wikitext, titles, categories, redirects, activity); inline lyrics with placeholder lines (keeping the ≥4-line classification); and only the namespaces, files and fields the importer reads are kept. Every count the tests assert is preserved. No name is hashed into the repository: `testdata/wiki-dump/denylist.txt` holds only salted hashes of the dropped lyrics' 8-word n-grams, and a unit test fails if any appears anywhere in the repository. The names, emails and phones are checked in clear, against the whole repository, by tests that run only when `BARDIC_RESEARCH_DIR` points at the research output, which is never committed; so do the tests against the real dump and the research oracle (`analysis/*.json`).

**Steps** (report §8):
1. Type the pages: Pieces / Bards, skipping meta pages, redirects and the test page.
2. Parse sections with a heading normaliser, and drop boilerplate hints.
3. Take facets from the real categories, classified by `taxonomy.json` rules, and report infobox drift in both directions.
4. Parse credits, including attribution and certainty, and split on `&`, `,`, `;`, `/` and "and", but not inside links.
5. Union the three bard signals: infobox credit, bard-name category, works list. A category or works list naming
   a person the infobox does not credit adds a `lyrics` credit of certainty `possible`, listed in the review, unless
   the person is related (heir_of, same_as) to a credited person; listed works without a page become stub pieces
   credited to the bard (certainty `probable`: it is the bard's own list), whose revision credits the listing bard
   pages' contributors. A part qualifier scopes a credit or a term to a part; "(chorus tune)" is the chorus's.
   Plain-text names are matched to persons by folded name (apostrophes, case, accents), then
   fuzzily (one edit, or a unique first word), and listed.
6. Extract links, with role taken from the section (official for a key link on a credited person's website), platform
   from the host, and the locator ("p. 63", "page 63", "#page=63"). A link belongs to a collection of the
   Collections page with its exact normalised URL (a fragment aside), else, for website, songbook website, wiki and
   blog collections not on YouTube, Google Docs, Scribd or Yumpu, the one whose path prefix (at least one segment) is
   longest; collections tying match none and are listed. A link's description is the text of its line around it
   (not for a performance item, whose text is the performance's), where a link after some text reads as its text
   (or, without text, its host), so no hole is left where it was; a link starting the line is left out. Consecutive
   link lines (blank lines aside) whose text around the links reads as at least 4 lines (a `<br>` breaks a line)
   could be lyrics: their links are kept without a description, and the run is listed. Deduplicate by normalised
   URL, keeping multiple roles. A `links` row holds one set of fields for every document attaching it (§4.1;
   roles are the attachment's), so no page's own words reach another's: the fields are the first document's (pieces
   by page ID before persons),
   but the title, description and locator are kept only when every document gives the same; otherwise the title is
   the link's collection's name, or none, and the description and locator none. A PDF that pages cite at different
   pages is first given each page's open parameter (`…pdf#page=14`), so each page is a link of its own with its own
   locator. Every difference is listed with the page's own fields (per-attachment titles and locators are
   deferred). Infobox credits naming a person by an external link attach it to the person
   (external_profile). The Collections page's duplicate URL is merged into one collection and listed.
7. Parse performance descriptions; the verbatim text is always kept (the import redacts hidden names in it).
   Performers ("of [[X]]") are matched as persons, or become stubs; events come from "at <event>", one per folded
   name, the venue pages included, with similar names listed, never merged. A page link to a work (a piece page or a
   works-list red link) is never a performer: it is the performance's piece when the wording names none ("her song
   [[W]]"). Performances are deduplicated by URL across pages (report §8.4): a bard-page item on a link a piece page
   records a performance on is not imported again, and one whose piece has a page is recorded once, on the piece's
   link; the bard keeps the link with role performance. Otherwise the bard's performance names its piece when that
   is a stub piece.
8. Turn redirects into aliases and red links into stubs.
9. **Drop inline lyrics** but set `legacy_had_inline_lyrics`; keep version commentary as `version_notes`. Lines
   joined by `<br>` on one line count as that many lines.
10. Write all imported data as approved revisions with `source = import`, crediting wiki contributors.
11. Write `import-review.md` for the moderators (John and Fabian), listing every flagged ambiguity, alias
    classification, withheld name and email, drift item and fuzzy-matched person, each with a stable ID
    (`ALIAS-007`). It is private: it names hidden aliases in clear, so it is written outside the repository only,
    and it never holds lyrics (only their page, section and line count).

**Writing into the BE2 schema** (§4.1):
- Write sort keys and `sort_letter` (`Validate` checks each letter is its key's). Build the `term_ancestors`
  closure from the terms' parents (`CatalogueImport.TermAncestors`), including each term's row for itself.
- Source a non-active person status to the link its date is read from (an obituary on the "Deceased <date>" line),
  unless that link is imported hidden; otherwise to the legacy page URL. The conservation source is the piece's
  legacy page URL (the wiki gives no other).
- Keep the prose of the link sections (Key Links, Sheet Music, Dancing Instructions, Translations), but their links,
  in the notes under the section's heading, and list it; a section with as many prose lines as inline lyrics (4,
  each `<br>`-separated segment of a line counting as a line) could hold lyrics, so its prose is never kept, only
  listed.
- Guess a stub person `historical` when it is credited on a piece of a traditional or pre-1900 origin, except for
  the words of a piece that is also of the SCA (SCA Pieces, Contrafacta): new words to an old tune are modern. List
  every guess, and flag the stubs credited by an external link (historical, or a group).
- Hide legal aliases, any `redirect` alias equal to a hidden alias, and any public alias holding a hidden alias.
- Withhold private names by confidence. **Legal names** (hidden aliases, `mediawiki.PrivateNames` marked legal:
  names after a marker or paired by "/" with a public name; and every placeholder of the scrubbed copy), honorific
  names ("Mr. Surname") and email addresses are withheld from every public field. **Unsure names** (the other
  private names: possible names the classifier cannot show are public) are withheld from free text only: notes,
  version, licensing and historical notes, credit notes, link titles and descriptions, performance descriptions
  and uploaders, term, collection and event descriptions; except that an unsure name the import publishes as a
  person's name (a bard's page title, or a stub person it creates) is kept in free text too, where withholding it
  would protect nothing, and each occurrence is listed. Each withheld occurrence (legal names in every
  `PrivateNameMatcher` form: honorific, reversed, URL forms; a name inside a wikilink takes the whole link) becomes
  `[name withheld]`, each email address `[email withheld]`. This is the one exception to keeping a performance
  description as written (§2.14), because an approved import revision cannot be redacted later.
- Names and titles never hold a placeholder (`Validate` rejects one in any name, title, alias, slug, sort key or
  uploader). A name the wiki itself gives an entity (a credited person, a piece title, alias or subtitle, a
  collection label, an event) is imported as written when it holds only an unsure name, and listed. When it holds a
  legal name, the entity is left out and listed: a stub person is not created (its credits are imported as unknown
  with the note `[name withheld]`; a bard's hidden alias already finds the bard), nor is a stub piece, a
  collection (its links are imported without it; also when its URL holds a legal name), an event (its
  performances are imported without it) or a piece or term alias; a withheld uploader is left out.
- A link whose URL holds a legal name is imported hidden (`links.is_hidden`); a URL holding an unsure name or a
  home directory (`/~user`) is listed. URLs are never rewritten to withhold a name. Link URLs, and the URLs in
  performance descriptions, are stored without the tracking parameters `NormalizeURL` drops (`si` share tokens can
  tie a link to its sharer; `domain.WithoutTrackingParameters`) and absolute (a "www." or "//" URL gets https,
  `domain.WithScheme`, so it is never a relative link), so the stored URL still normalizes to its
  `normalized_url`. Every withheld span, left-out entity, hidden link
  and listed URL is an item of `import-review.md`, so moderators can restore what may be public. Wiki user names
  credited as revision contributors are listed, flagged when one matches a private name or is shaped like a
  personal name ("Firstname Lastname"). No licence is inferred from prose ("public domain" is a review item).
- Normalise every imported string to NFC; list names that `domain.CheckTextCharacters` rejects. Drop control
  characters (C0 and C1 controls but tab and line feed, such as NUL and ESC, and the bidirectional embeddings,
  overrides and isolates U+202A–U+202E and U+2066–U+2069) from every imported string, and list each field that had
  them; in a URL they are percent-encoded instead, so it normalizes as before. `Validate` rejects any left.
- Convert wikitext to Markdown with no syntax but the converted one: text is escaped (a "!" before a converted
  link too, so it never becomes an image, and no raw HTML), and links point only to absolute http(s) URLs (a
  "www." or "//" URL gets https) without tracking parameters. The parsers are linear on hostile input (an
  unclosed "[http://" before many tokens, one very long word): pages are capped at 2 MiB, and tests hold each
  parser to a time budget at that size.
- Check the whole import before writing (`CatalogueImport.Validate`): it mirrors every constraint of the schema
  (unique keys as the collation compares them, primary keys, column widths, CHECKs, references and insert order),
  with error messages that name the entity, ID and field, never a value.
- Assign every ID in the import (1..N per table, revisions 1..M), so that documents reference final IDs and the
  dry run shows exactly what is written. This is safe only because `--apply` writes into an empty catalogue.
- Write in one transaction: first take the advisory lock `GET_LOCK('bardic-torch-import:<database>', 0)` (another
  import holding it fails with `ErrImportInProgress`), then, in the transaction, a locking read
  (`SELECT 1 … LIMIT 1 FOR UPDATE`) of every catalogue table (any row fails with `ErrCatalogueNotEmpty`), then every
  row in foreign-key order, all rows of a table before any table referring to it: revisions and their
  contributors, terms (parents and related affiliations first), aliases and closure, collections (parents
  first), events, persons and their child tables, pieces and their child tables, conservation history (one row
  per assessed piece, at its revision), links, link attachments, performances and performers. Each entity row
  carries its revision as `current_revision_id`. Any error rolls everything back, and a refused row is reported
  by step (the table, the lock, the commit) and MySQL error number only, never with the server's message, which
  quotes values; a malformed `DATABASE_URL` is reported without the URL, which holds the password.
- Write documents with editor-owned fields only. Write the link system state directly: `is_hidden` for a link whose
  URL holds a private name, and liveness left at the column default (`unknown`; the wiki records none).
- Write revision `summary` text from a fixed set of neutral templates ("Imported from the wiki (page revision N).",
  "… as a stub.", "… Collections page.", "… categories.", "… performance listings."): a summary is free text shown on
  every approved revision, including those of persons without a page, and nothing checks it against hidden
  aliases. BE4 should flag editor summaries that contain a hidden alias.
- Write every link's `normalized_url` as `domain.NormalizeURL(url)`, and drop (listing it in `import-review.md`) a
  URL that does not normalize. Hidden-link redaction relies on this invariant (§4.1 "Hidden links"; DEPLOY.md
  "The normalized URL invariant"): it normalises each document URL and looks it up by `normalized_url` only, so
  a link later edited to another spelling of the same URL stays redacted, or shown, in older snapshots.

**Modes:** `import-mediawiki --dump DIR (--dry-run | --apply) --review PATH`. Both modes build and validate the
import and write `import-review.md` to `PATH`, which is required, refused inside the repository and written with
mode 0600, then print counts only. `--dry-run` stops there: it never reads `DATABASE_URL` or opens a database.
The review path is checked with its symbolic links resolved (a link to a file in the repository is refused). The
resolved directory is opened once and checked again (it must still resolve to itself, outside the repository); an
existing file there that is not regular or has other hard links is refused, and the review is written to a new
temporary file in that directory, which then replaces the path, so no existing file is ever written through. `--apply` writes the review before it opens the transaction, then
writes the import into the empty catalogue at
`DATABASE_URL`. The launch run is in DEPLOY.md ("Importing the legacy wiki"): a dry run, the moderators' review,
then `--apply` once against the empty production database.

**Tests assert:**
- the research counts: 142 pieces (135/4/2/1 by kind), 48 bards, 62 collection entries (61 collections, written or withheld: the scrubbed copy's placeholders count as legal names, so 4 of its collections are withheld), 11 inline-lyrics pages flagged
- on the scrubbed copy (always): unit tests, the lyric n-gram scan and the structural privacy checks; against the research output (only with `BARDIC_RESEARCH_DIR`): the oracle, that the copy is up to date and keeps the research counts, the in-clear scan for names, emails and phones, an independent name oracle (every run of two or more capitalised words of the crawl's prose and link titles is a title, a reviewed non-name phrase, or a name the classifier decided on), that no legal name reaches any public field of the real import and no private name its free text, an independent lyrics oracle (every `<poem>` element and every run of four or more prose lines of the sections the research analysis lists) absent from the real import and the scrubbed copy, and a real dry run
- the dropped inline lyrics are absent from every public field
- golden snapshots of 16 tricky pages: Three Words, My Thing is My Own, Fight for the Midrealm, Band of Brothers, Black Alman, The Burning of Tara, Samantha Moore, Lasair inghen ui h’Airt, Auld Lang Syne, Malkin Grey, Dahrien Cordell, Greensleeves, Greensleeves (SCA Version), Voice of the Dragon, Birthday Dirge and Warrior's Wyrd, and of the review
- the writer (integration): every row written, every table empty after a failure planted in the last table, a catalogue that is not empty refused, exactly one of two concurrent imports succeeding, the normalised URL invariant and decodable snapshots
- the imported catalogue end to end (`TestHTTP_ImportedCatalogue`): every page of every list, every entity, history and revision read through the API is valid by the spec and shows no lyric placeholder, no legal-name, email or phone placeholder in any form and no hidden alias

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
  - an etiquette card that combines the bard's profile with piece-level overrides (R-9); FE2 shows a licensing card (status and sourced notes) until BE5 exposes profiles (§4.1)
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
- Repository tests against **real MySQL 8.4**. BE1's spike succeeded, so they run in the sandbox (`make test-integration-local`, a conda-forge mysqld), on the host (`make test-integration`, podman) and in CI (`make test-integration-external` against a `mysql:8.4` service, under `-race`).
- HTTP tests for every route, checking authentication, authorisation by role (anonymous, contributor, moderator, API token) and status codes, against the OpenAPI spec using kin-openapi response validation.
- Catalogue reads: exact-struct repository tests on shared fixtures built to tell the behaviours apart, an HTTP leak matrix that proves no hidden string reaches any public response, and parity tests between the database ENUMs, the domain vocabularies and the OpenAPI enum schemas (docs/TESTING.md).
- Importer golden tests (§7).
- `make validate` covers the format check, lint (golangci pinned via `go tool`), vacuum OpenAPI lint, unit tests, build and the generated-code check (sqlc and mockery output up to date). docs/TESTING.md describes every target.
- `make vulncheck` (govulncheck, pinned via `go tool`) runs as its own CI job on every push and pull request. It is outside `make validate` because it needs vuln.go.dev.

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
| BE1 | foundation | BE | H0 | Copy and strip the feed skeleton. Add the users table, roles and the moderator bootstrap, the auth carried over from the feed, the test harness (with the **in-sandbox MySQL spike**), the OpenAPI skeleton, CI, and the dokku files (Dockerfile, `app.json` predeploy migrate step). The cron entry lands with `cmd/check-links` in BE5. |
| FE1 | foundation | FE | H0 (runs alongside BE1) | Copy and strip the feed FE. Add the wiki-style design system and layout, dark mode, auth, the type-generation pipeline, the fixture server, and the Playwright, axe and screenshot harness. Add the static help pages. |
| BE2 | catalogue and read API | BE | BE1 | Every §4 catalogue table and migration, and every public read endpoint in §6, including facet counts, FULLTEXT search, A–Z and needs-work. The revisions table exists but only stores import data. Decisions refined in BE2 are in §4.1. |
| BE3 | importer | BE | BE2 | Everything in §7 (reading a live crawl directly is deferred). A dry-run report is produced for John and Fabian to read. |
| FE2 | read UI | FE | BE2 spec (runs alongside BE3) | Every read page in §8 (including the read-only history and diff pages), the licensing card (licensing status and sourced notes) and conservation badge, the mobile facet drawer, and SEO. |
| BE4 | revisions and moderation | BE | BE3 | Everything in §5, plus the moderation, link-report and contact (Turnstile) endpoints. (needs-work, a public read, moved to BE2.) |
| FE3 | editing | FE | BE4 spec | Edit forms for every entity type, the credit, link, term and sourced-field editors, preview, drafts, the duplicate warning, the pending banner, revert, and the pending-version diff. (The read-only history and diff pages are FE2's.) |
| BE5 | contributor extras | BE | BE4 | Repertoire, preference profiles and their validation, conservation suggestions, the `check-links` cron with Wayback lookup, API-token MCP tools, and rate limits. |
| FE4 | moderation and extras UI | FE | BE5 spec, FE3 | The moderation queue, diff and three-way review, the inboxes, repertoire, the etiquette card (preference profiles with piece-level overrides, once BE5 exposes them), the preference-profile editor, the needs-work dashboard, and the contact form. |
| H1 | launch | host | all | Full-stack e2e against the real import, a staging deploy, the real import run with its review report worked through, the click-through by John and Fabian, then production. |

**Concurrency:** at most two pipelines run at once, one per repo.

**Order:** H0 → (BE1 ∥ FE1) → BE2 → (BE3 ∥ FE2) → BE4 → (BE5 ∥ FE3) → FE4 → H1.
