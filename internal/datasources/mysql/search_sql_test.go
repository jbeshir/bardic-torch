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
	wildSearch = domain.SearchQuery{Text: "the wild heron", Tokens: []string{"wild", "heron"}}
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
		personCase:      {PersonSlug: &wrenSlug, Sort: domain.PieceSortTitle, Page: page},
		"person_and_terms": {PersonSlug: &wrenSlug, TermSlugs: []string{testWar, testSwords, testWarlike},
			Sort: domain.PieceSortTitle, Page: page},
	}
}

// wrenSlug is the slug of the person piece searches filter on, and wrenID their ID.
var wrenSlug = "wren-of-the-vale"

const (
	wrenID     = uint64(7)
	personCase = "person"
	// testAshGrove and testWrenName are a piece of the search rows and a name of its credit line.
	testAshGrove  = "ash-grove"
	testWildHeron = "the-wild-heron"
	testWrenName  = "Wren of the Vale"
)

// pieceFilterResults answers the resolution of the filters of pieceQueries' query: the terms as
// pieceTermResults does, and with a person, the person first and the terms with their facet's ordinal.
func pieceFilterResults(query datasources.PieceQuery) [][][]any {
	switch {
	case query.PersonSlug != nil:
		rows := [][]any{{wrenID, wrenSlug, nil, int64(0)}}
		if len(query.TermSlugs) > 0 {
			rows = append(rows, []any{uint64(3), testWarlike, string(domain.FacetMood), int64(1)}, []any{uint64(1), testWar, topic, int64(2)},
				[]any{uint64(2), testSwords, topic, int64(2)})
		}
		return [][][]any{rows}
	case len(query.TermSlugs) > 0:
		return [][][]any{pieceTermResults}
	default:
		return nil
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

			runner := &fakeRunner{results: pieceFilterResults(query)}
			c := runner.reader()
			c.pieceList(t.Context(), query)
			require.NoError(t, c.err)

			// At most three round trips for a page without pieces: one resolving the terms and the person if
			// there are any, the items, and every count. The credit lines are read only for pieces.
			want := 2
			if len(query.TermSlugs) > 0 || query.PersonSlug != nil {
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

	assert.Equal(t, []any{testWar, testSwords, testWarlike, string(domain.FacetAffiliation)}, terms.args)
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
	assert.Equal(t, append(append([]any{}, withArgs...), string(domain.FacetAffiliation), "mood", "topic", "mood", topic),
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

func TestPieceSearch_PersonFilter(t *testing.T) {
	t.Parallel()

	query := pieceQueries()["person_and_terms"]
	runner := &fakeRunner{results: pieceFilterResults(query)}
	c := runner.reader()
	c.pieceList(t.Context(), query)
	require.NoError(t, c.err)
	require.Len(t, runner.statements, 3, "the person and the terms are resolved in one round trip")
	filters, items, counts := runner.statements[0], runner.statements[1], runner.statements[2]

	// The person, then the terms, in one statement whose rows say which is which.
	assert.Equal(t, "SELECT "+string(executionTimeHint)+" lp.id, lp.slug, NULL AS facet, 0 AS ordinal "+
		"FROM listed_persons lp WHERE lp.slug = ? UNION ALL SELECT t.id, t.slug, t.facet, t.facet + 0 "+
		"FROM public_terms t WHERE t.slug IN (?, ?, ?) AND t.facet <> ? ORDER BY ordinal, id", filters.String())
	assert.Equal(t, []any{wrenSlug, testWar, testSwords, testWarlike, string(domain.FacetAffiliation)}, filters.args)

	// The person filters the candidates, so the page and every count, by the resolved ID; the terms are
	// grouped by facet as without a person.
	personFilter := " AND EXISTS (SELECT 1 FROM piece_credits pc WHERE pc.piece_id = p.id AND pc.person_id = ?))"
	for _, s := range []*statement{items, counts} {
		assert.Contains(t, s.String(), " WHERE p.current_revision_id IS NOT NULL"+personFilter)
		assert.Contains(t, s.args, wrenID)
		assert.NotContains(t, s.args, wrenSlug)
		assert.Contains(t, s.String(), "AS facet_mood, EXISTS")
		assert.Contains(t, s.String(), "AS facet_topic")
	}
	assert.Equal(t, []any{uint64(3), uint64(1), uint64(2), wrenID, int32(10), int32(20)}, items.args)

	// Without terms, the person alone.
	query = pieceQueries()[personCase]
	runner = &fakeRunner{results: pieceFilterResults(query)}
	c = runner.reader()
	c.pieceList(t.Context(), query)
	require.NoError(t, c.err)
	require.Len(t, runner.statements, 3)
	assert.Equal(t, "SELECT "+string(executionTimeHint)+" lp.id, lp.slug, NULL AS facet, 0 AS ordinal "+
		"FROM listed_persons lp WHERE lp.slug = ? ORDER BY ordinal, id", runner.statements[0].String())
	assert.Equal(t, []any{wrenID, int32(10), int32(20)}, runner.statements[1].args)
}

func TestPieceSearch_UnknownPerson(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		query   datasources.PieceQuery
		results [][][]any
		// wantTerm is the unknown term reported, if a term is unknown; otherwise the person is.
		wantTerm string
	}{
		// Unknown, unapproved and no-page persons alike: listed_persons has none of them.
		"person": {query: datasources.PieceQuery{PersonSlug: &wrenSlug, Sort: domain.PieceSortTitle}},
		"person_with_terms": {
			query: datasources.PieceQuery{PersonSlug: &wrenSlug, TermSlugs: []string{testWar},
				Sort: domain.PieceSortTitle},
			results: [][][]any{{{uint64(1), testWar, topic, int64(2)}}},
		},
		// A term that is unknown is reported first, as without a person.
		"term_and_person": {
			query: datasources.PieceQuery{PersonSlug: &wrenSlug, TermSlugs: []string{testWar, testSwords},
				Sort: domain.PieceSortTitle},
			results:  [][][]any{{{uint64(1), testWar, topic, int64(2)}}},
			wantTerm: testSwords,
		},
		"term_with_known_person": {
			query: datasources.PieceQuery{PersonSlug: &wrenSlug, TermSlugs: []string{testSwords},
				Sort: domain.PieceSortTitle},
			results:  [][][]any{{{wrenID, wrenSlug, nil, int64(0)}}},
			wantTerm: testSwords,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{results: tc.results}
			c := runner.reader()
			c.pieceList(t.Context(), tc.query)

			if tc.wantTerm != "" {
				var unknown *datasources.UnknownTermError
				require.ErrorAs(t, c.err, &unknown)
				assert.Equal(t, tc.wantTerm, unknown.Slug)
			} else {
				var unknown *datasources.UnknownPersonError
				require.ErrorAs(t, c.err, &unknown)
				assert.Equal(t, wrenSlug, unknown.Slug)
			}
			assert.Len(t, runner.statements, 1, "nothing runs after the resolution")
		})
	}
}

func TestCreditNamesStatement(t *testing.T) {
	t.Parallel()

	s := creditNamesStatement([]string{testWildHeron, testAshGrove})
	requireWellFormed(t, s)
	assert.Equal(t, "SELECT "+string(executionTimeHint)+" pi.slug, lp.id, lp.name FROM piece_credits pc "+
		"JOIN pieces pi ON pi.id = pc.piece_id JOIN listed_persons lp ON lp.id = pc.person_id "+
		"WHERE pi.slug IN (?, ?) ORDER BY pi.slug, pc.position, pc.id", s.String())
	assert.Equal(t, []any{testWildHeron, testAshGrove}, s.args)

	// No pieces, no statement.
	runner := &fakeRunner{}
	assert.Empty(t, runner.reader().creditNames(t.Context(), nil))
	assert.Empty(t, runner.statements)
}

func TestPieceSearch_Rows(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{results: [][][]any{
		{
			{testWildHeron, "The Wild Heron", song, "endangered", false, "poem,story"},
			{testAshGrove, "Ash Grove", "poem", "unassessed", true, nil},
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
		// The credit lines, by slug, then credit order: a person credited twice is named once.
		{
			{testAshGrove, uint64(9), "Ash Tallow"},
			{testWildHeron, wrenID, testWrenName},
			{testWildHeron, uint64(8), "Rook Hallow"},
			{testWildHeron, wrenID, testWrenName},
		},
	}}
	c := runner.reader()
	list := c.pieceList(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle})
	require.NoError(t, c.err)
	require.Len(t, runner.statements, 3)
	assert.Equal(t, []any{testWildHeron, testAshGrove}, runner.statements[2].args)
	requireWellFormed(t, runner.statements[2])

	assert.Equal(t, domain.PieceList{
		Pieces: []domain.PieceSummary{
			{Slug: testWildHeron, Title: "The Wild Heron", Kind: domain.PieceKindSong,
				SecondaryKinds:     []domain.PieceKind{domain.PieceKindPoem, domain.PieceKindStory},
				ConservationStatus: domain.ConservationStatusEndangered,
				CreditNames:        []string{testWrenName, "Rook Hallow"}},
			{Slug: testAshGrove, Title: "Ash Grove", Kind: domain.PieceKindPoem,
				ConservationStatus: domain.ConservationStatusUnassessed, IsStub: true,
				CreditNames: []string{"Ash Tallow"}},
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
				runner.results = [][][]any{{{uint64(7), affiliation, string(domain.FacetAffiliation)}}}
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

	assert.Equal(t, wildQuery, booleanQuery([]string{"wild", "heron"}))
}

// Test strings used more than twice.
const (
	noFiltersCase   = "none"
	everyFilterCase = "every_filter"
	testSwords      = "swords"
	testWar         = "war"
	testWarlike     = "warlike"
	wildQuery       = "+wild* +heron*"
	termBranch      = "term'"
	topic           = string(domain.FacetTopic)
	song            = string(domain.PieceKindSong)
)

// filterColumns are the candidates' filter columns of pieceQueries' every_filter search.
var filterColumns = []string{"facet_mood", "facet_topic", "kind_match", "status_match", "letter_match"}

// TestPieceSearch_EveryTermFacet checks, for each term facet, that a term of the facet is matched by the facet's
// own candidates column in the items, and left out of exactly its own facet's count branch.
func TestPieceSearch_EveryTermFacet(t *testing.T) {
	t.Parallel()

	for _, facet := range []domain.Facet{domain.FacetMood, domain.FacetTopic, domain.FacetCulture,
		domain.FacetLanguage, domain.FacetLegendaria, domain.FacetForm, domain.FacetOrigin, domain.FacetOther} {
		t.Run(string(facet), func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{results: [][][]any{{{uint64(7), "a-term", string(facet)}}}}
			c := runner.reader()
			c.pieceList(t.Context(), datasources.PieceQuery{TermSlugs: []string{"a-term"},
				Sort: domain.PieceSortTitle, Page: domain.Page{Limit: 10}})
			require.NoError(t, c.err)
			require.Len(t, runner.statements, 3)
			items, counts := runner.statements[1], runner.statements[2]
			requireWellFormed(t, items)
			requireWellFormed(t, counts)

			column := string(facetMatch(facet))
			assert.NotEqual(t, "facet_unknown", column)
			assert.Contains(t, items.String(), "AS "+column, "the facet has its own column")
			assert.Contains(t, items.String(), " AND c."+column, "the items match the facet's terms")

			// The branch counting the facet's own terms leaves the facet's selection out; the branch counting
			// the other facets' terms and the total keep it.
			branches := strings.Split(counts.String(), " UNION ALL SELECT '")
			assert.Contains(t, branches[0], "c."+column, "the total matches the facet's terms")
			withoutOwn := 0
			for _, branch := range branches[1:] {
				if strings.HasPrefix(branch, termBranch) && !strings.Contains(branch, "c."+column) {
					withoutOwn++
				}
			}
			assert.Equal(t, 1, withoutOwn, counts.String())
		})
	}
}

// TestPersonSearch_InjectionInputs checks that no byte sequence of a person search's q or affiliation slug
// reaches the SQL text, and that the statements stay well formed.
func TestPersonSearch_InjectionInputs(t *testing.T) {
	t.Parallel()

	for _, input := range injectionInputs {
		search, err := domain.ParseSearchQuery(input)
		if err != nil {
			continue
		}
		neutralSearch := domain.SearchQuery{Text: "x", Tokens: make([]string, len(search.Tokens))}
		run := func(q domain.SearchQuery, affiliation string) []*statement {
			runner := &fakeRunner{results: [][][]any{{{uint64(1), affiliation, string(domain.FacetAffiliation)}}}}
			c := runner.reader()
			c.personList(t.Context(), datasources.PersonQuery{AffiliationSlug: &affiliation, Search: &q,
				Sort: domain.PersonSortRelevance, Page: domain.Page{Limit: 5}})
			require.NoError(t, c.err)
			return runner.statements
		}
		got, want := run(search, input), run(neutralSearch, "x")
		require.Len(t, got, len(want))
		for i := range got {
			requireWellFormed(t, got[i])
			assert.NotContains(t, got[i].String(), "zqx", "an input marker reached the SQL text")
			requireNoInputBytes(t, got[i].String(), input, want[i].String())
			assert.Equal(t, want[i].String(), got[i].String(), "the SQL text depends on the inputs' content")
		}
	}
}

// TestNeedsWork_Statements checks that the needs-work statements are well formed with no reason filter and with
// each reason, that a reason adds its own column to the items' filter only, and that an unknown reason is an
// error rather than SQL.
func TestNeedsWork_Statements(t *testing.T) {
	t.Parallel()

	page := domain.Page{Limit: 7, Offset: 14}
	counts, err := needsWorkCountsStatement()
	require.NoError(t, err)
	requireWellFormed(t, counts)

	all, err := needsWorkItemsStatement(nil, page)
	require.NoError(t, err)
	requireWellFormed(t, all)
	assert.Equal(t, []any{int32(7), int32(14)}, all.args[len(all.args)-2:])
	assert.Contains(t, all.String(), "WHERE "+string(needsWork)+" ORDER BY c.entity_order, c.sort_key, c.id LIMIT ? OFFSET ?")

	for _, reason := range domain.NeedsWorkReasons() {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			items, err := needsWorkItemsStatement(&reason, page)
			require.NoError(t, err)
			requireWellFormed(t, items)
			column, err := needsWorkReasonColumn(reason)
			require.NoError(t, err)
			assert.Contains(t, items.String(), "WHERE "+string(needsWork)+" AND "+string(column)+" ORDER BY")
			assert.Equal(t, all.String(), strings.Replace(items.String(), " AND "+string(column), "", 1))
		})
	}

	unknown := domain.NeedsWorkReason("nonsense")
	_, err = needsWorkItemsStatement(&unknown, page)
	require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
}
