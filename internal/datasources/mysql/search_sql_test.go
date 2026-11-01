package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// fakeRunner is a statementRunner that records the statements it is given and answers the nth with results[n],
// each row a list of column values assigned to the scan destinations (through sql.Scanner when they have it).
type fakeRunner struct {
	statements []*statement
	results    [][][]any
}

func (f *fakeRunner) run(_ context.Context, s *statement, row rowFunc) error {
	n := len(f.statements)
	f.statements = append(f.statements, s)
	if n >= len(f.results) {
		return nil
	}

	for _, values := range f.results[n] {
		err := row(func(dest ...any) error {
			if len(dest) != len(values) {
				return fmt.Errorf("scanning %d columns into %d destinations", len(values), len(dest))
			}
			for i, d := range dest {
				if scanner, ok := d.(sql.Scanner); ok {
					if err := scanner.Scan(values[i]); err != nil {
						return err
					}
					continue
				}
				reflect.ValueOf(d).Elem().Set(reflect.ValueOf(values[i]))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// reader returns a catalogueReader running statements with f.
func (f *fakeRunner) reader() *catalogueReader {
	return &catalogueReader{run: f.run}
}

// requireWellFormed checks the rules every search statement follows: one placeholder per argument, and the
// execution time hint exactly once, right after the top-level SELECT (outside every parenthesis).
func requireWellFormed(t *testing.T, s *statement) {
	t.Helper()

	text := s.String()
	require.Equal(t, len(s.args), strings.Count(text, "?"), "placeholders and arguments:\n%s", text)
	require.Equal(t, 1, strings.Count(text, string(executionTimeHint)), text)

	at := strings.Index(text, "SELECT "+string(executionTimeHint))
	require.GreaterOrEqual(t, at, 0, "the hint follows a SELECT:\n%s", text)
	prefix := text[:at]
	assert.Equal(t, strings.Count(prefix, "("), strings.Count(prefix, ")"),
		"the hinted SELECT is the top-level one:\n%s", text)
	assert.True(t, at == 0 || strings.HasPrefix(text, "WITH "), text)
	assert.Equal(t, strings.Count(text, "("), strings.Count(text, ")"), "balanced parentheses:\n%s", text)
}

var (
	letterB    = domain.Letter("B")
	stubPieces = true
	wildSearch = domain.SearchQuery{Text: "the wild rover", Tokens: []string{"wild", "rover"}}
	theSearch  = domain.SearchQuery{Text: "the", Tokens: nil}
)

// pieceQueries are piece searches with every combination of filters the builder distinguishes.
func pieceQueries() map[string]datasources.PieceQuery {
	page := domain.Page{Limit: 10, Offset: 20}
	return map[string]datasources.PieceQuery{
		noFiltersCase: {Sort: domain.PieceSortTitle, Page: page},
		everyFilterCase: {
			TermSlugs:            []string{testWar, testSwords, testWarlike},
			Kinds:                []domain.PieceKind{domain.PieceKindSong, domain.PieceKindPoem},
			ConservationStatuses: []domain.ConservationStatus{domain.ConservationStatusEndangered},
			Stub:                 &stubPieces, Search: &wildSearch, Letter: &letterB,
			Sort: domain.PieceSortRelevance, Page: page,
		},
		"prefix_search": {Search: &theSearch, Sort: domain.PieceSortTitle, Page: page},
		"kinds":         {Kinds: []domain.PieceKind{domain.PieceKindStory}, Sort: domain.PieceSortTitle, Page: page},
		dimensionLetter: {Letter: &letterB, Sort: domain.PieceSortTitle, Page: page},
	}
}

// pieceTermResults answers the term resolution of pieceQueries' every_filter: war and swords are topics,
// warlike a mood.
var pieceTermResults = [][]any{
	{uint64(3), testWarlike, "mood"},
	{uint64(1), testWar, topic},
	{uint64(2), testSwords, topic},
}

func TestPieceSearch_Statements(t *testing.T) {
	t.Parallel()

	for name, query := range pieceQueries() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{}
			if len(query.TermSlugs) > 0 {
				runner.results = [][][]any{pieceTermResults}
			}
			c := runner.reader()
			c.pieceList(t.Context(), query)
			require.NoError(t, c.err)

			// At most three round trips: term resolution if there are terms, the items, and every count.
			want := 2
			if len(query.TermSlugs) > 0 {
				want = 3
			}
			require.Len(t, runner.statements, want)
			for _, s := range runner.statements {
				requireWellFormed(t, s)
			}
		})
	}
}

func TestPieceSearch_StatementShapes(t *testing.T) {
	t.Parallel()

	query := pieceQueries()[everyFilterCase]
	runner := &fakeRunner{results: [][][]any{pieceTermResults}}
	c := runner.reader()
	c.pieceList(t.Context(), query)
	require.NoError(t, c.err)
	require.Len(t, runner.statements, 3)
	terms, items, counts := runner.statements[0], runner.statements[1], runner.statements[2]

	assert.Equal(t, []any{testWar, testSwords, testWarlike, "affiliation"}, terms.args)
	assert.Contains(t, terms.String(), "t.facet <> ?")

	// The WITH clause's arguments: the boolean query four times, then the facets' term IDs in facet order,
	// the kinds twice (primary and secondary), the statuses, the letter and the stub filter.
	withArgs := []any{
		wildQuery, wildQuery, wildQuery, wildQuery,
		uint64(3), uint64(1), uint64(2),
		domain.PieceKindSong, domain.PieceKindPoem, domain.PieceKindSong, domain.PieceKindPoem,
		domain.ConservationStatusEndangered, "B", true,
	}
	assert.Equal(t, append(append([]any{}, withArgs...), int32(10), int32(20)), items.args)
	assert.Equal(t, append(append([]any{}, withArgs...), "affiliation", "mood", "topic", "mood", topic),
		counts.args)

	for _, column := range filterColumns {
		assert.Contains(t, items.String(), " AND c."+column, "the items match every filter")
	}
	assert.Contains(t, items.String(), "ORDER BY c.score DESC, c.sort_title, c.id LIMIT ? OFFSET ?")

	// Each facet's counts leave out exactly its own filter.
	branches := strings.Split(counts.String(), " UNION ALL SELECT '")
	require.Len(t, branches, 1+1+2+3, "total, unselected terms, two selected facets, kind, status, letter")
	branchFilters := []struct{ prefix, without string }{
		{termBranch, ""}, {termBranch, string(facetMatch(domain.FacetMood))}, {termBranch, string(facetMatch(domain.FacetTopic))},
		{"kind'", "kind_match"}, {"conservation_status'", "status_match"}, {"letter'", "letter_match"},
	}
	for i, want := range branchFilters {
		branch := branches[i+1]
		assert.True(t, strings.HasPrefix(branch, want.prefix), branch)
		for _, column := range filterColumns {
			if column == want.without {
				assert.NotContains(t, branch, "c."+column, branch)
			} else {
				assert.Contains(t, branch, "c."+column, branch)
			}
		}
	}
	assert.Contains(t, branches[0], "c.facet_mood AND c.facet_topic AND c.kind_match AND c.status_match AND "+
		"c.letter_match", "the total matches every filter")
}

func TestPieceSearch_PrefixFallback(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	search := domain.SearchQuery{Text: "100%_off!", Tokens: nil}
	c.pieceList(t.Context(), datasources.PieceQuery{Search: &search, Sort: domain.PieceSortRelevance})
	require.NoError(t, c.err)

	items := runner.statements[0]
	assert.Contains(t, items.String(), "p.title LIKE ? ESCAPE '!'")
	assert.Contains(t, items.String(), "a.text LIKE ? ESCAPE '!'")
	assert.NotContains(t, items.String(), "MATCH")
	assert.Equal(t, []any{"100!%!_off!!%", "100!%!_off!!%"}, items.args[:2])
}

func TestPieceSearch_RelevanceNeedsSearch(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	c.pieceList(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortRelevance})
	require.ErrorIs(t, c.err, errRelevanceWithoutSearch)
	assert.Empty(t, runner.statements)
}

func TestPieceSearch_UnknownTerm(t *testing.T) {
	t.Parallel()

	// The resolution finds war only: swords is unknown, unapproved or not a piece term alike, and it is
	// reported as given; no other statement runs.
	runner := &fakeRunner{results: [][][]any{{{uint64(1), testWar, topic}}}}
	c := runner.reader()
	c.pieceList(t.Context(), datasources.PieceQuery{TermSlugs: []string{testWar, testSwords, "spears"},
		Sort: domain.PieceSortTitle})

	var unknown *datasources.UnknownTermError
	require.ErrorAs(t, c.err, &unknown)
	assert.Equal(t, testSwords, unknown.Slug)
	assert.Len(t, runner.statements, 1)
}

func TestPieceSearch_Rows(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{results: [][][]any{
		{
			{"the-wild-rover", "The Wild Rover", song, "endangered", false, "poem,story"},
			{"ash-grove", "Ash Grove", "poem", "unassessed", true, nil},
		},
		{
			{dimensionConservationStatus, "endangered", int64(1)},
			{dimensionKind, song, int64(1)},
			{dimensionKind, "poem", int64(2)},
			{dimensionLetter, "0", int64(1)},
			{dimensionLetter, "A", int64(1)},
			{"term", testWar, int64(2)},
			{dimensionTotal, "", int64(2)},
		},
	}}
	c := runner.reader()
	list := c.pieceList(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle})
	require.NoError(t, c.err)

	assert.Equal(t, domain.PieceList{
		Pieces: []domain.PieceSummary{
			{Slug: "the-wild-rover", Title: "The Wild Rover", Kind: domain.PieceKindSong,
				SecondaryKinds:     []domain.PieceKind{domain.PieceKindPoem, domain.PieceKindStory},
				ConservationStatus: domain.ConservationStatusEndangered},
			{Slug: "ash-grove", Title: "Ash Grove", Kind: domain.PieceKindPoem,
				ConservationStatus: domain.ConservationStatusUnassessed, IsStub: true},
		},
		Total: 2,
		Facets: domain.PieceFacets{
			Terms: []domain.TermCount{{Slug: testWar, Count: 2}},
			Kinds: []domain.PieceKindCount{{Kind: domain.PieceKindSong, Count: 1}, {Kind: domain.PieceKindPoem, Count: 2}},
			ConservationStatuses: []domain.ConservationStatusCount{
				{ConservationStatus: domain.ConservationStatusEndangered, Count: 1},
			},
			Letters: []domain.LetterCount{{Letter: domain.LetterOther, Count: 1}, {Letter: "A", Count: 1}},
		},
	}, list)
}

