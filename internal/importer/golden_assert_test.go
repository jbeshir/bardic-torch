package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// goldenPiece returns the piece imported from the page titled title, and its review items.
func goldenPiece(t *testing.T, title string) (domain.ImportedPiece, []Item) {
	t.Helper()
	imp, review := scrubbedImport(t)
	g, ok := goldenFor(imp, review, title)
	require.True(t, ok)
	require.NotNil(t, g.Piece, title)

	return *g.Piece, g.Review
}

// goldenPerson returns the person imported from the bard page titled title, and its review items.
func goldenPerson(t *testing.T, title string) (domain.ImportedPerson, []Item) {
	t.Helper()
	imp, review := scrubbedImport(t)
	g, ok := goldenFor(imp, review, title)
	require.True(t, ok)
	require.NotNil(t, g.Person, title)

	return *g.Person, g.Review
}

func kinds(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Kind)
	}

	return out
}

func rolesOf(links []domain.DocPieceLink) []domain.LinkRole {
	var out []domain.LinkRole
	for _, l := range links {
		out = append(out, l.Roles...)
	}

	return out
}

func TestGoldenAssert_BlackAlman(t *testing.T) {
	p, _ := goldenPiece(t, "Black Alman")

	assert.Equal(t, domain.PieceKindDanceMusic, p.Doc.Kind)
	assert.Empty(t, p.Doc.Credits, "the wiki leaves dance_by and music_by empty")
	assert.Subset(t, rolesOf(p.Doc.Links), []domain.LinkRole{domain.LinkRoleSheetMusic,
		domain.LinkRoleDanceInstructions, domain.LinkRolePerformance})
	assert.Contains(t, p.Doc.Aliases, domain.DocPieceAlias{Text: "Black Allemande", Kind: domain.PieceAliasKindAltTitle})
}

func TestGoldenAssert_ThreeWords(t *testing.T) {
	p, items := goldenPiece(t, "Three Words")

	verses, chorus := domain.PiecePartVerses, domain.PiecePartChorus
	var tune []domain.DocCredit
	for _, c := range p.Doc.Credits {
		if c.Role == domain.CreditRoleTune {
			tune = append(tune, c)
		}
	}
	require.Len(t, tune, 2)
	assert.Equal(t, &verses, tune[0].Part, "the named tune credit is the verses'")
	assert.Equal(t, domain.AttributionNamed, tune[0].Attribution)
	assert.Equal(t, &chorus, tune[1].Part, "the anonymous one the chorus'")
	assert.Equal(t, domain.AttributionAnonymous, tune[1].Attribution)
	assert.Contains(t, kinds(items), "part_inferred")
	parts := map[domain.PiecePart]bool{}
	for _, term := range p.Doc.Terms {
		if term.Part != nil {
			parts[*term.Part] = true
		}
	}
	assert.True(t, parts[verses] && parts[chorus], "origins are scoped to the verses and chorus")
}

func TestGoldenAssert_AuldLangSyne(t *testing.T) {
	p, items := goldenPiece(t, "Auld Lang Syne")

	attributions := map[domain.CreditRole]domain.Attribution{}
	for _, c := range p.Doc.Credits {
		attributions[c.Role] = c.Attribution
	}
	assert.Equal(t, domain.AttributionTraditional, attributions[domain.CreditRoleTune],
		"traditional despite the page's unbalanced brackets")
	assert.Contains(t, kinds(items), string("unbalanced_wikilinks"))
	imp, _ := scrubbedImport(t)
	lyricist := personNamed(t, imp, "Robert Burns")
	assert.Equal(t, domain.PersonKindHistorical, lyricist.Doc.Kind, "a traditional piece's writer (N-2)")
}

func TestGoldenAssert_LasairHasNoStub(t *testing.T) {
	person, _ := goldenPerson(t, "Lasair inghen ui h’Airt")

	imp, _ := scrubbedImport(t)
	for _, p := range imp.Persons {
		if p.Doc.IsStub {
			assert.NotContains(t, strings.ToLower(p.Doc.Name), "lasair", "both spellings are the bard")
		}
	}
	assert.False(t, person.Doc.IsStub)
}

