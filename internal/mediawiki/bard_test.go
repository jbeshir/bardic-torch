package mediawiki

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	cannotFind   = "We cannot find a central page with this bard's preferences or licensing. Please update the wiki if you find one!"
	ashOfTheVale = "Ash of the Vale"
)

// bardWikitext is a bard page: its H1 and the sections given as heading and body pairs.
func bardWikitext(h1 string, sections ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "= %s =\n\n", h1)
	for k := 0; k+1 < len(sections); k += 2 {
		fmt.Fprintf(&b, "== %s ==\n%s\n\n----\n\n", sections[k], sections[k+1])
	}

	return b.String()
}

// parseFixtureBard adds a bard page to w and parses it.
func parseFixtureBard(w *wikiFixture, title, wikitext string, categories ...string) BardRecord {
	p := w.page(Page{Title: title, Wikitext: wikitext, Categories: append([]string{CategoryBards}, categories...)})

	return ParseBard(p, w.site())
}

func TestParseBard_Aliases(t *testing.T) {
	b := parseFixtureBard(standardWiki(), rowan, bardWikitext(rowan+" ("+ashOfTheVale+" / "+ash+")"))

	type alias struct {
		name   string
		source AliasSource
		kind   domain.PersonAliasKind
	}
	var got []alias
	for _, a := range b.Aliases {
		got = append(got, alias{a.Name, a.Source, a.Kind})
	}
	assert.Equal(t, []alias{
		{ashOfTheVale, AliasFromH1Parenthetical, domain.PersonAliasKindSCA},
		{ash, AliasFromH1Parenthetical, domain.PersonAliasKindLegal},
	}, got)
}

func TestParseBard_H1LeadIsAnAlias(t *testing.T) {
	b := parseFixtureBard(standardWiki(), wren, bardWikitext(ash+" ("+wren+")"))

	k := slices.IndexFunc(b.Aliases, func(a PersonAlias) bool { return a.Source == AliasFromH1Lead })
	require.GreaterOrEqual(t, k, 0)
	assert.Equal(t, ash, b.Aliases[k].Name)
	assert.Equal(t, wren, b.Title, "the page title stays the name")
}

func TestParseBard_RolesAndAffiliations(t *testing.T) {
	b := parseFixtureBard(standardWiki(), wren, bardWikitext(wren), "Singers", "Songwriters", "Poets",
		CategorySCABards, kingdom)

	assert.Equal(t, []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRoleSongwriter, domain.PersonRolePoet},
		b.Roles)
	assert.Equal(t, []string{CategorySCABards, kingdom}, b.Affiliations)
	assert.Equal(t, domain.PersonStatusActive, b.Status)
}

func TestParseBard_KingdomNamePlaceholderDropped(t *testing.T) {
	w := standardWiki()
	w.category(catKingdomName)
	b := parseFixtureBard(w, wren, bardWikitext(wren), catKingdomName, kingdom)

	assert.Equal(t, []string{kingdom}, b.Affiliations)
}

func TestParseBard_Deceased(t *testing.T) {
	b := parseFixtureBard(standardWiki(), wren, bardWikitext(wren,
		"Notes", "* [https://example.org/in-memoriam Deceased October 31, 2020]"), CategoryInMemoriam)

	assert.Equal(t, domain.PersonStatusDeceased, b.Status)
	assert.Equal(t, "- [Deceased October 31, 2020](https://example.org/in-memoriam)", b.NotesMD,
		"the date and its source stay in the notes")
}

func TestParseBard_LicensingUnknownSentence(t *testing.T) {
	unknown := parseFixtureBard(standardWiki(), wren, bardWikitext(wren, "Licensing & Etiquette", cannotFind))
	assert.Equal(t, domain.LicensingStatusUnknown, unknown.LicensingStatus)
	assert.Empty(t, unknown.LicensingNotesMD)

	known := parseFixtureBard(standardWiki(), ash, bardWikitext(ash, "Licensing & Etiquette",
		"Information about licensing, permissions, and performance etiquette should always be sourced from the author.\n"+
			"Sing it at events, with credit ([https://example.org/faq FAQ])."))
	assert.Equal(t, domain.LicensingStatusKnown, known.LicensingStatus)
	assert.Equal(t, "Sing it at events, with credit ([FAQ](https://example.org/faq)).", known.LicensingNotesMD)

	unsourced := parseFixtureBard(standardWiki(), rowan, bardWikitext(rowan, "Licensing & Etiquette",
		"Sing it at events."))
	assert.Equal(t, domain.LicensingStatusUnknown, unsourced.LicensingStatus)
	assert.Equal(t, "Sing it at events.", unsourced.LicensingNotesMD)
}

func TestParseBard_HeirOf(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: ash, Wikitext: bardWikitext(ash), Categories: []string{CategoryBards}})
	b := parseFixtureBard(w, rowan, bardWikitext(rowan, "Affiliations",
		"* The chosen heir of [["+ash+"]], and owner of all her music.\n* "+categoryLink(CategorySCABards)))

	assert.Equal(t, []string{ash}, b.HeirOf)
	assert.Equal(t, "### Affiliations\n\n- The chosen heir of "+ash+", and owner of all her music.", b.NotesMD)
}

func TestParseBard_HeadingNamedAfterTheBardIsNotes(t *testing.T) {
	b := parseFixtureBard(standardWiki(), wren, bardWikitext(wren,
		wren, "* [https://example.org/band Placeholder Band]", "Notes", "Sings at every revel."))

	assert.Equal(t, "### "+wren+"\n\n- [Placeholder Band](https://example.org/band)\n\nSings at every revel.", b.NotesMD)
}

func TestParseBard_Works(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, nil), Categories: pieceCategories()})
	w.redirect("A Song Again", aSong)
	b := parseFixtureBard(w, wren, bardWikitext(wren, "Songs & Works", "* [[A Song Again]]\n* [[An Unwritten Song]]"))

	assert.Equal(t, []WorkEntry{
		{Title: "A Song Again", Target: aSong, Exists: true},
		{Title: "An Unwritten Song", Target: "An Unwritten Song"},
	}, b.Works)
}

func TestParseBard_InlineLyricsDropped(t *testing.T) {
	b := parseFixtureBard(standardWiki(), wren, bardWikitext(wren, "Notes",
		"<poem>\nmarkerlyric one\nmarkerlyric two\nmarkerlyric three\nmarkerlyric four\n</poem>"))

	assert.NotContains(t, b.Wikitext, "markerlyric")
	assert.NotContains(t, b.NotesMD, "markerlyric")
}