func TestPieceSearch_UnknownEnumValues(t *testing.T) {
	t.Parallel()

	cases := map[string][][][]any{
		"secondary_kind": {{{"a", "A", song, "unassessed", false, "opera"}}},
		"kind_count":     {nil, {{dimensionKind, "opera", int64(1)}}},
		"letter_count":   {nil, {{dimensionLetter, "a", int64(1)}}},
		"dimension":      {nil, {{"mood", "a", int64(1)}}},
	}
	for name, results := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := (&fakeRunner{results: results}).reader()
			c.pieceList(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle})
			assert.Error(t, c.err)
		})
	}
}

// injectionInputs are search and term inputs shaped like SQL injection or FULLTEXT operators.
var injectionInputs = []string{
	"zqxj'; DROP TABLE pieces; --",
	`zqxj") OR 1=1 #`,
	"zqxj */ SELECT /*+ MAX_EXECUTION_TIME(1) */",
	`+zqxj* -zqxw ~zqxv "zqxu zqxt" @8 (zqxs) <zqxr >zqxq`,
	"zqxj%_!\\'\x00",
	"Ã©zqxj ÆØÅ ǅ 𝔷𝔮𝔵",
	"zqxj?zqxw??",
}

// FuzzPieceSearchSQL checks that the statements' SQL text depends only on the shape of a search (how many
// tokens and terms it has), never on its content, which reaches MySQL only as arguments.
func FuzzPieceSearchSQL(f *testing.F) {
	for _, input := range injectionInputs {
		f.Add(input, input)
	}

	f.Fuzz(func(t *testing.T, q, term string) {
		search, err := domain.ParseSearchQuery(q)
		if err != nil {
			t.Skip()
		}
		query := datasources.PieceQuery{TermSlugs: []string{term}, Search: &search, Sort: domain.PieceSortRelevance}
		neutralSearch := domain.SearchQuery{Text: "x", Tokens: make([]string, len(search.Tokens))}
		neutral := datasources.PieceQuery{TermSlugs: []string{"x"}, Search: &neutralSearch,
			Sort: domain.PieceSortRelevance}

		got := runPieceSearch(t, query, term)
		want := runPieceSearch(t, neutral, "x")
		require.Len(t, got, len(want))
		for i := range got {
			require.Equal(t, want[i].String(), got[i].String())
			requireWellFormed(t, got[i])
			requireNoInputBytes(t, got[i].String(), q, want[i].String())
			requireNoInputBytes(t, got[i].String(), term, want[i].String())
		}
	})
}

