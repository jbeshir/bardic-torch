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

func TestRevert_TakesTargetPreferences(t *testing.T) {
	t.Parallel()

	oldPreferences := []DocPreference{
		preference(PreferenceDimensionPerformancePermission, preferenceSource, PreferenceChoiceGiven),
	}
	tests := []struct {
		name            string
		target, current []DocPreference
	}{
		{name: "changed", target: oldPreferences, current: testPreferences()},
		{name: "added since", target: nil, current: testPreferences()},
		{name: "removed since", target: testPreferences(), current: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			person, currentPerson := fullPerson(), fullPerson()
			person.Preferences, currentPerson.Preferences = tt.target, tt.current
			assert.Equal(t, tt.target, RevertDocument(person, currentPerson).Preferences)
			piece, currentPiece := fullPiece(), fullPiece()
			piece.Preferences, currentPiece.Preferences = tt.target, tt.current
			assert.Equal(t, tt.target, RevertDocument(piece, currentPiece).Preferences)
		})
	}
}
