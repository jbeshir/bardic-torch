package importer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// goldenPiece returns the piece imported from the page titled title.
func goldenPiece(t *testing.T, title string) domain.ImportedPiece {
	t.Helper()
	imp := scrubbedImport(t)
	g, ok := goldenFor(imp, title)
	require.True(t, ok)
	require.NotNil(t, g.Piece, title)

	return *g.Piece
}

// goldenPerson returns the person imported from the bard page titled title.
func goldenPerson(t *testing.T, title string) domain.ImportedPerson {
	t.Helper()
	imp := scrubbedImport(t)
	g, ok := goldenFor(imp, title)
	require.True(t, ok)
	require.NotNil(t, g.Person, title)

	return *g.Person
}

func rolesOf(links []domain.DocPieceLink) []domain.LinkRole {
	var out []domain.LinkRole
	for _, l := range links {
		out = append(out, l.Roles...)
	}

	return out
}

func TestGoldenAssert_BlackAlman(t *testing.T) {
	p := goldenPiece(t, "Black Alman")

	assert.Equal(t, domain.PieceKindDanceMusic, p.Doc.Kind)
	assert.Empty(t, p.Doc.Credits, "the wiki leaves dance_by and music_by empty")
	assert.Subset(t, rolesOf(p.Doc.Links), []domain.LinkRole{domain.LinkRoleSheetMusic,
		domain.LinkRoleDanceInstructions, domain.LinkRolePerformance})
	assert.Contains(t, p.Doc.Aliases, domain.DocPieceAlias{Text: "Black Allemande", Kind: domain.PieceAliasKindAltTitle})
}

func TestGoldenAssert_ThreeWords(t *testing.T) {
	p := goldenPiece(t, "Three Words")

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
	parts := map[domain.PiecePart]bool{}
	for _, term := range p.Doc.Terms {
		if term.Part != nil {
			parts[*term.Part] = true
		}
	}
	assert.True(t, parts[verses] && parts[chorus], "origins are scoped to the verses and chorus")
}

func TestGoldenAssert_AuldLangSyne(t *testing.T) {
	p := goldenPiece(t, "Auld Lang Syne")

	attributions := map[domain.CreditRole]domain.Attribution{}
	for _, c := range p.Doc.Credits {
		attributions[c.Role] = c.Attribution
	}
	assert.Equal(t, domain.AttributionTraditional, attributions[domain.CreditRoleTune],
		"traditional despite the page's unbalanced brackets")
	imp := scrubbedImport(t)
	lyricist := personNamed(t, imp, "Robert Burns")
	assert.Equal(t, domain.PersonKindHistorical, lyricist.Doc.Kind, "a traditional piece's writer")
}

func TestGoldenAssert_LasairHasNoStub(t *testing.T) {
	person := goldenPerson(t, "Lasair inghen ui h’Airt")

	imp := scrubbedImport(t)
	for _, p := range imp.Persons {
		if p.Doc.IsStub {
			assert.NotContains(t, strings.ToLower(p.Doc.Name), "lasair", "both spellings are the bard")
		}
	}
	assert.False(t, person.Doc.IsStub)
}

func TestGoldenAssert_SamanthaMoore(t *testing.T) {
	person := goldenPerson(t, "Samantha Moore")

	assert.Equal(t, "Samantha Moore", person.Doc.Name, "the title is the guess")
}

func TestGoldenAssert_BandOfBrothers(t *testing.T) {
	p := goldenPiece(t, "Band of Brothers")

	assert.True(t, p.LegacyHadInlineLyrics)
}

func TestGoldenAssert_BurningOfTara(t *testing.T) {
	p := goldenPiece(t, "The Burning of Tara")

	assert.Equal(t, domain.PieceKindStory, p.Doc.Kind)
	assert.Equal(t, []domain.DocPieceAlias{
		{Text: "Fionn and the Dragon", Kind: domain.PieceAliasKindAltTitle},
		{Text: "How Finn became leader of the Fianna", Kind: domain.PieceAliasKindAltTitle},
	}, p.Doc.Aliases, "the also-known-as titles")
}

func TestGoldenAssert_MalkinGrey(t *testing.T) {
	person := goldenPerson(t, "Malkin Grey")

	assert.Equal(t, domain.PersonStatusDeceased, person.Doc.Status)
	require.NotNil(t, person.Doc.StatusDate)
	assert.Equal(t, "2020-10-31", person.Doc.StatusDate.Format("2006-01-02"))
	require.NotNil(t, person.Doc.StatusSourceURL)
	assert.Equal(t, "http://falconbanner.gladiusinfractus.com/2021/01/22/in-memoriam-the-passing-of-mistress-malkin-grey/",
		*person.Doc.StatusSourceURL, "the obituary the date is read from")
}

func TestGoldenAssert_FightForTheMidrealm(t *testing.T) {
	p := goldenPiece(t, "Fight for the Midrealm")

	assert.Equal(t, "Fight for the Midrealm", p.Doc.Title)
	assert.Empty(t, p.Doc.Aliases, "the infobox's other page title is not an alias")
}

func TestGoldenAssert_MyThingIsMyOwn(t *testing.T) {
	p := goldenPiece(t, "My Thing is My Own")

	imp := scrubbedImport(t)
	stub := pieceTitled(t, imp, "My Thing Is My Own")
	assert.True(t, stub.Doc.IsStub, "the case variant is a separate stub")
	assert.NotEqual(t, p.ID, stub.ID)
}

func TestGoldenAssert_Greensleeves(t *testing.T) {
	base := goldenPiece(t, "Greensleeves")
	sca := goldenPiece(t, "Greensleeves (SCA Version)")

	assert.Equal(t, []domain.DocPieceRelation{{ToPieceID: base.ID, Type: domain.PieceRelationTypeContrafactumOf}},
		sca.Doc.Relations)
}

func TestGoldenAssert_OtherPageTitleIsNoAlias(t *testing.T) {
	voice := goldenPiece(t, "Voice of the Dragon")

	assert.Equal(t, "Voice of the Dragon", voice.Doc.Title)
	fight := goldenPiece(t, "Fight for the Midrealm")
	for _, a := range fight.Doc.Aliases {
		assert.NotEqual(t, domain.CollationKey(voice.Doc.Title), domain.CollationKey(a.Text), "never an alias")
	}
}

// TestGoldenAssert_RelationsFromTitles: the report §8.1 pairs. "(SCA Version)" relates to its base whatever the
// case of the base's title, and with no bare page the "(Poem)" and the "(Song)" relate to each other, once.
func TestGoldenAssert_RelationsFromTitles(t *testing.T) {
	imp := scrubbedImport(t)
	knight, base := pieceTitled(t, imp, "When A Knight Won His Spurs (SCA Version)"),
		pieceTitled(t, imp, "When a Knight Won His Spurs")
	poem, song := pieceTitled(t, imp, "Ode to Commius (Poem)"), pieceTitled(t, imp, "Ode to Commius (Song)")

	require.Len(t, knight.Doc.Relations, 1)
	assert.Equal(t, base.ID, knight.Doc.Relations[0].ToPieceID)
	later, earlier := poem, song
	if song.ID > poem.ID {
		later, earlier = song, poem
	}
	assert.Empty(t, earlier.Doc.Relations, "related once, from the later page")
	assert.Equal(t, []domain.DocPieceRelation{{ToPieceID: earlier.ID, Type: domain.PieceRelationTypeSameTextOtherForm}},
		later.Doc.Relations)
}
