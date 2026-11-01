package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	anotherSong  = "Another Song"
	unwritten    = "An Unwritten Song"
	songsSection = "Songs & Works"
	notesPage    = "Notes Page"
)

// peopleOf parses the pieces and people of a fixture wiki.
func peopleOf(w *wikiFixture) (People, map[string]PieceRecord) {
	s := w.site()
	pieces := s.Pieces()
	byTitle := map[string]PieceRecord{}
	for _, p := range pieces {
		byTitle[p.Title] = p
	}

	return ParsePeople(s, pieces), byTitle
}

func addPiece(w *wikiFixture, title string, params [][2]string, categories ...string) {
	w.page(Page{Title: title, Wikitext: pieceWikitext(title, params), Categories: pieceCategories(categories...)})
}

func addBard(w *wikiFixture, title string, sections ...string) {
	w.page(Page{Title: title, Wikitext: bardWikitext(title, sections...), Categories: []string{CategoryBards}})
}

func disagreementsOf(p People, kind CreditDisagreementKind) []CreditDisagreement {
	var out []CreditDisagreement
	for _, d := range p.Disagreements {
		if d.Kind == kind {
			out = append(out, d)
		}
	}

	return out
}

func TestUnion_InfoboxCreditsResolved(t *testing.T) {
	w := standardWiki()
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + wren + "]] & " + rowan}}, wren)

	people, pieces := peopleOf(w)

	credits := people.Credits[pieces[aSong].PageID]
	require.Len(t, credits, 2)
	assert.Equal(t, PersonRef(wren), credits[0].Person)
	assert.Equal(t, []CreditSignal{SignalInfobox, SignalCategory}, credits[0].Signals)
	assert.Equal(t, PersonRef(rowan), credits[1].Person, "an unmatched text name is a stub person")
	_, ok := people.Persons.Stub(rowan)
	assert.True(t, ok)
}

func TestUnion_CategoryOnlyAddsPossibleCredit(t *testing.T) {
	w := standardWiki()
	addPiece(w, aSong, [][2]string{{lyricsByParam, "Unknown"}}, wren)

	people, pieces := peopleOf(w)

	credits := people.Credits[pieces[aSong].PageID]
	require.Len(t, credits, 2)
	assert.Equal(t, PieceCredit{
		CreditRecord: CreditRecord{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
			Certainty: domain.CertaintyPossible, Name: PersonName{Name: wren, Form: NameCategory}},
		Person: wren, Signals: []CreditSignal{SignalCategory},
	}, credits[1])
	assert.Equal(t, []CreditDisagreement{{Kind: DisagreementCategoryOnly, Piece: aSong, Person: wren, Name: wren}},
		disagreementsOf(people, DisagreementCategoryOnly))
	assert.Equal(t, []CreditDisagreement{{Kind: DisagreementCategoryWithoutLinkedCredit, Piece: aSong, Person: wren,
		Name: wren}}, disagreementsOf(people, DisagreementCategoryWithoutLinkedCredit))
}

func TestUnion_WorksListOnlyAddsPossibleCredit(t *testing.T) {
	w := standardWiki()
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}})
	addBard(w, rowan, songsSection, "* [["+aSong+"]]")

	people, pieces := peopleOf(w)

	credits := people.Credits[pieces[aSong].PageID]
	require.Len(t, credits, 2)
	assert.Equal(t, PersonRef(rowan), credits[1].Person)
	assert.Equal(t, domain.CertaintyPossible, credits[1].Certainty)
	assert.Equal(t, domain.CreditRoleLyrics, credits[1].Role)
	assert.Equal(t, []CreditSignal{SignalWorksList}, credits[1].Signals)
	assert.Len(t, disagreementsOf(people, DisagreementWorksListOnly), 1)
}

func TestUnion_RelatedPersonSignalAddsNoCredit(t *testing.T) {
	w := standardWiki()
	addBard(w, ash)
	addBard(w, rowan, "Affiliations", "* The chosen heir of [["+ash+"]].")
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}}, ash, rowan)

	people, pieces := peopleOf(w)

	credits := people.Credits[pieces[aSong].PageID]
	require.Len(t, credits, 1)
	assert.Equal(t, PersonRef(ash), credits[0].Person)
	assert.Equal(t, []CreditDisagreement{{Kind: DisagreementRelatedPerson, Piece: aSong, Person: rowan, Name: rowan}},
		disagreementsOf(people, DisagreementRelatedPerson))
}

func TestUnion_ReferenceDisagreements(t *testing.T) {
	w := standardWiki()
	addBard(w, ash, songsSection, "* [["+anotherSong+"]]")
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + wren + "]]"}, {tuneByParam, "[[" + ash + "]]"}}, wren)
	addPiece(w, anotherSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}}, ash)

	people, _ := peopleOf(w)

	assert.Equal(t, []CreditDisagreement{{Kind: DisagreementMissingBardCategory, Piece: aSong, Person: ash, Name: ash}},
		disagreementsOf(people, DisagreementMissingBardCategory))
	assert.Empty(t, disagreementsOf(people, DisagreementCategoryWithoutLinkedCredit))
	assert.Equal(t, []CreditDisagreement{
		{Kind: DisagreementNotListed, Piece: aSong, Person: wren, Name: wren},
		{Kind: DisagreementNotListed, Piece: aSong, Person: ash, Name: ash},
	}, disagreementsOf(people, DisagreementNotListed))
}

