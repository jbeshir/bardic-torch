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
	assert.Empty(t, p.Doc.Credits, "the page credits no one")
	assert.Subset(t, rolesOf(p.Doc.Links), []domain.LinkRole{domain.LinkRoleSheetMusic,
		domain.LinkRoleDanceInstructions, domain.LinkRolePerformance})
	assert.Contains(t, p.Doc.Aliases, domain.DocPieceAlias{Text: "Black Allemande", Kind: domain.PieceAliasKindAltTitle})
}

func TestGoldenAssert_ThreeWords(t *testing.T) {
	p := goldenPiece(t, "Three Words")

	var tune []domain.Attribution
	for _, c := range p.Doc.Credits {
		if c.Role == domain.CreditRoleTune {
			tune = append(tune, c.Attribution)
		}
	}
	assert.Equal(t, []domain.Attribution{domain.AttributionNamed, domain.AttributionAnonymous}, tune)
	require.NotNil(t, p.Doc.NotesMD)
	assert.Contains(t, *p.Doc.NotesMD, "Chorus tune: Anon. Sephardic song", "the notes say whose the chorus is")
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
	require.NotNil(t, person.Doc.NotesMD)
	assert.Equal(t, "- [Deceased October 31, 2020](http://falconbanner.gladiusinfractus.com/2021/01/22/"+
		"in-memoriam-the-passing-of-mistress-malkin-grey/)", *person.Doc.NotesMD, "the date and its obituary")
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
	sca := goldenPiece(t, "Greensleeves (SCA Version)")

	require.NotNil(t, sca.Doc.NotesMD)
	assert.Equal(t, "New words to the tune of Greensleeves.", *sca.Doc.NotesMD)
}

func TestGoldenAssert_OtherPageTitleIsNoAlias(t *testing.T) {
	voice := goldenPiece(t, "Voice of the Dragon")

	assert.Equal(t, "Voice of the Dragon", voice.Doc.Title)
	fight := goldenPiece(t, "Fight for the Midrealm")
	for _, a := range fight.Doc.Aliases {
		assert.NotEqual(t, domain.CollationKey(voice.Doc.Title), domain.CollationKey(a.Text), "never an alias")
	}
}

// TestGoldenAssert_OtherPieceFromTitles: the report §8.1 pairs. "(SCA Version)" names its base in its notes
// whatever the case of the base's title, and with no bare page the later of the "(Poem)" and the "(Song)" names the
// other, once.
func TestGoldenAssert_OtherPieceFromTitles(t *testing.T) {
	imp := scrubbedImport(t)
	knight := pieceTitled(t, imp, "When A Knight Won His Spurs (SCA Version)")
	poem, song := pieceTitled(t, imp, "Ode to Commius (Poem)"), pieceTitled(t, imp, "Ode to Commius (Song)")

	require.NotNil(t, knight.Doc.NotesMD)
	assert.True(t, strings.HasSuffix(*knight.Doc.NotesMD, "\n\nNew words to the tune of When a Knight Won His Spurs."),
		"after the page's own notes")
	later, earlier := poem, song
	if song.ID > poem.ID {
		later, earlier = song, poem
	}
	require.NotNil(t, earlier.Doc.NotesMD)
	assert.NotContains(t, *earlier.Doc.NotesMD, "The same text as", "said once, on the later page")
	require.NotNil(t, later.Doc.NotesMD)
	assert.Equal(t, "The same text as "+earlier.Doc.Title+", in another form.", *later.Doc.NotesMD)
}
