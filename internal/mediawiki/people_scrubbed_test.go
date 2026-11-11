package mediawiki

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// scrubbedPeople parses the people of the committed scrubbed dump.
func scrubbedPeople(t *testing.T) (People, map[string]PieceRecord) {
	t.Helper()
	s := scrubbedSite(t)
	pieces := s.Pieces()
	byTitle := map[string]PieceRecord{}
	for _, p := range pieces {
		byTitle[p.Title] = p
	}

	return ParsePeople(s, pieces), byTitle
}

func TestScrubbedPeople_ResearchCounts(t *testing.T) {
	people, _ := scrubbedPeople(t)
	s := scrubbedSite(t)

	assert.Len(t, people.Bards, 48)
	namespaces := map[int]int{}
	classes := map[RedLinkClass]int{}
	for _, r := range ClassifyRedLinks(s, s.Pieces(), people.Bards) {
		namespaces[r.NS]++
		classes[r.Class]++
	}
	assert.Equal(t, map[int]int{NSMain: 180, NSCategory: 22, NSSpecial: 1}, namespaces)
	assert.Equal(t, map[RedLinkClass]int{RedLinkWork: 78, RedLinkCredit: 40, RedLinkPerson: 2, RedLinkPerformer: 58,
		RedLinkMalformedURL: 1, RedLinkNotImported: 2, RedLinkCategory: 22}, classes)
	missingWorks := 0
	for _, b := range people.Bards {
		for _, w := range b.Works {
			if !w.Exists {
				missingWorks++
			}
		}
	}
	assert.Equal(t, 78, missingWorks)
	assert.Len(t, people.StubPieces, 78)
}

func TestScrubbedPeople_Credits(t *testing.T) {
	people, pieces := scrubbedPeople(t)
	credits := func(title string) []PieceCredit { return people.Credits[pieces[title].PageID] }

	// Both spellings of the apostrophe name one bard, with no stub.
	for _, title := range []string{"Fight for the Midrealm", "Valkyrie's Lullaby"} {
		require.NotEmpty(t, credits(title), title)
		assert.Equal(t, PersonRef("Lasair inghen ui h’Airt"), credits(title)[0].Person, title)
	}
	_, ok := people.Persons.Stub("Lasair inghen ui h'Airt")
	assert.False(t, ok)

	band := credits("Band of Brothers")
	require.Len(t, band, 3)
	assert.Equal(t, PersonRef("Ken Theriot"), band[1].Person)
	assert.Equal(t, PersonRef("Lisa Theriot"), band[2].Person)

	three := credits("Three Words")
	require.Len(t, three, 3)
	assert.Equal(t, domain.AttributionAnonymous, three[2].Attribution)
	require.NotNil(t, three[2].Part)
	assert.Equal(t, domain.PiecePartChorus, *three[2].Part)

	assert.Equal(t, domain.CertaintyProbable, credits("Moose")[0].Certainty)
	assert.Equal(t, domain.CertaintyDisputed, credits("Wild Mountain Thyme")[0].Certainty)
	greensleeves := credits("Greensleeves")
	require.Len(t, greensleeves, 3)
	assert.Equal(t, domain.AttributionTraditional, greensleeves[0].Attribution)
	assert.Equal(t, domain.CertaintyPossible, greensleeves[1].Certainty)
	for _, title := range []string{"My Thing is My Own", "Auld Lang Syne"} {
		tune := credits(title)[len(credits(title))-1]
		assert.Equal(t, domain.AttributionTraditional, tune.Attribution, title)
	}
	assert.Empty(t, credits("Black Alman"))

	// March of Cambreadth's category for the heir adds no credit.
	for _, c := range credits("March of Cambreadth") {
		assert.Equal(t, PersonRef("Heather Alexander"), c.Person)
	}
}

func TestScrubbedPeople_Bards(t *testing.T) {
	people, _ := scrubbedPeople(t)
	bards := map[string]BardRecord{}
	for _, b := range people.Bards {
		bards[b.Title] = b
	}

	malkin := bards["Malkin Grey"]
	assert.Equal(t, domain.PersonStatusDeceased, malkin.Status)
	require.NotNil(t, malkin.StatusDate)
	assert.Equal(t, time.Date(2020, 10, 31, 0, 0, 0, 0, time.UTC), malkin.StatusDate.Time)
	assert.Equal(t, []PersonRelation{{Type: domain.PersonRelationTypeHeirOf, To: "Heather Alexander"}},
		bards["Alexander James Adams"].Relations)
	assert.Equal(t, domain.LicensingStatusUnknown, bards["Efenwealt Wystle"].LicensingStatus)
	assert.Equal(t, domain.LicensingStatusKnown, bards["Heather Dale"].LicensingStatus)
	var redirect PersonAlias
	for _, a := range bards["Jim Pipkin"].Aliases {
		if a.Source == AliasFromRedirect {
			redirect = a
		}
	}
	assert.Equal(t, domain.PersonAliasKindRedirect, redirect.Kind)
}