// runPieceSearch returns the statements a piece search runs when its one term resolves to a topic.
func runPieceSearch(t *testing.T, query datasources.PieceQuery, term string) []*statement {
	t.Helper()

	runner := &fakeRunner{results: [][][]any{{{uint64(1), term, topic}}}}
	c := runner.reader()
	c.pieceList(t.Context(), query)
	require.NoError(t, c.err)

	return runner.statements
}

// requireNoInputBytes fails if any 3-byte run of input appears in text, except runs the text of a search with
// neutral inputs has too (the fixed SQL around the arguments).
func requireNoInputBytes(t *testing.T, text, input, neutralText string) {
	t.Helper()

	for i := 0; i+3 <= len(input); i++ {
		run := input[i : i+3]
		if strings.Contains(text, run) {
			require.Contains(t, neutralText, run, "input bytes %q appear in the SQL text:\n%s", run, text)
		}
	}
}

func TestPieceSearch_InjectionInputs(t *testing.T) {
	t.Parallel()

	for _, input := range injectionInputs {
		search, err := domain.ParseSearchQuery(input)
		if err != nil {
			continue
		}
		for _, s := range runPieceSearch(t, datasources.PieceQuery{TermSlugs: []string{input}, Search: &search,
			Sort: domain.PieceSortRelevance}, input) {
			requireWellFormed(t, s)
			assert.NotContains(t, s.String(), "zqx", "an input marker reached the SQL text")
		}
	}
}

