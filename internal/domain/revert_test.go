package domain

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRevertDocument(t *testing.T) {
	t.Parallel()

	target := fullPerson()
	target.Aliases = append(slices.Clone(target.Aliases),
		DocPersonAlias{Name: otherLegalName, Kind: PersonAliasKindStage})
	target.Preferences = nil
	current := fullPerson()
	current.Aliases = []DocPersonAlias{
		{Name: secondAliasName, Kind: PersonAliasKindSCA}, hiddenLegalName,
	}
	current.NotesMD = ptr("Current notes.")

	t.Run("AliasHiddenSinceStaysHidden", func(t *testing.T) {
		t.Parallel()
		got := RevertDocument(target, current)
		assert.Equal(t, []DocPersonAlias{
			{Name: secondAliasName, Kind: PersonAliasKindSCA},
			hiddenLegalName,
			{Name: morwenTheBard, Kind: PersonAliasKindNickname, IsPublic: true},
			{Name: otherLegalName, Kind: PersonAliasKindStage},
		}, got.Aliases, "hidden in current, or absent from it and hidden in the target")
		assert.Equal(t, fullPerson().Aliases, target.Aliases[:3], "the target is not changed")
	})
	t.Run("CurrentPreferencesKept", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, testPreferences(), RevertDocument(target, current).Preferences)
		piece := fullPiece()
		piece.Preferences = nil
		assert.Equal(t, testPreferences(), RevertDocument(piece, fullPiece()).Preferences)
	})
	t.Run("HiddenLinksKept", func(t *testing.T) {
		t.Parallel()
		piece := fullPiece()
		currentPiece := removeLink(piece, hiddenLinkURL)
		assert.Contains(t, linkURLs(RevertDocument(piece, currentPiece)), hiddenLinkURL,
			"links hidden since stay in the document, hidden by their state")
	})
	t.Run("DiffShowsRevertedFields", func(t *testing.T) {
		t.Parallel()
		got := RevertDocument(target, current)
		assert.Equal(t, []FieldPath{"notes_md", "aliases[morwen the bard]", "aliases[wren r. placeholder]"},
			DiffDocuments(current, got))
	})
	t.Run("OtherTypes", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, validTerm(), RevertDocument(validTerm(), termReferencing(0, 0)))
		assert.Equal(t, validEvent(), RevertDocument(validEvent(), EventDocument{}))
	})
}
