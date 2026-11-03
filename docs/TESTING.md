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
  bad dates rejected, `TestDocuments_EnumFieldsAreValidated`, and `TestValidateEnums_MapsAndArrays` for enums in
  arrays and maps); the snapshot views (`snapshot_test.go`, pure
  functions over a hand-built `SnapshotVisibility`); and `TestSpec_EnumSchemasMatchDomain`, which loads
  `openapi/api.yaml` and requires every named enum schema to list exactly the values, in order, of the domain
  vocabulary it describes (`specEnums` in `spec_enums_test.go` maps each schema to its list; a new enum schema
  fails until it is added). Inline enums (error codes, changed-field names, entity-type tags) are not compared.
- **Importer domain rules** (`internal/domain`): `NormalizeURL` (`url_test.go`: one table per rule, each row also
  checked for idempotence, `TestNormalizeURL_Rejects` with `errors.Is(err, ErrInvalidURL)`, and `FuzzNormalizeURL`,
  seeded with the table rows and every URL of the scrubbed wiki dump, for ASCII, `url.Parse`-able, at most 2048-byte and idempotent output; `go test -fuzz
  FuzzNormalizeURL ./internal/domain/` explores further), `TestWithoutTrackingParameters` (only what normalising
  drops as tracking goes, and the result normalizes alike); `FoldLetters`, `Slugify` and `SlugAllocator`
  (`slug_test.go`), `SortTitle` and `SortLetter` (`sort_key_test.go`, including every transliteration and that each
  letter parses with `ParseLetter`); and `TermClosure` (`term_closure_test.go`: self rows, cycles, depth over 3).
- **Wiki dump and wikitext** (`internal/mediawiki`, synthetic `fstest.MapFS` and strings only, with placeholder
  names such as "Wren Placeholder"): `LoadDump` (`TestLoadDump_*`: a missing or malformed file, a line that is not
  one JSON value and a page record over `MaxPageRecordBytes` are `ErrMalformedDump`, with messages naming the file
  and line but no content); the lexer ported from the research's `wikitext.mjs` (`wikitext_test.go`; RE2 has no
  backreference or lookbehind, so `Sections` and the bare-URL rule are hand-coded and pinned by
  `TestSections_Levels` with `== a = b ==` and `=x=`, and `TestFindExternalLinks_BareURLAfterEqualsOrQuote`;
  `TestFindTemplates_NestingCap` pins `MaxNestingDepth`); `ParseValue` (`value_test.go`); page typing, section keys,
  hints and section content (`page_test.go`); inline lyrics (`lyrics_test.go`: every `<poem>` form is dropped
  whatever its length, the flag counts lines per section, the prose rule of Lyrics/Text sections, lines joined by
  `<br>` counting as lines (`TestTextLines`, `TestInlineLyricsBlocks_LinesJoinedByBreaks`), spans stay
  offsets into the original text with a comment before the poem); the alias classifier, which fails closed
  (`TestClassifyPersonAlias`, `TestBardAliases_*`); `NameMatcher` and `FoldName`, which live in `internal/domain`
  since BE4 so the importer, the scrubber and the moderation flags share one matcher (`domain/names_test.go`:
  case, accents, apostrophes, `_`/`-`/`.` and concatenated forms, first and last word, and phrases found only as
  their words in order; `domain/fold_test.go`); `ParsePerformanceText`,
  `PersonNameCandidates` and `RepairMarkup`/`FindMarkupIssues`. `FuzzFindTemplates`, `FuzzParseValue`,
  `FuzzSections` and `FuzzInlineLyricsBlocks` check that nothing panics, spans are in bounds and in order, and no
  `<poem` tag survives `WithoutInlineLyrics`; `FuzzWikitextToMarkdown` that the Markdown holds no image, raw HTML
  or link but to an absolute http(s) URL. `TestHostileInput_TimeBudget` runs every parser over unclosed `{{[[(`
  up to 1 MB, and `TestHostileInput_QuadraticShapes` the shapes that once made a parser quadratic (an unclosed
  "[http://" before every token, for `ParseValue` and `WikitextToMarkdown`; one long word for `Words`; 2000 names'
  concatenated forms searched in one long word) up to 2 MiB. Neither has a wall-clock budget: `linearGrowth` runs
  each parser at six sizes, each 4x the one before, and fails as soon as a size takes over 10x the CPU time of the
  size before or 6x that of the first size of at least 20 ms scaled linearly (never under 1 s), so linear code
  passes on any machine, on a busy one and under `-race`, while quadratic code fails as soon as it runs over.
  `TestLinearGrowth_FailsQuadratic` pins that on synthetic linear and quadratic code. The race detector does not
  slow the assembly of `strings.Index` and its kin, so a rescan done only by it is caught only without `-race`
  (`make validate`).
