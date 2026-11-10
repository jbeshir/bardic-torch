# CHANGES: BE-S (simplify)

Branch `milestone/be-simplify`, from `8ad3b48` (BE3 plus the rewritten PLAN). BE-S does what PLAN §10 "BE-S removes" lists and nothing else. Owner questions are in the pipeline's QUESTIONS.md, not in the repo.

## Size

The counts come from the same script at both ends. It counts physical lines and skips `.git` and every `testdata` directory.
- **Generated:** Go files with a `Code generated … DO NOT EDIT.` header (sqlc and mockery).
- **Test Go:** other `*_test.go` files plus `internal/testutil/**`.
- **Prod Go:** all other `.go` files.
- **Docs:** `*.md` outside testdata, including PLAN.md (unchanged, 401 lines) and this file.
- **OpenAPI:** `openapi/*.yaml`.

| | Before (`8ad3b48`) | After | Change |
|---|---:|---:|---:|
| Prod Go | 24,853 | 21,759 | −12% |
| Test Go | 32,086 | 20,324 | −37% |
| Generated Go | 8,737 | 5,159 | −41% |
| Docs (`*.md`) | 1,892 | 1,418 | −25% (PLAN.md unchanged; incl. this file) |
| OpenAPI | 4,659 | 4,380 | −6% |
| Migrations SQL | 491 | 569 | +78 (010, 011) |
| Go files | 340 | 248 | −92 |
| Importer goldens (testdata) | 5,202 | 2,985 | −43% |

Per document: TESTING.md 653 → 111, DEPLOY.md 556 → 515, CLAUDE.md 86 → 85, README.md 196 → 186.

The diff from `8ad3b48` is 256 files, +2.1k / −23.8k lines, before this file.

## What was removed or simplified

**API tokens.**
- Removed `/v1/tokens`, `/v1/tokens/{token_id}`, the token validator in the auth middleware, `user_api|` principals, `Principal.Method`, and the token command, repository, queries, controllers and tests.
- From the spec: the `apiTokenBearer` scheme, the `APITokenForbidden` response, four schemas, and the `api_token_not_allowed` and `token_limit_reached` error codes.
- `AUTH_DRIVERS` is `auth0` only, in `.env.dist` and DEPLOY.md.

**Hidden aliases.**
- `person_aliases.is_public` is dropped by migration 011.
- Every alias is listed on person pages and matched by person search, ranked by `MATCH` like names; the score-0 alias branch is gone.
- Person snapshots show the document's own name and aliases. The snapshot name rules and their tests are gone.

**Draft status and MCP source.** The `revisions.status` value `draft` and the `revisions.source` value `mcp` are gone from the domain, the spec (`RevisionSource` is now `user | import`) and the schema (migration 010).

**Claims and preferences.**
- `persons.claimed_by_user_id` and the `piece_preferences` and `person_preferences` tables are dropped by migration 010.
- Also gone: the preference document fields, `PreferenceDimension`, and the importer and writer code for both.

**Importer name withholding.**
- Removed: the private-name classifier and its candidates, `NameMatcher`, `[name withheld]`, entities left out for holding a name, links hidden for a name in their URL, the hidden-alias key, the NAME review section, and the name scans and oracles.
- Email withholding (`[email withheld]`, the EMAIL review section) stays, per PLAN §7.

**Scrubber.**
- `internal/scrub` keeps only its lyric half: `lyrics.go`, `denylist.go`, and the n-gram test `TestRepositoryHasNoDenylistedText` with its repository walker.
- The name, wiki-user, email, phone, honorific and title scrubbing is gone.
- `cmd/scrub-dump` is lyric-only (`--in`/`--out`). The committed `testdata/wiki-dump` copy is frozen and was not regenerated.

**Review file write guards.** `internal/repopath` is deleted; `--review` writes the file with mode 0600.

**Tests brought to §9.**
- **Deleted:**
  - the hostile-input CPU-time tests (the five `Fuzz*` targets stay);
  - every `BARDIC_RESEARCH_DIR` research-oracle test, which per §10 is run once from BE3's branch before the H1 import;
  - the tests of test helpers and the planted-leak self-tests;
  - the generated-SQL string tests (all search integration tests and eight builder unit tests stay);
  - the schema-mirroring import validator test.
