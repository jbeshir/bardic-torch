package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

func TestParsePiece_Kind(t *testing.T) {
	for name, tc := range map[string]struct {
		template   string
		categories []string
		want       domain.PieceKind
		secondary  []domain.PieceKind
	}{
		"song infobox":             {"Infobox song", pieceCategories(), domain.PieceKindSong, nil},
		"poem infobox":             {"Infobox poem", []string{CategoryPieces, CategoryPoems}, domain.PieceKindPoem, nil},
		"story infobox, also poem": {"Infobox story", []string{CategoryPieces, CategoryPoems, CategoryStories}, domain.PieceKindStory, []domain.PieceKind{domain.PieceKindPoem}},
		"dance infobox":            {"Infobox dance&music", []string{CategoryPieces, CategoryDances, CategoryInstrumental}, domain.PieceKindDanceMusic, nil},
		"dances and instrumental":  {"Infobox song", pieceCategories(CategoryDances, CategoryInstrumental), domain.PieceKindDanceMusic, []domain.PieceKind{domain.PieceKindSong}},
		"no infobox, poem":         {"", []string{CategoryPieces, CategoryPoems}, domain.PieceKindPoem, nil},
		"no infobox, no genre":     {"", []string{CategoryPieces}, domain.PieceKindSong, nil},
		"no infobox, instrumental": {"", []string{CategoryPieces, CategoryInstrumental}, domain.PieceKindInstrumental, nil},
	} {
		t.Run(name, func(t *testing.T) {
			text := "= A Song =\n"
			if tc.template != "" {
				text += "{{" + tc.template + "\n| title = A Song\n}}\n"
			}
			p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: tc.categories})

			assert.Equal(t, tc.want, p.Kind)
			assert.Equal(t, tc.secondary, p.SecondaryKinds)
		})
	}
}

func TestParsePiece_SecondaryInstrumental(t *testing.T) {
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories(CategoryInstrumental)})

	assert.Equal(t, domain.PieceKindSong, p.Kind)
	assert.Equal(t, []domain.PieceKind{domain.PieceKindInstrumental}, p.SecondaryKinds)
}

func TestParsePiece_TermsFromCategories(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{
		{"mood", categoryLink(catComedic) + ", " + categoryLink(catWarlike)},
		{"topic", categoryLink(patriotic) + " " + "[[:Category:Patriotic (Calontir)|(Calontir)]]"},
		{"culture", categoryLink(catWelsh)},
	})
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text,
		Categories: pieceCategories(catEndangered, patriotic, catPatrioticKingdom, catSCAPieces, catWar,
			catWarlike, wren)})

	// Infobox order first (Warlike, Patriotic, Patriotic (Calontir)), then the remaining categories in order;
	// Comedic and Welsh are in the infobox but not tagged, so they are not terms.
	assert.Equal(t, []PieceTerm{
		{Category: catWarlike, IsPrimary: true},
		{Category: patriotic, IsPrimary: true},
		{Category: catPatrioticKingdom},
		{Category: catSCAPieces, IsPrimary: true},
		{Category: catWar},
	}, p.Terms)
	assert.Equal(t, []domain.ConservationStatus{domain.ConservationStatusEndangered}, p.ConservationStatuses)
	assert.Equal(t, domain.ConservationStatusEndangered, p.Conservation())
	assert.Equal(t, []string{wren}, p.BardNameCategories)
}

// TestParsePiece_PartQualifiers: "(chorus tune)" scopes an origin to the chorus, as it does a credit.
func TestParsePiece_PartQualifiers(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{
		{"culture", categoryLink(catWelsh) + " (" + categoryLink(catPatrioticWales) + ") (chorus tune)"},
		{sourceParam, categoryLink(catSCAPieces) + " (verses) / " + categoryLink(catContrafacta) + " (chorus)"},
	})
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text,
		Categories: pieceCategories(catContrafacta, catPatrioticWales, catSCAPieces, catWelsh)})

	part := func(p domain.PiecePart) *domain.PiecePart { return &p }
	assert.Equal(t, []PieceTerm{
		{Category: catWelsh, IsPrimary: true, Part: part(domain.PiecePartChorus)},
		{Category: catPatrioticWales, IsPrimary: true, Part: part(domain.PiecePartChorus)},
		{Category: catSCAPieces, IsPrimary: true, Part: part(domain.PiecePartVerses)},
		{Category: catContrafacta, Part: part(domain.PiecePartChorus)},
	}, p.Terms)
}

