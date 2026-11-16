package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairMarkup_DoubleBracketURL(t *testing.T) {
	text := "* [[https://example.org/v?si=x Recording by Wren Placeholder on YouTube]] and [[//example.org/w]]"
	got := RepairMarkup(text)

	assert.Equal(t, "* [https://example.org/v?si=x Recording by Wren Placeholder on YouTube] and [//example.org/w]", got)
	links := FindExternalLinks(got)
	require.Len(t, links, 2)
	assert.Equal(t, "Recording by Wren Placeholder on YouTube", links[0].Text)
	assert.Empty(t, FindWikilinks(got))
}

func TestRepairMarkup_SingleBracketCategory(t *testing.T) {
	text := "{{Infobox song\n| tune_by = [:Category:Traditional & Folk|Traditional & Folk]\n| mood = x\n}} " +
		"[[:Category:Fine|Fine]] [:Category:Also]]"
	broken := FindTemplates(text)[0].Params
	require.Len(t, broken, 3, "the single bracket's pipe splits the parameter")
	assert.Equal(t, "1", broken[1].Name)

	got := RepairMarkup(text)
	assert.Equal(t, "{{Infobox song\n| tune_by = [[:Category:Traditional & Folk|Traditional & Folk]]\n| mood = x\n}} "+
		"[[:Category:Fine|Fine]] [[:Category:Also]]", got)
	params := FindTemplates(got)[0].Params
	require.Len(t, params, 2)
	assert.Equal(t, tuneByParam, params[0].Name)
	assert.Equal(t, ValueCategoryLink, ParseValue(params[0].Value).Items[0].Type)

	unchanged := RepairMarkup("[[:Category:Fine]] [https://example.org/ x]")
	assert.Equal(t, "[[:Category:Fine]] [https://example.org/ x]", unchanged)
}