- **Mocks:** the 14 catalogue and 6 user one-method interfaces became `datasources.Catalogue` and `datasources.Users` (inside `Repository`). mockery, its mocks and `.mockery.yaml` are gone. Controller and command tests use one hand-written fake, `internal/testutil/fakerepo`.
- **Routes:** the auth matrix, routing and per-status controller tests became one table, `router/routes_test.go` `TestRoutes`. It checks route, spec and table parity, and every operation against anonymous, contributor, moderator, blocked, bad-credential and unresolvable callers through the spec-checking helper. A short list-query rule table and a few specific tests stay.
- **Review golden:** the import-review golden became per-section item counts (`TestReview_SectionItemCounts`). The 16 page goldens stay.
- **Deploy files:** `deploy_files_test.go` is cut to `TestEnvDistListsEveryEnvVar`.

**Hidden-link snapshot redaction.**
- Replaced with the §4.1 deny-set. A snapshot drops a link whose URL does not normalise or normalises to a hidden link's URL, and otherwise shows the document's own URL.
- Before, a link was shown only if it matched an existing visible row, and then under that row's spelling. Old revisions therefore now show links since deleted, moved or recased.

**Leak matrix.** It was changed only where the removals forced it: the hidden-alias, former-name and deleted, moved or recased link fixtures went. The prefers-no-page and hidden-link core stays.

**Docs.** TESTING.md, CLAUDE.md and README.md were rewritten to the plan. CLAUDE.md's old test policy (a test per documented status, the auth matrix, generated mocks) is replaced by PLAN §9 and the route table. DEPLOY.md lost everything about removed features and its rule essays; its operational runbook stays.

**Conda MySQL mode.** Kept. The sandbox has no container engine and cannot use CI's service image, so §10's condition for dropping it is not met.

### Import counts (scrubbed copy)

Withholding is gone, so the frozen dump's `Legal Name NNN` placeholders now flow through like any other text. No real names enter the repo: every name in the goldens already occurs in the frozen dump.

| Count | Before | After |
|---|---:|---:|
| Collections | 57 | 61 (matches PLAN §7) |
| Persons | 146 | 161 |
| Stub persons | 98 | 113 |
| Stub pieces | 75 | 78 |
| Links imported hidden | 14 | 0 |
| Revisions | 587 | 609 |

Unchanged: pieces 142 (135/4/2/1), bards 48, inline-lyrics pages 11, events 38, terms 129, links 563.

In the review, STUB goes 176 → 194, PERSON 189 → 204, COLL 71 → 75 and PERF 331 → 330, and the NAME section is gone.

## Migrations and their ordering

- **`010_drop_tokens_claims_preferences`**
  - Drops `api_tokens`, `piece_preferences`, `person_preferences` and `persons.claimed_by_user_id` (with its foreign key).
  - Narrows `revisions.source` to `('user','import')` and `revisions.status` to `('pending','approved','rejected','withdrawn')`.
  - Recreates the views `listed_persons` and `public_link_attachments`, which expand `persons.*`.
  - Down restores the 009 shape.
- **`011_drop_person_alias_is_public`**
  - Drops `person_aliases.is_public`.
  - Down re-adds it, marking existing rows public, with the 006 default.

**Ordering.** CLAUDE.md and DEPLOY.md say to drop or narrow only in a release after the one that stops using the old shape. That rule protects a release that is serving or one you might roll back to. Neither exists here:
- dokku deploys `main`, which holds only the initial commit and the plan;
- BE1–BE3 were never merged or deployed, and staging and production come in H1.

So each drop ships in the same release as the code that stops reading it, and DEPLOY.md records this. If a BE build has in fact been deployed, ship this code first and 010/011 one release later. The code no longer reads the dropped columns, and they are nullable or defaulted, so the old schema keeps working.

## Public API change

- **Removed:** `POST|GET /v1/tokens` and `DELETE /v1/tokens/{token_id}`, the `apiTokenBearer` security scheme, the `APITokenForbidden` response, and the two token error codes. These are the token routes and nothing else.
- **`RevisionSource`** loses `mcp`, which no route ever returned.
- **All other response shapes are unchanged.**
  - Person pages and search now list and match aliases that were hidden. This is the same shape with more data, per PLAN §2.6.
  - Revision views now follow the §4.1 deny-set.
