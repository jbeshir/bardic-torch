# Testing

The policy is [PLAN §9](PLAN.md): test the behaviour that matters at the cheapest level that proves it, one test
per rule rather than one per status code or combination. A test earns its place by catching a bug someone could
plausibly write. There are no tests of test helpers, of generated SQL strings, or of config files beyond "every
env var is in `.env.dist`".

| Command | What it runs | Needs |
|---|---|---|
| `make validate` | `fmt-check`, `lint` (golangci-lint, including `integration`-tagged files), `lint-openapi` (vacuum), `go test -short ./...`, `go build ./...` + `go build tool`, `check-generated` | Go only |
| `make test-integration` | the integration suite against a throwaway `mysql:8.4` podman container | podman |
| `make test-integration-local` | the same against a throwaway `mysqld` from a conda prefix | `MYSQL_PREFIX` |
| `make test-integration-external` | the same against an already-running MySQL | `TEST_MYSQL_ADMIN_URL` |
| `make vulncheck` | `go tool govulncheck ./...`: known vulnerabilities the code can reach | network access to vuln.go.dev |

Every change must pass `make validate`, `make vulncheck` and one of the integration targets. `make vulncheck` is
separate because it downloads the vulnerability database; offline, CI runs it. A finding is fixed by upgrading
the module or the Go version (`go.mod` and the `Dockerfile` base image). Tools (golangci-lint, vacuum, sqlc,
govulncheck) are pinned with `tool` directives in `go.mod` and run with `go tool`; nothing needs installing.

## Tiers

