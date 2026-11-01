# Testing

| Command | What it runs | Needs |
|---|---|---|
| `make validate` | `fmt-check`, `lint` (golangci-lint, including `integration`-tagged files), `lint-openapi` (vacuum), `go test -short ./...`, `go build ./...` + `go build tool`, `check-generated` | Go only |
| `make test-integration` | the integration suite against a throwaway `mysql:8.4` podman container | podman |
| `make test-integration-local` | the integration suite against a throwaway `mysqld` from a local mysql-server prefix | `MYSQL_PREFIX` (see below) |
| `make test-integration-external` | the integration suite against an already-running MySQL | `TEST_MYSQL_ADMIN_URL` |
| `make vulncheck` | `go tool govulncheck ./...`: known vulnerabilities in the Go standard library and modules that the code can reach | network access to vuln.go.dev |

Every change must pass `make validate`, `make vulncheck` and one of the integration targets. `make vulncheck`
is separate from `make validate` because it downloads the vulnerability database; offline, CI runs it. A
finding is fixed by upgrading the affected module or the Go version (`go.mod` and the `Dockerfile` base image).
Tools (golangci-lint, vacuum, sqlc, mockery, govulncheck) are pinned with `tool` directives in `go.mod` and run
with `go tool`, so nothing needs installing.

## Test layers

### Unit tests (no build tag)

- **Domain** (`internal/domain`): pure functions and types, e.g. `TestNormalizeDisplayName`, `TestParseRole`,
  `TestCheckTextCharacters` (the character rule shared by display names and token names, including the cap on
  combining marks per base character) and `TestHashAPITokenSecret`.
- **Catalogue domain** (`internal/domain`): `TestEnums_ValidAndParse` and the `Parse*` table tests; `ParseSearchQuery`
  (`TestParseSearchQuery`: the tokeniser rules, including `_` as a word character, the 84-character token limit and
  combining marks); `DecodeDocument` (`TestDecodeDocument_*`: unknown fields ignored, unknown vocabulary values and
  bad dates rejected, `TestDocuments_EnumFieldsAreValidated`); the snapshot views (`snapshot_test.go`, pure
  functions over a hand-built `SnapshotVisibility`); and `TestSpec_EnumSchemasMatchDomain`, which loads
  `openapi/api.yaml` and requires every named enum schema to list exactly the values, in order, of the domain
  vocabulary it describes (`specEnums` in `spec_enums_test.go` maps each schema to its list; a new enum schema
  fails until it is added). Inline enums (error codes, changed-field names, entity-type tags) are not compared.
