package importer

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// rowanUnsure is an unsure private name.
const rowanUnsure = "Rowan Unsure"

// testWithholder withholds the legal names legal and the unsure names unsure.
func testWithholder(legal, unsure []string) withholder {
	var names []mediawiki.PrivateName
	for _, n := range legal {
		names = append(names, mediawiki.PrivateName{Text: n, Legal: true})
	}
	for _, n := range unsure {
		names = append(names, mediawiki.PrivateName{Text: n})
	}

	return newWithholder(mediawiki.Corpus{PrivateNames: names}, nil)
}

func TestWithholder_Text(t *testing.T) {
	w := testWithholder([]string{hiddenAlias}, []string{"Sample Uploader"})

	out, found := w.text("Mail ash.hiddenname@example.org; " + hiddenAlias + " and Ms. Otherperson, by Sample Uploader.")

	assert.Equal(t, "Mail "+domain.WithheldEmail+"; "+domain.WithheldName+" and Ms. "+domain.WithheldName+", by "+
		domain.WithheldName+".", out)
	assert.Len(t, found, 4)
	for _, f := range found {
		assert.NotContains(t, f.Context, "hiddenname", "contexts are withheld too")
		assert.NotContains(t, f.Context, "Hiddenname")
	}
	assert.True(t, found[0].Email)
}

func TestWithholder_NameWithholdsOnlyLegalNames(t *testing.T) {
	w := testWithholder([]string{hiddenAlias}, []string{"Sample Uploader"})

	out, found := w.name("Sample Uploader")
	assert.Equal(t, "Sample Uploader", out, "an unsure name the wiki gives an entity is imported as written")
	assert.Empty(t, found)

	out, found = w.name(hiddenAlias + "'s Song")
	assert.Equal(t, domain.WithheldName+"'s Song", out)
	assert.Len(t, found, 1)

	out, found = w.name("Song for Mr. Otherperson")
	assert.Equal(t, "Song for Mr. "+domain.WithheldName, out, "an honorific name is withheld from names too")
	assert.Len(t, found, 1)
}

func TestWithholder_OverlappingFormsWithheldOnce(t *testing.T) {
	// "Mr. Hiddenname" is both an honorific form of the legal name and an honorific name: one span, one finding,
	// and a context that reads cleanly.
	w := testWithholder([]string{hiddenAlias}, nil)

	out, found := w.text("Sung with Mr. " + hiddenAlias + ", please, and Mr. Hiddenname alone.")

	assert.Equal(t, "Sung with Mr. "+domain.WithheldName+", please, and "+domain.WithheldName+" alone.", out)
	assert.Len(t, found, 2, "each occurrence is listed once")
	for _, f := range found {
		assert.NotContains(t, f.Context, strings.Repeat("withheld] ", 2))
	}
}

func TestWithholder_WikilinkWithheldWhole(t *testing.T) {
	w := testWithholder([]string{hiddenAlias}, nil)

	out, found := w.text("Sung by [[" + hiddenAlias + "]] and [[Wren Placeholder|" + hiddenAlias + "]].")

	assert.Equal(t, "Sung by "+domain.WithheldName+" and "+domain.WithheldName+".", out,
		"no [[[name withheld]]] is left")
	assert.Len(t, found, 2)
}

func TestWithholder_PlaceholderNumberWithheld(t *testing.T) {
	// A candidate rule of the scrubbed copy captured only the placeholder's words; its number goes with them.
	w := testWithholder([]string{"Legal Name"}, nil)

	out, found := w.text("compiled by Legal Name 030, in 2003; Legal Name 2004")

	assert.Equal(t, "compiled by "+domain.WithheldName+", in 2003; "+domain.WithheldName+" 2004", out)
	assert.Len(t, found, 2)
}

func TestWithholder_URLs(t *testing.T) {
	w := testWithholder([]string{hiddenAlias}, []string{rowanUnsure})

	for _, u := range []string{"https://example.org/wiki/Ash_Hiddenname", "https://ashhiddenname.example.org/",
		"https://example.org/2013/11/song-ash-hiddenname.html"} {
		assert.True(t, w.hidesURL(u), u)
	}
	assert.False(t, w.hidesURL("https://example.org/ash/songs"))
	assert.False(t, w.hidesURL("https://rowanunsure.example.org/"), "only a legal name hides a URL")
	assert.NotEmpty(t, w.urlNote("https://rowanunsure.example.org/"), "an unsure name in a URL is listed")
	assert.NotEmpty(t, w.urlNote("https://www.example.edu/~rplaceholder/songs/"), "a home directory is listed")
	assert.Empty(t, w.urlNote("https://example.org/ash/songs"))
}

func TestNewWithholder_PlaceholdersAndHiddenAliasesAreLegal(t *testing.T) {
	c := mediawiki.Corpus{
		PrivateNames: []mediawiki.PrivateName{{Text: rowanUnsure}},
		Privacy:      []mediawiki.PrivacyDecision{{Text: "Legal Name 007", Reason: mediawiki.PrivacyPlaceholder}},
	}

	w := newWithholder(c, []string{hiddenAlias})

	var legal []string
	for _, n := range w.legal {
		legal = append(legal, n.Text)
	}
	assert.ElementsMatch(t, []string{hiddenAlias, "Legal Name 007"}, legal)
	assert.False(t, slices.ContainsFunc(w.legal, func(n mediawiki.PrivateName) bool { return n.Text == rowanUnsure }))
}

func TestAround_KeepsRuneBoundaries(t *testing.T) {
	s := "ééééé" + domain.WithheldName + "ééééé"

	assert.Equal(t, s, around(s, len("ééééé"), len("ééééé")+len(domain.WithheldName)))
}