- **Pieces and taxonomy** (`internal/mediawiki`, synthetic wikis built by `site_fixtures_test.go`, and the
  scrubbed copy): category classes (`TestClassifyCategories_*`); the term plan (`TestTermPlan_*`: facets and
  parents, each PLAN §2.10 parent, display names without the facet suffix, Patriotic topics related to their
  kingdom, local groups under their bards' kingdom, redirect categories as aliases, dropped placeholders,
  descriptions as Markdown without lyrics, and a tree `domain.TermClosure` accepts); pieces (`TestParsePiece_*`:
  kind and secondary kinds, terms from the categories ordered by the infobox with parts from qualifiers, drift both
  ways, redirect categories, the most severe conservation status, markup repaired before parsing); alternate titles
  (`TestPieceTitles_*`: infobox splits, H1 parentheticals as titles, other_titles and "Also known as" lines, other
  pages' titles and category words rejected, unparsed prose listed, redirects); content fields
  (`TestWikitextToMarkdown`, `TestPieceFields_*`: dropped lyrics absent from every field by reflection, version
  notes kept, the prose of link sections in the notes unless as long as lyrics, `<br>`-joined lines included,
  licensing hints stripped, "public
  domain" a review fact and never a licence, the period from century origins). On the scrubbed copy, `TestScrubbedPieces_ResearchCounts` asserts 142 pieces (135/4/2/1 by kind), 11
  with inline lyrics, 11 assessed and the drift of 13/24/4 pages, and `TestScrubbedPieces_NoLyricMarkerInAnyField`
  that no placeholder lyric reaches a record.
- **Credits, persons and red links** (`internal/mediawiki`, synthetic wikis and the scrubbed copy): credits
  (`TestParseCredits_*`: splitting never inside a link, page links read through redirects, the traditional forms
  including the repaired single-bracket category, unknown/anonymous/various, possibly/probably/disputed, part
  qualifiers, notes that never hold a capitalised word, and parts inferred from origins scoped to verses and
  chorus); bard pages (`TestParseBard_*`: aliases with redirects hidden when equal to a hidden alias, the H1 lead
  as an ambiguous alias, roles, affiliations and the dropped Kingdom Name placeholder, deceased with a date, the
  "cannot find" licensing sentence, heir_of, the stray heading, works, dropped lyrics); person matching
  (`TestPersonIndex_*`, `TestResolvePersons_*`: apostrophe folding, one edit to an alias, a unique first word,
  ambiguity as a stub with candidates, hidden aliases that match but never name a person, names with digits never
  fuzzy, stubs by folded name with their kind guess); the signal union (`TestUnion_*`: category-only and
  works-list-only credits as lyrics/possible, no credit for a related person, the reference's disagreements, stub
  pieces credited probable to the listing bards, and case variants) and red links (`TestRedLinks_Keys`, `TestClassifyRedLink_*`). On the scrubbed copy,
  `TestScrubbedPeople_ResearchCounts` asserts 48 bards, red links 180/22/1 classified 78 works, 40 credits, 2 prose
  persons, 58 performers, 22 categories, 1 malformed URL and 2 not imported, 78 stub pieces and the 15/4
  disagreement pages, and `TestScrubbedPeople_*` the examples (Lasair's two spellings, Dunstan, Samantha Moore,
  My Thing Is My Own, March of Cambreadth, Malkin Grey).
- **Links, collections, performances and events** (`internal/mediawiki`, synthetic wikis and the scrubbed copy):
  links (`TestExtractLinks_*`: the role of each section, official key links on a credited person's website, one
  link per normalised URL keeping the first URL, text and locator and every role, platforms and place types,
  repaired double-bracket and bare URLs, URLs that do not normalise dropped, infobox links only as occurrences;
  `TestLocator`, `TestHasTrackingParameter`); the Collections page (`TestParseCollections_*`: entries and
  nesting, every kind, the not-bardic note, the duplicate URL merged, the region kingdom; `TestMatchCollection_*`:
  exact URL first, a fragment aside, the longest prefix only for website-like kinds off shared document hosts,
  ties matching none); performances (`TestParsePerformance_*`, `TestParsePerformanceText_EventCapitalisedAndCut`,
  `TestPlacePerformance_*`: on the piece page's link, a bard page's performance of a known piece recorded once on
  the piece, repeated and unlinked items listed; `TestParsePerformance_WorkLinkIsThePieceNotAPerformer`: a link to
  a work, after a comma included, is the piece and never a performer), events (`TestEvents_*`: one per folded name, venue pages,
  similar names listed), performers (`TestPerformers_ResolveToBardsAndStubs`), canonical links per normalised URL
  with conflicts (`TestParseLinks_CanonicalPerURL`; `TestParseLinks_PageSpecificFieldsNeverFromAnotherPage`: a
  PDF cited at different pages is a link per page with its own locator, and a title or description the pages
  disagree on is never one page's; `TestParseLinks_DescriptionReadsTheLinkText`: a link after some text reads as
  its text, or host, so a description has no hole; `TestParseLinks_DescriptionRunsAsLongAsLyricsDropped`: runs
  of link lines whose text reads as 4 lines keep their links without descriptions and are recorded) and profile
  links of infobox credits. On the scrubbed copy,
  `TestScrubbedLinks_ResearchCounts` asserts 62 collection entries (61 collections, the West Kingdom duplicate
  merged), 2 nested, 1 not bardic, the 12 kinds, 112 tracking-parameter URLs, 35 pages repeating a URL and 2 bare
  URLs, and `TestScrubbedLinks_*` the examples (the Green Dragon venue, Marian of Heatherdale matched to Heather
  Dale, Dahrien Cordell's repaired performance link on Rite of Passage, one set of fields per URL, YouTube links
  never in a collection by prefix, ties).
- **Building the import** (`internal/importer`, `domain.CatalogueImport`, synthetic wikis and the scrubbed copy):
  `TestCatalogueImportValidate_*` mirror each schema constraint (IDs, slugs, legacy page IDs, summaries, documents
  decoding, column widths, collation-folded uniques, child primary keys, CHECKs, references, the insert order of
  term parents and related affiliations, link rows, the closure, sort letters, NFC, control characters), each failing with `ErrInvalidImport` and
  never showing the value. `TestBuild_*` cover revisions and contributors, neutral summaries, stable slug
  collisions, sort keys, the closure, status sources (`TestBuild_PersonStatusSource`: the link the date is read
  from unless it is hidden, else the wiki page) and conservation sources (the piece's own page), hidden, redirect and unsure
  aliases, every kind of free text withheld and every entity named by a legal name left out
  (`TestBuild_FreeTextContainingHiddenAliasListed`, `TestBuild_StubPersonWithLegalNameNotCreated`,
  `TestBuild_CollectionWithLegalNameInURLNotImported`), an unsure name published as a stub person kept in free
  text and listed (`TestBuild_UnsureNamePublishedAsPersonKeptInText`), a public alias
  holding a hidden alias hidden (`TestBuild_PublicAliasHoldingHiddenAliasHidden`), emails, one link row per
  normalised URL, URLs without tracking parameters (`TestBuild_URLsWithoutTrackingParameters`), stub pieces
  crediting the listing bard pages' contributors, legacy columns, NFC, control characters removed and listed
  (`TestBuild_ControlCharactersRemovedAndListed`, `TestBuild_ControlCharactersRemovedFromNamesAndContributors`:
  aliases, legacy titles, contributors) and determinism. `TestCatalogueImportValidate_RejectsWithheldPlaceholders`
  pins that no structured field holds a placeholder. `TestWithholder_*` pin the withholder: legal names in names,
  unsure names in free text only (but those published as a person's name), overlapping spans withheld once, whole wikilinks, URLs hidden only by a legal
  name and listed for an unsure name or a home directory. On the scrubbed copy, `TestBuild_ResearchCounts` asserts
  142 pieces (135/4/2/1), 48 bards, 62 collection entries (61 collections, written or withheld) and 11 pieces with
  inline lyrics,
  `TestBuild_NoLyricMarkerAnywhere` that no lyric placeholder line is in any field or the review, and
  `TestBuild_NoPlaceholderInPublicFields` that no name placeholder is in a public field (hidden aliases, legacy
  columns and hidden links aside); `TestCorpus_ResearchCounts` asserts the research counts (`Stats`, in
  `stats_test.go` with the other lookups only tests need). `TestRenderReview_*` and
  `TestReview_*` check the review: every section in order, determinism, the PLAN §2.9 ambiguities, every alias
  listed. `TestGoldenPages` (16 tricky pages, with semantic checks in `golden_assert_test.go`) and
  `TestReviewGolden` compare with `internal/importer/testdata/`; after a deliberate change, rewrite them with
  `go test ./internal/importer -run Golden -update` and review the diff (the repository privacy scans cover them).
  Gated: the same counts on the real crawl, no legal name (in clear, every form) or email in any public field of
  the real import and no private name in its free text (but an unsure name in a published person's name, which
  the review must list as kept), and no 5-word window or whole line of the crawl's inline
  lyrics in the real import or its review, the lyrics found without the detector under test
  (`wikidumptest.OracleLyrics` in the sections the research analysis lists).
  `internal/testutil/importtest` gives other packages the scrubbed import, built once per test binary
  (`importtest.Scrubbed`).
- **Import command** (`cmd/import-mediawiki`, `internal/command`, `internal/repopath`): `TestParseOptions` (exactly one
  mode, `--dump` and `--review` required in both), `TestRun_RefusesReviewInsideRepository` and
  `_RefusesReviewIntoRepositoryThroughSymlinks`, `TestRun_DryRunNeedsNoDatabase` (the run never asks for
  `DATABASE_URL`, and the review is written with mode 0600 over a 0644 file), `TestRun_DryRunScrubbedCounts` (the
  research counts on stdout), the integration test `TestRun_ApplyThenApplyAgainRefuses` (a refused `--apply` still
  writes its review, 0600, so the review precedes the transaction) and, gated, `TestRun_RealDumpDryRun`
  (`main_research_test.go`: no deny-listed lyric n-gram in the real review or stdout, and no private name or email
  on stdout). `TestApplyCatalogueImport_*` (mocks): an invalid import never reaches the writer, and the writer's
  typed errors pass through. `internal/repopath` (`TestInside`, `TestWritePrivate_*`) pins the private-file
  writer both commands share: symbolic links to a file or a directory in the repository, dangling or not, are
  refused, and so are a hard link into the repository and anything but a regular file; the mode is 0600 over an
  existing file, no temporary file is left, and `TestCheckOpenedDirectory_*` refuse a directory swapped before or
  after it is opened. `TestConfigFromURL_ErrorsNeverQuoteThePassword` covers a
  malformed `DATABASE_URL`.
- **Research-gated tests** (`*_research_test.go`): they read the private research crawl through
  `wikidumptest.Research`, which skips unless `BARDIC_RESEARCH_DIR` is set and not empty (CI never sets it; set it
  to the research output to run them). The crawl holds legal names and lyrics, so these tests report
  only through `wikidumptest.Equal` (page ID, field and offset, with digests in place of values) and
  `wikidumptest.NoError` (the error's type only); `TestResearchTestsReportOnlyThroughEqual` fails any test file that
  reads the crawl and also imports testify, logs or prints. `oracle_research_test.go` compares the Go port with the
  research analysis (`analysis/*.json`): page types, sections and their content, infobox values, the inline-lyrics
  flag (11 pieces) and bard aliases; `oracle_pieces_research_test.go` the taxonomy (every category's class, facets,
  ancestors and redirect), pieces (kind, section keys, H1, bard-name categories, both drift lists, conservation and
  alternate titles) and the quality lists (drift, redirect-category links, inline-lyrics pages, assessed pieces);
  `oracle_privacy_research_test.go` checks the privacy classifier independently of its candidate rules: every run of
  two or more capitalised words of the crawl's prose and link titles (its own simple rules: URLs, link targets,
  `<poem>` bodies and lyric lines blanked, runs split at generic non-name words) must be words of an entity the
  parse names (page titles, categories, alternate titles, stub pieces and persons, events, collections), of a
  link label that is its wiki article's title, of a reviewed non-name phrase (`oracleNonNamePhrases`, no personal
  name), or lie in a name the classifier decided on; a run that is none of these is a name form no rule sees.
  `oracle_people_research_test.go` the infobox person items, bard pages (redirects, works, roles, kingdom and group
  categories, In Memoriam, licensing), the red-link map, the credit-signal quality lists, and that the scrubbed copy
  keeps every people count; `oracle_links_research_test.go` every external link occurrence (URL, text, brackets and
  section key), the links deduplicated by normalised URL, the Collections page (letter, label, URL, host, note,
  depth, parent, kind, description) and the link quality lists (bare URLs, tracking parameters, repeated URLs). Each deliberate difference is an entry of `oracleDeviations` with its reason;
  any other is a failure.
- **Privacy scrubber** (`internal/scrub`, `cmd/scrub-dump`): the committed scrubbed copy of the crawl,
  `testdata/wiki-dump/`, is what every other wiki test reads (`wikidumptest.Scrubbed`). Unit tests on synthetic
  dumps (`scrub_test.go`) pin each rule: a legal alias replaced everywhere with one number (wikitext, URL forms,
  titles, categories, redirects, activity), its last word before its first and its honorific forms, a fresh
  placeholder for any other honorific name in prose, fail-closed candidate decisions
  (`TestScrub_CandidatesFailClosed`), poem and prose lyrics replaced by at most four placeholder lines keeping the
  inline-lyrics flag, emails, phones in prose only, user names only in activity and user links, dropped namespaces,
  determinism, idempotence and preserved structure. `TestPublicNamesArePublishedInPlan` keeps `PublicNames` to
  names this plan already publishes. `TestRun_*` cover the command: it prints counts only, and refuses to write
  the private candidates list inside the repository. Always on, against the committed copy:
  - `TestRepositoryHasNoDenylistedText` scans every text file of the repository (skipping `.git`, binary files
    and what `.gitignore` ignores) for the 8-word n-grams of the dropped lyrics, whose salted hashes are
    `testdata/wiki-dump/denylist.txt` (no name is ever hashed);
  - `TestScrubbedDumpStructuralPrivacy`: lyric blocks (by the detector, and every run of four or more prose lines of
    a Lyrics or Text section found without it) are only placeholder lines, no email outside
    `example.invalid`, no phone number in prose, every honorific followed by a placeholder, every hidden H1 alias a
    placeholder, no possible person name left that would be a deny phrase, and only placeholder wiki users;
  - `TestScrubbedDumpIsAFixedPoint`: scrubbing the copy again changes nothing.

  Research-gated (`scrub_research_test.go`): `TestRepositoryHasNoResearchPrivateText` scans the whole repository
  in clear for every deny phrase of the crawl in every form `NameMatcher` finds, and for its emails and phones;
  `TestScrubbedDumpIsUpToDate` regenerates the copy and requires it byte for byte; `TestScrubbedCopyHasNoResearchLyrics`
  finds the crawl's inline lyrics without the detector (`wikidumptest.OracleLyrics`) and requires no 5-word window
  of them in the copy; `TestScrubPreservesResearchCounts`
  compares page types, bards, inline-lyrics pieces, redirects, categories and members, sections, links, templates,
  infoboxes, Collections page URLs and performance items with performers, per page and in total.
- **Revision engine** (`internal/domain`, pure functions over hand-built documents and visibilities; the test IDs
  are those of the BE4 plan's tables):
  - the state machine: `TestCheckTransition` (one subtest per transition row, T01–T54) and
    `TestCheckTransition_Exhaustive` (every role × author × status × action against an independent legal list);
  - document validation: `Test<Type>Document_Validate` per type (every row also checks that no problem holds a
    document value), `TestValidate_*` (problems never hold values; unchanged no-page credits and relations stay
    editable, added ones are refused; restored references need only exist), `TestSourcedFields` (SF01–SF15, each as
    contributor and moderator × a present, absent and each invalid source URL), `TestDraftProblems` (the structural
    checks of a draft) and `TestValidateSubmission_*` (two-phase validation: a rename to a hidden alias answers as
    a rename to a fresh name and only flags it, caps count visible elements, a hidden licensing source never
    satisfies a contributor, editor paths);
  - withhold and restore: `TestEditable` (W rows), `TestEditable_NeverHoldsWithheld`, `TestRestore`,
    `TestRestore_RoundTrip` (restore of an untouched projection is the base, for every type and both roles) and
    `TestRestore_PreferencesEveryActor`; the author's view: `TestAuthorView` (typed values kept, received ones
    redacted by the current state, inputs unchanged);
  - diff and merge: `TestMerge` (R rows), `TestMerge_Properties`, `TestDiffDocuments` and
    `TestMergeFields_CoverEveryDocumentField` (every document field is in a merge table, so a new field fails
    until it is classified);
  - planning: `TestPlanSubmit`, `TestPlanApproval` (R01–R52), `TestPlanApproval_Terms`, `TestPlanApproval_FlagsGate`,
    `TestPlanRebase`, `TestPlanRevert`, `TestRevertDocument`, `TestCheckReviewNote` and `TestLinkRowUpdate` (each
    row of the link write rule);
  - duplicates and display names: `TestDuplicateKey` (apostrophes, ł/ø/æ/ß, NFC/NFD), `TestFindDuplicates`,
    `TestDisplayNameKey` and `TestCheckDisplayNameReserved`.

  `FuzzMergePiece` and `FuzzRestorePerson` (`revision_fuzz_test.go`) are seeded with every piece and person of the
  scrubbed import (`importtest.Scrubbed`), so plain runs check every imported document: the merge never panics, is
  deterministic, never invents a link and is idempotent when clean; restore of any contributor edit keeps every
  hidden alias and hidden link of the base (by key) and its preferences, an untouched projection restores the
  base, and the next projection still withholds them. `go test -fuzz FuzzRestorePerson ./internal/domain/`
  explores further.
- **Write transactions** (`internal/datasources/mysql`): `TestWithWriteTx_*` (unit: a deadlock or lock-wait timeout
  is retried at most twice, then `ErrUnavailable`; other errors are not retried; the deadline), `TestWriteTxBudget`
  (`internal/app`: `WriteTxDeadline` stays under the server's `WriteTimeout`) and
  `TestQueries_LockingReadsCarryTheHint` (every `FOR UPDATE`/`FOR SHARE` in `queries.sql` carries
  `MAX_EXECUTION_TIME`).
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
  `ErrModeratorNotBlockable`). The revision commands act as the principal's effective role
  (`TestReadCommands_ActAsTheEffectiveRole`, `TestSubmitRevision_ActsAsTheEffectiveRole`: an API token is a
  contributor), contributors never see flags (`TestSubmitRevision_ContributorNeverSeesFlags`), and
  `TestModeratorCommands_RequireAnInteractiveModerator` refuses every moderator command to a contributor and to a
  moderator's API token. The contact commands verify before storing and never put personal data in an error
  (`TestSubmitContactRequest_*`), and list and resolve purge expired requests (`TestListContactRequests_*`,
  `TestResolveContactRequest_*`).
- **Turnstile verifier** (`internal/datasources/turnstile`): every test answers from a loopback `httptest`
  siteverify keyed by the `secret` it receives, using Cloudflare's documented test secrets (`1x…AA` passes, `2x…AA`
  fails, `3x…AA` reports a spent token). `TestVerifyTurnstile_Outcomes` covers each error-code class (user-side
  codes → rejected, server-side and unknown codes → unavailable at error level), both allowlisted hostnames, an
  empty allowlist, a foreign hostname, a wrong or missing action and malformed JSON, and checks that neither the
  token nor the secret is in the logs or the error; `_SendsOnlySecretAndResponse`,
  `_TransportFailuresAreUnavailable` (non-200 statuses, an oversized body, a timeout), `_UnreachableIsUnavailable`
  and `_ConcurrencyLimit` (the ninth concurrent call is unavailable at once). **No test reaches the network:**
  command and controller tests use the `mocks.TurnstileVerifier` mock, the HTTP integration tests a passing fake
  verifier (`passingTurnstile`), and Setup's tests run with empty Turnstile variables.
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
  `TestSetup_InvalidDatabaseURL`. `TestContactVerifierFromEnv_*` read the Turnstile variables (an empty secret
  disables the form). `TestContactSubmit_NeverLogsPII` sends contact requests through the production handler, with
  the real verifier calling a loopback siteverify, for every outcome (success, each invalid field, malformed JSON,
  an unknown field, 413, each Turnstile failure, disabled, the cap, a busy and a failing database) and captures
  slog at DEBUG: no record and no response holds the name, email, message, URL or token.
- **Deployment files** (`internal/app/deploy_files_test.go`): `TestEnvDistListsEveryEnvVar` parses the non-test
  Go code under `cmd/` and `internal/` (except `internal/testutil`) and fails unless `.env.dist` lists exactly
  the environment variables it reads. `TestAppJSON`, `TestProcfile` and `TestDockerfile_MatchesDokkuConfiguration`
  pin the dokku configuration: the predeploy migration, the `/readyz` startup check, the web command, and a
  Dockerfile with no `ENTRYPOINT` or `EXPOSE`.

### Regenerating the scrubbed wiki dump

After a change to the scrubber, or to anything in `internal/mediawiki` it uses, regenerate the copy from the
research output and review the decisions before committing it:

```
go run ./cmd/scrub-dump --in "$BARDIC_RESEARCH_DIR/dump" --out testdata/wiki-dump --candidates /private/scrub-candidates.md
BARDIC_RESEARCH_DIR=/path/to/research go test ./internal/scrub/
```

The command prints counts only. `--candidates` writes every possible person name of the crawl with the rule that
found it and the decision (legal or kept, and why) for the privacy review; it names real people, so it is written
with mode 0600 and refused inside the repository. Never commit it, paste from it, or copy it under `/out`.

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
  public catalogue routes with invalid and valid credentials (never authenticating), path-cleaning redirects,
  trailing slashes, encoded literal segments and percent-encoded IDs and slugs. A routing change that alters any
  of them must change the corpus.
- `TestRouter_AuthMatrix` (`internal/app/http_handler_auth_matrix_test.go`) builds the production handler with
  `app.NewHTTPHandler`, fake prefix validators (`Bearer auth0|<user id>`, `Bearer user_api|<user id>`) and mocks,
  and sends a request as every principal to every operation in the spec: anonymous, unrecognised credentials,
  invalid credentials, credentials the validator cannot check, Auth0 contributor, Auth0 moderator, API-token
  contributor, API-token moderator, blocked Auth0 contributor, blocked API-token contributor, and a user whose
  lookup fails. Each cell asserts the status, the error `code` (none on success) and the contract. Each operation
  also gets a preflight row: `OPTIONS` with every principal's credentials gets 204 and never reaches
  authentication.

  The expected outcomes live in that one file: one `access` table per authorisation middleware (`public`,
  `authenticatedUser`, `interactiveUser`, `interactiveModerator`), listing every principal, and `authMatrix`,
  mapping each operation to its access and a request that succeeds when allowed (the revision and moderation rows
  are in `http_handler_auth_matrix_revisions_test.go` and `http_handler_auth_matrix_moderation_test.go`). Every
  moderator operation is `interactiveModerator`: a contributor gets 403 `moderator_required` with either
  credential, and a moderator's API token 403 `api_token_not_allowed`. The list of operations comes from the spec,
  so a new operation without a row fails the test.

  `public` operations (the health checks and every catalogue read) are route-table entries without
  `authenticated`, so the auth middleware never runs for them: they answer any credentials, even invalid,
  unverifiable or a blocked user's, as they would an anonymous request, and never look a user up.
- `TestHTTP_LeakMatrix` checks that no public read shows hidden data; see [Leak matrix](#leak-matrix).

### Integration tests (`//go:build integration`)

Run against real MySQL 8.4 by the `test-integration*` targets:

- **Repository** (`internal/datasources/mysql/*_integration_test.go`): every query in `queries.sql`, migrations up
  → down → up (`TestMigrations_UpDownUp`), column collations (`TestMigrations_ColumnCollations`), case-sensitive
  IDs, trailing-space-significant user IDs (`TestRepository_UserIDsTrailingSpacesAreSignificant`), idempotent
  revoke/block, FK cascade, FK and unique-hash violations asserted by MySQL error number (1452/1062), a 4-byte
  UTF-8 round trip (`TestRepository_EnsureUser_StoresFourByteCharacters`), throttled last-used updates.
- **Revisions and moderation** (`internal/datasources/mysql`, on `newEditingFixture`, a writable database with
  catalogtest's import revisions as bases): drafts (`TestRepository_SaveDraft_*`, `TestRepository_Drafts_*`),
  submit and withdraw (`TestRepository_SubmitDraft_*`, `TestRepository_WithdrawRevision_*`: placeholders with no
  content, deleted with their children), the author's reads (`TestRepository_GetMyRevision_*`,
  `TestRepository_GetEditable_*`), duplicates (`TestRepository_FindDuplicates*`), the approval writer
  (`TestEntityWriter_RoundTrip`: a public read equals the public view of the document written), approval
  (`TestRepository_ApproveRevision_*`: stale merges, pending references approved alongside, slugs, history order,
  links never deleted and never un-hidden, performances by owner, flags, the 1452 → `ErrEntityGone` mapping,
  search after commit), reject, rebase and revert, link reports (`TestRepository_LinkReports`), contact requests
  (`TestRepository_ContactRequests*`: the cap, and the purge with the clock passed in) and display-name keys
  (`TestRepository_UpdateDisplayName_KeyUnique`). The races run goroutines released together and assert outcomes
  only, never timing: `TestRepository_ApproveRevision_ConcurrentDoubleApprove`, `_ConcurrentSameEntity` (no lost
  update), `_WithdrawRace`, `_SupersedeRace`, `_CrossingApprovals` (both always succeed, deadlocks retried),
  `_ApproveVersusModeratorEdit` and `_ConcurrentOptOut`, `TestRepository_RevertTo_ConcurrentSubmit` and
  `_ConcurrentApprove`, `TestRepository_SaveDraft_Concurrent` and `TestRepository_LinkHideResolveRace`. They are
  only meaningful under `-race`, so a milestone's final gate includes `GOTESTFLAGS=-race make
  test-integration-external` (CI's integration job).
- **Migration 010**: `TestMigrations_ModerationConstraints` (its CHECKs, foreign keys and unique key by MySQL error
  number) and `TestMigration010_BackfillsPerformanceOwners`, which writes the scrubbed import with BE3's
  performance insert (no owners), runs the migration's backfill statements and checks that every performance is
  owned by the entity whose current document lists it.
- **Migrate command** (`cmd/migrate/main_integration_test.go`): `TestMigrateRun_UpVersionDown` (up, version and
  down through `run`), `TestMigrateRun_UpWithNewerSchemaIsNoChange` (up on a clean schema newer than every
  embedded migration, as when rolling back a release, succeeds and applies nothing) and
  `TestMigrateRun_UpWithDirtySchemaFails` (up on a dirty schema fails with `migrate.ErrDirty`, whether or not
  its version is newer).
- **End to end** (`internal/app/http_handler_integration_test.go`): `TestHTTP_EndToEnd` serves the production
  handler (`NewHTTPHandler` with Setup's own `repositoryHTTPDependencies`) over a real repository, the real Auth0
  validator against a local `auth0test` issuer and the real API-token validator, plus the real
  `BootstrapModerators` command. It walks a user's life: expired, wrong-audience, badly signed and oversized
  Auth0 tokens get the spec's 401 `unauthenticated` body without creating the user (and a 200 from the public
  `GET /v1/pieces`, which never authenticates), the first valid request
  creates the user, rename persists, an
  API token authenticates as them, a bootstrapped moderator blocks them (both credentials get 403
  `user_blocked`), unblocks them, the token is revoked (401), and a second bootstrap run without the moderator's
  ID does not demote them, and bootstrapping the blocked contributor makes them an unblocked moderator. It also
  asserts that a token's `created_at` from create equals the listed one. Every response goes through the contract
  helper.
- **Revision end to end** (`revision_end_to_end_integration_test.go`): `TestHTTP_RevisionEndToEnd` runs a
  contributor's whole flow, as an Auth0 user and again with an API token: the editable document withholds an
  imported hidden alias, draft, submit and `…/pending` (the public read still shows the approved version),
  withdraw and resubmit, the pending starting point on its submitted base, a moderator's edit approved at once,
  and a superseding submit whose approval keeps the moderator's change. A blocked contributor gets 403 throughout.
- **Moderation end to end** (`http_handler_moderation_integration_test.go`): `TestHTTP_ModerationEndToEnd`, on the
  scrubbed import, through the contract helper: a stale approval rebased automatically, a conflict resolved with
  `…/rebase`, a pending term approved with the piece that references it, a link reported, hidden and kept hidden
  by a revert, a rejected new person whose placeholder is gone, search finding an approved title, a moderator's
  API token acting as a contributor (403 `api_token_not_allowed` on the queue, approve and the contact inbox), the
  contact inbox, a rename to a hidden alias answered as a fresh rename and refused at approval, and a blocked
  author. Its handler logs at DEBUG to a capture, and no record holds a hidden alias of the import, the report's
  message or the contact request's personal data (`logCapture.requireNoneOf`).
- **Setup** (`internal/app/app_integration_test.go`): `TestSetup_BootstrapsModeratorsAndServes` (Setup from the
  environment bootstraps the moderators, serves `/readyz` on a real listener and returns after cancel, purges
  expired contact requests at startup and answers 503 on `/v1/contact` with an empty Turnstile secret),
  `TestSetup_BootstrapFailureFailsStartup` and `TestStoredTime_RoundTripsThroughDatabase` (a clock with a
  fractional second stores and returns the same whole-second `created_at`).

`internal/testutil/mysqltest` gives each test its own database: `mysqltest.NewDatabase(t)` (a `*sql.DB`) or
`mysqltest.NewDatabaseURL(t)` (a `DATABASE_URL`-style URL) creates `bt_test_<random hex>` through
`TEST_MYSQL_ADMIN_URL`, applies every migration, and drops it when the test ends, so integration tests can run
in parallel. A missing `TEST_MYSQL_ADMIN_URL` fails the test rather than skipping it.

### Catalogue tests

A migrated database is expensive (timings under [Integration test modes](#integration-test-modes)), and MySQL
serialises much of that DDL, so parallel tests do not hide it. Read-only tests therefore share a
seeded database (see catalogtest), and new private-database tests should be kept to what needs one. A test that
only writes rows takes `catalogtest.Writable(t)`: an empty migrated database that only that test uses, emptied
with `DELETE` (not `TRUNCATE`, which is DDL and costs about as much as migrating a new database) and handed to a
later test, so the package migrates about one database per parallel test instead of one per test. It needs
`catalogtest.Main` as `TestMain`. A test that changes the schema or a global setting, or closes the pool, must
use `mysqltest.NewDatabase` (or `NewDatabaseURL`), and a test that creates a table drops it again in a cleanup.
Under `-race` on a default-settings server this cut `internal/datasources/mysql` from about 1190 s to about 340 s
(20 cores).

- **Schema** (`internal/datasources/mysql/*schema*_integration_test.go`, `migrate_integration_test.go`):
  - `TestMigrations_UpDownUp` checks the exact sets of tables and views after up, none after down and the
    version, and reads every view after each up; `TestMigrations_DroppedViewColumnBreaksReads` pins that MySQL
    lets a migration drop a column a view selects and fails the view's reads instead (error 1356);
    `TestMigrations_ViewsCheckTheReader` checks that every view is `SQL SECURITY INVOKER`;
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
- **Catalogue import writer** (`repository_catalogue_import_integration_test.go`, the scrubbed import on a shared
  fixture and on private databases): `TestRepository_ImportCatalogue_WritesEverything` (the row count of every
  catalogue table, closure self rows, approved import revisions with no author, each entity's own current
  revision, contributors in order, liveness left `unknown`), `_SystemFields` (hidden links, legacy columns, the 11
  inline-lyrics flags), `_NormalizedURLInvariant`, `_SnapshotsDecode`, `_ReadAPI` (golden pieces, and no hidden
  alias on a person's page), `_RefusesNonEmpty` (a revision, or rows of later tables only, give
  `ErrCatalogueNotEmpty` and nothing is written), `_RollsBackOnError` (a duplicate planted in
  `performance_performers`, the last table written, leaves every table empty, and the error names the table and
  MySQL error number but not the server's message), `_ConcurrentImports` (of two imports at once exactly one
  succeeds, the other gets `ErrImportInProgress` or `ErrCatalogueNotEmpty`, and the lock is released) and
  `_CatalogueTablesAreEveryCatalogueTable`. `TestCatalogueImportValidate_MirrorsSchema` plants one violation of each
  constraint class `Validate` mirrors (a collation-folded UNIQUE, a child primary key, a VARCHAR width, a CHECK) and
  checks that `Validate` rejects it and MySQL, given it unvalidated, refuses it with that class's error number. Every
  MySQL error of the import (a refused row, the lock, the commit) is an `ImportDatabaseError` without the server's
  message. `catalogtest`'s `c.Import(imp)` writes an import through the writer.
  `TestRun_ApplyThenApplyAgainRefuses` (`cmd/import-mediawiki`) applies through the command line and refuses a
  second run.
- **Document coverage** (`TestDocumentCoverage`, `TestDocumentFieldPaths`): classifies every column of the
  tables each snapshot document owns as a document field or system-owned, with a reason, and every document field
  as stored by exactly one column. A new column, document field or table fails it until it is classified.
- **catalogtest** (`internal/testutil/catalogtest`) seeds catalogue fixtures with plain SQL from the
  `internal/domain` snapshot documents: `c := catalogtest.New(t, db)` then `c.Term`, `c.Person`, `c.Piece`,
  `c.Collection`, `c.Event`, `c.Revision`, `c.SetLinkState` and `c.Exec` (for system columns no document holds).
  Approved entities (the zero value) get an approved `import` revision as their current revision; `Unapproved:
  true` leaves it NULL. Term closure rows come from a recursive CTE, so parents are seeded first. Slugs, sort keys
  and letters default to what the importer writes (`domain.Slugify`, `domain.SortTitle` for titles, the name as
  written for persons, and `domain.SortLetter`); tests about them pass literals. Every call commits before it returns, because
  FULLTEXT sees committed rows only. The package's own tests are `catalogtest_test.go`.
  - **Shared read-only fixture**: tests that only read share one seeded database per fixture through
    `fx := catalogtest.NewShared(seed)` and `db, refs := fx.Get(t)`. The first `Get` creates and seeds it; the
    pool is opened with `transaction_read_only=ON`, so a stray write fails with error 1792. Use one `Shared` per
    independent fixture, because counts that several features assert on break when anyone adds data.
  - **`TestMain` is required**: a package that uses `Shared` must have `func TestMain(m *testing.M) {
    catalogtest.Main(m) }`, which drops the shared databases after `m.Run`; `Get` fails without it.
  - Tests that write, or that need statistics a shared fixture would disturb (FULLTEXT scoring), use
    `catalogtest.Writable(t)` and `catalogtest.New`; tests that change the schema (`ALTER TABLE`) or a global
    setting, or close the pool, use `mysqltest.NewDatabase(t)` (see above).
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
  name that is now a hidden alias, with and without a page; links matched by normalized URL
  (`TestRepository_GetPublicRevision_LinksVisibleNow`: deleted, moved and hidden, or hidden under another path case
  since the revision are dropped, while another host case shows); a link respelled to the same normalized URL
  since, which stays redacted once hidden (`_RespelledHiddenLinkStaysRedacted`) and is still shown otherwise
  (`_RespelledVisibleLinkStillShown`); links whose URLs hold non-ASCII characters, which must read (they reach the
  ASCII `normalized_url` only percent-encoded, never as MySQL error 3988) and match the link whose normalized URL
  is their percent-encoding (`TestRepository_GetPublicRevision_NonASCIILinkURL`, and
  `TestHTTP_RevisionWithNonASCIILinkURL` for the 200); and a person's performances on links with no public
  attachment. catalogtest writes every link's `normalized_url` as `domain.NormalizeURL(url)`, and fixtures that
  edit a link's URL in SQL keep that invariant (docs/DEPLOY.md).
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
  catalogtest commits on every call. Production code must follow the same rule: a write that searches does so only
  after its transaction commits.

### Leak matrix

`TestHTTP_LeakMatrix` (`internal/app/leak_matrix_integration_test.go`) sends requests to every public catalogue
read through the production handler over a real repository holding a shared fixture of hidden data: a hidden legal
alias, a `prefers_no_page` person with notes, aliases, roles, an affiliation, relations, credits, performances and
a link, a hidden link, unapproved pieces, persons, terms, collections and events referred to by public ones,
performances of a public person on links attached only to no-page or unapproved persons, persons renamed since
their first revision whose former name is now a hidden alias (with and without a page), a piece whose first
revision holds links since deleted, moved and hidden, hidden under another case, or respelled to the same
normalized URL and hidden, and never-exposed system fields (legacy titles, claims, normalized URLs). Since every
`normalized_url` must be `NormalizeURL(url)`, the normalized-URL marker is a link entered with a tracking
parameter (`https://example.org/leakmark-normalized?utm_source=x`): the URL as entered is a reveal on the rows
that show the link, and a body holding the bare normalized form fails the scan. Every marked string contains
`leakmark`, and
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

The shared `leakCatalogue` also holds BE4's hidden data that public reads must never show: a rejected revision's
review note, a rebased revision's original document (only in `submitted_snapshot_json`), a link report's message
and an open contact request's name, email, message and URL, each checked as stored by `requireLeakMarkersStored`.

### Revision leak matrix

`TestHTTP_RevisionLeakMatrix` (`internal/app/revision_leak_matrix_integration_test.go`) does the same for the
**authenticated** reads, on a private database whose contributor revisions are all written over HTTP as their
authors would write them. The viewers are the author A, another contributor B, an interactive moderator M, M's API token MT, and a blocked user (403 `user_blocked`, the Error body only, on every
row). Each row names its viewer, path, status and reveals, and the scan is the public matrix's:

- **contributor reads** (editable, pending, drafts with `preview` and `references`, `me/revisions`, reference
  labels, the no-page person): A sees their own data and no `leakmark`; B gets 404 for A's draft and revisions;
  MT gets the contributor projection with `link_states: []`; M gets the full document with `link_states`
  (`full` rows assert the hidden data is there instead of scanning);
- **provenance**: what A typed is present in A's pending view, and a link A received that was hidden since is
  absent;
- **validation oracle**: A's rename to a hidden alias and to a fresh name give byte-equal responses once the names
  are masked;
- **moderator-only reads** (`moderatorOnly`: the queue, the moderation view, both inboxes, the link lookup): A and B
  get 403 `moderator_required`, MT 403 `api_token_not_allowed`, each with the Error body only, and M's row asserts
  the moderator data (the `hidden_data_conflict` flag, the report message, the contact request's personal data, a
  hidden link) is present;
- a report of a hidden link gets a 404 byte-equal to an unknown URL's.

The handler logs at DEBUG to a capture (`logCapture`), and no record of the whole test may hold `leakmark`, which
covers the contact request's personal data the test sends. Two self-tests keep it honest:
`TestHTTP_RevisionLeakMatrix_PlantedLeakFails` (a hidden alias planted in a copy of an editable document is
reported, and a typed value removed from a pending view fails the provenance check) and
`TestHTTP_RevisionLeakMatrix_EveryAuthenticatedReadHasARow` (every authenticated GET in the spec has a row, or a
listed exemption with a reason).

### Log capture

The privacy rule covers logs too: no hidden alias, hidden link, legal name, contact request field or Turnstile
token may reach a log record at any level. Three tests capture slog at DEBUG through the production handler:
`TestHTTP_RevisionLeakMatrix` (every authenticated read, no `leakmark`), `TestHTTP_ModerationEndToEnd` (the whole
moderation flow on the scrubbed import: no hidden alias of the import, report message or contact field) and
`TestContactSubmit_NeverLogsPII` (every contact outcome). Controllers log IDs and kinds only; a test that adds a
log line with request data must extend one of them.

### Catalogue end to end

`TestHTTP_CatalogueEndToEnd` (`internal/app/http_handler_catalogue_integration_test.go`) builds the production
handler like `TestHTTP_EndToEnd`, seeds a small invented catalogue (a term tree, a person with a hidden alias,
pieces including a stub and an unapproved draft, and a second approved revision) with catalogtest, and walks list →
facet filter (counts) → search → piece, term and person detail → history → revision detail → needs-work. The
reads are public, so it sends no credentials, and every response goes through the contract helper.

`TestHTTP_ImportedCatalogue` (`internal/app/imported_catalogue_integration_test.go`) applies the scrubbed import to
a shared database and reads all of it through the production handler and the contract helper: every page of every
list, every piece, person, term (every page of its members), collection and event, every history and every
revision. Every response is 200 and valid by the spec, and no body holds a lyric placeholder line
(`scrubbedlyric`), a legal-name placeholder in any form (`Legal Name 001`, `legal-name-001`, `Legal_Name_001`,
`LegalName001`), a withheld marker followed by a placeholder number (`[name withheld] 001`), or a hidden alias as
whole words. `TestHTTP_ImportedCatalogue_PlantedLeakFails` is its self-test: with one hidden alias made public, the
person's page shows it and the scan reports it, as it reports each planted placeholder form.

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
   A public catalogue read (`security: []`, a route-table entry without `authenticated`, like the existing ones)
   also needs a **leak-matrix row** (see "Leak matrix"): requests that reach every list it shows, with
   `reveals` for anything it may legitimately show. An authenticated read needs a **revision-leak-matrix row**
   for every viewer (see "Revision leak matrix"). Any other operation goes in `leakExempt` with a reason. A
   moderator operation uses `requireInteractiveModerator` and the `interactiveModerator` access, so API tokens get
   403 `api_token_not_allowed`. List endpoints also need their query-parameter cases (invalid, repeated,
   out-of-range) as `ServeResponseOnly` tests.
4. **Per-status tests**: a contract-validated controller test case for every documented status the controller
   produces (`Serve`, or `ServeResponseOnly` for deliberately invalid requests). Middleware statuses (401, 403,
   500 from a failed credential check or user lookup) are covered by the matrix.
5. **Integration**: integration tests for every new query, and extend `TestHTTP_EndToEnd` when the endpoint
   changes a user-visible flow.

## Integration test modes

All three modes run `scripts/test-integration.sh <mode>`, which builds `cmd/migrate`, waits until the
`bardic_torch` database accepts connections, runs `migrate up` and `migrate version` on it, and then runs
`go test -tags integration -count=1 -timeout 20m ./...` with `TEST_MYSQL_ADMIN_URL` set and any extra `go test`
flags from `GOTESTFLAGS` (default none; a `-timeout` there overrides the script's), e.g. `make test-integration-local GOTESTFLAGS=-race`. The race detector needs
cgo, which CI has but the sandbox Go image may not, so only CI sets it. Throwaway servers listen on a random
free `127.0.0.1` port and keep their files in a private temporary directory removed on exit, so concurrent runs
do not collide.

The script passes an explicit `-timeout 20m` (`test_timeout`) because go test's default of 10m is too tight for
`internal/datasources/mysql` under `-race` on a 2-core CI runner: the package takes about 340 s on 20 cores,
mostly creating migrated databases. Keep the package well inside the timeout rather than raising it.

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
