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
