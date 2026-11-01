package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypePage(t *testing.T) {
	tests := map[string]struct {
		page Page
		want PageType
	}{
		"redirect":          {Page{Title: "Wren P", Redirect: true, Categories: []string{CategoryBards}}, PageTypeRedirect},
		"meta":              {Page{Title: "Main Page", Categories: []string{CategoryPieces}}, PageTypeMeta},
		"test":              {Page{Title: "Roses of the East"}, PageTypeTest},
		"bard":              {Page{Title: wren, Categories: []string{CategoryBards, CategoryPieces}}, PageTypeBard},
		"piece by category": {Page{Title: aSong, Categories: []string{CategoryPieces}}, PageTypePiece},
		"piece by infobox":  {Page{Title: aSong, Wikitext: "{{ Infobox poem\n}}"}, PageTypePiece},
		"venue":             {Page{Title: greenDragon, Categories: []string{"Venue"}}, PageTypeVenue},
		"other":             {Page{Title: "Something", Wikitext: "{{Other}}"}, PageTypeOther},
		"other namespace":   {Page{NS: NSCategory, Title: "Category:Bards", Categories: []string{CategoryBards}}, PageTypeOther},
		"infobox in a comment, as in the reference": {Page{Title: aSong, Wikitext: "<!-- {{Infobox song}} -->"}, PageTypePiece},
	}
	for name, tt := range tests {
		assert.Equal(t, tt.want, TypePage(tt.page), name)
	}
}

func TestSectionKey_Variants(t *testing.T) {
	tests := map[string]SectionKey{
		"Key Links":                        SectionKeyLinks,
		"key link":                         SectionKeyLinks,
		lyricsHeading:                      SectionLyrics,
		"Text":                             SectionText,
		"Licensing & Etiquette":            SectionLicensingEtiquette,
		"Licencing and Etiquette":          SectionLicensingEtiquette,
		"Performances & Recordings":        SectionPerformancesRecordings,
		"Performance and Recordings":       SectionPerformancesRecordings,
		"Sheet Music":                      SectionSheetMusic,
		"Dancing Instructions":             SectionDancingInstructions,
		"Timeline of Text Sources":         SectionTextSources,
		"Bard's Website":                   SectionWebsite,
		"Bard’s Website":                   SectionWebsite,
		"Bard’s Websites":                  SectionWebsite,
		"Affiliations":                     SectionAffiliations,
		"Songs":                            SectionWorks,
		"Songs & Works":                    SectionWorks,
		"Songs and Works":                  SectionWorks,
		"Music":                            SectionWorksMusic,
		"Other Pages & External Resources": SectionOtherResources,
		"Notes":                            SectionNotes,
		"Other Categories":                 SectionOtherCategories,
		"Known Performances":               SectionKnownPerformances,
		"Translations":                     SectionTranslations,
		"'''Lyrics'''":                     SectionLyrics,
		"[[Wren Placeholder|Wren]]":        "other:Wren",
		"Legal Name":                "other:Legal Name",
	}
	for heading, want := range tests {
		assert.Equal(t, want, KeyOf(Section{Level: 2, Heading: heading}), heading)
	}
	assert.Equal(t, SectionLead, KeyOf(Section{}))
	assert.Equal(t, SectionTitle, KeyOf(Section{Level: 1, Heading: lyricsHeading}))
	assert.True(t, SectionKey("other:x").IsOther())
	assert.False(t, SectionNotes.IsOther())
	assert.True(t, SectionText.IsLyrics())
	assert.False(t, SectionNotes.IsLyrics())
}

func TestIsHint(t *testing.T) {
	for line, want := range map[string]bool{
		"":                                 true,
		"*":                                true,
		"  *  ":                            true,
		"* Link":                           true,
		"*link ":                           true,
		"* (Add sourced information here)": true,
		"Information about licensing, permissions, and performance etiquette should always be sourced from the " +
			"author.": true,
		"'''Note:''' Please ensure lyrics are only included here when allowed":       true,
		"'''Note:''' please ensure poems' texts are only included here when allowed": true,
		"----":           true,
		"------  ":       true,
		"* Link to more": false,
		"---":            false,
		"Some prose.":    false,
	} {
		assert.Equal(t, want, IsHint(line), line)
	}
}

