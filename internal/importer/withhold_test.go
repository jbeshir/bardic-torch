package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

func TestWithholder_Text(t *testing.T) {
	w := withholder{matcher: mediawiki.NewPrivateNameMatcher([]mediawiki.PrivateName{
		{Text: hiddenAlias, Legal: true}, {Text: "Sample Uploader"},
	})}

	out, found := w.text("Mail ash.hiddenname@example.org; " + hiddenAlias + " and Ms. Otherperson, by Sample Uploader.")

	assert.Equal(t, "Mail "+EmailWithheld+"; "+NameWithheld+" and Ms. "+NameWithheld+", by "+NameWithheld+".", out)
	assert.Len(t, found, 4)
	for _, f := range found {
		assert.NotContains(t, f.Context, "hiddenname", "contexts are withheld too")
		assert.NotContains(t, f.Context, "Hiddenname")
	}
	assert.True(t, found[0].Email)
}

func TestWithholder_URLs(t *testing.T) {
	w := withholder{matcher: mediawiki.NewPrivateNameMatcher([]mediawiki.PrivateName{{Text: hiddenAlias, Legal: true}})}

	for _, u := range []string{"https://example.org/wiki/Ash_Hiddenname", "https://ashhiddenname.example.org/",
		"https://example.org/2013/11/song-ash-hiddenname.html"} {
		assert.True(t, w.hidesURL(u), u)
	}
	assert.False(t, w.hidesURL("https://example.org/ash/songs"))
}

func TestAround_KeepsRuneBoundaries(t *testing.T) {
	s := "ééééé" + NameWithheld + "ééééé"

	assert.Equal(t, s, around(s, len("ééééé"), len("ééééé")+len(NameWithheld)))
}