- **Unit tests (no build tag; `go test -short ./...`).** Domain rules in `internal/domain`: revision states,
  validation, sourced fields, the duplicate key, the stale check, URL normalisation, the snapshot views (pure
  functions over a hand-built `SnapshotVisibility`), the search tokeniser, and `TestSpec_EnumSchemasMatchDomain`
  (every enum schema in `openapi/api.yaml` lists the domain's values). Commands (`internal/command`) and controllers run over the one fake below.
- **Route table (`internal/transport/web/router/routes_test.go`).** One row per spec operation: who may call it
  (public, signed in, moderator) and a happy-path request. `TestRoutes` checks that the routes, the spec and the
  table agree, then sends each row as anonymous, contributor, moderator, blocked, invalid-credentials and
  unresolvable-user callers through `openapitest`'s `Serve`, which fails unless the request and response conform
  to the spec. `TestRoutes_Unrouted` covers requests that match no operation. Other tests beside it cover the
  Auth0 validator, CORS, security headers and request logging.
- **The fake (`internal/testutil/fakerepo`).** The one hand-written fake of `datasources.Repository` and
  `CatalogueImporter`: users in a map, canned catalogue values, an `Err` to inject. There are no generated mocks.
- **Contract helper (`internal/testutil/openapitest`).** `openapitest.New(t).Serve(t, handler, req)` validates
  request and response against the embedded spec (including closed objects, enforced by
  `TestSpec_ResponseObjectsAreClosed`). Use it for any test of a spec operation. Controller tests of list queries
  assert one case per rule (`list_query_test.go`), not per status.
- **Integration tests (`//go:build integration`, `go test -tags integration`).** Real MySQL 8.4 through
  `internal/testutil/mysqltest`, which gives each test its own migrated database (25 tables, four with FULLTEXT
  indexes), and `internal/testutil/catalogtest`, which seeds catalogue rows. They cover the queries, the approval
  write, the search, the redaction of revision views, `TestMigrations_UpDownUp` and the other schema checks, and
  `TestHTTP_EndToEnd` for the flows. `document_coverage_integration_test.go` requires every document field to map
  to a column or be listed as excluded.
- **Leak test (`internal/app/leak_matrix_integration_test.go`).** One test seeds a hidden link with credits,
  performances and citations, requests the public reads that could show it (piece, person, event and collection
  pages, lists and search, history, revisions, needs-work) and checks no response holds it. A new public read that
  could show it is added to its request list.
- **Importer tests (`internal/mediawiki`, `internal/importer`).** They run on synthetic wikis and on the committed
  scrubbed dump in `testdata/wiki-dump`, which is frozen: it is never regenerated, and the real crawl, which holds
  the wiki's lyrics, is never committed. Counts (`TestBuild_ResearchCounts` and the per-parser count tests) pin what
  the import produces; the 16 page goldens (`TestGoldenPages`) are regenerated with
  `go test ./internal/importer -run Golden -update` and the diff read before committing;
  `Fuzz*` targets check the parsers do not panic
  (`go test -fuzz FuzzFindTemplates ./internal/mediawiki/` explores further).

## Adding an endpoint

1. **Spec**: add the operation to `openapi/api.yaml` with a camelCase `operationId`, its security, closed request
   and response schemas with examples, and the statuses its handler and middleware can produce (reuse
   `components/responses`). `make lint-openapi` must stay clean.
2. **Route**: register it in `router.go`. `TestRoutes` fails until the spec, the router and the route table agree.
3. **Route table row**: add a row to `routeTable` with its `access`, a happy-path request and its success status,
   seeding the fake with what the handler reads. Authorisation is then checked for every kind of caller.
4. **One test per rule**: a controller or command test for each validation or business rule the endpoint adds
   (not one per status).
5. **Integration**: tests for new queries, and extend `TestHTTP_EndToEnd` when a user-visible flow changes.

## Integration test modes

All three modes run `scripts/test-integration.sh <mode>`, which builds `cmd/migrate`, waits until the
`bardic_torch` database accepts connections, runs `migrate up` and `migrate version`, and then runs
`go test -tags integration -count=1 ./...` with `TEST_MYSQL_ADMIN_URL` set and any extra flags from `GOTESTFLAGS`
(for example `make test-integration-local GOTESTFLAGS=-race`; the race detector needs cgo, which CI has and the
sandbox Go image may not). Throwaway servers listen on a random free `127.0.0.1` port, keep their files in a
private temporary directory removed on exit, and start with durability flags (`throwaway_server_flags` in the
script) that trade crash safety for speed and change no SQL semantics.

- **podman**: `make test-integration`. Starts `docker.io/library/mysql:8.4` and removes it on exit.
- **local (conda)**: `make test-integration-local [MYSQL_PREFIX=<prefix>]`. Starts `mysqld` from a conda-forge
  `mysql-server` 8.4 prefix, with no container engine, which is how sandboxes without podman run the suite.
  `MYSQL_PREFIX` defaults to `.mysql-local` (gitignored). Create it once with `make mysql-local-install` (needs
  conda), which runs `conda create -y -p .mysql-local -c conda-forge --override-channels mysql-server=8.4.2`; the
  version pin is required because conda-forge's newest builds are 9.x. The package has no `mysql` or `mysqladmin`
  client, so readiness is a TCP poll followed by retrying `migrate version`. The server is started with
  `--character-set-server=utf8mb4 --collation-server=utf8mb4_0900_ai_ci` (the default is utf8mb3) and an empty
  root password, so `TEST_MYSQL_ADMIN_URL=mysql://root@127.0.0.1:<port>`.
- **external**: `TEST_MYSQL_ADMIN_URL=mysql://root:<password>@127.0.0.1:3306 make test-integration-external`.
  The URL names an account that may create and drop databases and has no database path; the server must already
  have a `bardic_torch` database.

To run one integration test against a running server:

```
TEST_MYSQL_ADMIN_URL=mysql://root:<password>@127.0.0.1:3306 go test -tags integration -count=1 -run TestHTTP_EndToEnd ./internal/app/
```

## CI

`.github/workflows/ci.yml` runs on every push and pull request: **validate** (`make validate`), **vulncheck**
(`make vulncheck`), **integration** (a `mysql:8.4` service container, then `make test-integration-external` with
`GOTESTFLAGS=-race`) and **docker** (`make docker-build`, which catches a broken `Dockerfile`, as `make validate`
does not build the image). Dependabot opens weekly pull requests for Go modules (pinned tools grouped), GitHub
Actions and the Dockerfile's base images; actions are pinned to a full commit SHA with the release tag in a
trailing comment. A tool update can make MVS downgrade another tool's dependency; check `go.mod` in those pull
requests.