func TestPersonSearch_Statements(t *testing.T) {
	t.Parallel()

	affiliation := "northmarch"
	page := domain.Page{Limit: 5}
	cases := map[string]struct {
		query       datasources.PersonQuery
		resolutions int
	}{
		noFiltersCase: {query: datasources.PersonQuery{Sort: domain.PersonSortName, Page: page}},
		everyFilterCase: {
			query: datasources.PersonQuery{
				Kinds:           []domain.PersonKind{domain.PersonKindGroup},
				Roles:           []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
				AffiliationSlug: &affiliation, Search: &wildSearch, Letter: &letterB,
				Sort: domain.PersonSortRelevance, Page: page,
			},
			resolutions: 1,
		},
		"prefix_search": {query: datasources.PersonQuery{Search: &theSearch, Sort: domain.PersonSortName, Page: page}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{}
			if tc.resolutions > 0 {
				runner.results = [][][]any{{{uint64(7), affiliation, "affiliation"}}}
			}
			c := runner.reader()
			c.personList(t.Context(), tc.query)
			require.NoError(t, c.err)

			require.Len(t, runner.statements, 2+tc.resolutions)
			for _, s := range runner.statements {
				requireWellFormed(t, s)
			}
			if tc.resolutions > 0 {
				assert.Contains(t, runner.statements[0].String(), "t.facet = ?")
				items := runner.statements[1].String()
				// Hidden aliases, and every alias of a person who prefers no page, are filtered inside the alias
				// branch, which is not scored, as the alias index's statistics include hidden aliases; persons
				// who prefer no page match no role or affiliation.
				assert.Contains(t, items, "SELECT p.id AS person_id, MATCH (p.name) AGAINST (? IN BOOLEAN MODE) AS score")
				assert.Contains(t, items, "UNION ALL SELECT a.person_id, 0 AS score FROM person_aliases a "+
					"JOIN listed_persons ap ON ap.id = a.person_id WHERE a.is_public AND MATCH (a.name)")
				assert.NotContains(t, items, "MATCH (a.name) AGAINST (? IN BOOLEAN MODE) AS score")
				assert.Contains(t, items, "AND EXISTS (SELECT 1 FROM listed_persons lp WHERE lp.id = p.id) "+
					"AND EXISTS (SELECT 1 FROM person_roles")
				assert.Contains(t, items, "AND EXISTS (SELECT 1 FROM listed_persons lp WHERE lp.id = p.id) "+
					"AND EXISTS (SELECT 1 FROM person_affiliations pa JOIN term_ancestors ta ON ta.descendant_id = "+
					"pa.term_id JOIN public_terms dt ON dt.id = ta.descendant_id")
				assert.Contains(t, items, "ORDER BY c.score DESC, c.sort_name, c.id LIMIT ? OFFSET ?")
			}
		})
	}
}

func TestPersonSearch_PrefixFallbackFiltersHiddenAliases(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	c.personList(t.Context(), datasources.PersonQuery{Search: &theSearch, Sort: domain.PersonSortName})
	require.NoError(t, c.err)
	assert.Contains(t, runner.statements[0].String(), "JOIN listed_persons ap ON ap.id = a.person_id "+
		"WHERE a.is_public AND a.name LIKE ? ESCAPE '!'")
}

