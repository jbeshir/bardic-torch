# CLAUDE.md - bardic-torch (Go API)

[docs/PLAN.md](docs/PLAN.md) is the authoritative product and implementation spec: data model, API surface,
moderation rules and build sequence. Read the relevant sections before changing behaviour, and update it when a
decision changes. Its preamble sets the bar: proportion. Build the plainest thing that does the job, and test what
matters at the cheapest level that proves it; the test policy is [PLAN §9](docs/PLAN.md) (how to run each tier and
add a route: docs/TESTING.md).

## Validation

To validate changes in this repository, run the following automated checks:

- Format check, lint, OpenAPI lint, unit tests, build and generated-code check: `make validate`
- Integration tests against real MySQL 8.4: `make test-integration` (podman), or
  `make test-integration-local MYSQL_PREFIX=<prefix>` without a container engine (see docs/TESTING.md)
- Known vulnerabilities reachable from the code: `make vulncheck` (govulncheck). It is not part of
  `make validate` because it needs network access to vuln.go.dev; where that is unreachable, say so and leave it
  to CI. Fix a finding by upgrading the module or the Go version (`go.mod`, the `Dockerfile` base image), never
  by ignoring it.

All automated steps must pass before changes can be merged. CI runs `make validate`, `make vulncheck`, the
integration tests and the image build on every push and pull request.

### Rules for fixing failures

- **Fix forward.** Never add golangci-lint exclusions, `//nolint` comments or vacuum rule disables to get a check
  green; change the code or the spec instead.
- **Pinned tools only.** Tools are `tool` directives in `go.mod` and run with `go tool <name>`. Never use
  `go install` or `@latest`. To upgrade a tool, `go get -tool <module>@<version>` and check that `go get` did
  not downgrade another tool's dependencies.
- After changing `queries.sql`, run `make generate` (sqlc); never edit generated code.

Additionally, perform the following manual checks:

### Manual Quality Checks

After automated validation passes, review code for these issues:

1. **Unnecessarily Optional Fields**: Check for dependencies being defensively allowed to be optional, and simplify by assuming we always provide them.

2. **Duplicate Code**: Check for repeated definitions (constants, types, helper functions) that should be consolidated.

3. **Incomplete or Disconnected Logic**: Look for:
   - Legacy mechanisms or functions left in the code instead of being removed
   - Closures closing over data that may be outdated
   - Fields in types/interfaces that are never read or written
   - Functions that are defined but never called
   - Features partially implemented but not wired up
   - TODO comments indicating unfinished work

4. **Validation Script Coverage**: Ensure any new validations are:
   - Added to `make validate` or the integration targets, and so to CI

5. **OpenAPI Spec Completeness**: When adding or modifying API endpoints, verify that `openapi/api.yaml`:
   - Lists every route registered in `internal/transport/web/router/router.go` (`TestRoutes` checks the parity)
   - Has closed request/response schemas (`additionalProperties: false`) matching the controller implementations
   - Documents all query parameters, path parameters, and request bodies

   A new route gets a row in the route table (`router/routes_test.go`: who may call it, and a happy path checked
   against the spec) and one test per rule it adds, not per status (PLAN §9; docs/TESTING.md "Adding an endpoint").

6. **Environment Variables**: Every variable the code reads is in `.env.dist` with a comment, and every variable
   in `.env.dist` is read (`TestEnvDistListsEveryEnvVar`). New production variables are added to the
   `config:set` command in docs/DEPLOY.md.

7. **Migration Backward Compatibility**: dokku applies migrations while the previous release is still serving,
   and a rollback runs old code against the new schema. Each migration must work with the previous release's
   code: add tables, nullable or defaulted columns and indexes freely; drop or rename only in a later release
   once no deployed code uses the old shape. Never edit a deployed migration. See docs/DEPLOY.md.

   Every `CREATE TABLE` in a migration states `DEFAULT CHARSET=utf8mb4 COLLATE=...` explicitly, and every text
   column whose comparison matters (IDs, hashes) states its own `COLLATE`. Never rely on the server or database
   default: dokku-mysql's server default (`utf8mb4_unicode_ci`) differs from every development and test server
   (`utf8mb4_0900_ai_ci`), so an omitted collation would pass every test and behave differently in production.
   User-ID columns use `utf8mb4_0900_bin`, which is binary and NO PAD, so case and trailing spaces are
   significant; `utf8mb4_bin` would ignore trailing spaces.

8. **Redundant fields or parameters**: Look for:
   - Fields that contain information present in or inferrable from other fields
   - Parameters that contain information present in or inferrable from other fields

9. **Poor use of types**: Look for:
   - String comparisons on errors instead of using typed errors and errors.Is or errors.As

All steps must pass before changes can be merged.