func TestUnion_WorksListMissingPageBecomesStub(t *testing.T) {
	w := standardWiki()
	addBard(w, ash, songsSection, "* [["+unwritten+"]]")
	addBard(w, rowan, songsSection, "* [["+unwritten+"]]")

	people, _ := peopleOf(w)

	require.Len(t, people.StubPieces, 1)
	sp := people.StubPieces[0]
	assert.Equal(t, unwritten, sp.Title)
	assert.Empty(t, sp.CaseVariantOf)
	require.Len(t, sp.Credits, 2)
	for k, bard := range []PersonRef{ash, rowan} {
		assert.Equal(t, bard, sp.Credits[k].Person)
		assert.Equal(t, domain.CreditRoleLyrics, sp.Credits[k].Role)
		assert.Equal(t, domain.CertaintyPossible, sp.Credits[k].Certainty)
		assert.Equal(t, []CreditSignal{SignalWorksList}, sp.Credits[k].Signals)
	}
}

func TestUnion_CaseVariantIsSeparateStub(t *testing.T) {
	w := standardWiki()
	addPiece(w, "My Song is My Own", nil)
	addBard(w, ash, songsSection, "* [[My Song Is My Own]]")

	people, _ := peopleOf(w)

	require.Len(t, people.StubPieces, 1)
	assert.Equal(t, "My Song Is My Own", people.StubPieces[0].Title)
	assert.Equal(t, "My Song is My Own", people.StubPieces[0].CaseVariantOf)
	assert.Contains(t, people.Ambiguities(), Ambiguity{Kind: AmbiguityCaseVariantWork, Page: ash,
		Name: "My Song Is My Own", Guess: "My Song Is My Own", Alternatives: []string{"My Song is My Own"}})
}

func TestPeople_Ambiguities(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: ash, Wikitext: bardWikitext(rowan + " (" + ash + ")"), Categories: []string{CategoryBards}})
	addPiece(w, aSong, [][2]string{{lyricsByParam, dunstanFirstWord}})
	addPiece(w, anotherSong, [][2]string{{lyricsByParam, "[[" + dunstanStub + "]]"}})

	people, _ := peopleOf(w)

	assert.Equal(t, []Ambiguity{
		{Kind: AmbiguityCanonicalName, Page: ash, Name: rowan, Guess: ash, Alternatives: []string{rowan}},
		{Kind: AmbiguityFuzzyPerson, Page: aSong, Name: dunstanFirstWord, Guess: dunstanStub,
			Alternatives: []string{dunstanFirstWord}},
	}, people.Ambiguities())
}

func TestRedLinks_Keys(t *testing.T) {
	w := standardWiki()
	w.describe(catWar, "")
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}}, catWar)
	w.page(Page{Title: notesPage, Wikitext: "[[" + aSong + "]] [[" + ash + "]] [[:Category:" + catWar + "]] " +
		"[[:Category:Missing]] [[Category:Tagged Only]] [[Special:Search]] [[Template:Gone]] [[w:Elsewhere]]"})

	got := RedLinks(w.site())

	assert.Equal(t, map[string][]string{
		"0:" + ash:   {aSong, notesPage},
		"14:Missing": {notesPage},
		"-1:Search":  {notesPage},
	}, got)
}

func TestClassifyRedLink_Positions(t *testing.T) {
	w := standardWiki()
	addPiece(w, aSong, [][2]string{{lyricsByParam, "[[" + ash + "]]"}, {tuneByParam, "[[" + catTraditional + "]]"}})
	w.page(Page{Title: anotherSong, Categories: pieceCategories(), Wikitext: pieceWikitext(anotherSong, nil,
		"Lyrics", "* [https://example.org/a Words] adapted by [["+rowan+" the Bold]]",
		"Performances & Recordings", "* [https://example.org/b Recording] of [["+wrenOfTheVale+"]]")})
	addBard(w, rowan, songsSection, "* [["+unwritten+"]]", "Performances & Recordings",
		"* [[https://example.org/c Recording by Placeholder on YouTube]]")
	w.page(Page{Title: "Easy Start Add a Song", Wikitext: "[[The Placeholder Example]]"})

	people, _ := peopleOf(w)

	classes := map[string]RedLinkClass{}
	for _, r := range people.RedLinks {
		classes[RedLinkKey(r.NS, r.Target)] = r.Class
	}
	assert.Equal(t, map[string]RedLinkClass{
		"0:" + ash:                 RedLinkCredit,
		"0:" + catTraditional:      RedLinkCredit,
		"0:" + rowan + " the Bold": RedLinkPerson,
		"0:" + wrenOfTheVale:       RedLinkPerformer,
		"0:" + unwritten:           RedLinkWork,
		"0:Https://example.org/c Recording by Placeholder on YouTube": RedLinkMalformedURL,
		"0:The Placeholder Example":                                   RedLinkNotImported,
	}, classes)
	// Persons named in prose are stub persons; performers are matched with the performances.
	_, ok := people.Persons.Stub(rowan + " the Bold")
	assert.True(t, ok)
	_, ok = people.Persons.Stub(wrenOfTheVale)
	assert.False(t, ok)
}

func TestClassifyRedLink_Unclassified(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: aSong, Categories: pieceCategories(), Wikitext: pieceWikitext(aSong, nil,
		"Notes", "See also [[A Placeholder Topic]].")})

	people, _ := peopleOf(w)

	require.Len(t, people.RedLinks, 1)
	assert.Equal(t, RedLink{NS: NSMain, Target: "A Placeholder Topic", Pages: []string{aSong},
		Class: RedLinkUnclassified}, people.RedLinks[0])
	assert.Empty(t, people.Persons.Stubs)
}