func TestGoldenAssert_SamanthaMoore(t *testing.T) {
	person, items := goldenPerson(t, "Samantha Moore")

	assert.Equal(t, "Samantha Moore", person.Doc.Name, "the title is the guess")
	assert.Equal(t, []domain.DocPersonAlias{{Name: "Ambra Michelli", Kind: domain.PersonAliasKindLegal}},
		person.Doc.Aliases, "the H1 lead is a hidden alias")
	assert.Contains(t, kinds(items), "canonical_name", "listed as ambiguous")
}

func TestGoldenAssert_BandOfBrothers(t *testing.T) {
	p, items := goldenPiece(t, "Band of Brothers")

	assert.True(t, p.LegacyHadInlineLyrics)
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), lyricMarker, "no lyric line in any field")
	assert.Contains(t, kinds(items), "dropped")
}

func TestGoldenAssert_BurningOfTara(t *testing.T) {
	p, _ := goldenPiece(t, "The Burning of Tara")

	assert.Equal(t, domain.PieceKindStory, p.Doc.Kind)
	assert.Equal(t, []domain.DocPieceAlias{
		{Text: "Fionn and the Dragon", Kind: domain.PieceAliasKindAltTitle},
		{Text: "How Finn became leader of the Fianna", Kind: domain.PieceAliasKindAltTitle},
	}, p.Doc.Aliases, "the also-known-as titles")
}

func TestGoldenAssert_MalkinGrey(t *testing.T) {
	person, items := goldenPerson(t, "Malkin Grey")

	assert.Equal(t, domain.PersonStatusDeceased, person.Doc.Status)
	require.NotNil(t, person.Doc.StatusDate)
	assert.Equal(t, "2020-10-31", person.Doc.StatusDate.Format("2006-01-02"))
	require.NotNil(t, person.Doc.StatusSourceURL)
	assert.Equal(t, "https://bardictorch.miraheze.org/wiki/Malkin_Grey", *person.Doc.StatusSourceURL)
	assert.Contains(t, kinds(items), "link_url", "the profile link naming the legal name is hidden")
}

func TestGoldenAssert_FightForTheMidrealm(t *testing.T) {
	p, items := goldenPiece(t, "Fight for the Midrealm")

	assert.Equal(t, "Fight for the Midrealm", p.Doc.Title)
	assert.Empty(t, p.Doc.Aliases, "the infobox's other page title is not an alias")
	assert.Contains(t, kinds(items), "other_page_title")
}

func TestGoldenAssert_MyThingIsMyOwn(t *testing.T) {
	p, items := goldenPiece(t, "My Thing is My Own")

	assert.Contains(t, kinds(items), "single_bracket_category")
	imp, _ := scrubbedImport(t)
	stub := pieceTitled(t, imp, "My Thing Is My Own")
	assert.True(t, stub.Doc.IsStub, "the case variant is a separate stub")
	assert.NotEqual(t, p.ID, stub.ID)
}

func TestGoldenAssert_Greensleeves(t *testing.T) {
	base, _ := goldenPiece(t, "Greensleeves")
	sca, items := goldenPiece(t, "Greensleeves (SCA Version)")

	assert.Equal(t, []domain.DocPieceRelation{{ToPieceID: base.ID, Type: domain.PieceRelationTypeContrafactumOf}},
		sca.Doc.Relations)
	assert.Contains(t, kinds(items), "relation")
	assert.Contains(t, kinds(items), "other_page_title")
}

func TestGoldenAssert_DahrienCordell(t *testing.T) {
	_, items := goldenPerson(t, "Dahrien Cordell")

	assert.Contains(t, kinds(items), "double_bracket_url")
	assert.Contains(t, kinds(items), "malformed_url")
}

func TestGoldenAssert_TitlesListed(t *testing.T) {
	_, dirge := goldenPiece(t, "Birthday Dirge")
	_, wyrd := goldenPiece(t, "Warrior's Wyrd")
	_, voice := goldenPiece(t, "Voice of the Dragon")

	assert.Contains(t, kinds(dirge), "rejected", "the category word title")
	assert.Contains(t, kinds(wyrd), "unparsed", "the prose naming other titles")
	assert.NotEmpty(t, voice)
}
