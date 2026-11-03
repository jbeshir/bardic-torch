package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPersonNameCandidates(t *testing.T) {
	text := "= Wren of the Vale (Wren Placeholder, W. Placeholder) =\n" +
		"Lead by Ash Placeholder and from Rowan de Placeholder.\n" +
		"== Notes ==\nSung with Rowan of Placeholder (Rowan Q. Placeholder) and by [[Ash Placeholder]].\n" +
		"Her mundane name is Hazel Placeholder.\n" +
		"* [https://example.org/by/Not_A_Name Recording by Oak Placeholder on YouTube]\n" +
		"<!-- by Hidden Placeholder -->"
	got := PersonNameCandidates(Page{PageID: 42, Wikitext: text}, testMaxYear)

	type found struct {
		Text    string
		Rule    CandidateRule
		Section int
	}
	var founds []found
	for _, c := range got {
		assert.Equal(t, int64(42), c.PageID)
		assert.Equal(t, c.Text, text[c.Offset:c.Offset+len(c.Text)], "offsets point into the wikitext")
		founds = append(founds, found{c.Text, c.Rule, c.Section})
	}
	assert.Equal(t, []found{
		{wren, CandidateH1Parenthetical, 1},
		{"W. Placeholder", CandidateH1Parenthetical, 1},
		{ash, CandidateByFrom, 1},
		{"Rowan de Placeholder", CandidateByFrom, 1},
		{"Rowan of Placeholder", CandidateParenPair, 2},
		{"Rowan Q. Placeholder", CandidateParenPair, 2},
		{"Hazel Placeholder", CandidateMarker, 2},
		{"Oak Placeholder", CandidateByFrom, 2},
		{"Oak Placeholder", CandidateUploader, 2},
		{"Hidden Placeholder", CandidateByFrom, 2},
	}, founds)
}

func TestPersonNameCandidates_None(t *testing.T) {
	text := "= A Song =\nWritten by the author, from a tune by [[Wren Placeholder]] (verses) in England.\n" +
		"Sung from memory (Twice)."
	assert.Empty(t, PersonNameCandidates(Page{Wikitext: text}, testMaxYear))
}

func TestPersonNameCandidates_ProseNameForms(t *testing.T) {
	text := "= A Song =\n== Other Pages & External Resources ==\n" +
		"* [https://example.org/a.pdf A Song], credited to Wren of the Vale / Ash Placeholder 1997\n" +
		"* attributed to \"2008 Rowan Placeholder\"; copyright to Hazel Q. Placeholder, 1992\n" +
		"* [https://example.org/b A Song on Larch Placeholder's YouTube]\n" +
		"* [https://example.org/c Elm Placeholder singing, recorded on YouTube]\n" +
		"Based on a song by Birch Placeholder and Alder Placeholder; also known as: Wren Q. Vale, Wren Other.\n" +
		"* [https://www.pine-placeholder.example/ Pine Placeholder]\n" +
		"* The Long Song / The Short Song, from The Fyne and Nice Book.\n"
	got := PersonNameCandidates(Page{PageID: 7, Wikitext: text}, testMaxYear)

	byRule := map[CandidateRule][]string{}
	for _, c := range got {
		assert.Equal(t, c.Text, text[c.Offset:c.Offset+len(c.Text)], "offsets point into the wikitext")
		byRule[c.Rule] = append(byRule[c.Rule], c.Text)
		if c.Rule == CandidateSlashPair {
			assert.NotEmpty(t, c.Partner, c.Text)
		}
	}
	assert.Equal(t, []string{"Wren of the Vale", "Ash Placeholder"}, byRule[CandidateSlashPair],
		"a pair led by an article is a pair of titles")
	assert.Equal(t, []string{"Wren of the Vale", "Rowan Placeholder", "Hazel Q. Placeholder"},
		byRule[CandidateCredited])
	assert.Equal(t, []string{"Larch Placeholder"}, byRule[CandidatePossessive])
	assert.Equal(t, []string{"Elm Placeholder"}, byRule[CandidatePerformer])
	assert.Equal(t, []string{"Birch Placeholder", "Alder Placeholder"}, byRule[CandidateConjunction],
		"a heading is not prose, and a side led by an article is a title")
	assert.Equal(t, []string{"Wren Q. Vale", "Wren Other"}, byRule[CandidateAlsoKnownAs])
	assert.Equal(t, []string{"Pine Placeholder"}, byRule[CandidateSiteName])
}