- **Search and needs-work builders** (`internal/datasources/mysql/search_sql_test.go`, no database): the list
  statements (`search_sql.go`, `piece_search.go`, `person_search.go`, `collection_list.go`, `needs_work.go`) are
  built in Go, so a fake `statementRunner` records each statement. The tests pin the round trips (at most three for a
  piece search), one placeholder per argument, and the `MAX_EXECUTION_TIME` hint on the top-level `SELECT`
  (`requireWellFormed`); each count branch leaving out exactly its own filter (`TestPieceSearch_StatementShapes`,
  `TestPieceSearch_EveryTermFacet` for every term facet's column); the person builder
  (`TestPersonSearch_Statements`, `TestPersonSearch_InjectionInputs`); and `TestNeedsWork_Statements` (nil and every
  reason, and an unknown reason is an error). `FuzzPieceSearchSQL` checks that the SQL text depends only on a
  search's shape, never on the bytes of `q` or a term (`go test -fuzz FuzzPieceSearchSQL
  ./internal/datasources/mysql/` explores further; plain runs use the seed corpus). `TestFetch_QueryTimeout` and
  `TestMigratorConfig` cover the timeout mapping and the migrator's connection settings.
- **Commands** (`internal/command`): business rules over the small datasource interfaces in
  `internal/datasources`, using the mockery mocks in `internal/datasources/mocks`. Mocks are generated from
  `.mockery.yaml` (every interface in `internal/datasources`) by `make generate`; never edit them by hand.
  `make check-generated` regenerates sqlc and mockery output in a temporary copy and fails on any difference.
  Commands take their clock as `Now func() time.Time`, so tests use fixed times. Examples:
  `TestCreateAPIToken_InvalidNames` (the length limit and the character rule),
  `TestSetUserBlocked_RetriesContributorCreatedDuringUpdate` (a contributor created between the update and the
  lookup is updated by one retry) and `TestSetUserBlocked_ContributorNeverMatchingFails` (an internal error, not
  `ErrModeratorNotBlockable`).
- **Middleware and validators** (`internal/transport/web/router`): `TestAuthMiddleware_*`, `TestRequireAuth_*`,
  `TestRequireNonAPIToken_*`, `TestRequireModerator_*`, `TestCORSMiddleware_*`, `TestSecurityHeaders_*`,
  `TestRequestLogger_*`, `TestAPITokenValidator_*` (mocks) and `TestAuth0Validator_*`, which validate real RS256
  JWTs against a local issuer from `internal/testutil/auth0test` (an `httptest` server serving the OpenID
  discovery document and a JWKS). `TestAuth0Validator_InvalidTokens` includes the attack cases, built with
  `auth0test.SignWithHeader` where they need a crafted header: `alg: none`, HS256 keyed with the public key,
  RS384, an unknown, missing or empty `kid`, a `crit` header, a signature by another key under the issuer's
  `kid`, a wrong, missing or slash-less issuer, a wrong or missing audience, `nbf` and `exp` beyond the clock
  skew (the within-skew cases are in `TestAuth0Validator_ValidTokens`), a future `iat`, a missing `exp`, and
  client-credentials (`gty` or `@clients`) tokens; each case asserts the reason it is rejected, so it cannot
  pass by failing an earlier check. The validator's own header check rejects the missing `kid` and the `crit`
  header, which the JWT library would accept. Two library behaviours that only a token the issuer signs can
  reach are pinned as they are: an `exp` holding a numeric string is read as that date (`string_expiry` is
  accepted, `expired_string_expiry` rejected), and the JWS JSON serialization is rejected; only the compact
  form is accepted. `TestAuth0Validator_OversizedTokenIsRejectedBeforeParsing` pins the token size cap, and
  `TestAuth0Validator_UnavailableKeysAreNotInvalidCredentials` pins that a key-fetch failure is
  `errSigningKeysUnavailable` (500) while tokens rejected before a key is needed stay 401.
  `TestAuth0KeyCache_*` pin the signing-key cache: one fetch per `auth0KeysCacheTTL`, stale keys served while a
  single background refresh runs, stale keys no longer served once that refresh fails, the refresh and the
  first fetch outliving the request that starts them, concurrent requests with no keys cached sharing one fetch
  and its success or failure while each stops waiting when its own context ends, and a failed fetch reported as
  unavailable keys (500, not 401) with nothing cached. They control the clock and hold fetches on channels, and
  only poll to wait for a background refresh to finish, so no outcome depends on timing.
- **Server, JSON helpers and wiring** (`internal/transport/web/server`, `internal/transport/web/httpjson`,
  `internal/app`): `TestServer_ShutdownWaitsForInFlightRequest`, `TestServer_HeaderSizeLimit` (431 over 16 KiB
  of request headers), `TestDecodeJSONBody` (the exact client-facing messages) and
  `TestDecodeJSONBody_LogsDecodeErrorAtDebug`, `TestNewHTTPHandler_UnroutedRequests` (JSON 404 for an unknown
  path, 405 with `Allow` for a wrong method, neither authenticating) and `TestNewHTTPHandler_PreflightToUnknownPath`,
  `TestAuth0IssuerURL`, `TestAuthValidatorsFromEnv_*`, `TestStoredTime_UTCWholeSeconds` and
  `TestSetup_InvalidDatabaseURL`.
- **Deployment files** (`internal/app/deploy_files_test.go`): `TestEnvDistListsEveryEnvVar` parses the non-test
  Go code under `cmd/` and `internal/` (except `internal/testutil`) and fails unless `.env.dist` lists exactly
  the environment variables it reads. `TestAppJSON`, `TestProcfile` and `TestDockerfile_MatchesDokkuConfiguration`
  pin the dokku configuration: the predeploy migration, the `/readyz` startup check, the web command, and a
  Dockerfile with no `ENTRYPOINT` or `EXPOSE`.

### Contract tests

`internal/testutil/openapitest` checks traffic against the embedded `openapi/api.yaml`:

```go
contract := openapitest.New(t)
rec := contract.Serve(t, handler, req)
```

`Serve` fails the test unless the request and the response both conform to the spec, including an undocumented
route, method, status code or content type, a missing required field, an extra field (every response object has
`additionalProperties: false`, enforced by `TestSpec_ResponseObjectsAreClosed`) or a body on a response
documented without one. `ServeResponseOnly` skips request validation, for tests that deliberately send what the
spec forbids (malformed or oversized bodies, wrong content type); the response is still checked. The helper's own
behaviour is pinned by the `TestValidator_*` meta-tests.

Every controller test (`internal/transport/web/controller/*_test.go`) and every handler test of a spec operation
goes through the helper. Tests of unrouted requests (`TestNewHTTPHandler_UnroutedRequests`,
`TestNewHTTPHandler_PreflightToUnknownPath`) have no operation to validate against, so they assert the `Error`
body directly. Controller tests set the principal directly in the request context and drive one status per case.
`TestAPITokenCreate_NameRulesWithCommand` runs the real `command.CreateAPIToken` (over mocked datasources)
through the helper, so the command's name rules are checked against the spec's `maxLength`.

### Router tests: route parity and the auth matrix

- `TestRouter_RoutesMatchOpenAPI` (`router/router_test.go`) walks the route table and fails, listing the
  difference, if a route × method is missing from the spec or a spec operation has no route. It also checks
  that the router built from the table routes each entry's method and path to that entry.
- `TestRouter_Routing` (`router/router_routing_test.go`) sends a fixed corpus through `router.New` and asserts
  every observable part of each answer: status, `Allow`, `Location`, CORS and security headers, `Content-Type`,
  body, whether authentication ran, the ID the controller received and the logged route. It covers every
  route × method (plus `HEAD` and `OPTIONS`), unrouted paths with invalid credentials (404/405, never 401),
  path-cleaning redirects, trailing slashes, encoded literal segments and percent-encoded IDs. A routing change
  that alters any of them must change the corpus.
- `TestRouter_AuthMatrix` (`internal/app/http_handler_auth_matrix_test.go`) builds the production handler with
  `app.NewHTTPHandler`, fake prefix validators (`Bearer auth0|<user id>`, `Bearer user_api|<user id>`) and mocks,
  and sends a request as every principal to every operation in the spec: anonymous, unrecognised credentials,
  invalid credentials, credentials the validator cannot check, Auth0 contributor, Auth0 moderator, API-token
  contributor, API-token moderator, blocked Auth0 contributor, blocked API-token contributor, and a user whose
  lookup fails. Each cell asserts the status, the error `code` (none on success) and the contract. Each operation
  also gets a preflight row: `OPTIONS` with every principal's credentials gets 204 and never reaches
  authentication.

  The expected outcomes live in that one file: one `access` table per authorisation middleware (`public`,
  `authenticatedUser`, `interactiveUser`, `moderator`), listing every principal, and `authMatrix`, mapping each
  operation to its access and a request that succeeds when allowed. The list of operations comes from the spec,
  so a new operation without a row fails the test.

  `public` operations (the health checks and every catalogue read) are registered on the root router, outside
  the `/v1` auth middleware, so they answer any credentials, even invalid, unverifiable or a blocked user's, as
  they would an anonymous request, and never look a user up.
- `TestHTTP_LeakMatrix` checks that no public read shows hidden data; see [Leak matrix](#leak-matrix).

### Integration tests (`//go:build integration`)

Run against real MySQL 8.4 by the `test-integration*` targets:

- **Repository** (`internal/datasources/mysql/*_integration_test.go`): every query in `queries.sql`, migrations up
  → down → up (`TestMigrations_UpDownUp`), column collations (`TestMigrations_ColumnCollations`), case-sensitive
  IDs, trailing-space-significant user IDs (`TestRepository_UserIDsTrailingSpacesAreSignificant`), idempotent
  revoke/block, FK cascade, FK and unique-hash violations asserted by MySQL error number (1452/1062), a 4-byte
  UTF-8 round trip (`TestRepository_EnsureUser_StoresFourByteCharacters`), throttled last-used updates.
- **Migrate command** (`cmd/migrate/main_integration_test.go`): `TestMigrateRun_UpVersionDown` (up, version and
  down through `run`), `TestMigrateRun_UpWithNewerSchemaIsNoChange` (up on a clean schema newer than every
  embedded migration, as when rolling back a release, succeeds and applies nothing) and
  `TestMigrateRun_UpWithDirtySchemaFails` (up on a dirty schema fails with `migrate.ErrDirty`, whether or not
  its version is newer).
- **End to end** (`internal/app/http_handler_integration_test.go`): `TestHTTP_EndToEnd` serves the production
  handler (`NewHTTPHandler` with Setup's own `repositoryHTTPDependencies`) over a real repository, the real Auth0
  validator against a local `auth0test` issuer and the real API-token validator, plus the real
  `BootstrapModerators` command. It walks a user's life: expired, wrong-audience, badly signed and oversized
  Auth0 tokens get the spec's 401 `unauthenticated` body without creating the user, the first valid request
  creates the user, rename persists, an
  API token authenticates as them, a bootstrapped moderator blocks them (both credentials get 403
  `user_blocked`), unblocks them, the token is revoked (401), and a second bootstrap run without the moderator's
  ID does not demote them, and bootstrapping the blocked contributor makes them an unblocked moderator. It also
  asserts that a token's `created_at` from create equals the listed one. Every response goes through the contract
  helper.
- **Setup** (`internal/app/app_integration_test.go`): `TestSetup_BootstrapsModeratorsAndServes` (Setup from the
  environment bootstraps the moderators, serves `/readyz` on a real listener and returns after cancel),
  `TestSetup_BootstrapFailureFailsStartup` and `TestStoredTime_RoundTripsThroughDatabase` (a clock with a
  fractional second stores and returns the same whole-second `created_at`).

`internal/testutil/mysqltest` gives each test its own database: `mysqltest.NewDatabase(t)` (a `*sql.DB`) or
`mysqltest.NewDatabaseURL(t)` (a `DATABASE_URL`-style URL) creates `bt_test_<random hex>` through
`TEST_MYSQL_ADMIN_URL`, applies every migration, and drops it when the test ends, so integration tests can run
in parallel. A missing `TEST_MYSQL_ADMIN_URL` fails the test rather than skipping it.

### Catalogue tests

A migrated database is expensive (timings under [Integration test modes](#integration-test-modes)), and MySQL
serialises much of that DDL, so parallel tests do not hide it. Read-only tests therefore share a
seeded database (see catalogtest), and new private-database tests should be kept to what needs one.

- **Schema** (`internal/datasources/mysql/*schema*_integration_test.go`, `migrate_integration_test.go`):
  - `TestMigrations_UpDownUp` checks the exact sets of tables and views after up, none after down and the
    version, and `TestMigrations_ViewsCheckTheReader` that every view is `SQL SECURITY INVOKER`;
  - `TestMigrations_ColumnCollations` has an exact map of the non-default collations (user IDs `utf8mb4_0900_bin`;
    and `links.url` `utf8mb4_0900_bin`; slugs, `sort_letter`, `normalized_url`, `platform` and token columns
    `ascii_bin`) and requires every other collated column of a base table to be `utf8mb4_0900_ai_ci` (views'
    columns have their tables' collations);
  - `TestMigrations_EnumColumns` builds the expected `column_type` of every ENUM column from the Go constants in
    `internal/domain`, so value order matters, and fails for an ENUM column of a base table that is not listed;
  - `TestMigrations_Constraints` breaks one CHECK, unique or foreign-key rule per case and asserts the MySQL error
    number (3819, 1062, 1451, 1452) after `seedCatalogue` has shown that valid rows are accepted;
    `TestMigrations_ConstraintsAllowCascades` and `TestMigrations_PersonAliasHiddenByDefault` cover the cascades
    and the fail-closed `is_public` default.
- **Stopwords** (`TestSearchStopwords_MatchInnoDB`): `domain.ParseSearchQuery` must drop every word of the server's
  `INFORMATION_SCHEMA.INNODB_FT_DEFAULT_STOPWORD`, and keep words that are not on it.
- **Session time limit** (`TestConnect_SelectTimeLimit`): a connection from `ConfigFromURL` has
  `max_execution_time` 2000, the server stops a `SELECT SLEEP(5)` after 2 s, and an `INSERT … SELECT SLEEP(3)`
  runs to the end. `TestMigratorConfig` (unit) checks that the migrator clears the limit, and
  `TestFetch_QueryTimeout` that a sqlc read the server stops is `datasources.ErrQueryTimeout`.
- **Document coverage** (`TestDocumentCoverage`, `TestDocumentFieldPaths`): classifies every column of the
  tables each snapshot document owns as a document field or system-owned, with a reason, and every document field
  as stored by exactly one column. A new column, document field or table fails it until it is classified.
- **catalogtest** (`internal/testutil/catalogtest`) seeds catalogue fixtures with plain SQL from the
  `internal/domain` snapshot documents: `c := catalogtest.New(t, db)` then `c.Term`, `c.Person`, `c.Piece`,
  `c.Collection`, `c.Event`, `c.Revision`, `c.SetLinkState` and `c.Exec` (for system columns no document holds).
  Approved entities (the zero value) get an approved `import` revision as their current revision; `Unapproved:
  true` leaves it NULL. Term closure rows come from a recursive CTE, so parents are seeded first. Slugs, sort keys
  and letters are naive defaults (tests about them pass literals). Every call commits before it returns, because
  FULLTEXT sees committed rows only. The package's own tests are `catalogtest_test.go`.
  - **Shared read-only fixture**: tests that only read share one seeded database per fixture through
    `fx := catalogtest.NewShared(seed)` and `db, refs := fx.Get(t)`. The first `Get` creates and seeds it; the
    pool is opened with `transaction_read_only=ON`, so a stray write fails with error 1792. Use one `Shared` per
    independent fixture, because counts that several features assert on break when anyone adds data.
  - **`TestMain` is required**: a package that uses `Shared` must have `func TestMain(m *testing.M) {
    catalogtest.Main(m) }`, which drops the shared databases after `m.Run`; `Get` fails without it.
  - Tests that write, or that need statistics a shared fixture would disturb (FULLTEXT scoring, `ALTER TABLE`),
    use `mysqltest.NewDatabase(t)` and `catalogtest.New`.
- **Search integration** (`search_integration_test.go`, `search_text_integration_test.go`): the semantics against
  hand-computed sets on a shared `searches` fixture: facet OR/AND and descendants, disjunctive counts (two cases
  where each selected facet has a piece that only its own selection keeps out, so conjunctive counting fails them),
  letter counts, kinds, prefix fallback, hidden aliases never matching, no-page persons matching by name only, and
  injection-shaped input for pieces and persons (LIKE wildcards, FULLTEXT operators, SQL text). Sort orders are
  asserted on fixtures where the sort key differs from the title or name (`sort_title`, `sort_name`) and from
  insertion order. `piece_facets_integration_test.go` has its own small `facetSearches` fixture with a term in
  every term facet but affiliation: `TestRepository_ListPublicPieces_EachTermFacet` selects each in turn, and
  `TestRepository_ListPublicPieces_ThreeTermFacets` selects an origin, a topic and a mood with hand-computed counts. The text searches also run after `ALTER TABLE … FORCE` rebuilds the FULLTEXT indexes, and
  `TestSearchStatements_ExecutionTimeLimit` checks that the server enforces the time limit (error 3024 → 500).
- **Snapshot views** (the pure functions are unit-tested, see above): `repository_revision_integration_test.go`
  checks them against current database state (an alias, links and a person hidden after the revision was written,
  corrupt snapshots, bases). `repository_redaction_integration_test.go` covers the fail-closed rules: a former
  name that is now a hidden alias, with and without a page; links deleted, moved and hidden, or hidden under
  another case, since the revision; and a person's performances on links with no public attachment.
  `TestRepository_GetPublicRevision_Undecodable` checks that a stored revision of another schema version, or with
  an unknown vocabulary value, is an error other than not-found, and one with an extra field is served. `needs_work_integration_test.go` covers the
  needs-work reasons.
- **Repository reads** (`repository_*_integration_test.go`): exact structs for details, lists and caps (201 → 200
  with a total), each on a shared fixture built so that position, sort key, insertion order and alphabetical order
  disagree where the read must follow one of them (credits and links by position, contributors, needs-work and
  collection sort keys, a term tree whose pre-order differs from breadth-first), plus `TestRepository_CatalogueReads_UnknownEnumValue` (an unknown ENUM
  value is an error, not a 404).
- **FULLTEXT sees committed rows only.** InnoDB's FULLTEXT index is updated at commit, so a search never sees
  rows in an open transaction, and a fixture that searches inside the transaction that inserted would find nothing.
  catalogtest commits on every call. Production code must follow the same rule: BE4 searches only after its
  approval transaction commits.

### Leak matrix

`TestHTTP_LeakMatrix` (`internal/app/leak_matrix_integration_test.go`) sends requests to every public catalogue
read through the production handler over a real repository holding a shared fixture of hidden data: a hidden legal
alias, a `prefers_no_page` person with notes, aliases, roles, an affiliation, relations, credits, performances and
a link, a hidden link, unapproved pieces, persons, terms, collections and events referred to by public ones,
performances of a public person on links attached only to no-page or unapproved persons, persons renamed since
their first revision whose former name is now a hidden alias (with and without a page), a piece whose first
revision holds links since deleted, moved and hidden, or hidden under another case, and never-exposed system
fields (legacy titles, claims, normalized URLs). Every marked string contains `leakmark`, and
each raw response body must not contain it (case-insensitively), except for the strings a request lists in
`reveals` (for example the no-page person's identity on their own page or in a person list). A reveal is exact: it
removes only a whole JSON string value equal to it, quotes included and case-sensitive (`leakedMarker` in
`leak_scan_test.go`), so a longer string that merely starts with a revealed name, such as that person's notes, is
still a leak. An error message is revealed as the whole message (`unknown term: <slug>`). A reveal the response does
not show as a whole value fails the row (`unusedReveals`), so a row cannot list one that does nothing. Each
response also goes through the contract helper. `requireLeakMarkersStored` checks with SQL that each kind of marker is stored, so
the matrix cannot pass vacuously. Three self-tests keep the check honest: `TestLeakedMarker` and
`TestUnusedReveals` (unit) run the scanner on synthetic bodies with planted leaks next to revealed strings, and
`TestHTTP_LeakMatrix_PlantedLeakFails` plants a no-page note next to the revealed name in a real response.
`TestHTTP_LeakMatrix_RedactedOnlyRevisionsChangeNothing` reads two revisions that differ from their base only in
redacted data and requires `changed_fields` to be `[]`.

`leakMatrix` maps each operation to its requests, and `leakExempt` lists, with a reason, the operations that read no
catalogue data. Every spec operation must be in exactly one of them, and a public `/v1` GET cannot be exempt.

**To add a row** for a new public read:

1. Add an entry to `leakMatrix`, keyed by the spec operation, returning requests that reach every list the
   endpoint shows (members, citations, performances, facets, history) and at least one 404 for a non-public entity:
   ```go
   {Method: http.MethodGet, Path: "/v1/terms/{slug}"}: func(fx leakFixture) []leakRequest {
       return []leakRequest{
           {path: "/v1/terms/westmarch", status: http.StatusOK},
           {path: "/v1/terms/leakmark-unapproved-term", status: http.StatusNotFound},
       }
   },
   ```
2. If the endpoint legitimately shows a marked string, list it in `reveals` as the whole JSON string value it
   shows (a name, a slug, or a whole error message); `absent` lists strings that must not appear even though they
   are unmarked.
3. If the endpoint needs data the fixture lacks, add it to the `leakCatalogue` seed with `leakmark` in every string
   that must stay hidden, and a `catalogtest.Ref` field on `leakFixture` if requests need it.
4. Check the test can fail: remove the visibility filter from the query under test and confirm the matrix
   reports it.

### Catalogue end to end

`TestHTTP_CatalogueEndToEnd` (`internal/app/http_handler_catalogue_integration_test.go`) builds the production
handler like `TestHTTP_EndToEnd`, seeds a small invented catalogue (a term tree, a person with a hidden alias,
pieces including a stub and an unapproved draft, and a second approved revision) with catalogtest, and walks list →
facet filter (counts) → search → piece, term and person detail → history → revision detail → needs-work. The
reads are public, so it sends no credentials, and every response goes through the contract helper.

## Adding an endpoint

1. **Spec**: add the operation to `openapi/api.yaml` with a camelCase `operationId`, its security, closed
   request and response schemas with examples, and **every status its handler and middleware can produce**
   (reuse `components/responses` such as `Unauthenticated`, `Forbidden`, `Internal`). `make lint-openapi` must
   stay clean.
2. **Route**: add it to the route table in `router.routes`: method, path pattern, handler behind the right
   `require*` middleware, and `authenticated: true` unless it is public.
   `TestRouter_RoutesMatchOpenAPI` fails until the spec and the router agree.
3. **Auth matrix**: add a row to `authMatrix` with the operation's `access` and a successful request (path
   parameters, body, success status, mock expectations). `TestRouter_AuthMatrix` fails until you do. A new
   authorisation rule gets a new `access` table listing every principal.
   A public catalogue read (`security: []`, registered on the root router like the existing ones, before the `/v1`
   subrouter) also needs a **leak-matrix row** (see "Leak matrix"): requests that reach every list it shows, with
   `reveals` for anything it may legitimately show. Any other operation goes in `leakExempt` with a reason. List
   endpoints also need their query-parameter cases (invalid, repeated, out-of-range) as `ServeResponseOnly` tests.
4. **Per-status tests**: a contract-validated controller test case for every documented status the controller
   produces (`Serve`, or `ServeResponseOnly` for deliberately invalid requests). Middleware statuses (401, 403,
   500 from a failed credential check or user lookup) are covered by the matrix.
5. **Integration**: integration tests for every new query, and extend `TestHTTP_EndToEnd` when the endpoint
   changes a user-visible flow.

## Integration test modes

All three modes run `scripts/test-integration.sh <mode>`, which builds `cmd/migrate`, waits until the
`bardic_torch` database accepts connections, runs `migrate up` and `migrate version` on it, and then runs
`go test -tags integration -count=1 ./...` with `TEST_MYSQL_ADMIN_URL` set and any extra `go test` flags from
`GOTESTFLAGS` (default none), e.g. `make test-integration-local GOTESTFLAGS=-race`. The race detector needs
cgo, which CI has but the sandbox Go image may not, so only CI sets it. Throwaway servers listen on a random
free `127.0.0.1` port and keep their files in a private temporary directory removed on exit, so concurrent runs
do not collide.

Both throwaway servers (podman and local) also start with durability flags, defined once in
`scripts/test-integration.sh` (`throwaway_server_flags`): `--skip-log-bin --innodb-flush-log-at-trx-commit=0
--innodb-doublewrite=OFF --innodb-flush-method=nosync`. They trade crash safety for speed and change no SQL
semantics; the data is discarded on exit. Creating the 27 tables (25 of them the catalogue's; four have FULLTEXT indexes, each of which
creates auxiliary tables) for every test database otherwise spends most of its time waiting for disk syncs: a
migrated database takes about 2.4 s with the flags and 7.8 s without. The external mode runs against a server you
own and does not change its settings, so it is slower per database.

- **podman** — `make test-integration`. Starts `docker.io/library/mysql:8.4` with a random root password,
  published on `127.0.0.1:<port>`, waits for `mysqladmin ping --protocol=tcp` inside the container, and removes the
  container on exit.
- **local** — `make test-integration-local [MYSQL_PREFIX=<prefix>]`. Starts `mysqld` from a conda-forge
  mysql-server 8.4 prefix; no container engine needed. `MYSQL_PREFIX` defaults to `.mysql-local` (gitignored);
  create it once with `make mysql-local-install` (needs conda), which runs
  `conda create -y -p .mysql-local -c conda-forge --override-channels mysql-server=8.4.2`.
- **external** — `TEST_MYSQL_ADMIN_URL=mysql://root:<password>@127.0.0.1:3306 make test-integration-external`.
  Uses a running server; the URL names an account that may create and drop databases and has **no** database
  path, and the server must already have a `bardic_torch` database (CI creates it with `MYSQL_DATABASE`).

To run a single integration test against a running server:

```
TEST_MYSQL_ADMIN_URL=mysql://root:<password>@127.0.0.1:3306 go test -tags integration -count=1 -run TestHTTP_EndToEnd ./internal/app/
```

## MySQL without a container engine

Sandboxes and machines without podman or docker can still run the integration suite: a spike on 2026-10-31
confirmed that conda-forge's mysql-server prefix is self-contained (RPATH'd libraries) and its `mysqld` runs in
a plain Debian Go image with no network access.

- Provision once, somewhere with conda and package-manager access:
  ```
  conda create -y -p <prefix> -c conda-forge --override-channels 'mysql-server>=8,<9'
  ```
  This resolves to `mysql-server 8.4.2` (+ `mysql-common 8.4.2`), about 202 MB. conda-forge's newest builds are
  9.x, so the version constraint (or `=8.4.2`, as `make mysql-local-install` uses) is required. The package has
  `mysqld` but no `mysql` or `mysqladmin` client.
- The local mode then runs, with a private temporary directory for the data directory, socket and pid file:
  ```
  mysqld --no-defaults --initialize-insecure --user=$(id -un) --basedir=<prefix> --datadir=<tmp>/data
  mysqld --no-defaults --user=$(id -un) --basedir=<prefix> --datadir=<tmp>/data \
         --port=<port> --bind-address=127.0.0.1 --mysqlx=OFF \
         --socket=<tmp>/mysqld.sock --pid-file=<tmp>/mysqld.pid --log-error=<tmp>/mysqld.log \
         --character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci \
         --skip-log-bin --innodb-flush-log-at-trx-commit=0 --innodb-doublewrite=OFF \
         --innodb-flush-method=nosync \
         --init-file=<tmp>/init.sql &
  ```
  The last line of flags is the throwaway server's durability trade-off (see "Integration test modes"); leave it
  off for a server whose data matters.
  Initialisation takes about 7 s and start-up about 1 s. Without `--character-set-server=utf8mb4` this build
  defaults to utf8mb3, so always pass it. `--user=$(id -un)` lets it run as root. With no client binaries,
  readiness is a TCP poll of the port followed by retrying a Go connection (`migrate version`). The root account
  has an empty password: `TEST_MYSQL_ADMIN_URL=mysql://root@127.0.0.1:<port>`.
- Then run `make test-integration-local MYSQL_PREFIX=<prefix>`, pointing `MYSQL_PREFIX` at wherever the prefix
  was provisioned.

## CI

GitHub Actions (`.github/workflows/ci.yml`, on every push and pull request) runs four jobs:

- **validate**: `make validate`, with the Go version from `go.mod`.
- **vulncheck**: `make vulncheck`, with the Go version from `go.mod`, so a newly published advisory for the
  standard library or a module the code reaches fails CI.
- **integration**: a `mysql:8.4` service container with `MYSQL_DATABASE=bardic_torch` and a TCP health check
  (`mysqladmin ping -h 127.0.0.1 --protocol=tcp`), then `make test-integration-external` with
  `TEST_MYSQL_ADMIN_URL=mysql://root:root@127.0.0.1:3306` and `GOTESTFLAGS=-race`, so the integration suite
  runs under the race detector.
- **docker**: `make docker-build CONTAINER_ENGINE=docker`, which builds the production image from the
  `Dockerfile`. `make validate` does not build the image, so this job is what catches a broken Dockerfile before a deploy.

Dependabot (`.github/dependabot.yml`) opens weekly pull requests for Go modules (the pinned tools in one group,
other minor and patch updates in another, major updates separately), GitHub Actions (in one group) and the
Dockerfile's base images. The workflow pins every action to a full commit SHA with its release tag in a trailing
comment (`actions/checkout@<sha> # v7.0.1`); dependabot updates both. A tool update can make MVS downgrade
another tool's dependency; check `go.mod` in those pull requests.
