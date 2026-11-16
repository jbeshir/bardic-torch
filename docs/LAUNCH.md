# Launch checklist

One ordered list for John and Fabian, from nothing to production. Each step links to the runbook that has the
commands: this repository's [DEPLOY.md](DEPLOY.md) (the API on dokku, Auth0, the import) and the FE repository's
`docs/DEPLOY.md` (Cloudflare, Workers, Auth0 application, Turnstile), cited as **FE §n**. Don't skip ahead: later
steps need the values earlier ones produce.

- **[Owner]** marks a step only the owners can do: it needs an account, a dashboard, the dokku host or the private
  crawl.
- **[Anyone]** marks a step anyone with a checkout can do once the owners hand over the public values it needs.

Staging is the **preview Worker** (`https://preview.bardictorch.org`, built from the FE's `preview` branch) against
the **production API** (`https://api.bardictorch.org`) and its database. So the real import is applied once, before
staging, and anything done on staging (edits, approvals, contact requests, takedowns) is done to the production
catalogue: undo test changes as the click-through says.

## 1. Accounts

1. **[Owner]** A Cloudflare account on the **Workers Paid** plan; the zone can stay Free (FE §2 "Plan").
2. **[Owner]** Access to the registrar for `bardictorch.org`, so its nameservers can move to Cloudflare.
3. **[Owner]** Admin on both GitHub repositories (branch protection, Workers Builds' GitHub app).
4. **[Owner]** The dokku host: dokku 0.35.0 or newer, BuildKit, the mysql and letsencrypt plugins
   ([Requirements](DEPLOY.md#requirements)).
5. **[Owner]** An Auth0 tenant ([Tenant](DEPLOY.md#tenant)), a Google Cloud project and a Meta for Developers app
   (step 4).
6. **[Owner]** The research crawl's dump directory, kept outside both repositories (it holds lyrics and email
   addresses and is never committed).

## 2. Cloudflare zone and DNS

1. **[Owner]** Add the zone and wait until it is **Active** (FE §3 "The zone").
2. **[Owner]** Zone TLS settings, the `www` record and redirect rule (FE §3 "The site").
3. **[Owner]** The rate-limiting rule, exactly as FE §3 "Rate limiting" gives it.
4. **[Owner]** `A` (and `AAAA`) `api` → the dokku host, **DNS only** (FE §3 "The API";
   [Requirements](DEPLOY.md#requirements)).

## 3. The API on dokku

1. **[Owner]** App, MySQL 8.4 service, link, and the database collation **before** the first deploy ([App and
   database](DEPLOY.md#app-and-database)); domain and ports ([Domain and ports](DEPLOY.md#domain-and-ports)).
2. **[Owner]** `dokku config:set` with every variable ([Configuration](DEPLOY.md#configuration)). Use the values in
   the table in section 5, with `TURNSTILE_HOSTNAME=preview.bardictorch.org` for staging. If Auth0 isn't set up yet,
   come back to this step after step 4; the API refuses to start without `AUTH0_DOMAIN`.
3. **[Owner]** First deploy, TLS, and the checks in [Verify](DEPLOY.md#verify): `/healthz` and `/readyz` 200,
   `/v1/me` 401, `migrate version` not dirty.

## 4. Auth0, Google, Facebook and Turnstile

1. **[Owner]** Auth0 API with identifier `https://api.bardictorch.org`, RS256, offline access on
   ([API](DEPLOY.md#api)), and the post-login Action that adds the name claim, deployed and in the Login flow
   ([Name claim](DEPLOY.md#name-claim)); without it every sign-in gets 401.
2. **[Owner]** The Regular Web Application with the settings in **FE §4** (callbacks `/auth/callback` on both
   origins, logout URLs, Client Secret (Basic), refresh-token rotation with a reuse interval) ([Application for the
   FE](DEPLOY.md#application-for-the-fe)).
3. **[Owner]** Google and Facebook connections, published / Live ([Google connection](DEPLOY.md#google-connection),
   [Facebook connection](DEPLOY.md#facebook-connection)), enabled on the application, database connection off
   ([Connections on the application](DEPLOY.md#connections-on-the-application)). The privacy URL is the FE's
   `/help/privacy`, and the data-deletion URL the contact form's opt-out.
4. **[Owner]** Both moderators log in once with **Try Connection** and copy their `user_id`s ([Moderator user
   IDs](DEPLOY.md#moderator-user-ids)); set `BOOTSTRAP_MODERATOR_USER_IDS` with them ([Add a
   moderator](DEPLOY.md#add-a-moderator)) and check the `bootstrapped moderator` log lines.
5. **[Owner]** The Turnstile widget with **both** hostnames, `bardictorch.org` and `preview.bardictorch.org` (FE
   §5). The secret key goes to the API (step 3.2); the site key to `wrangler.jsonc` (step 6.1).

## 5. Every secret and variable

| name | set on | by | value |
|---|---|---|---|
| `DATABASE_URL` | API (dokku) | `dokku mysql:link` | set by dokku; never by hand |
| `PORT` | API (dokku) | dokku | 5000, set by dokku; never by hand |
| `LOG_LEVEL` | API (dokku) | `config:set` | `INFO` |
| `AUTH_DRIVERS` | API (dokku) | `config:set` | `auth0` |
| `AUTH0_DOMAIN` | API (dokku) | `config:set` | the tenant domain (or custom domain); the **same** value as the FE's |
| `AUTH0_AUDIENCE` | API (dokku) | `config:set` | `https://api.bardictorch.org` |
| `BOOTSTRAP_MODERATOR_USER_IDS` | API (dokku) | `config:set` | the moderators' Auth0 user IDs, quoted |
| `TURNSTILE_SECRET_KEY` | API (dokku) | `config:set` | the widget's secret key (secret) |
| `TURNSTILE_HOSTNAME` | API (dokku) | `config:set` | `preview.bardictorch.org` while staging, `bardictorch.org` from step 9 |
| `BARDIC_TORCH_API_BASE_URL` | both Workers | `wrangler.jsonc` (commit) | `https://api.bardictorch.org` (already set) |
| `AUTH0_DOMAIN` | both Workers | `wrangler.jsonc` (commit) | the tenant domain; check the committed value matches the real tenant |
| `AUTH0_CLIENT_ID` | both Workers | `wrangler.jsonc` (commit) | the application's client ID (public) |
| `AUTH0_DEFAULT_REDIRECT_URI` | both Workers | `wrangler.jsonc` (commit) | already set per Worker |
| `AUTH0_AUDIENCE` | both Workers | `wrangler.jsonc` (commit) | `https://api.bardictorch.org` (already set) |
| `TURNSTILE_SITE_KEY` | both Workers | `wrangler.jsonc` (commit) | the widget's site key (public) |
| `AUTH_SESSION_SECRET` | each Worker | `wrangler secret put` | random, **different** per Worker (secret) |
| `AUTH0_CLIENT_SECRET` | each Worker | `wrangler secret put` | the application's client secret (secret) |
| `SENTRY_DSN` | each Worker, optional | `wrangler secret put` | the Sentry DSN, or unset |
| `AUTH_MODE` | nowhere | — | never set |

The FE's table with notes is FE §2 "Environment variables"; the API's is [Configuration](DEPLOY.md#configuration)
and `.env.dist`.

## 6. The FE, preview first

1. **[Anyone]** A pull request replacing the `AUTH0_CLIENT_ID` and `TURNSTILE_SITE_KEY` placeholders (and checking
   `AUTH0_DOMAIN`) in `wrangler.jsonc`, both `vars` blocks (FE §1 step 1).
2. **[Owner]** Protect `main` once that pull request has run its checks, then merge it; create and protect the
   `preview` branch (FE §1 steps 2 and 3).
3. **[Owner]** Each Worker's secrets (FE §2 "Bootstrap").
4. **[Owner]** Connect Workers Builds to the **preview Worker only**, push to `preview`, read the first build log,
   wait for the certificate (FE §1 step 7; FE §2 "Workers Builds").

## 7. The real import

1. **[Owner]** **Dry run** on a workstation with the crawl ([Importing the legacy
   wiki](DEPLOY.md#importing-the-legacy-wiki), step 1). The H1 run of the current crawl printed:
   ```
   pieces 142 (dance_music 1, poem 4, song 135, story 2); stub pieces 78; with inline lyrics 11
   persons 160 (bards 48, stubs 112)
   terms 129; collections 61 (of 62 entries); events 38
   links 563; revisions 608
   ```
   Any other numbers mean the crawl or the importer changed: stop and find out why. The site then shows **220
   pieces** (142 plus 78 stubs from red links) and **160 people**.
2. **[Owner]** **Apply, once**, on the server (same section, step 2), then delete the copy (step 3).
3. **[Owner]** Take a backup straight away: `dokku mysql:export bardic-torch-db > bardic-torch-after-import.sql`,
   kept somewhere private (it holds the import, not the crawl, but it is the only way back to "just imported").
4. **[Owner]** Check: `curl -s https://api.bardictorch.org/v1/pieces?limit=1` shows `"total":220`, and
   `…/v1/persons?limit=1` shows `"total":160`.

## 8. Staging: smoke checks and the click-through

1. **[Owner]** The smoke checks against `https://preview.bardictorch.org` (FE §8), including the browser checks:
   both sign-ins, the name in Settings, log out, theme, no CSP errors, no URLs with `code=` in Workers Logs.
2. **[Owner]** The [click-through](#click-through) below, both of you. Note anything wrong; fix data by editing on
   the site (as moderators your edits go live at once), and report code problems as issues.

## 9. The production switch

1. **[Owner]** Point the contact form at production: `dokku config:set bardic-torch-api
   TURNSTILE_HOSTNAME=bardictorch.org` (restarts the API; the form now fails on preview, by design, FE §5).
2. **[Owner]** Connect Workers Builds to the **production Worker**, merge (or retry) to build `main`, wait for the
   certificate (FE §1 step 8).
3. **[Owner]** The smoke checks against `https://bardictorch.org` (FE §8), including `www` → apex and `robots.txt`
   allowing crawling; send one contact message and resolve it; trigger the rate limit once (FE §3 "Rate limiting",
   "Check it").
4. **[Owner]** Turn on HSTS only now, once every name serves correctly (FE §3 step 2).

## 10. Rollback

- **FE:** roll back the Worker to the previous version (FE §9). Before production exists, there is nothing to roll
  back to: fix forward on `preview`.
- **API code:** push the last good commit ([Rollback](DEPLOY.md#rollback)); migrations are backward compatible.
- **API configuration:** correct the value with `dokku config:set`; nothing to roll back.
- **The import:** `import-mediawiki --apply` refuses a non-empty catalogue, so it can't be re-run over itself.
  Before launch, if the import itself was wrong: `dokku mysql:export` first if anything since is worth keeping, then
  `dokku mysql:unlink`, `mysql:destroy` and recreate the service as in [App and
  database](DEPLOY.md#app-and-database) (collation included), redeploy (which runs the migrations) and repeat step
  7. After launch, prefer fixing pages by editing; to go back to the state right after the import, `dokku
  mysql:import` the step 7.3 backup into a freshly created, linked service.
- **Auth0, DNS, Cloudflare rules, Turnstile:** not versioned; undo in their dashboards (FE §3–§5 list every value).

## Click-through

For John and Fabian, on `https://preview.bardictorch.org` after the real import, in about an hour. Each line is one
thing to look at; the H1 full-stack run found these worth a human eye. Signed-in steps change the production
catalogue: undo what the line says.

**Lists and counts**

1. Home: "220 pieces and 160 people and groups so far". Decide if stubs (78 pieces, 112 people) should count.
2. Pieces: five pages of 50; stubs are labelled "Stub"; the letter bar and the Kind filter (Poem shows 4).
3. Bards: 160 entries, historical figures and the one group among them; the lists call them all "bards".
4. Categories: every facet (Mood, Topic, Culture, Language, Legendaria, Origin, Affiliation) lists terms with
   counts; a kingdom with "0 bards" is expected where only pieces name it.
5. Needs work: about 369 pages, most for unknown licensing; is that the to-do list you want to show?

**Pieces**

6. Search "greensleeves": both Greensleeves and Greensleeves (SCA Version); Greensleeves' notes say "See also
   Greensleeves (SCA Version)." as plain text, not a link: add the link if you want one.
7. Birthday Dirge: Words "Unknown", Tune "Traditional", the long alternative-names note and nine lyrics links.
8. Wild Mountain Thyme: a disputed credit beside "Traditional"; does it read right?
9. A stub piece (any "Stub" in the list): credits only, often "(probably)", from the bard's works list; spot-check
   three of the 79 "probably" credits against the old wiki.
10. Oathkeeping: its lyrics link was malformed on the wiki (no closing bracket), so it shows as raw text in the
    version notes and the piece has no links. Fix by editing.
11. Song of the Waves: the same malformed lyrics link in its version notes. Fix by editing.
12. Vivat the Dream: the licensing notes start with a stray bracket from the wiki. Tidy by editing.
13. Auld Lang Syne: its notes show `[[:Category:Gaelic|Gaelic]` (unbalanced on the wiki). Tidy by editing.
14. The Burning of Tara and The Salmon of Wisdom: the "Timeline of Text Sources" section moved into the notes.
15. A piece with performances (Savage Daughter): the performance descriptions read as plain sentences, with no
    `[https://…]` or `[[…]]` left; the licensing quote appears three times on the page (link, card, sources): too
    much?
16. God Rest Ye Frantic Autocrat: four links shown "In <collection>", and the collection pages they open; one
    collection name lost its closing bracket on the wiki ("Redwulf Songbook (Mooneschadowe, Ansteorra").
17. Look for lyrics: open the 11 pieces whose wiki pages had lyrics inline (listed in the H1 report) and check no
    verse made it into notes or version notes. Three keep a lead-in that now points at nothing ("Lyrics:", "Lyrics
    from Wikipedia:", "Some versions contain an additional, final verse:"): delete those lines.

**People, terms, collections, events**

18. The bard with the most works: words and tunes listed separately, so a song appears under both; fine?
19. A bard page's "Links": recordings without a title show only "youtube.com", often repeating a performance.
20. Historical figures and bands: one band was imported as a historical figure; set its kind to Group. Their stub
    pages say "We don't know how this person licenses their work"; fine for, say, a Tudor king?
21. Aliases: the wiki's legal or mundane names are public aliases (PLAN §2.6). Read the bards' "Also known as" and
    remove any you know someone wants gone.
22. Category descriptions came from the wiki and some still give wiki-editing instructions ("When you create a new
    topic, ensure you add the Topics category…", on War and others). Edit them.
23. Collections: 61, many with "Nothing cited from this collection yet"; events: 38, most with one performance.

**Signed in (undo afterwards)**

24. Each of you signs in with Google, the other with Facebook too; the account menu shows the name on that Google
    or Facebook account.
25. One of you, signed in with a second, non-moderator account, edits a piece (add a link) and submits; the pending
    banner shows; a moderator approves it from the queue; then revert it from History.
26. Make a stale edit on purpose (the non-moderator edits, a moderator edits the same piece, then reviews the first)
    and see the warning; reject it with a note and check the author sees the note under "My contributions".
27. The contact form on preview (Turnstile is live here only while `TURNSTILE_HOSTNAME` is the preview host): send a
    message, then resolve it in Moderation → Contact.
28. Report a link from a piece, find it in the inbox, hide it (gone from the piece for a signed-out reader), then
    restore it and resolve the report.
29. A busy piece and a bard page on a phone, and in dark mode: nothing runs off the right edge.
