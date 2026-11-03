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
		{wrenOfTheVale, AliasFromLegalNameMarker, AliasClassification{legal, true, AliasRuleLegalMarker}},
		{ash, AliasFromMundaneNameMarker, AliasClassification{legal, true, AliasRuleLegalMarker}},
		{wrenOfTheVale, AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRuleByname}},
		{"Wren of Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRuleByname}},
		{"Ash the Bold", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRuleByname}},
		{"Rowan ap Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRulePatronymic}},
		{"Rowan inghen Placeholder", AliasFromAKAMarker, AliasClassification{sca, true, AliasRulePatronymic}},
		{"Ingrid Haraldsdottir", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRulePatronymicSuffix}},
		{"Ingrid Haraldsdóttir", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRulePatronymicSuffix}},
		{"Kari Placeholderdatter", AliasFromH1Parenthetical, AliasClassification{sca, true, AliasRulePatronymicSuffix}},
		{"Maria de Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false, AliasRuleParticle}},
		{"Hans von Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false, AliasRuleParticle}},
		{"Sean Mac Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false, AliasRuleParticle}},
		{"Aoife Ó Placeholder", AliasFromH1Parenthetical, AliasClassification{sca, false, AliasRuleParticle}},
		{wren, AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{"J. Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{wrenFirst, AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{"Wren of", AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{"Wren Of Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{"Nic Placeholder", AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
		{"Dottir", AliasFromH1Parenthetical, AliasClassification{legal, false, AliasRuleDefault}},
	}
	for _, tt := range tests {
		got := ClassifyPersonAlias(AliasCandidate{Name: tt.name, Source: tt.source})
		assert.Equal(t, tt.want, got, tt.name)
		assert.Equal(t, tt.want.Kind == sca && tt.want.Confident, got.Public(), tt.name)
	}
}
