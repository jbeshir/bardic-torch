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
- `TestHTTP_LeakMatrix` (`internal/app/leak_matrix_integration_test.go`, integration) sends requests to every
  public catalogue read through the production handler over a real repository holding a shared fixture of
  hidden data: a hidden legal alias, a `prefers_no_page` person with notes, aliases, roles, an affiliation,
  relations, credits, performances and a link, a hidden link, unapproved pieces, persons, terms, collections and
  events referred to by public ones, and never-exposed system fields (legacy titles, claims, normalized URLs).
  Every marked string contains `leakmark`, and each raw response body must not contain it (case-insensitively),
  except for the strings a request lists in `reveals` (the no-page person's identity on their own page). Each
  response also goes through the contract helper. `leakMatrix` maps each operation to its requests, and
  `leakExempt` lists, with a reason, the operations that read no catalogue data. Every spec operation must be in
  exactly one of them, and a public `/v1` GET cannot be exempt, so a new catalogue read fails until it has a row.

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

A migrated database costs about 2.5 s, and MySQL serialises much of that DDL, so parallel tests do not hide
it. `internal/testutil/catalogtest` seeds catalogue fixtures with plain SQL from the `internal/domain` snapshot
documents. Approved entities get an approved revision as their current revision; `Unapproved: true` leaves it
NULL. Term closure rows are built by a recursive CTE. Every call commits before it returns, because FULLTEXT sees
committed rows only. Tests that only read should share one seeded database per fixture through
`catalogtest.NewShared(seed)` and `Get(t)`, with `catalogtest.Main` as the package's `TestMain`. The shared pool
is read-only (`transaction_read_only`), so a stray write fails with error 1792. The package doc has examples.

`TestDocumentCoverage` (A1.8) classifies every column of the tables each snapshot document owns as a document
field or system-owned, with a reason. A new column, document field or table fails it until it is classified.

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
   A public catalogue read (`security: []`, registered on the root router like the existing ones) also needs a
   `leakMatrix` row whose requests reach every list it shows; any other operation goes in `leakExempt`.
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
         --init-file=<tmp>/init.sql &
  ```
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