func TestParsePiece_RedirectCategoryResolves(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{{sourceParam, categoryLink(catTraditionalRedirect)}})
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text,
		Categories: pieceCategories(catTraditional, catTraditionalRedirect)})

	assert.Equal(t, []PieceTerm{{Category: catTraditional, IsPrimary: true}}, p.Terms,
		"both the redirect and its target are one term, first in infobox order")
}

func TestParsePiece_ConservationMostSevere(t *testing.T) {
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories(catEndangered, "Critically Endangered")})

	assert.Equal(t, []domain.ConservationStatus{domain.ConservationStatusEndangered,
		domain.ConservationStatusCriticallyEndangered}, p.ConservationStatuses)
	assert.Equal(t, domain.ConservationStatusCriticallyEndangered, p.Conservation())

	unassessed := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories()})
	assert.Equal(t, domain.ConservationStatusUnassessed, unassessed.Conservation())
}

func TestParsePiece_AffiliationCategoriesGiveNothing(t *testing.T) {
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil),
		Categories: pieceCategories(kingdom, CategorySCABards)})

	assert.Empty(t, p.Terms)
	assert.Empty(t, p.BardNameCategories)
}

func TestParsePiece_RepairsMarkupBeforeParsing(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{{sourceParam, "[:Category:Traditional & Folk|Traditional & Folk]"}})
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text,
		Categories: pieceCategories(catTraditional)})

	assert.Equal(t, []PieceTerm{{Category: catTraditional, IsPrimary: true}}, p.Terms)
	names := []string{}
	for _, param := range p.Parsed.Infoboxes[0].Params {
		names = append(names, param.Name)
	}
	assert.Equal(t, []string{sourceParam}, names, "no bogus positional parameter")
}

// TestScrubbedPieces_ResearchCounts asserts the research counts that pieces give on the committed scrubbed copy:
// 142 pieces (135 songs, 4 poems, 2 stories, 1 dance and music), 11 with inline lyrics and 11 with a conservation
// status.
func TestScrubbedPieces_ResearchCounts(t *testing.T) {
	pieces := scrubbedSite(t).Pieces()
	kinds := map[domain.PieceKind]int{}
	lyrics, conservation := 0, 0
	for _, p := range pieces {
		kinds[p.Kind]++
		if p.Lyrics.HadInlineLyrics {
			lyrics++
		}
		if p.Conservation() != domain.ConservationStatusUnassessed {
			conservation++
		}
	}

	assert.Len(t, pieces, 142)
	assert.Equal(t, map[domain.PieceKind]int{domain.PieceKindSong: 135, domain.PieceKindPoem: 4,
		domain.PieceKindStory: 2, domain.PieceKindDanceMusic: 1}, kinds)
	assert.Equal(t, 11, lyrics)
	assert.Equal(t, 11, conservation)
}

func TestScrubbedPieces_Examples(t *testing.T) {
	pieces := map[string]PieceRecord{}
	for _, p := range scrubbedSite(t).Pieces() {
		pieces[p.Title] = p
	}
	part := func(p domain.PiecePart) *domain.PiecePart { return &p }

	black := pieces["Black Alman"]
	assert.Equal(t, domain.PieceKindDanceMusic, black.Kind)
	assert.Empty(t, black.SecondaryKinds)

	three := pieces["Three Words"]
	assert.Contains(t, three.Terms, PieceTerm{Category: catSCAPieces, IsPrimary: true,
		Part: part(domain.PiecePartVerses)})
	assert.Contains(t, three.Terms, PieceTerm{Category: catContrafacta, Part: part(domain.PiecePartChorus)})
	assert.Contains(t, three.Terms, PieceTerm{Category: catSephardic, IsPrimary: true,
		Part: part(domain.PiecePartChorus)}, "(chorus tune): the chorus")

	assert.Equal(t, []domain.PieceKind{domain.PieceKindInstrumental}, pieces["Burden of the Crown"].SecondaryKinds)
	assert.Equal(t, domain.ConservationStatusCriticallyEndangered, pieces["Oathkeeping"].Conservation())
	assert.True(t, pieces["Band of Brothers"].Lyrics.HadInlineLyrics)
}
