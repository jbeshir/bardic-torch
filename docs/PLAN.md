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
- **Hosting:** the Go API runs on John's dokku instance (MySQL through the dokku mysql plugin). The React Router 8 FE runs on Cloudflare Workers (static assets), deployed by Workers Builds.
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
   - The suggestion's places are the distinct place types of the piece's visible links that are not dead (unknown
     and blocked links count as live) and carry a content role (official, lyrics, text, sheet_music,
     dance_instructions, text_source, translation, performance): none is critically endangered, one to three
     endangered, four or more low risk, and a restricted piece has none. It is read-only
     (`Piece.conservation_suggestion`, and on the moderation revision view from the revision's links), pending the owners' choice of places (distinct
     place types or distinct hosts).
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
- **`bardic-torch-fe`** (React Router 8 framework mode on Cloudflare Workers)
  - Auth: openid-client against Auth0, with a React Router cookie session and token refresh. It keeps the feed's server-side proxy routes (`/api/*`).
  - Types are generated from the BE's OpenAPI spec (openapi-typescript) and validated at runtime with zod.
  - Tailwind 4 design tokens (a CSS-first `@theme` generated from the token module) support light and dark themes.

**Removed from the feed skeleton:** Pinecone, VoyageAI, recommendations and clustering, RSS, chat, D1 and MiniMax.

**Kept and reused:**
- the multi-validator auth middleware (Auth0 JWT plus `user_api|` tokens), for future MCP and bulk entry. A
  `user_api|` token acts with contributor powers only (§5.1 "API tokens")
- the null-driver pattern
- the API token endpoints

## 4. Data model (MySQL 8)

**Editable entities** (each has a `current_revision_id`, plus `legacy_wiki_pageid` and `legacy_title` columns used for import):
- **`pieces`**:
  - identity: `slug`, `title`, `sort_title` (articles ignored)
  - type and status: `kind`, `conservation_status`, `is_stub`
  - Markdown fields: `notes_md`, `version_notes_md`, `licensing_notes_md`
  - licensing: `licensing_status` (known / unknown), `licence` (pd, cc-by…, arr, other, null) and, from BE5 (migration 012), `licence_version` (1.0, 2.0, 2.5, 3.0, 4.0, null). A version belongs to a `cc_*` licence only: validation refuses it beside any other (`out_of_range` at `/licence_version`), and a read shows it only beside one, as there is no CHECK constraint (the previous release's writer does not know the column, so a constraint could fail its writes after a rollback). The importer leaves it null
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
- **`links`**: `url`, `normalized_url` (unique; tracking parameters stripped), `title`, `description`, `platform`, `place_type`, `collection_id`, `locator`, `liveness` (unknown / ok / dead / blocked), `liveness_failures` (BE5: consecutive definite failures of the link checker), `last_checked_at`, `archived_url`, `archive_checked_at` (BE5: when the Wayback Machine was last asked; the link checker owns all four, and none is a document field), `is_hidden` (moderator takedown, R-11).
  - `link_attachments(link_id, piece_id | person_id, role, position)`. The roles are official, lyrics, text, sheet_music, dance_instructions, text_source, translation, performance, licensing_source, website, external_profile, other. One link can carry several roles (R-21, R-14).
  - `performances(link_id, piece_id, event_id, year, uploader, description_verbatim, owner_piece_id | owner_person_id)`, plus `performance_performers(person_id)`. The owner (BE4, migration 010) is the entity whose document lists the performance: a piece document owns its performances and a person document its own, so approving a document replaces exactly the rows it owns.
- **`events`**: name, kind (venue / event), description. Seeded with Green Dragon and with events parsed from performance descriptions.

**Workflow tables:**
- **`revisions`**: `id`, `entity_type`, `entity_id`, `base_revision_id`, `author_user_id`, `source` (user / import / mcp), `status` (draft / pending / approved / rejected / withdrawn), `snapshot_json`, `summary`, `reviewer_user_id`, `reviewed_at`, `review_note`, `created_at`, and from BE4 (migration 010) `submitted_snapshot_json`, `submitted_base_revision_id`, `received_keys_json`, `flags_json`, `updated_at`, `submitted_at`, `rebased_by_user_id` and `rebased_at`.
  - The snapshot is the full document, including owned child rows. Links appear inline by URL as entered (the normalized URL is derived; §4.1).
  - Drafts are revisions with `status = draft` (§5.1). `entity_id` is NULL only for a draft of a new entity (a CHECK).
  - `submitted_snapshot_json` is the author's document as submitted, `submitted_base_revision_id` the base they edited from (never changed, unlike `base_revision_id`, which a rebase moves), and `received_keys_json` the element keys of the editable projection the author was given. Together they build the author's views (§5.1). They are NULL for imported and interactive-moderator revisions.
  - `flags_json` holds a submission's moderator flags (§5.1). `submitted_at` is what queue age is measured from, and `updated_at` is the last draft save.
  - `created_at` of an approved revision is its **approval time** (= `reviewed_at`), so history (`created_at DESC, id DESC`) always lists the current revision first.
- **`users`**: Auth0 user ID, `display_name`, `display_name_key` (BE4: the folded display name, unique; §5.1 "Display names"), `public_id` (BE5: 16 random lowercase base32 characters, unique, the only handle of a user in a public URL, never the Auth0 ID; set when the user is created and by a startup backfill for any user without one), `role` (contributor / moderator), `created_at`, `blocked_at`. Moderators are not seeded by migration, and no user IDs live in migrations. At startup the API promotes the users listed in `BOOTSTRAP_MODERATOR_USER_IDS`, creating any who have not signed in yet. Promotion also unblocks a blocked user: listing an ID trusts that user, and moderators cannot be blocked or unblocked. This is promote-only and idempotent: removing an ID never demotes the user, and demotion is done in SQL until a moderation endpoint exists.
- **`repertoire`**: user_id, piece_id, notes (private to the owner, never in a public list), visibility (private / public; defaults to private, so an entry whose visibility is forgotten is never listed), added_at, updated_at. A user's entry for a piece is unique; deleting a user or a piece deletes its entries.
- **`link_reports`**: for removal requests (R-11): `link_id`, `reporter_user_id`, `reason` (broken / wrong_link / privacy / copyright / lyrics / other), `message`, `status` (open / resolved / dismissed), `created_at`, `resolved_by_user_id`, `resolved_at`, `resolution_note`.
- **`contact_requests`**: `kind` (takedown / opt_out / correction / general), `name`, `email`, `message`, `url`, `status` (open / resolved / dismissed), `created_at`, `resolved_by_user_id`, `resolved_at`, `resolution_note`, `purged_at`. These need no account and are protected by Cloudflare Turnstile (R-13). `name`, `email`, `message` and `url` are personal data, set to NULL 180 days after resolution (§5.1).
- **`api_tokens`**: kept from the feed.

### 4.1 Catalogue decisions refined in BE2

BE2 built the catalogue schema (migrations 003–009) and the public read API. These decisions refine §4 and §6;
the DDL is in `migrations/003`–`009`.

- **Visibility is approval.** An entity is public exactly when `current_revision_id IS NOT NULL`; a term also needs
  every ancestor approved. The `terms.status` column was dropped as redundant: a pending suggestion is a term with
  no approved revision. References from a public entity to a non-public one are omitted from every read, and an
  unknown slug and a non-public slug answer identically. BE4 keeps this without a status column (§5.1 "New
  entities"):
  - a new entity is a **placeholder** row created at submit, holding no document content (a `pending-<type>-<hex>`
    slug and empty names; a term also gets its self closure row, so `public_terms` never lists it);
  - a placeholder is deleted when its last pending revision is rejected or withdrawn;
  - only new terms and persons may be referenced while pending (term suggestions, red-link persons);
  - no approved row references an unapproved entity: approval approves the referenced pending terms and persons
    alongside, or refuses.
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
  A source URL outside a document's links follows the same rule (`domain.ShownSource`, fail closed): a preference
  whose `source_url` is a hidden link's (in any spelling) or does not normalize is dropped from every read,
  snapshot and author's view, and a person's `status_source_url` or a conservation `source_url` (the piece's, in
  snapshots, and each `conservation_history` entry's) is served as null. Contributors' editable views withhold
  them too (§5.1 "Editable projection"), and a submission attaching one is flagged `hidden_link_attached`.
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
  not hidden names. BE4's drafts and placeholders hold no words in these tables (drafts are revision rows, and
  placeholders have empty names), so editing does not shift the statistics; only imported or approved entities
  that are no longer public would.
- **Snapshots** hold only editor-owned fields. Left out:
  - IDs, `current_revision_id`, `legacy_*`, `claimed_by_user_id` and sort keys;
  - `conservation_history`, which is appended on approval from the document's `conservation {status,
    source_url}`;
  - link system and moderator state (`liveness`, `liveness_failures`, `last_checked_at`, `archived_url`,
    `archive_checked_at`, `is_hidden`,
    `normalized_url`).

  So approving an old snapshot cannot un-hide a link or undo a liveness check. Links are inline by URL, documents
  own only their outgoing relations, and other entities are referenced by ID. A list's order is its rows'
  `position`. Piece and person documents include their preference profiles (`dimension`, `value` `{choices, notes}`,
  `source_url`; "Preference profiles" below). Each performance holds its `key`, the editor's opaque identity of it
  ("Restore on submit"): it is editor-owned, held only by the stored documents (no column), and never shown in a
  view. A coverage test fails when a column is added without being classified as document-owned or system-owned.
- **Public revision views** never serve stored documents raw:
  - references are resolved to public summaries, or omitted;
  - redaction applies the *current* alias, person and link visibility, so hiding an alias later also hides it in
    old revisions;
  - `changed_fields` (the diff) is computed on the redacted views.

  Only approved revisions are ever public. A contributor's views of their own drafts and pending, rejected or
  withdrawn revisions (§5.1 "Author's view") and the moderators' views are authenticated endpoints. Authors show
  only a display name, and imports list the wiki
  contributors (`revision_contributors`).

  In detail (`domain.New*Snapshot`, given a `SnapshotVisibility` the repository loads for exactly what the
  snapshot and its base reference):
  - A revision is public if it is approved and its entity is public now; anything else is the same 404. History
    lists approved revisions newest first (`created_at`, then ID).
  - Views hold the document's editor-owned fields; links have no liveness or other system state. A preference is
    shown only if its `source_url` normalizes and is no hidden link's now, and the person's status source and the
    piece's conservation source are null unless they do. A link is shown only if its normalized URL is now that of
    a link that is not hidden ("Hidden links" above).
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
- **Preference profiles** (`person_preferences`, `piece_preferences`) state, per dimension of the Bard Preference
  Checklist, what a creator allows. A preference is `{dimension, value, source_url}` and its `value` is the closed
  shape `{choices, notes}`: `choices` are codes from the dimension's vocabulary (`internal/domain/preference.go`),
  `notes` (null or text) say what the codes cannot ("specify" items of the checklist). Each dimension groups its
  codes, and each group allows exactly one, at most one, at least one or any number of its codes:
  - *licensing* (persons only): one licence, whose codes are the `licence` values (`pd`, `cc_by`, …, `arr`,
    `other`) plus `varies_by_work`;
  - *performance_permission*: one permission (not given, ask first, ask a representative, group members, given)
    and at most one informing choice (inform required, welcome, not needed, not wanted);
  - *adaptations*: one permission (encouraged, allowed, allowed with conditions, ask first, live only, not allowed),
    any conditions, at most one informing choice, and any allowed kinds (`allow_*`) and refused kinds (their
    `refuse_*` twins);
  - *attribution*: at least one name to credit (`legal_name` names the kind of name, never the name), at most one
    timing, any extras;
  - *commercial_use*: one stance (non-commercial only, allowed, allowed with conditions, ask first, not allowed),
    any allowed uses, any conditions;
  - *recording*: at most one writing choice and at most one recording choice;
  - *performance_etiquette*: at most one choice each for singing along, competitions, circles and learning;
  - *objectionable_use*: at least one scope (purposes, contexts, content), any purposes, contexts, content and
    extras.

  The rules (422 `invalid_document`, each at its path, never quoting a value; moderators too):
  - each dimension at most once; a piece may not have the licensing dimension (`dimension_not_allowed`): a piece's
    licence is its `licensing_status`, `licence` and `licensing_notes_md`, and its other seven dimensions are
    piece-level overrides of the credited person's profile, which the FE combines (a piece's dimension replaces the
    person's);
  - `value` and `choices` are present; each choice is a code of the dimension (`out_of_range`), at most once
    (`duplicate`), and each group has as many as it allows (`required`, `too_many`); a dimension whose groups all
    allow none needs a choice or notes (`required`);
  - choices that contradict each other are `choice_conflict`: informing when permission is not given; an allowed
    kind with its refused twin; allowed kinds when adaptations are not allowed; adaptation conditions without
    "allowed with conditions" or "ask first"; allowed uses when commercial use is not allowed; commercial
    conditions without "allowed with conditions" or "ask first"; a list of objectionable purposes, contexts or
    content without the scope that names it;
  - `group_members` needs `notes` naming the group; `notes` are not blank, at most 2000 code points, NFC, with no
    control characters (document text, §5.1 "Line breaks"); hidden aliases in them are flagged like other text;
  - every preference needs its `source_url` (§5 "Sourced fields").

  Drafts get only the vocabulary and character rules. Choices are stored in canonical order (dimensions in their
  order, each value's choices in vocabulary order), so reordering is never a change; problems name the order the
  client sent. A code may be added like an ENUM value (§4.1); renaming or removing one is free until the first
  preference is stored, and needs a document migration after. The profiles are edited in the editor format
  (§5.1), shown in snapshots and on the public pages (`Piece.preferences`, `PersonProfile.preferences`, in dimension
  order; a no-page person has no profile, so none); a preference whose source is a hidden link is withheld from
  contributors and left out of every view ("Hidden links"). FE2 builds its licensing card from `licensing_status`
  and `licensing_notes_md`; the full etiquette card (profile with piece-level overrides) is FE4's (§10).
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
    (collections and revisions are addressed by ID by design) and user IDs.
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
3. Write the snapshot into the entity and its child tables: upsert links by `normalized_url`, rebuild `term_ancestors` if a term is new or moved, append to `conservation_history` if the status changed.
4. Point `current_revision_id` at the approved revision.

BE4's transaction (lock order, pending references, links, performances, retries) is in §5.1 "Approval".

**Stale revisions** are rebased automatically onto the current version when the two edits touched different fields. Otherwise the moderator sees a three-way view and resolves it.

**Edit rules:**
- **Contributors:** edits go to `pending`. Contributors see their own pending version on the entity's own page, its history and "my contributions", with a banner, through the authenticated `…/pending` and `/v1/me/revisions` endpoints. Public reads (lists, search, facets, counts, needs-work) stay credential-free and approved-only. Contributors may have at most 30 open pending revisions.
- **Moderators:** edits made in an interactive (Auth0) session are approved as they are saved, but still recorded as revisions. An API token's edits are a contributor's (§5.1 "API tokens").
- **Sourced fields** (licensing notes, licence and its version, preferences, person status, conservation status) are rejected at validation without a source URL. The rule applies to moderators too (R-12).
- **New entities**, whether created by hand or as term suggestions, exist with `current_revision_id = NULL` and are invisible to the public until approved. When a moderator reviews a piece, any pending terms it references are shown and approved alongside it.

**Roles and blocking:** every user starts as a contributor. The moderator role is granted only through `BOOTSTRAP_MODERATOR_USER_IDS` (§4). Moderators block and unblock contributors, but not other moderators or themselves. A blocked user gets 403 on every authenticated endpoint.
- **Duplicate check on create:** normalised titles, names and aliases are compared, with apostrophes and case folded, Unicode NFC applied and accents stripped for matching only (R-17).

**Revert** creates a new revision copying an older snapshot (§5.1 "Revert").

**History:** every entity has a public history list, and each revision a diff view. Wiki contributors are credited on imported revisions.

### 5.1 Revision and moderation decisions refined in BE4

BE4 built the revision engine and the moderation, link-report and contact endpoints (migration 010). These
decisions refine §4, §5 and §6. Two rules hold throughout:
- **Validation is never an oracle for hidden data.** A problem caused by data the caller could not see is a
  moderator flag, never a contributor error.
- **Reference rules apply only to references an edit adds or changes.** Unchanged or restored references are
  checked only for existence, so a person opting out of a page never makes other entities uneditable.

- **Drafts** are `revisions` rows with `status = draft`, private to their author (moderators do not see them).
  `POST /v1/drafts` creates one, and `GET|PUT|DELETE /v1/drafts/{draft_id}` reads, replaces and discards it; a
  draft has its own ID because slug `new` is a valid piece slug, and the ID becomes the pending revision's ID on
  submit. One draft per author and existing entity: creating another replaces it (200, ID and base kept), under a
  lock on the author's `users` row. New-entity drafts (`entity_id` NULL) may be several. At most 50 drafts per
  user. The draft's base is an approved revision of the entity (default: the base of the document the editor
  serves: the pending start's submitted base if the author has a pending revision of the entity, else current;
  none for a new entity). Drafts are checked only structurally (vocabulary values, NFC, control characters,
  references that resolve), since they may be incomplete; full validation runs at submit.
- **State machine** (`domain.CheckTransition`, called by the repository on the status it read `FOR UPDATE`):
  draft → pending (contributor or API-token submit) or approved (interactive moderator submit); pending →
  approved, rejected, withdrawn (by its author, or superseded by the author's next submit of that entity) or
  pending (rebase). Approved, rejected and withdrawn are final. Another user's draft or revision is 404; one's own
  that is no longer a draft is 409 `not_draft`; an action on a revision that is not pending is 409
  `not_pending`. A replayed submit of a submitted draft answers 200 with the revision: a draft that is now pending
  or approved answers 200; one now rejected or withdrawn is 409 `not_draft`.
- **The editor format.** Editable documents reference pieces, persons, terms and events by slug (collections by
  ID), never by internal ID, and have no `schema_version`; they hold the preferences as stored. An unresolvable or
  forbidden slug is the same 422 `unknown_reference`. A field the document type does not have is 400
  `invalid_request`, but a field added after a client was built (`preferences`, and a piece's `licence_version`) may be left out of a request (`preferences` also
  sent as null; a null `licence_version` is a version cleared): the document then keeps the base's value, as the author is treated as never having received it
  (its received keys are dropped from the draft, and only ever shrink), and a rebase document that leaves it out
  keeps current's. Responses always carry it. Each performance has a `key` (16 hex digits) the server issues:
  an editable document of the current revision gives every performance one (one stored without gets a key derived
  from what the caller is shown of it; one starting from the caller's pending revision holds the keys as they
  submitted them), a client sends it back unchanged, and sends null (or leaves it out) for a new performance.
- **Editable projection** (`GET /v1/{type}/{key}/editable`): the current document minus every **withheld
  element** for a contributor or API token: hidden aliases, hidden links (by normalized URL, failing closed),
  credits, performers and relations of persons not public with a page, references to non-public entities (but the
  caller's own pending terms and persons), preferences whose `source_url` is a hidden link's (by normalized URL)
  or does not normalize, a person's `status_source_url` or a piece's `conservation.source_url` that is
  such a link (served as null; a no-page person's status source too), and a no-page person's whole profile but its
  identity. A placeholder (pending new entity) is editable only by its author, even by a moderator (404 otherwise); moderators rebase its revision instead. If the caller has a pending revision of the entity, the
  projection starts from it instead (`starting_point: pending`, on its submitted base). An interactive moderator gets the full document with
  `link_states` (link ID, hidden, liveness); others get `[]`.
- **Restore on submit**, for every actor: each withheld element of the draft's base is put back (keyed lists after
  their nearest preceding surviving element, sets unioned, nulled references restored unless replaced, the no-page
  profile copied whole; preferences are a keyed list by dimension, so a withheld one is put back, and a submitted
  preference of its dimension replaces it; a status or conservation source withheld as null is put back if the
  submission leaves it null, and a submitted URL replaces it), as is every base element the author never received (not in
  `received_keys_json`). A submitted element whose key equals a withheld one (a public alias equal to a hidden
  alias, a link URL of a hidden link) is kept, and the collision is a moderator flag; the contributor is told
  nothing. A submitted performance is matched to the base performance it edits by its `key` first, so it keeps its
  withheld performers and event whatever the author edited: a key only one base performance has matches it (the
  first submitted performance with it; a repeated key is a new performance), and a key the base does not have is a
  new performance. A performance without a key, or with a derived key two base performances share, is matched by
  its year, description and piece instead. A matched performance keeps the base's key; every other gets a fresh
  random one. Derived keys hash only what the caller is shown (a piece they may not see counts as none), so a key
  never reveals hidden data. A base performance with withheld performers or event that no submitted performance
  matches (deleted, or edited without its key) is the flag `hidden_data_conflict`, never a silent removal. **A contributor submit never merges**: it is stored on the draft's own base, and stale
  handling is approval's. Contributors cannot add a hidden alias other than a `legal` one, every `legal` alias they
  add is hidden, and they cannot make a hidden alias public.
- **Two-phase validation** of a contributor or API-token submit or revert: phase 1 validates the editor
  document as sent, against the projection of its base (caps count what the caller sees, sourced-field rules see
  visible links only, paths are editor paths). Only phase-1 problems are ever shown to a contributor (422
  `invalid_document`, problems with a path and code, never a value). Phase 2 validates the restored document
  against the full base; a problem only it finds becomes the flag `hidden_data_conflict`, and the response is the
  same as without it. Approval refuses that flag (409 `hidden_data_conflict`) until a rebase clears it.
- **Author's view** (`…/pending`, `/v1/me/revisions[/{revision_id}]`, a draft's `preview`) is defined by
  provenance: elements the author typed are kept whatever their hidden state, and elements they received are
  redacted by the current state, through the public `New*Snapshot` views extended with their own pending terms
  and persons, their typed links and public aliases and their pending rename. A typed `legal` alias is never shown.
  The base view and `changed_fields` use `submitted_base_revision_id`. An approved revision's view is its public
  view.
- **Fields and merge.** A field is a top-level document key, with groups (piece `licensing`, `period`,
  `conservation`; person `status`, `licensing`) so a sourced value is never merged apart from its source, keyed
  lists merged per element (aliases, terms, relations, links by normalized URL, preferences), sets (secondary
  kinds, roles, affiliations, term aliases) and credits as one unit. The three-way merge (base, revision, current)
  takes one-sided changes, equal changes once, and makes every other overlap a conflict, served only in moderator
  views. Sets never conflict; a removal wins, even over the other side's re-spelling under the same key.
- **Sourced fields** (R-12, R-15, also for moderators), compared with the base at submit and with current at
  approval: changed licensing notes, licence, licence version or a licensing status of known need a link with role
  `licensing_source`; a person status other than active, and any status change, needs `status_source_url`; a
  conservation status other than unassessed needs its `source_url`, and a change also a non-empty summary (the
  history reason); every preference needs its `source_url`, changed or not. A source is an http(s) URL `NormalizeURL`
  accepts, at most 2048 characters. Imported documents satisfy the absolute rules, and the others trigger only
  on change.
- **Validation** (`Validate` per document type) shares its width, character and uniqueness checks with
  `CatalogueImport.Validate`: column widths, NFC, no control characters, non-blank names, element caps (aliases,
  terms, credits and relations ≤100, links ≤200, performances per link ≤200, performers ≤50), the credit and
  facet CHECKs, one primary term per facet, term depth ≤3 without cycles and an immutable facet, collection
  parents without cycles, and a term name unique in its facet (422 `duplicate` against a public term at a
  contributor's submit; 409 `duplicate_name` wherever the term is approved: at approval, and at an interactive
  moderator's own submit or revert, which are approved at once).
- **Duplicate check** (R-17) on new-entity drafts, submits and the moderation view: titles, names and aliases
  compared by `domain.DuplicateKey` (NFC, apostrophes removed, `FoldName`) against whole name sets of public
  entities, in Go. Contributor checks **never** compare hidden aliases, nor any alias of a person who prefers no
  page, who is found only by name (§2); moderator checks compare both. A match on a no-page person's name blocks a
  new person (422 `duplicate_blocked`; at approval also on any of its aliases), and other matches need
  `acknowledge_duplicates` (409 `possible_duplicate`); moderators get the `possible_duplicate` flag. The check
  runs at approval of every new entity, including the pending references approved alongside: a match on one
  refuses the approval with `pending_reference_flagged` (its `flags` include `possible_duplicate`), and the
  moderator approves that revision on its own first. The check is made outside the write transaction, on the
  document version the write then locks: the write compares the names checked with its locked document's, and a
  change between the two is checked again and retried, at most twice, then 503 `unavailable`. A moderator's own
  submission approved at once learns its pending references only under its locks: the write names those it has no
  check of, and the submit checks them and retries the same way.
- **New entities** are placeholder rows created at submit with no document content (§4.1). At first approval the
  writer allocates the real slug (`Slugify`, then `-2`, `-3`…), fixed thereafter, and the sort keys. A rejected
  or withdrawn new entity's placeholder is deleted with its children when no other pending revision has it; its
  revision rows stay. Term suggestions are term drafts submitted like any other and referenced by placeholder slug.
- **Approval** is one READ COMMITTED transaction in `withWriteTx`:
  - every BE4 write takes locks in one global order: `users` rows, `revisions` rows by ascending ID, entity rows
    by (type, ID), then `links` rows by normalized URL, then `link_reports` rows. Within an entity type the rows
    written (`FOR UPDATE`) and those referenced (`FOR SHARE`) are locked in one ascending pass; for a facet whose
    closure an approval may rebuild (any term approved), all of its term rows are locked `FOR UPDATE` first among
    the term rows, and no other lock is taken on them. The rows the entity's current document references are
    locked in the same pass: the current revision is read before the lock and checked under it. The revisions of
    pending references come from an unlocked read, so once locked they are read again, and any found anew (a
    superseding revision) locked too, at most three rounds, then the approval is refused as changed (above). A
    moderator's own submission approved at once locks the revisions of the pending references it approves alongside
    (found from its draft's document) before any entity row, and takes their rows, its facet lock and its own entity
    row in one entity pass, as an approval does. The `links` rows of every document an approval writes, its own and
    its pending references', are upserted in one ascending pass by normalized URL;
  - every write that locks a `users` row checks the row again under the lock, since the request was authenticated
    before it: a user blocked since is refused with 403 `user_blocked`. Moderator-only writes (approve, reject,
    rebase, hiding or showing a link, resolving or dismissing a link report, dismissing a contact request) lock the
    acting moderator's row first, before any revision, and refuse a user no longer a moderator with 403
    `moderator_required`, writing nothing. Whether a moderator's own submission or revert is approved at once follows
    the role their locked row holds: if it no longer holds the moderator role, the transaction is refused and run
    again once as a contributor's, which stays pending (fail closed); a role changed again on that attempt is 503
    `unavailable`;
  - rows discovered later can still deadlock: the current document of an entity approved since it was read, and the
    S-locks foreign-key checks take at the end of a transaction on `users` rows other than the actor's, which
    those writes lock first. A deadlock (1213) or lock-wait timeout (1205) is retried at most twice;
  - referenced public entities are locked `FOR SHARE`, so a concurrent opt-out or placeholder delete waits or is
    seen; a vanished parent (1452) is 409 `entity_gone`;
  - stale revisions are merged; a conflict is 409 `rebase_required`, and the moderator resolves it with
    `…/rebase` (`current_revision_id` must still be current, else 409 `stale`). A clean merge that fails validation
  against current (for example a licence change merged with the removal of its `licensing_source` link, or two
  primaries in a facet) is 422 `invalid_document` with the merged document's problems; the moderator resolves it
  with `…/rebase`;
  - pending terms and persons of the revision's author that the document references (recursively) are approved alongside, parents first, only
    when they carry no flag of their own and pass the duplicate check; otherwise 409 `pending_reference_flagged`,
    and the moderator handles that revision first. A moderator's own submission approved at once checks them too
    (`pending_reference_flagged`); a revert approves no pending reference, since the documents it merges are approved
    ones;
  - the entity's own child rows are deleted and reinserted from the document by the writer the importer shares;
    the facet's closure is rebuilt when a term is new or moved; conservation history is appended;
  - links are upserted by normalized URL; the shared fields are written only where this editor changed them, or
    where the row's value is NULL for a link newly attached; `is_hidden` and liveness are never written, and links
    rows are never deleted, so a taken-down URL stays hidden when re-added;
  - performances are replaced by owner (§4);
  - the revision's `created_at` becomes the approval time;
  - search happens only after commit;
  - the session `innodb_lock_wait_timeout` is 3 s and locking reads carry `MAX_EXECUTION_TIME(4000)`. Each request
    has a 13 s budget (`RequestBudget`, its context's deadline) under the server's 15 s `WriteTimeout`; the
    transaction gets at most 11 s of it (`WriteTxDeadline`), less a 1.5 s reserve for the response's reads after
    commit (`PostCommitReserve`), so work before it (the pre-reads, a Turnstile verification) shortens the
    transaction, not the request. With under 500 ms left the write is not tried. Past the deadline, or after the
    retries, the answer is 503 `unavailable`, as it is for any request, read or write, whose budget ran out (an
    error from its context's deadline passing). A database that cannot be reached is 503 `unavailable` too, on
    every operation (§6), as is one that turns the connection away (`Too many connections`, 1040; the
    account's `max_user_connections`, 1203; shutting down, 1053); a query that fails for any other reason stays
    500 `internal`.
- **Flags** (stored in `flags_json`, recomputed in the moderation view and at approval; never shown to
  contributors): `hidden_alias_in_summary` (review C10), `hidden_alias_in_text`, `hidden_alias_in_review_note`,
  `hidden_alias_collision`, `hidden_link_collision`, `hidden_link_attached`, `hidden_data_conflict`,
  `possible_duplicate`, `references_pending`. Hidden aliases are found by `domain.NameMatcher` (the importer's),
  in every form the importer withholds. At approval `hidden_alias_collision` and `hidden_data_conflict` refuse
  (409 `hidden_data_conflict`) until the moderator rebases the revision, which clears the flags only the
  submission could find (`hidden_alias_collision`, `hidden_link_collision`, `hidden_data_conflict`; the others are
  found again at approval), or rejects it; every other flag but `references_pending` must be acknowledged (409
  `unacknowledged_flags`), and a flagged summary also needs a clean replacement. A review note is checked the same
  way at approve and reject, as it reaches the author.
- **Revert** (`POST /v1/revisions/{revision_id}/revert`) builds the target's document with every alias hidden in
  current or in the target still hidden and the target's preferences, on base = current read under the entity lock
  (so never stale), with the default summary `Revert to revision <id>.`. Validation sees the request's summary, so a
  revert across a conservation-status change needs an explicit `summary` (422 `reason_required`); the default
  summary is only stored. A contributor's is pending (capped,
  superseding), a moderator's approved. A replay answers 200 with the revision it already created.
- **Moderation views** (`GET /v1/moderation/queue`, `GET /v1/moderation/revisions/{revision_id}`) show the full
  documents (revision, submitted, base, current, merged), link states, conflicts, pending references, flags,
  duplicates and the author with user ID, role and blocked state (null for imported revisions, whose wiki
  contributors are listed). The queue is oldest first and hides blocked authors' revisions unless
  `include_blocked=true`. Conflicts and changed paths are in the editor format of the view's documents: fields
  by their editor names (`term`, `parent`, `affiliations`), list elements keyed by a reference by its slug
  (`terms[drinking-songs]`, `relations[the-copper-kettle:variant_of]`), and conflict values as
  an editor document holds them. A reference the view cannot name is `pending-<type>-<id>`; a preference is named
  by its dimension (`preferences[recording]`).
- **Link reports** address a link by the URL the page shows (link IDs are not public): `POST /v1/link-reports`
  with a reason and message. Only a visible link can be reported; a hidden and an unknown URL are the same 404.
  One open report per reporter and link (a repeat answers 200 with it), at most 20 open per reporter. Hiding a
  link (`PUT /v1/links/{link_id}/hidden`, or resolving a report with `hide_link`) resolves its open reports.
  `GET /v1/moderation/links?url=` finds any link's ID, state and attachments.
- **Line breaks and control characters in text.** A control character is a C0 or C1 control other than tab and
  line feed, or a bidirectional embedding, override or isolate (U+202A–U+202E, U+2066–U+2069), which could rewrite
  how a moderator's screen shows the text after it. CRLF and a lone carriage return are what a browser sends for a
  textarea line break without JavaScript. The rule depends on the field's class:
  - *Moderation and contact free text*: a link report's `message`, a `review_note`, the approve and rebase
    `summary`, the `note` resolving a link report or a contact request, and the contact form's `message`. CRLF and
    a lone CR are first replaced by LF; the length (counted on the replaced text, so a spec `maxLength` is the
    limit on the stored text) and the control characters are then checked, and a failure is 400
    `invalid_request`, naming the field and never its value. The stored and forwarded text holds LF only.
  - *Revision summaries* of drafts and reverts (`summary` of `POST /v1/drafts`, `PUT /v1/drafts/{draft_id}` and
    `POST /v1/revisions/{revision_id}/revert`): CRLF and a lone CR are replaced by LF in the same way, and the
    summary is then checked with the document, so a control character, a non-NFC string or more than 500
    characters is 422 `invalid_document` at `/summary`.
  - *Document text* (every string of a draft's or rebase's document) is not normalised: a CR there is a control
    character, 422 `invalid_document` `invalid_characters` at its path, preference notes included. The FE converts
    CRLF and CR to LF and NFC-normalises before it sends a document; the server never rewrites document text.
  - *Single-line fields* (the contact form's name, email, URL and Turnstile token, a link report's URL) are not
    normalised: a CR, like any control character, is 400 `invalid_request`.
- **Contact requests** (`POST /v1/contact`, no account): kind, name, email, message, URL and a Turnstile token;
  202 with no ID. Turnstile is verified server-side with siteverify: `success`, a hostname in
  `TURNSTILE_HOSTNAMES` (bardictorch.org and preview.bardictorch.org, compared case-insensitively; empty disables
  the hostname check, for development only, with a startup warning) and the action `contact` must all hold. Only
  user-side token failures are 400 `turnstile_failed`; Cloudflare-side failures, misconfiguration, an empty secret
  (contact disabled), more than 8 verifications at once and 1000 open requests (a soft cap: the count is not
  locked, so concurrent requests can pass it by about as many as are inserting at once; BE5 adds rate limits) are
  503. A URL typed without a scheme is stored with https, so the inbox never shows a relative link. The name,
  email, message and URL are returned only by the moderators' inbox and never logged, and are purged 180 days
  after a request is resolved or dismissed (at startup, after each resolution and before each inbox list); open
  requests are kept.
- **API tokens** (a decision BE5 may revisit): a `user_api|` token acts as its user with **contributor powers
  only**: drafts, submit, withdraw, revert, their own revisions and link reports, with every submit pending and
  the contributor projection. Every moderator operation, the link-report inbox and the contact-request inbox
  answer 403 `api_token_not_allowed` to a moderator's token, because moderation and contact PII need an
  interactive Auth0 session; `setUserBlocked` follows the same rule. BE5's MCP work may widen this, for example
  with read-only queue access, by editing this paragraph.
- **Display names** (impersonation controls): `display_name_key` is the display name folded like duplicates with
  only letters and digits kept, unique, so `Mor.wen` and `MORWEN` collide (409 `display_name_taken`; this reveals
  that someone holds the name, which is public anyway). A new display name is 400 `invalid_request`, naming the
  field and never its value, unless it passes these rules too: it contains no blank letters (Hangul fillers, the
  braille blank, default-ignorable characters; also checked after compatibility decomposition), it does not mix
  Latin, Cyrillic and Greek letters (a name wholly in one script is allowed), it contains at least one letter or
  digit, so its key is never empty, and its key has at most 255 characters (the column's width). The key also drops
  blank letters, so an older name holding one collides with the name it looks like. Names whose key is reserved
  (`mod`, `system`, `import`, `wiki`, `anonymous`, `unknown`, `traditional`), contains `moderator` or
  `bardictorch`, starts with `admin`, or is shaped like another user's default name (`bard` + 8 hex digits) are 422
  `display_name_reserved`. `admin` and `mod` are not refused inside a key, which would refuse ordinary names such
  as "Badminton" and "Modesty". Look-alike letters within one script (a confusable skeleton) are an accepted risk
  for now. Existing holders are not renamed (DEPLOY.md lists them), and a startup backfill sets missing or stale
  keys; it leaves an oversized key NULL, with a warning naming the user ID only, like a collision, so stored names
  never stop startup. Authenticated author objects carry the `role`, so the FE can badge moderators.
- **Body sizes:** document bodies up to 1 MiB, `/v1/contact` up to 64 KiB, everything else 16 KiB (413).
- **No public schema or document schema change:** BE4 adds no field to an existing response, and
  `DocumentSchemaVersion` stays 1.

## 6. API surface (all specified in OpenAPI; the FE never calls the API directly from the browser)

- **Public reads** (built in BE2; `openapi/api.yaml` is the exact contract):
  - `GET /v1/pieces`: faceted search.
    - Filters: `term` (repeatable slugs), `kind`, `conservation_status`, `stub`, `q`, `letter` (`A`–`Z` or `0`), `sort` (`title` / `relevance`), `limit` and `offset`.
    - Terms combine with OR within a facet and AND across facets, and a term matches its descendants (closure table). `kind` and `conservation_status` act as two more facets.
    - `q` is FULLTEXT over title, aliases and notes (§4.1).
    - Facet counts are disjunctive: each facet's counts ignore that facet's own selection. Letter counts are returned for the A–Z bar.
  - `GET /v1/pieces/{slug}`, with the piece's preferences and its `conservation_suggestion` (§2.3; null for a
    restricted piece), derived from its public links only, so it is no oracle for hidden links
  - `GET /v1/persons` (`kind`, `role`, `affiliation`, `q`, `letter`, `sort`) and `GET /v1/persons/{slug}`
  - `GET /v1/terms?facet=…`: the term tree with piece and person counts. It is not paged or searched: a client finds terms by name itself, matching on the same folding as the `Search` rule of §4.1 (combining marks removed, case ignored)
  - `GET /v1/terms/{slug}`, with its pieces or persons (paginated, including descendants)
  - `GET /v1/collections`, `GET /v1/collections/{collection_id}`
  - `GET /v1/events` (added in BE2) and `GET /v1/events/{slug}`
  - history:
    - `GET /v1/{pieces|persons|terms|events}/{slug}/history` and `GET /v1/collections/{collection_id}/history`
    - `GET /v1/revisions/{revision_id}`: the redacted snapshot view, its base and the changed fields
  - From BE5 (`info.version` 1.1.0, all additive): every entity page carries `updated_at`, when its current revision was approved (the revision's `created_at`); a history carries `entity`, the public entity it belongs to (`entity_type`, `slug` or `collection_id`, `name`); the links of a performance (on a person's or an event's page) and the citations of a collection carry `liveness`, `last_checked_at` and `archived_url`, as a piece's and a person's links do (a hidden link is shown nowhere, so neither is its archive); a piece and its snapshots carry `licence_version`, null unless `licence` is a `cc_*` code (a Creative Commons licence's URL is derived from the two, not stored); and a symmetric relation (`same_text_other_form` between pieces, `same_as` between persons) stored on both entities is listed once, as outgoing. Every one of them is data of a public entity, so the 404 of an entity that is not public is unchanged.
  - `GET /v1/needs-work`: stubs, unassessed conservation status, missing licensing information, dead links (R-18). Restricted pieces appear only as stubs, and `prefers_no_page` persons never appear.
  - Lists page with `limit` (at most 100) and `offset` (at most 10000), and every ordering ends with the ID, so pages are stable. Nested lists that grow with data are capped at 200 entries, each with a `*_total` field.
  - A database outage is 503 `unavailable`, as on every operation that reads the database (all but `/healthz`; for an authenticated operation also when the caller's user cannot be looked up). A query that fails for another reason is 500 `internal`.
  - IDs, in paths and bodies, are at most 2^53−1 (9007199254740991), the largest integer a JSON number holds exactly; a larger path ID is 400 `invalid_request`. The IDs are AUTO_INCREMENT columns, far below it.
- **Authenticated:**
  - `GET /v1/me`, and `PATCH /v1/me` to set the display name
  - `GET /v1/{pieces|persons|terms|events}/{slug}/editable|pending` and `GET /v1/collections/{collection_id}/editable|pending`: the caller's editable document, and their pending revision of the entity (a placeholder slug addresses their own pending new entity)
  - drafts: `POST /v1/drafts`, `GET|PUT|DELETE /v1/drafts/{draft_id}` (a draft has its own ID, since slug `new` is a valid piece slug)
  - `POST /v1/revisions` to submit a draft, `POST /v1/revisions/{id}/withdraw` and `…/revert`
  - `GET /v1/me/revisions` and `GET /v1/me/revisions/{id}`: the caller's drafts and revisions, in the author's view
  - `GET|PUT|DELETE /v1/me/repertoire/{piece}` (BE5)
  - `GET /v1/users/{id}/repertoire`: public lists (BE5)
  - `POST /v1/link-reports`: report a link by its URL (link IDs are not public)
  - suggestions for new terms are submitted as term revisions
- **Moderator** (an interactive Auth0 session; API tokens get 403 `api_token_not_allowed`, §5.1):
  - `GET /v1/moderation/queue` (filters by type, author, age and `include_blocked`) and `GET /v1/moderation/revisions/{id}`
  - `POST /v1/revisions/{id}/approve`, `…/reject`, `…/rebase`
  - `GET /v1/moderation/link-reports` and `POST /v1/moderation/link-reports/{id}` (resolve or dismiss); `GET /v1/moderation/links?url=`
  - `GET /v1/moderation/contact-requests` and `POST /v1/moderation/contact-requests/{id}` (resolve or dismiss)
  - `PUT /v1/links/{id}/hidden`
  - `PUT /v1/users/{id}/blocked`: block or unblock a contributor. Author IDs are shown in the moderation queue, the moderation revision view and the inboxes.
- **No account needed:** `POST /v1/contact`, verified server-side with Turnstile: `success`, a hostname in the allowlist (`TURNSTILE_HOSTNAMES`: bardictorch.org and preview.bardictorch.org) and the fixed action `contact` must all hold. Cloudflare-side failures and misconfiguration are 503, and only user-side token failures are 400.
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
- Write every performance with its owner (`owner_piece_id` for a piece document's, `owner_person_id` for a person
  document's; the startup backfill owns BE3's rows), and the import and the approval writer share one entity writer.
  After the import, approvals write a links row's shared fields only where the approving editor changed them, or
  where the row is NULL (§5.1 "Approval"). Per-attachment titles and locators (backlog 10) are deferred to after
  BE5: they need new columns and every link read to change.
- Write revision `summary` text from a fixed set of neutral templates ("Imported from the wiki (page revision N).",
  "… as a stub.", "… Collections page.", "… categories.", "… performance listings."): a summary is free text shown on
  every approved revision, including those of persons without a page, and nothing checks it against hidden
  aliases. BE4 flags editor summaries that contain a hidden alias (§5.1 "Flags").
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
- pages are server-rendered by a Worker, and the browser never calls the API directly
- WCAG 2.2 AA, fully keyboard operable
- every style works in light and dark themes
- works at 360px width
- no layout shift on load
- server-rendered read pages for SEO, with page metadata

## 9. Testing strategy (heavy, at both layers)

**BE:**
- Unit tests for domain and commands, using mockery mocks. The revision engine gets table-driven tests for every state transition, stale/rebase case and sourced-field rule.
- Repository tests against **real MySQL 8.4**. BE1's spike succeeded, so they run in the sandbox (`make test-integration-local`, a conda-forge mysqld), on the host (`make test-integration`, podman) and in CI (`make test-integration-external` against a `mysql:8.4` service, under `-race`). A milestone's final gate includes `GOTESTFLAGS=-race make test-integration-external`, since the approval races are only meaningful under the race detector.
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
- `npm run validate` covers format, lint (ESLint 10), typecheck, generated-file checks, knip, vitest and the build checks. The Playwright suite is the separate required `e2e` CI job.
- `npm run audit` (no advisory of any severity in production dependencies, none high or critical anywhere) and `npm audit signatures` run as their own CI job on every push and pull request, like `make vulncheck`.

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
| H0 | host setup | — | decisions in §2 | Create both GitHub repos and copy this plan into `bardic-torch/docs/`. Set up the dokku app, MySQL, letsencrypt and the api.bardictorch.org domain. Create the Cloudflare zone and two Workers connected through Workers Builds: `bardic-torch-fe` on bardictorch.org (with a www → apex redirect rule) and `bardic-torch-fe-preview` on preview.bardictorch.org. Create the Auth0 application with Google and Facebook connections, plus a Turnstile site key. *The Auth0, Cloudflare and DNS dashboard steps are John's.* |
| BE1 | foundation | BE | H0 | Copy and strip the feed skeleton. Add the users table, roles and the moderator bootstrap, the auth carried over from the feed, the test harness (with the **in-sandbox MySQL spike**), the OpenAPI skeleton, CI, and the dokku files (Dockerfile, `app.json` predeploy migrate step). The cron entry lands with `cmd/check-links` in BE5. |
| FE1 | foundation | FE | H0 (runs alongside BE1) | Copy and strip the feed FE. Add the wiki-style design system and layout, dark mode, auth, the type-generation pipeline, the fixture server, and the Playwright, axe and screenshot harness. Add the static help pages. |
| BE2 | catalogue and read API | BE | BE1 | Every §4 catalogue table and migration, and every public read endpoint in §6, including facet counts, FULLTEXT search, A–Z and needs-work. The revisions table exists but only stores import data. Decisions refined in BE2 are in §4.1. |
| BE3 | importer | BE | BE2 | Everything in §7 (reading a live crawl directly is deferred). A dry-run report is produced for John and Fabian to read. |
| FE2 | read UI | FE | BE2 spec (runs alongside BE3) | Every read page in §8 (including the read-only history and diff pages), the licensing card (licensing status and sourced notes) and conservation badge, the mobile facet drawer, and SEO. |
| BE4 | revisions and moderation | BE | BE3 | Everything in §5 (§5.1 records the decisions), migration 010, and these endpoints: the editable and pending reads of each entity type, drafts (`POST /v1/drafts`, `GET|PUT|DELETE /v1/drafts/{id}`), submit, withdraw and revert, `GET /v1/me/revisions[/{id}]`, link reports by URL, the moderation queue and revision view, approve, reject and rebase, the link-report and contact-request inboxes, the moderator link lookup and `PUT /v1/links/{id}/hidden`, and the public contact form (Turnstile). Also the display-name impersonation controls (backlog 2). (needs-work, a public read, moved to BE2; repertoire is BE5's.) |
| FE3 | editing | FE | BE4 spec | Edit forms for every entity type, the credit, link, term and sourced-field editors, preview, drafts, the duplicate warning, the pending banner, revert, and the pending-version diff. (The read-only history and diff pages are FE2's.) |
| BE5 | contributor extras | BE | BE4 | Repertoire, preference profiles and their validation, conservation suggestions, the `check-links` cron with Wayback lookup, API-token MCP tools, and rate limits. |
| FE4 | moderation and extras UI | FE | BE5 spec, FE3 | The moderation queue, diff and three-way review, the inboxes, repertoire, the etiquette card (preference profiles with piece-level overrides, once BE5 exposes them), the preference-profile editor, the needs-work dashboard, and the contact form. |
| H1 | launch | host | all | Full-stack e2e against the real import, a staging deploy, the real import run with its review report worked through, the click-through by John and Fabian, then production. |

**Concurrency:** at most two pipelines run at once, one per repo.

**Order:** H0 → (BE1 ∥ FE1) → BE2 → (BE3 ∥ FE2) → BE4 → (BE5 ∥ FE3) → FE4 → H1.