func TestPersonSearch_Rows(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{results: [][][]any{
		{
			{"wren", "Wren", "community_bard", "active", false},
			{"hollis", "Hollis", "historical", "prefers_no_page", false},
		},
		{{dimensionLetter, "H", int64(1)}, {dimensionLetter, "W", int64(1)}, {dimensionTotal, "", int64(2)}},
	}}
	c := runner.reader()
	list := c.personList(t.Context(), datasources.PersonQuery{Sort: domain.PersonSortName})
	require.NoError(t, c.err)
	assert.Equal(t, domain.PersonList{
		Persons: []domain.PersonSummary{
			{Slug: "wren", Name: "Wren", Kind: domain.PersonKindCommunityBard},
			{Slug: "hollis", Name: "Hollis", Kind: domain.PersonKindHistorical, IsStub: true},
		},
		Total:   2,
		Letters: []domain.LetterCount{{Letter: "H", Count: 1}, {Letter: "W", Count: 1}},
	}, list)
}

func TestPersonSearch_UnknownAffiliation(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := runner.reader()
	slug := "westmarch"
	c.personList(t.Context(), datasources.PersonQuery{AffiliationSlug: &slug, Sort: domain.PersonSortName})

	var unknown *datasources.UnknownTermError
	require.ErrorAs(t, c.err, &unknown)
	assert.Equal(t, slug, unknown.Slug)
	assert.Len(t, runner.statements, 1)
}

func TestPersonSearch_RelevanceNeedsSearch(t *testing.T) {
	t.Parallel()

	c := (&fakeRunner{}).reader()
	c.personList(t.Context(), datasources.PersonQuery{Sort: domain.PersonSortRelevance})
	require.ErrorIs(t, c.err, errRelevanceWithoutSearch)
}

func TestCollectionList_Statements(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]datasources.CollectionQuery{
		noFiltersCase: {Page: domain.Page{Limit: 50}},
		everyFilterCase: {
			Kinds:  []domain.CollectionKind{domain.CollectionKindWiki, domain.CollectionKindBlog},
			Letter: &letterB, Page: domain.Page{Limit: 1, Offset: 2},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, s := range []*statement{collectionListItemsStatement(query), collectionListCountsStatement(query)} {
				requireWellFormed(t, s)
			}
			assert.Contains(t, collectionListItemsStatement(query).String(), "ORDER BY c.sort_name, c.id LIMIT ? OFFSET ?")
		})
	}
}

func TestQueryError(t *testing.T) {
	t.Parallel()

	timeout := &mysqldriver.MySQLError{Number: erQueryTimeout, Message: "maximum statement execution time exceeded"}
	require.ErrorIs(t, queryError(fmt.Errorf("wrapped: %w", timeout)), datasources.ErrQueryTimeout)
	require.ErrorIs(t, queryError(timeout), timeout, "the server's error is kept")

	other := &mysqldriver.MySQLError{Number: 1146, Message: "table does not exist"}
	require.NotErrorIs(t, queryError(other), datasources.ErrQueryTimeout)
	require.NotErrorIs(t, queryError(errors.New("connection refused")), datasources.ErrQueryTimeout)
	assert.NoError(t, queryError(nil))
}

func TestFetch_QueryTimeout(t *testing.T) {
	t.Parallel()

	// A sqlc read the server stops, under the session limit every connection has, is a timeout as a search's is.
	c := &catalogueReader{}
	fetch(t.Context(), c, "terms", func(context.Context, int) (int, error) {
		return 0, &mysqldriver.MySQLError{Number: erQueryTimeout, Message: "maximum statement execution time exceeded"}
	}, 0)
	require.ErrorIs(t, c.err, datasources.ErrQueryTimeout)

	c = &catalogueReader{}
	fetch(t.Context(), c, "terms", func(context.Context, int) (int, error) {
		return 0, errors.New("connection refused")
	}, 0)
	require.Error(t, c.err)
	require.NotErrorIs(t, c.err, datasources.ErrQueryTimeout)
}

func TestPrefixPattern(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "the%", prefixPattern("the"))
	assert.Equal(t, "!!!%!_x%", prefixPattern("!%_x"))
	assert.Equal(t, `back\slash%`, prefixPattern(`back\slash`), "backslashes are ordinary with ESCAPE '!'")
}

func TestBooleanQuery(t *testing.T) {
	t.Parallel()

	assert.Equal(t, wildQuery, booleanQuery([]string{"wild", "rover"}))
}

// Test strings used more than twice.
const (
	noFiltersCase   = "none"
	everyFilterCase = "every_filter"
	testSwords      = "swords"
	testWar         = "war"
	testWarlike     = "warlike"
	wildQuery       = "+wild* +rover*"
	termBranch      = "term'"
	topic           = string(domain.FacetTopic)
	song            = string(domain.PieceKindSong)
)

// filterColumns are the candidates' filter columns of pieceQueries' every_filter search.
var filterColumns = []string{"facet_mood", "facet_topic", "kind_match", "status_match", "letter_match"}
