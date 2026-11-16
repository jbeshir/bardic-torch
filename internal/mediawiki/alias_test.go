package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

func bardPage(title, wikitext string) Page {
	return Page{Title: title, Categories: []string{CategoryBards}, Wikitext: wikitext}
}

func TestBardAliases_H1Groups(t *testing.T) {
	p := bardPage(wren, "= Wren Placeholder (Ash Placeholder / Wren of the Vale) (Rowan Placeholder, "+
		"R. Placeholder; Wren aka Little Wren) =\nText.")

	assert.Equal(t, []AliasCandidate{
		{Name: ash, Source: AliasFromH1Parenthetical},
		{Name: wrenOfTheVale, Source: AliasFromH1Parenthetical},
		{Name: rowan, Source: AliasFromH1Parenthetical},
		{Name: "R. Placeholder", Source: AliasFromH1Parenthetical},
		{Name: wrenFirst, Source: AliasFromH1Parenthetical},
		{Name: "Little Wren", Source: AliasFromH1Parenthetical},
	}, BardAliases(p))
}

func TestBardAliases_TitleEqualH1ParentheticalUsesLead(t *testing.T) {
	p := bardPage(rowan, "= Wren of the Vale (Rowan Placeholder) =\n")

	assert.Equal(t, []AliasCandidate{{Name: wrenOfTheVale, Source: AliasFromH1Lead}}, BardAliases(p))
}

func TestBardAliases_TitleAndProseMarkers(t *testing.T) {
	p := bardPage("Wren (Singer)", "= Wren =\nHer legal name is Rowan Placeholder. Mundane name: "+
		"Ash Q. Placeholder. Also a.k.a. Little Wren, aka '''Wren the Bold'''. A credit naming the full legal name "+
		"(specify) is fine. Ask for aka filk. <!-- legal name Hidden Placeholder -->")

	assert.Equal(t, []AliasCandidate{
		{Name: "Singer", Source: AliasFromTitleParenthetical},
		{Name: rowan, Source: AliasFromLegalNameMarker},
		{Name: "Ash Q. Placeholder", Source: AliasFromMundaneNameMarker},
		{Name: "Little Wren", Source: AliasFromAKAMarker},
		{Name: "Wren the Bold", Source: AliasFromAKAMarker},
	}, BardAliases(p))
}

func TestBardAliases_NoneEqualToTheTitle(t *testing.T) {
	p := bardPage("Wren O’Placeholder", "= Wren O'Placeholder (wren o’placeholder) =\naka Wren O'Placeholder")

	assert.Empty(t, BardAliases(p))
	assert.Empty(t, BardAliases(bardPage(wren, "no heading")))
}

func TestClassifyPersonAlias(t *testing.T) {
	legal, sca := domain.PersonAliasKindLegal, domain.PersonAliasKindSCA
	tests := []struct {
		name   string
		source AliasSource
		want   AliasClassification
	}{
		{wrenOfTheVale, AliasFromLegalNameMarker, AliasClassification{legal, true}},
		{ash, AliasFromMundaneNameMarker, AliasClassification{legal, true}},
		{wrenOfTheVale, AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Wren of Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Ash the Bold", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Rowan ap Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Rowan inghen Placeholder", AliasFromAKAMarker, AliasClassification{sca, true}},
		{"Ingrid Haraldsdottir", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Ingrid Haraldsdóttir", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Kari Placeholderdatter", AliasFromH1Parenthetical, AliasClassification{sca, true}},
		{"Maria de Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false}},
		{"Hans von Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false}},
		{"Sean Mac Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false}},
		{"Aoife Ó Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false}},
		{wren, AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{"J. Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{wrenFirst, AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{"Wren of", AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{"Wren Of Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{"Nic Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false}},
		{"Dottir", AliasFromH1Parenthetical, AliasClassification{legal, false}},
	}
	for _, tt := range tests {
		got := ClassifyPersonAlias(AliasCandidate{Name: tt.name, Source: tt.source})
		assert.Equal(t, tt.want, got, tt.name)
	}
}

func TestFoldName_ApostrophesAndAccents(t *testing.T) {
	for in, want := range map[string]string{
		"Lasair inghen ui h’Airt": "lasair inghen ui hairt",
		"Lasair inghen ui h'Airt": "lasair inghen ui hairt",
		"  Thórvé   Skáld  ":      "thorve skald",
		"Ælfric ʼOʼ `Brien‘":      "aelfric o brien",
		"Astrið Æsa":              "astrid aesa",
		"WREN\tPLACEHOLDER":       "wren placeholder",
	} {
		assert.Equal(t, want, FoldName(in), in)
	}
}