func TestIsLicensingUnknown(t *testing.T) {
	assert.True(t, IsLicensingUnknown("We cannot find a central page with this bard's preferences or licensing."))
	assert.False(t, IsLicensingUnknown("Free to perform with credit."))
}

func TestAnalyzeSection_Counts(t *testing.T) {
	content := "\n* [https://example.org/a A site] by [[Wren Placeholder]]\n** nested [[Category:Songs]]\n" +
		"#: numbered\n* Link\n*\n[[Category:Songs]]\nProse with https://example.org/b.\n<!-- hidden [[Hidden]] -->\n" +
		"<nowiki>[[Not a link]]</nowiki>\n----\n"
	got := AnalyzeSection(content)

	assert.Equal(t, 4, got.MeaningfulLines)
	assert.False(t, got.Empty())
	assert.Equal(t, []ListItem{
		{Depth: 1, Raw: "[https://example.org/a A site] by [[Wren Placeholder]]", Text: "A site by Wren Placeholder"},
		{Depth: 2, Raw: "nested [[Category:Songs]]", Text: "nested Category:Songs"},
		{Depth: 2, Raw: "numbered", Text: "numbered"},
	}, got.Items)
	require.Len(t, got.ExternalLinks, 2)
	assert.Equal(t, "https://example.org/a", got.ExternalLinks[0].URL)
	assert.Equal(t, "A site", got.ExternalLinks[0].Text)
	assert.Equal(t, "* [https://example.org/a A site] by [[Wren Placeholder]]", got.ExternalLinks[0].Line)
	assert.Equal(t, "https://example.org/b", got.ExternalLinks[1].URL)
	assert.Equal(t, "Prose with https://example.org/b.", got.ExternalLinks[1].Line)
	require.Len(t, got.Wikilinks, 1, "category tags, comments and nowiki content are not links")
	assert.Equal(t, wren, got.Wikilinks[0].Target)
	assert.Contains(t, got.PlainText, "A site by Wren Placeholder")
	assert.NotContains(t, got.PlainText, "Hidden")

	assert.True(t, AnalyzeSection("\n* Link\n----\n[[Category:Songs]]\n").Empty())
}

func TestParsePage_InfoboxesAndSections(t *testing.T) {
	wikitext := "= A Song (Placeholder) =\n{{Infobox song\n| title = A Song <!-- c -->\n| lyrics_by = " +
		"[[Wren Placeholder]]\n| title = Second\n}}\nLead text.\n== Lyrics ==\n* [https://example.org/ Lyrics]\n" +
		"== Notes ==\nA note."
	got := ParsePage(wikitext)

	assert.True(t, got.HasH1)
	assert.Equal(t, "A Song (Placeholder)", got.H1)
	require.Len(t, got.Infoboxes, 1)
	box := got.Infoboxes[0]
	assert.Equal(t, "Infobox song", box.Template)
	assert.Equal(t, []string{titleParam, lyricsByParam, titleParam}, []string{box.Params[0].Name, box.Params[1].Name,
		box.Params[2].Name})
	assert.Equal(t, aSong, box.Params[0].Raw)
	title, ok := box.Param(titleParam)
	require.True(t, ok)
	assert.Equal(t, "Second", title.Raw, "a repeated parameter's last value wins")
	_, ok = box.Param(tuneByParam)
	assert.False(t, ok)
	assert.Equal(t, wren, box.Params[1].Value.Items[0].Target)

	require.Len(t, got.Sections, 4)
	keys := []SectionKey{got.Sections[0].Key, got.Sections[1].Key, got.Sections[2].Key, got.Sections[3].Key}
	assert.Equal(t, []SectionKey{SectionLead, SectionTitle, SectionLyrics, SectionNotes}, keys)
	assert.Equal(t, "Lead text.", got.Sections[1].Content.PlainText, "the infobox is not section text")
	assert.Equal(t, "A note.", got.Sections[3].Content.PlainText)
	assert.Equal(t, "== Lyrics ==", wikitext[got.Sections[2].Start:got.Sections[2].Start+12])

	assert.False(t, ParsePage("no heading").HasH1)
}
