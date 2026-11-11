package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	curlyBard        = "Wren inghen ui h’Placeholder"
	heatherBard      = "Heather Placeholder"
	marionAlias      = "Marion of Placeholderdale"
	dunstanStub      = "Dunstan le Placeholder"
	dunstanFirstWord = "Dunstan"
)

func indexOf(t *testing.T, w *wikiFixture, pages ...Page) PersonIndex {
	t.Helper()
	for _, p := range pages {
		w.page(p)
	}

	return NewPersonIndex(w.site().Bards())
}

func indexedBard(title, h1 string) Page {
	return Page{Title: title, Wikitext: bardWikitext(h1), Categories: []string{CategoryBards}}
}

func TestPersonIndex_ApostropheFold(t *testing.T) {
	x := indexOf(t, newWikiFixture(), indexedBard(curlyBard, curlyBard))

	m := x.Match("Wren inghen ui h'Placeholder")

	assert.Equal(t, PersonMatch{Kind: MatchExact, Person: curlyBard}, m)
}

func TestPersonIndex_FuzzyAliasEditDistance(t *testing.T) {
	x := indexOf(t, newWikiFixture(), indexedBard(heatherBard, heatherBard+" ("+marionAlias+")"))

	m := x.Match("Marian of Placeholderdale")

	assert.Equal(t, PersonMatch{Kind: MatchFuzzy, Person: heatherBard}, m)
	assert.Equal(t, MatchNone, x.Match("Mariam of Placeholderdaly").Kind, "two edits are no match")
}

func TestPersonIndex_SwapIsOneEdit(t *testing.T) {
	x := indexOf(t, newWikiFixture(), indexedBard(heatherBard, heatherBard))

	assert.Equal(t, MatchFuzzy, x.Match("Haether Placeholder").Kind)
	assert.Equal(t, MatchFuzzy, x.Match("Heather Placeholders").Kind)
	assert.Equal(t, MatchFuzzy, x.Match("Heathe Placeholder").Kind)
	assert.Equal(t, MatchNone, x.Match("Heat").Kind)
}

func TestPersonIndex_DigitsAreNeverFuzzy(t *testing.T) {
	x := indexOf(t, newWikiFixture(), indexedBard(wren, wren+" (Legal Name 001)"))

	assert.Equal(t, MatchNone, x.Match("Legal Name 002").Kind)
	assert.Equal(t, MatchExact, x.Match("legal name 001").Kind)
}

func TestPersonIndex_FirstTokenUnique(t *testing.T) {
	x := indexOf(t, newWikiFixture(), indexedBard(wren, wren), indexedBard(ash, ash))

	assert.Equal(t, PersonMatch{Kind: MatchFuzzy, Person: wren}, x.Match(wrenFirst))
}

func TestPersonIndex_FirstTokenAmbiguousIsStub(t *testing.T) {
	w := newWikiFixture()
	x := indexOf(t, w, indexedBard(wren, wren), indexedBard(wrenOfTheVale, wrenOfTheVale))

	m := x.Match(wrenFirst)
	assert.Equal(t, PersonMatch{Kind: MatchNone}, m)

	mention := PersonMention{Name: PersonName{Name: wrenFirst, Form: NameText}, Page: aSong}
	persons := ResolvePersons(x, []PersonMention{mention}, w.site())
	ref, ok := persons.Resolve(mention.Name)
	require.True(t, ok)
	assert.Equal(t, PersonRef(wrenFirst), ref)
	require.Len(t, persons.Stubs, 1)
}

func TestResolvePersons_TextMatchesStubsOfLinkedNames(t *testing.T) {
	w := newWikiFixture()
	text := PersonMention{Name: PersonName{Name: dunstanFirstWord, Form: NameText}, Page: aSong}
	linked := PersonMention{Name: PersonName{Name: dunstanStub, Form: NamePageLink}, Page: "Another Song"}

	persons := ResolvePersons(NewPersonIndex(nil), []PersonMention{text, linked}, w.site())

	ref, _ := persons.Resolve(text.Name)
	assert.Equal(t, PersonRef(dunstanStub), ref)
	require.Len(t, persons.Stubs, 1)
	assert.Equal(t, StubPerson{Name: dunstanStub, Kind: domain.PersonKindCommunityBard, LegacyTitle: dunstanStub},
		persons.Stubs[0])
}

func TestResolvePersons_StubsByFoldedName(t *testing.T) {
	w := newWikiFixture()
	const url = "https://en.wikipedia.org/wiki/Ash_Placeholder"
	mentions := []PersonMention{
		{Name: PersonName{Name: ash, Form: NameExternalLink, URL: url}, Page: aSong},
		{Name: PersonName{Name: "Ash placeholder", Form: NameCategory}, Page: aSong},
		{Name: PersonName{Name: "Placeholder Waits", Form: NameText}, Page: aSong},
	}

	persons := ResolvePersons(NewPersonIndex(nil), mentions, w.site())

	require.Len(t, persons.Stubs, 2)
	assert.Equal(t, StubPerson{Name: ash, Kind: domain.PersonKindHistorical, ProfileURLs: []string{url}},
		persons.Stubs[0])
	ref, ok := persons.Resolve(mentions[1].Name)
	require.True(t, ok)
	assert.Equal(t, PersonRef(ash), ref, "the folded name finds the first stub")
	assert.Equal(t, domain.PersonKindGroup, persons.Stubs[1].Kind)
}
