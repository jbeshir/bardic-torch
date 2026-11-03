# Deploying the API

The API runs on dokku as the app `bardic-torch-api` at https://api.bardictorch.org. It uses a dokku-managed
MySQL 8.4 service, and Auth0 for login with Google and Facebook. This document covers first-time setup, deploys,
moderators, migrations, rollback and the one-off import of the legacy wiki.

## Placeholders

Replace these in the commands below:

| Placeholder | Meaning | Example |
|---|---|---|
| `<DOKKU_HOST>` | SSH host name of the dokku server | `dokku.example.org` |
| `<LETSENCRYPT_EMAIL>` | Email address for Let's Encrypt expiry notices | `john@example.org` |
| `<AUTH0_DOMAIN>` | Auth0 tenant domain, as a bare host name with no `https://` or trailing slash. Use the custom domain if the tenant has one (see [Custom domain](#custom-domain)). | `bardic-torch.eu.auth0.com` |
| `<MODERATOR_USER_IDS>` | Comma-separated Auth0 user IDs of the moderators | `google-oauth2\|1234,facebook\|5678` |
| `<USER_ID>` | One Auth0 user ID | `google-oauth2\|1234` |
| `<GOOD_COMMIT>` | Commit of the last release that worked | `3f2c1ab` |
| `<VERSION>` | A schema migration version number | `2` |
| `<CRAWL_DIR>` | The research crawl's dump directory, outside this repository | `~/private/wiki-crawl/dump` |
| `<PRIVATE_DIR>` | A private directory outside this repository for the import review | `~/private/import` |

## Requirements

- **dokku 0.35.0 or newer**: dokku-mysql 2.6.0 needs 0.35.0. The app.json `healthchecks`, the `ports:*`
  commands, and reading `app.json` and `Procfile` from the repository root need 0.31.0. Check with
  `dokku version`. On modern dokku, `app.json` and
  `Procfile` are read from the root of the pushed repository. The Dockerfile also copies both into `/app`, the
  image's working directory, so versions that read them from the image find the same files.
- **Docker builds with BuildKit**: the Dockerfile uses `RUN --mount=type=cache` and starts with
  `# syntax=docker/dockerfile:1`, which makes BuildKit pull the `docker/dockerfile:1` frontend from Docker Hub at
  build time (the server needs to reach Docker Hub, as it does for the base images). Docker Engine 23 or newer
  builds with BuildKit by default; it needs the buildx plugin. Check on the server:
  ```
  docker version --format '{{.Server.Version}}'
  docker buildx version
  ```
  On an older engine, set `DOCKER_BUILDKIT=1` for dokku's builds:
  ```
  echo 'export DOCKER_BUILDKIT=1' | sudo tee -a /etc/default/dokku
  ```
- The dokku plugins [dokku-mysql](https://github.com/dokku/dokku-mysql) and
  [dokku-letsencrypt](https://github.com/dokku/dokku-letsencrypt). On the server:
  ```
  sudo dokku plugin:install https://github.com/dokku/dokku-mysql.git --committish 2.6.0 --name mysql
  sudo dokku plugin:install https://github.com/dokku/dokku-letsencrypt.git --committish 0.25.2 --name letsencrypt
  ```
  `--committish` pins each plugin to a release (the latest when this was written). Upgrade later with
  `sudo dokku plugin:update <name> <release>`.
- A DNS `A` (and `AAAA` if the server has IPv6) record for `api.bardictorch.org` pointing at the server. If DNS is
  on Cloudflare, make it **DNS only** (grey cloud), so Let's Encrypt's HTTP challenge reaches dokku.
- The Auth0 setup below, done first: production needs `AUTH0_DOMAIN`, and the moderators' user IDs come from Auth0.

Commands run on the dokku server unless noted. From a workstation, `ssh dokku@<DOKKU_HOST> <command>` runs the
same commands without the `dokku` prefix.

## Auth0

The FE logs users in with Auth0 (Google or Facebook) and calls the API server-side with the user's Auth0 access
token. The API verifies that token against `https://<AUTH0_DOMAIN>/` and the audience
`https://api.bardictorch.org`, and uses its `sub` claim (for example `google-oauth2|1234`) as the user ID.

### Tenant

1. Create a tenant at https://manage.auth0.com. Pick the region nearest the dokku server (EU or US); it cannot be
   changed later. The tenant domain, for example `bardic-torch.eu.auth0.com`, is `<AUTH0_DOMAIN>` unless you set
   up a custom domain.
2. Use this tenant for production only. Development uses a separate application, or better a separate tenant
   (see [Development application](#development-application)).

### Custom domain

Optional. Decide **before** setting up the social connections, because their redirect URIs use the domain.

- Under **Branding → Custom Domains**, add e.g. `login.bardictorch.org` and create the DNS record Auth0 shows.
  Auth0 may ask for card verification.
- Tokens from logins through the custom domain have `https://login.bardictorch.org/` as their issuer, so
  `<AUTH0_DOMAIN>` is then `login.bardictorch.org` everywhere in this document: the API's `AUTH0_DOMAIN`, the
  Google and Facebook redirect URIs, and the FE's Auth0 domain. The API accepts exactly one issuer, so the FE must
  log in through the same domain the API is configured with.

### API

1. **Applications → APIs → Create API**:
   - Name: `Bardic Torch API`
   - Identifier: `https://api.bardictorch.org`. It is the audience and cannot be changed later. The API's
     `AUTH0_AUDIENCE` is set to this value.
   - JSON Web Token (JWT) Profile: Auth0
   - JSON Web Token Signing Algorithm: **RS256**. The API only accepts RS256 tokens, verified against the tenant's
     JWKS.
2. In the API's **Settings**:
   - Turn **Allow Offline Access** on, so the FE can get refresh tokens.
   - Leave RBAC off. Roles live in the API's database, not in Auth0.
   - The default token expiration is fine; the FE refreshes tokens before they expire.

### Application for the FE

1. **Applications → Applications → Create Application**: name `Bardic Torch`, type **Regular Web Applications**.
2. In **Settings**:
   - **Allowed Callback URLs**:
     ```
     https://bardictorch.org/auth/auth0/callback, https://www.bardictorch.org/auth/auth0/callback
     ```
   - **Allowed Logout URLs**:
     ```
     https://bardictorch.org, https://www.bardictorch.org
     ```
   - Leave Allowed Web Origins and Allowed Origins (CORS) empty: the FE talks to Auth0 from its server.

   The exact paths **must match the FE repository's routes**. The values above are the alignment-feed FE's
   pattern (the remix-auth Auth0 strategy's callback route `/auth/auth0/callback`, and the site root after
   logout). Check them against the bardic-torch-fe auth routes and change both lists if the FE differs.
   Only `https://` production URLs belong in this application.
3. In **Settings → Advanced Settings → Grant Types**, keep **Authorization Code** and **Refresh Token** on.
4. In **Settings → Refresh Token Rotation**, turn **Allow Refresh Token Rotation** on and keep the default
   reuse interval. In **Refresh Token Expiration**, set an absolute lifetime and an inactivity lifetime, for
   example 30 days and 15 days. With rotation, a refresh token is replaced on every use, and a reused one revokes
   the whole family.
5. Give the FE its **Domain** (`<AUTH0_DOMAIN>`), **Client ID** and **Client Secret** through its own
   configuration. They are not API settings.

### Development application

Development callback URLs (e.g. `http://localhost:5173/auth/auth0/callback`) never go in the production
application. Create a separate application with those URLs, preferably in a separate development tenant with
its own API using the same identifier, so development logins never create production Auth0 users. A local API
runs with `AUTH_DRIVERS=` (no authentication) by default; to test logins locally, set `AUTH_DRIVERS=auth0`,
`AUTH0_DOMAIN` (the development tenant) and `AUTH0_AUDIENCE` in `.env`.

### Google connection

1. In the Google Cloud console (https://console.cloud.google.com), create a project, e.g. `Bardic Torch`.
2. **Google Auth Platform → Branding** (the OAuth consent screen):
   - App name `Bardic Torch`, a support email, and the app logo if wanted.
   - App domain: home page `https://bardictorch.org`, privacy policy `https://bardictorch.org/privacy` (an FE
     page; see the note under Facebook).
   - Authorized domains: `bardictorch.org`, and `auth0.com` if `<AUTH0_DOMAIN>` is an `auth0.com` domain.
3. **Audience**: user type **External**.
4. **Data Access**: add only the scopes `openid`, `.../auth/userinfo.email` and `.../auth/userinfo.profile`.
   These are non-sensitive, so Google does not need to verify the app.
5. **Audience → Publish app** to move it from Testing to **In production**. While it is in Testing, only listed
   test users can log in.
6. **Clients → Create client**: application type **Web application**, name `Auth0`, and under **Authorized
   redirect URIs**:
   ```
   https://<AUTH0_DOMAIN>/login/callback
   ```
   No JavaScript origins are needed. Copy the client ID and client secret.
7. In Auth0, **Authentication → Social → google-oauth2** (create it if missing): paste the **Client ID** and
   **Client Secret**, and keep the **email** and **profile** attributes. Never leave the keys empty in
   production: Auth0 then uses its shared development keys, which are for testing only.

### Facebook connection

1. At https://developers.facebook.com, **My Apps → Create App**, use case **Authenticate and request data from
   users with Facebook Login**, app type **Consumer**, name `Bardic Torch`.
2. **Use cases → Facebook Login → Customize → Settings** (or **Facebook Login → Settings**):
   - **Client OAuth login**, **Web OAuth login** and **Enforce HTTPS** on.
   - **Valid OAuth Redirect URIs**:
     ```
     https://<AUTH0_DOMAIN>/login/callback
     ```
3. **Use cases → Facebook Login → Permissions**: add **public_profile** and **email**. Both are standard access,
   so they need no app review.
4. **App settings → Basic**:
   - App domains: `bardictorch.org`
   - Privacy Policy URL: `https://bardictorch.org/privacy`
   - User data deletion: **Data deletion instructions URL**, `https://bardictorch.org/data-deletion`
   - Category and app icon.

   **Flag for the FE:** the privacy policy and data-deletion pages are FE pages. They must exist at these URLs (or
   these settings must change to match) before the app can go Live. Google's consent screen links the same
   privacy policy.
5. Switch **App Mode** from Development to **Live**. In Development mode only the app's own roles can log in.
6. In Auth0, **Authentication → Social → facebook**: paste the **App ID** and **App Secret**, and tick the
   **email** and **public_profile** permissions.

### Connections on the application

1. In **Applications → Applications → Bardic Torch → Connections**, enable **google-oauth2** and **facebook**.
2. Disable **Username-Password-Authentication** for the application (same tab), or delete the database
   connection under **Authentication → Database**. Login is social only.

### How the FE calls the API

- The FE's authorization request must include `audience=https://api.bardictorch.org` and the scope
  `openid profile email offline_access`. Without the audience, Auth0 issues an access token the API cannot verify,
  and every request gets 401.
- The FE's server sends the **access token** (not the ID token) with the feed's prefix:
  ```
  Authorization: Bearer auth0|<access_token>
  ```
  Personal API tokens use `Authorization: Bearer user_api|<token>`; see `openapi/api.yaml`.
- The FE refreshes the access token with its refresh token before it expires. An expired token gets 401
  `unauthenticated`.

### Moderator user IDs

Auth0 creates a user the first time someone logs in. To get John's and Fabian's IDs before the FE is live, use
**Authentication → Social → google-oauth2 (or facebook) → Try Connection** and log in once. Then open **User
Management → Users**, select the user, and copy the **user_id** field, e.g. `google-oauth2|1234` or
`facebook|5678`. The ID is case-sensitive. These form `<MODERATOR_USER_IDS>`.

## First-time setup

### App and database

```
dokku apps:create bardic-torch-api
dokku builder:set bardic-torch-api selected dockerfile
dokku git:set bardic-torch-api deploy-branch main
```

Create a MySQL 8.4 service and link it to the app:

```
dokku mysql:create bardic-torch-db --image mysql --image-version 8.4.13
dokku mysql:link bardic-torch-db bardic-torch-api
```

- MySQL 8.4 LTS is the version everywhere. Development (`make db-up`), the podman integration tests and CI use
  the `mysql:8.4` tag, which tracks the newest 8.4 patch release, and the sandbox recipe in TESTING.md uses 8.4.2.
  Production pins a patch release so that the server only changes when you upgrade it. Use the newest `8.4.x`
  tag on Docker Hub at the time (8.4.13 when this was written). Upgrade later with
  `dokku mysql:upgrade bardic-torch-db --image-version 8.4.<x>`.
- Always pass `--image-version`. dokku-mysql's default image is an Innovation release, not an LTS.
- Stay on 8.4 even though 9.7 is a newer LTS. From 9.6, MySQL checks foreign keys in the SQL layer rather than
  InnoDB, and 9.7.4 does not enforce a foreign key whose index has a further column that is NULL in the new row
  (MySQL bug #121438, open). `api_tokens.fk_api_tokens_user` uses such an index, and
  `TestRepository_APITokens_CreateRequiresExistingUser` fails on 9.7.4. Move to 9.7 only once a 9.7 release
  fixes that bug and the integration tests pass on it.
- Moving to a later series is one-way: MySQL upgrades the data directory in place, and the old server cannot
  read it afterwards. Take `dokku mysql:export bardic-torch-db > bardic-torch-db.sql` first. To roll back, create
  a service on the old version and `dokku mysql:import` the export into it.
- `mysql:link` sets `DATABASE_URL` on the app, in the form
  `mysql://mysql:<password>@dokku-mysql-bardic-torch-db:3306/bardic_torch_db`. The database name is the service
  name with hyphens replaced by underscores. The API reads exactly this form; do not pass `mysql:link` a query
  string (`-q`), because the API rejects URLs with query parameters. Check it with
  `dokku config:get bardic-torch-api DATABASE_URL`.

Every connection the API opens sets the session variable `max_execution_time` to 2000, so the server stops any
`SELECT` that runs longer than 2 seconds (a logged 500), and a slow anonymous read cannot hold a pooled connection
that `/readyz` and authentication need. The limit applies to every `SELECT`, including locking reads
(`FOR UPDATE`) and `GET_LOCK` waits, but not to `INSERT`, `UPDATE`, `DELETE` or DDL statements, nor to a `SELECT`
inside them. `migrate` clears it on its own connections, as golang-migrate waits up to 10 seconds for its lock with
`GET_LOCK`. `import-mediawiki --apply` raises it to 60 seconds for its locking reads of the empty tables, which may
wait for a running write. Nothing needs configuring on the server.

Make the database default `utf8mb4` / `utf8mb4_0900_ai_ci`, as in development, the integration tests and CI.
dokku-mysql configures its server with `utf8mb4_unicode_ci`, so without this a table or column whose migration
omits `COLLATE` would compare and sort text differently in production than in every test:

```
echo 'ALTER DATABASE bardic_torch_db CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;' | dokku mysql:connect bardic-torch-db
echo "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = 'bardic_torch_db';" | dokku mysql:connect bardic-torch-db
```

The second command prints `utf8mb4` and `utf8mb4_0900_ai_ci`. Do this before the first deploy, which creates the
tables.

### Domain and ports

```
dokku domains:set bardic-torch-api api.bardictorch.org
dokku ports:set bardic-torch-api http:80:5000
```

The image exposes no port, so dokku routes to its default port, 5000, and sets `PORT=5000` on the app; the API
listens on `PORT`. Do not set `PORT` yourself. `dokku ports:report bardic-torch-api` shows the mapping, and
Let's Encrypt adds `https:443:5000` below.

### Configuration

Every variable in `.env.dist` except the two dokku manages (`DATABASE_URL` from `mysql:link`, `PORT` from dokku):

```
dokku config:set --no-restart bardic-torch-api \
  LOG_LEVEL=INFO \
  AUTH_DRIVERS=auth0,api_token \
  AUTH0_DOMAIN=<AUTH0_DOMAIN> \
  AUTH0_AUDIENCE=https://api.bardictorch.org \
  BOOTSTRAP_MODERATOR_USER_IDS='<MODERATOR_USER_IDS>'
```

- Keep the single quotes around the user IDs: they contain `|`, which the shell would otherwise treat as a pipe.
- `BOOTSTRAP_MODERATOR_USER_IDS` must be set, but may be empty if the moderator IDs are not known yet. Set it
  later as in [Moderators](#moderators).
- `dokku config:show bardic-torch-api` lists the result. The API refuses to start if any variable is missing,
  if `AUTH_DRIVERS` names an unknown driver, or if `AUTH0_DOMAIN` or `AUTH0_AUDIENCE` is empty with `auth0`
  enabled.

### First deploy

From a clone of this repository on your workstation:

```
git remote add dokku dokku@<DOKKU_HOST>:bardic-torch-api
git push dokku main
```

dokku builds the Dockerfile, then runs app.json's `predeploy` script, `/app/migrate up`, in the new image before
it starts the new container. The new container must then pass the `readyz` startup check (`GET /readyz` answers
200 once the API can reach MySQL; 5 attempts, 2 s apart, 5 s timeout) before it receives traffic. If either step
fails, the deploy fails and the previous release keeps serving.

### TLS

Once `api.bardictorch.org` resolves to the server and the app is deployed:

```
dokku letsencrypt:set bardic-torch-api email <LETSENCRYPT_EMAIL>
dokku letsencrypt:enable bardic-torch-api
dokku letsencrypt:cron-job --add
```

The cron job renews certificates before they expire. The API itself serves plain HTTP; dokku's nginx terminates
TLS.

### Verify

From anywhere:

```
curl -fsS https://api.bardictorch.org/healthz
curl -fsS https://api.bardictorch.org/readyz
curl -sS -i https://api.bardictorch.org/v1/me
```

- `/healthz` returns `{"status":"ok"}`.
- `/readyz` returns 200. A 503 `unavailable` means the API cannot reach MySQL.
- `/v1/me` without credentials returns 401 `{"code":"unauthenticated",…}`.

On the server:

```
dokku ps:report bardic-torch-api
dokku run bardic-torch-api /app/migrate version
dokku logs bardic-torch-api --num 100
```

- `migrate version` prints the current schema version, e.g. `version: 2`, without `(dirty)`.
- The logs show one `bootstrapped moderator` line per configured moderator at startup, then a `request completed`
  line per request.

With the FE live, log in as a moderator: the FE's `GET /v1/me` returns `"role":"moderator"`.

### Importing the legacy wiki

The catalogue starts as an import of the legacy MediaWiki site (PLAN §7), run **once**, at launch, against the
empty production database: `import-mediawiki --apply` refuses a catalogue that holds any row, including a
revision. Its input is the research crawl's dump directory (`pages.jsonl`, `categories.json`, `redirects.json`,
`activity.json`); producing the crawl is not part of this repository.

1. **Dry run.** On a workstation, from a clone of this repository, with the crawl outside it:
   ```
   go run ./cmd/import-mediawiki --dry-run --dump <CRAWL_DIR> --review <PRIVATE_DIR>/import-review.md
   ```
   It needs no database and never reads `DATABASE_URL`. It builds and validates the whole import, writes the
   review and prints the counts (pieces by kind and with inline lyrics, persons, terms, collections of the
   Collections page's entries, events, links, revisions and review items).
2. **Review.** The moderators work through the review (`import-review.md`): every alias classification, every
   name withheld from public text (and every possible private name kept in it because the import publishes it as
   a person's name), every collection, event, stub, alias or uploader left out because its name
   holds a possible legal name, every URL that may name a person, the wiki users shaped like personal names
   (credited publicly as written), the ambiguous records, the stubs, the links whose pages disagree on their
   title, description or locator (one links row serves every page; each page's own fields are in the item), the
   link-section prose moved into notes, the runs of link lines imported without descriptions because their text
   could be lyrics, and the rest. Where the importer guessed
   wrong, note the item ID: it is corrected in the catalogue after the import (an entity left out is created by
   hand once its name is cleared). Run the dry run again whenever the crawl changes.
3. **Apply**, once, on the server, before the FE is launched. The image holds `/app/import-mediawiki`; give the
   one-off container a private directory holding the crawl, which the image's user (10001) can write the review
   to:
   ```
   sudo mkdir -p /var/lib/bardic-torch-import
   sudo cp -r <CRAWL_DIR> /var/lib/bardic-torch-import/dump
   sudo chown -R 10001:10001 /var/lib/bardic-torch-import
   sudo chmod -R go= /var/lib/bardic-torch-import
   dokku docker-options:add bardic-torch-api run "-v /var/lib/bardic-torch-import:/import"
   dokku run bardic-torch-api /app/import-mediawiki --apply --dump /import/dump --review /import/import-review.md
   dokku docker-options:remove bardic-torch-api run "-v /var/lib/bardic-torch-import:/import"
   ```
   It builds the same import, writes the review before it touches the database, and then writes every row in
   one transaction: on any error nothing is written. It prints the counts and `applied: the catalogue holds the
   import`. If another import is running it fails with `another catalogue import is in progress`, and on a
   catalogue that is not empty with `catalogue is not empty`; neither writes anything. Its errors never quote a
   value of the import, so the logs hold no name.
4. **Delete the copies** once the import is applied and reviewed: `sudo rm -r /var/lib/bardic-torch-import`.

**The review is private.** It names hidden aliases and other possible legal names in clear so that the
moderators can check them (it never holds lyrics). Never publish it, commit it or attach it to an issue; the
command writes it with mode 0600 and refuses a path inside this repository, which is public (also through a
symbolic or hard link); it replaces an existing file instead of writing through it. Share it with the
moderators privately, and delete the copies when the review is done.

After the import, every imported entity has one approved revision of source `import`, crediting the wiki users who
wrote its page; liveness of every link is `unknown` until a link checker runs.

## Deploys

```
git push dokku main
```

Every deploy runs the migrations first (predeploy), then the startup check. `dokku logs bardic-torch-api -t`
follows the logs.

### Migrations must stay backward compatible

The predeploy step applies the new release's migrations **while the old release is still serving**, and a
rollback runs the old code against the new schema. So every migration must work with the code of the release
before it:

- Allowed in one release: new tables, new nullable columns or columns with defaults, new indexes, widening a
  column.
- Never in one release: dropping or renaming a column or table that the running code uses, adding a `NOT NULL`
  column without a default, or narrowing a column. Do these in two releases: first a release that stops using the
  old shape (and writes both, if needed), then a release whose migration removes it.
- Never edit a migration that has been deployed; add a new one.
- **Enum growth**: appending a value to a catalogue `ENUM` column is an instant, compatible migration, but old code
  fails when it reads a value it does not know (the read path returns a logged 500 for it). So ship the code that
  understands a new value in one release, and only a later release may write rows that use it. Never roll back to a
  release older than one whose data uses a value it lacks; if you must, first delete or rewrite those rows. Removing
  or reordering ENUM values is a narrowing change: use the two-release rule above. The same holds for vocabulary
  values inside revision snapshot documents, which reads validate too. The OpenAPI enum schema of the vocabulary changes in the same release as the domain list
  (`TestSpec_EnumSchemasMatchDomain` fails otherwise); a client generated from the spec then knows the value before
  any data uses it.
- **Snapshot documents** (`revisions.snapshot_json`) follow the same rule as columns. A reader ignores document
  fields it does not know, so a release may add a field (for a new column) without a new `schema_version`: after
  a rollback, the older release keeps serving the revisions written in between, showing only the fields it knows.
  Removing, renaming or retyping a field, or changing what one means, needs a new `schema_version`, which every
  older release refuses (a logged 500 for each such revision): ship the reader for the new version one release
  before any release writes it. A new field that hides or redacts other document data (a privacy flag on a link
  or an alias, say) is not additive, even though older readers would parse it: they would ignore it and show
  what it hides. Such a field needs a new `schema_version`, so that an older release refuses those documents
  rather than failing open.
- **Views** (`public_terms`, `listed_persons`, `public_link_attachments`, migration 009) define the visibility
  rules once. MySQL fixes a view's columns when it is created, so a migration that adds a column a public read
  needs from `terms`, `persons` or `link_attachments` also recreates the view (`CREATE OR REPLACE VIEW`). MySQL
  does not stop a migration from dropping or renaming a column a view selects: the `ALTER TABLE` succeeds, but
  the view is then invalid and every read using it fails (error 1356), so every public read would answer 500
  once the migration ran. A migration that drops or renames such a column must recreate the views without it in
  the same migration. `TestMigrations_UpDownUp` reads every view after migrating up.

### The normalized URL invariant

Every row of `links` has `normalized_url = domain.NormalizeURL(url)`. The importer and every later writer keep
it; a URL that does not normalize is never written. Public revision reads rely on it: a snapshot's link is shown
only if `NormalizeURL` of the URL in the document is the `normalized_url` of a link that is not hidden, and then
under that link's `url`. A row that broke the invariant would make the read show or drop the wrong links.

A change to what `NormalizeURL` returns (a new tracking parameter, say) is therefore a data change, not just a code
change. Ship it with a command or migration that re-normalises every `links` row, and run that before the release
whose reads rely on the new output. Until the rows are re-normalised, the new code is not safe: a document URL
that now normalizes differently is dropped if it matches no row, but it can match another row. A hidden link's
URL that differed from a visible link's only in a newly dropped parameter now normalizes to the visible row's
`normalized_url`, so the revision shows it under the visible link. Only the merge below, which keeps a group
hidden if any of its rows is, closes that. A rollback to the old code after re-normalising is safe: merged rows
keep their hidden state, and a URL that no longer matches a row is dropped.

Re-normalising can give two rows the same `normalized_url`, which the unique index refuses. Merge each such group
into its row with the lowest ID, in one transaction: keep that row's `url` and editor-owned fields, set its
`is_hidden` if any row of the group is hidden (a takedown is never undone by a merge), move the other rows'
`link_attachments` (dropping any that would duplicate one the kept row has for the same role and entity) and
`performances` to it, delete the other rows, and only then write the new `normalized_url`. List the merged groups
for the moderators.

### Failed migrations (dirty state)

If a migration fails part-way, golang-migrate records its version as **dirty**, the deploy fails, and the old
release keeps serving. Later deploys fail with `Dirty database version <VERSION>. Fix and force version.` until
you repair it. MySQL cannot roll back DDL, so a failed migration may be half-applied.

1. See the state:
   ```
   dokku run bardic-torch-api /app/migrate version
   dokku mysql:connect bardic-torch-db
   ```
   ```sql
   SELECT version, dirty FROM schema_migrations;
   SHOW CREATE TABLE <table the migration touches>;
   ```
2. In the same MySQL session, undo whatever part of the failed migration was applied (compare with its `.up.sql`
   file), then record the last version that applied completely, the failed version minus one:
   ```sql
   UPDATE schema_migrations SET version = <VERSION>, dirty = 0;
   ```
   If the failed migration is the first one (version 1), no version applied completely; record that by
   removing the row instead, since golang-migrate has no version 0:
   ```sql
   DELETE FROM schema_migrations;
   ```
   If instead you completed the failed migration by hand, set its own version with `dirty = 0`.
3. Fix the migration in the repository, then `git push dokku main` again.

## Rollback

To go back to an earlier release, push its commit:

```
git push --force dokku <GOOD_COMMIT>:main
```

dokku rebuilds and deploys that commit with the same predeploy and startup checks. If a later release added
migrations, the schema is newer than any migration the older release has; its `migrate up` then applies nothing
and succeeds, printing e.g. `version: 3 (newer than this release's migrations, which end at 2; nothing
applied)`. Backward-compatible migrations mean the old code works against that schema. A dirty schema still
fails the predeploy step; repair it first as in [Failed migrations](#failed-migrations-dirty-state).

Leave the schema alone unless the new migration itself is the problem. If it is, roll it back **before** pushing
the old commit, while the newer image (which has the down migration) is still deployed:

```
dokku run bardic-torch-api /app/migrate down 1
dokku run bardic-torch-api /app/migrate version
```

Down migrations can drop data; check the `.down.sql` file first.

## Moderators

Moderators are named in `BOOTSTRAP_MODERATOR_USER_IDS`. At every start, the API promotes each listed user to
moderator and unblocks them, creating users who have never logged in. It never demotes anyone.

### Add a moderator

1. The person logs in once (through the FE, or **Try Connection** in Auth0), so that Auth0 has a user for them.
2. In Auth0, **User Management → Users**, open the user and copy **user_id**.
3. Set the full list, including existing moderators. Setting config restarts the app, which promotes them:
   ```
   dokku config:set bardic-torch-api BOOTSTRAP_MODERATOR_USER_IDS='<MODERATOR_USER_IDS>'
   dokku logs bardic-torch-api --num 50 | grep 'bootstrapped moderator'
   ```

### Remove a moderator

Removing an ID from the list does not demote the user. Remove it from the list first (otherwise the next start
promotes them again), then demote them in SQL:

```
dokku config:set bardic-torch-api BOOTSTRAP_MODERATOR_USER_IDS='<MODERATOR_USER_IDS>'
dokku mysql:connect bardic-torch-db
```

```sql
UPDATE users SET role = 'contributor' WHERE id = '<USER_ID>';
```

The change applies from the user's next request.

### Blocked users

Promotion also unblocks: if a blocked contributor is listed in `BOOTSTRAP_MODERATOR_USER_IDS`, the next start
makes them an unblocked moderator. Listing an ID means trusting that person, and a blocked moderator could not be
unblocked through the API, because moderators cannot be blocked or unblocked. To block someone who is a
moderator, demote them first as above, then block them through `PUT /v1/users/{user_id}/blocked`.
