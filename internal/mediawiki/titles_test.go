package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

func titlesOf(t *testing.T, w *wikiFixture, title, wikitext string) PieceTitles {
	t.Helper()

	return parseFixturePiece(w, Page{Title: title, Wikitext: wikitext, Categories: pieceCategories()}).Titles
}

func TestPieceTitles_InfoboxSplit(t *testing.T) {
	for name, tc := range map[string]struct {
		title, value string
		want         []AltTitle
	}{
		"slash": {"Black Alman", "Black Alman / Black Allemande", []AltTitle{
			{Text: "Black Allemande", Kind: domain.PieceAliasKindAltTitle, Source: TitleFromInfobox},
		}},
		"parenthetical with slash": {"Rise", "Rise (We Shall Rise / Rise Placeholder)", []AltTitle{
			{Text: "We Shall Rise", Kind: domain.PieceAliasKindAltTitle, Source: TitleFromInfobox},
			{Text: "Rise Placeholder", Kind: domain.PieceAliasKindAltTitle, Source: TitleFromInfobox},
		}},
		"parenthetical subtitle": {"Wren's Song", "Wren's Song (The Vale Sings Softly)", []AltTitle{
			{Text: "The Vale Sings Softly", Kind: domain.PieceAliasKindSubtitle, Source: TitleFromInfobox},
		}},
		"other title with a parenthetical": {"Quantas Sabedes Amar Amigo", "Quantas Sabedes (Cantiga V)", []AltTitle{
			{Text: "Quantas Sabedes (Cantiga V)", Kind: domain.PieceAliasKindAltTitle, Source: TitleFromInfobox},
		}},
		"the title with its own parenthetical": {"Ode (Poem)", "Ode (Poem)", nil},
	} {
		t.Run(name, func(t *testing.T) {
			text := pieceWikitext(tc.title, [][2]string{{titleParam, tc.value}})

			assert.Equal(t, tc.want, titlesOf(t, standardWiki(), tc.title, text).Alternates)
		})
	}
}

func TestPieceTitles_H1ParentheticalIsATitleNeverAnAlias(t *testing.T) {
	text := "= A Song, Again (Sung at Every Feast) =\n{{Infobox song\n| title = A Song Again\n}}\n"
	got := titlesOf(t, standardWiki(), "A Song Again", text)

	assert.Equal(t, []AltTitle{
		{Text: "Sung at Every Feast", Kind: domain.PieceAliasKindSubtitle, Source: TitleFromH1},
	}, got.Alternates)
}

func TestPieceTitles_OtherTitlesAndAlsoKnownAsLead(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{{otherTitlesParam, "The Placeholder Song; Song of Wren"}}) +
		"Also known as: \"Wren's Lament\", 'The Vale' & The Long Road or Ash Tree\nA note about the song.\n"
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	var texts []string
	for _, a := range p.Titles.Alternates {
		texts = append(texts, a.Text)
	}
	assert.Equal(t, []string{"The Placeholder Song", "Song of Wren", "Wren's Lament", "The Vale", "The Long Road",
		"Ash Tree"}, texts)
	assert.Equal(t, TitleFromAlsoKnownAs, p.Titles.Alternates[2].Source)
	assert.Equal(t, "A note about the song.", p.Content.NotesMD, "the also-known-as line is not a note")
}

func TestPieceTitles_RejectsOtherPageTitle(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: otherPageTitle, Categories: pieceCategories()})
	got := titlesOf(t, w, aSong, pieceWikitext(aSong, [][2]string{{titleParam, otherPageTitle}}))

	assert.Empty(t, got.Alternates)
	assert.Equal(t, []RejectedTitle{{Text: otherPageTitle, Source: TitleFromInfobox, Reason: TitleIsOtherPage}},
		got.Rejected)
}

func TestPieceTitles_RejectsCategoryWord(t *testing.T) {
	for _, word := range []string{"Miscellaneous", catWarlike, catTopics} {
		got := titlesOf(t, standardWiki(), aSong, pieceWikitext(aSong, [][2]string{{titleParam, word}}))

		assert.Empty(t, got.Alternates, word)
		assert.Equal(t, []RejectedTitle{{Text: word, Source: TitleFromInfobox, Reason: TitleIsCategoryWord}},
			got.Rejected, word)
	}
}

func TestPieceTitles_PunctuationOnlyVariantSkipped(t *testing.T) {
	text := "= What Price Honor? =\n{{Infobox song\n| title = (What) Price Honor\n}}\n"
	got := titlesOf(t, standardWiki(), "What Price Honor", text)

	assert.Empty(t, got.Alternates)
	assert.Empty(t, got.Rejected)
}

func TestPieceTitles_RedirectsAreAliases(t *testing.T) {
	w := standardWiki()
	w.redirect("A Song (Placeholder)", aSong)
	w.redirect("A song", aSong)
	text := "= A Song =\n{{Infobox song\n| title = A Song / A Song (Placeholder)\n}}\n"
	got := titlesOf(t, w, aSong, text)

	assert.Equal(t, []AltTitle{
		{Text: "A Song (Placeholder)", Kind: domain.PieceAliasKindRedirect, Source: TitleFromRedirect},
		{Text: "A song", Kind: domain.PieceAliasKindRedirect, Source: TitleFromRedirect},
	}, got.Alternates, "a redirect is kept even when it differs only by case; a title equal to one is not repeated")
	assert.Empty(t, got.Rejected)
}

func TestPieceTitles_UnparsedProseListed(t *testing.T) {
	text := pieceWikitext(aSong, nil) + "This song is also known as 'Wren Rising' and 'The Vale'.\n"
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	assert.Empty(t, p.Titles.Alternates)
	assert.Equal(t, []UnparsedTitle{{Section: SectionTitle,
		Text: "This song is also known as 'Wren Rising' and 'The Vale'."}}, p.Titles.Unparsed)
	assert.Equal(t, "This song is also known as 'Wren Rising' and 'The Vale'.", p.Content.NotesMD,
		"an unparsed line stays in the notes")
}
