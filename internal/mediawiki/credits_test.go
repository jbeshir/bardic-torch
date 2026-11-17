package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// creditShape is what a test checks of a credit.
type creditShape struct {
	role        domain.CreditRole
	attribution domain.Attribution
	certainty   domain.Certainty
	name        PersonName
}

func shapes(credits []CreditRecord) []creditShape {
	out := make([]creditShape, 0, len(credits))
	for _, c := range credits {
		out = append(out, creditShape{role: c.Role, attribution: c.Attribution, certainty: c.Certainty, name: c.Name})
	}

	return out
}

func lyricsCredits(raw string) []CreditRecord {
	return ParseCredits(domain.CreditRoleLyrics, ParseValue(raw), standardWiki().site())
}

func named(name string, form NameForm, certainty domain.Certainty) creditShape {
	return creditShape{role: domain.CreditRoleLyrics, attribution: domain.AttributionNamed, certainty: certainty,
		name: PersonName{Name: name, Form: form}}
}

func unnamed(a domain.Attribution) creditShape {
	return creditShape{role: domain.CreditRoleLyrics, attribution: a, certainty: domain.CertaintyCertain}
}

func TestParseCredits_AmpersandTwoNamed(t *testing.T) {
	assert.Equal(t, []creditShape{
		named(wren, NamePageLink, domain.CertaintyCertain), named(ash, NamePageLink, domain.CertaintyCertain),
	}, shapes(lyricsCredits("[["+wren+"]] & [["+ash+"]]")))
}

func TestParseCredits_NamedAndAnonymous(t *testing.T) {
	assert.Equal(t, []creditShape{
		named(wren, NamePageLink, domain.CertaintyCertain), unnamed(domain.AttributionAnonymous),
	}, shapes(lyricsCredits("[["+wren+"]] & anonymous")))
}

func TestParseCredits_SingleBracketCategoryIsTraditional(t *testing.T) {
	text := pieceWikitext(aSong, [][2]string{{tuneByParam, "[:Category:" + catTraditional + "|" + catTraditional + "]"}})
	p := parseFixturePiece(standardWiki(), Page{Title: aSong, Wikitext: text, Categories: pieceCategories()})

	require.Len(t, p.Credits, 1)
	assert.Equal(t, domain.CreditRoleTune, p.Credits[0].Role)
	assert.Equal(t, domain.AttributionTraditional, p.Credits[0].Attribution)
}

func TestParseCredits_TraditionalForms(t *testing.T) {
	for _, raw := range []string{
		categoryLink(catTraditional), "[[:Category:" + catTraditionalRedirect + "|Traditional]]",
		"[[" + catTraditional + "]]", "Folk", "traditional", "Traditional & Folk",
	} {
		assert.Equal(t, []creditShape{unnamed(domain.AttributionTraditional)}, shapes(lyricsCredits(raw)), raw)
	}
}

func TestParseCredits_FolkPossiblyNamed(t *testing.T) {
	assert.Equal(t, []creditShape{
		unnamed(domain.AttributionTraditional), named(rowan, NameText, domain.CertaintyPossible),
	}, shapes(lyricsCredits("Folk; possibly "+rowan)))
}

func TestParseCredits_ProbablySuffix(t *testing.T) {
	assert.Equal(t, []creditShape{named(wren, NamePageLink, domain.CertaintyProbable)},
		shapes(lyricsCredits("[["+wren+"]], probably")))
	assert.Equal(t, []creditShape{named(wren, NamePageLink, domain.CertaintyProbable)},
		shapes(lyricsCredits("probably, [["+wren+"]]")))
}

func TestParseCredits_DisputedExternal(t *testing.T) {
	credits := lyricsCredits("[https://en.wikipedia.org/wiki/Ash_Placeholder " + ash + "] (Disputed)")

	require.Len(t, credits, 1)
	assert.Equal(t, domain.CertaintyDisputed, credits[0].Certainty)
	assert.Equal(t, PersonName{Name: ash, Form: NameExternalLink, URL: "https://en.wikipedia.org/wiki/Ash_Placeholder"},
		credits[0].Name)
}

func TestParseCredits_Unknown(t *testing.T) {
	assert.Equal(t, []creditShape{unnamed(domain.AttributionUnknown)}, shapes(lyricsCredits("Unknown")))
	assert.Equal(t, []creditShape{unnamed(domain.AttributionUnknown)}, shapes(lyricsCredits("Unknown/Many")))
	assert.Equal(t, []creditShape{
		unnamed(domain.AttributionUnknown), named("W. Placeholder", NameText, domain.CertaintyPossible),
	}, shapes(lyricsCredits("Unknown; possibly W. Placeholder")))
}

func TestParseCredits_Various(t *testing.T) {
	assert.Equal(t, []creditShape{unnamed(domain.AttributionVarious)}, shapes(lyricsCredits("Various")))
	assert.Equal(t, []creditShape{unnamed(domain.AttributionVarious)}, shapes(lyricsCredits("Many")))
}

func TestParseCredits_NoSplitInsideLink(t *testing.T) {
	assert.Equal(t, []creditShape{named("Wren and Ash & Co", NamePageLink, domain.CertaintyCertain)},
		shapes(lyricsCredits("[[Wren and Ash & Co|Wren and Ash]]")))
	credits := lyricsCredits("[https://example.org/band Ash and Rowan & Co]")
	require.Len(t, credits, 1)
	assert.Equal(t, "Ash and Rowan & Co", credits[0].Name.Name)
}

func TestParseCredits_PageLinkReadThroughRedirects(t *testing.T) {
	w := standardWiki()
	w.redirect("Wren P.", wren)

	credits := ParseCredits(domain.CreditRoleTune, ParseValue("[[Wren P.]]"), w.site())

	require.Len(t, credits, 1)
	assert.Equal(t, PersonName{Name: wren, Form: NamePageLink}, credits[0].Name)
}

func TestParseCredits_OtherQualifiersDropped(t *testing.T) {
	assert.Equal(t, []creditShape{
		named(wren, NamePageLink, domain.CertaintyCertain), named(ash, NamePageLink, domain.CertaintyCertain),
		named(rowan, NamePageLink, domain.CertaintyCertain),
	}, shapes(lyricsCredits("[["+wren+"]] (verses) & [["+ash+"]] (chorus, arranged) & [["+rowan+"]] (Legal Name)")))
}
