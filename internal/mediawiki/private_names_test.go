package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateNameMatcher_LegalNameForms(t *testing.T) {
	m := NewPrivateNameMatcher([]PrivateName{{Text: "Ash Hidden-Name", Legal: true}, {Text: "Sample Words"}})

	for _, text := range []string{"Ash Hidden-Name", "Hidden-Name Ash", "Mr. Hidden-Name", "Dr Name", "ash_hidden_name"} {
		found := m.Find(text)
		require.Len(t, found, 1, text)
		assert.Equal(t, 0, found[0].Name, text)
	}
	assert.True(t, m.Find("Mrs. Hidden-Name")[0].Honorific)
	assert.Len(t, m.Find("Sample Words"), 1)
	assert.Empty(t, m.Find("Words Sample"), "a phrase that is not legal is found only as its words in order")
	assert.Empty(t, m.Find("Mr. Words"))
}

func TestPrivateNames_HiddenAliasesAndUnsureCandidates(t *testing.T) {
	pages := []Page{
		{NS: NSMain, Title: "Wren Placeholder", PageID: 1, Categories: []string{CategoryBards},
			Wikitext: "= Wren Placeholder (Ash Hiddenname, Wren of the Glen) =\n"},
		{NS: NSMain, Title: "A Song", PageID: 2, Wikitext: "{{Infobox Song}}\nRecorded by Sample Uploader on YouTube."},
	}

	names, decisions := PrivateNames(pages, nil)

	assert.Equal(t, []PrivateName{{Text: "Ash Hiddenname", Legal: true}, {Text: "Sample Uploader"}}, names)
	reasons := map[string]PrivacyReason{}
	for _, d := range decisions {
		if _, ok := reasons[d.Text]; !ok {
			reasons[d.Text] = d.Reason
		}
		assert.Equal(t, d.Reason.Private(), d.Private, d.Text)
	}
	assert.Equal(t, PrivacyPublicAlias, reasons["Wren of the Glen"])
	assert.Equal(t, PrivacyUnsure, reasons["Sample Uploader"])
}

func TestIsPlaceholderName(t *testing.T) {
	for _, s := range []string{"Legal Name 001", "legal-name-002", "LegalName003", "Wiki User 01"} {
		assert.True(t, IsPlaceholderName(s), s)
	}
	assert.False(t, IsPlaceholderName("Legally Named"))
}
