package domain

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The author's view is never an oracle for hidden links through the sources the author typed (docs/PLAN.md §4.1,
// "Hidden links", and §5.1, "Author's view"): a preference, status source or conservation source the author typed,
// or a received one they edited to another URL, is shown the same whether its URL is a hidden link's or unknown.
// Only a source received and left as received is withheld when it is hidden now.
func TestAuthorVisibility_TypedSourcesAreNotAnOracle(t *testing.T) {
	const (
		candidate = "https://example.org/@candidate-name"
		given     = "https://example.org/given-source"
	)
	pref := func(url string) []DocPreference {
		return []DocPreference{preference(PreferenceDimensionRecording, url, PreferenceChoiceAskBeforeRecording)}
	}
	piece := func(url string) PieceDocument {
		d := PieceDocument{SchemaVersion: DocumentSchemaVersion, Title: "Fixture Piece", Preferences: pref(url)}
		d.Conservation.SourceURL = &url

		return d
	}
	person := func(url string) PersonDocument {
		return PersonDocument{SchemaVersion: DocumentSchemaVersion, Name: "Fixture Person",
			Kind: PersonKindCommunityBard, Status: PersonStatusActive, StatusSourceURL: &url, Preferences: pref(url)}
	}
	// hiding is the visibility under which the URLs of hidden are hidden links.
	hiding := func(t *testing.T, hidden ...string) EditorVisibility {
		t.Helper()
		set := map[string]bool{}
		for _, url := range hidden {
			normalized, err := NormalizeURL(url)
			require.NoError(t, err)
			set[normalized] = true
		}

		return EditorVisibility{SnapshotVisibility: SnapshotVisibility{HiddenSources: maps.Clone(set),
			Persons: map[int64]PersonSummary{personID: {Slug: "fixture-person", Name: "Fixture Person"}}},
			HiddenLinkURLs: set, Role: RoleContributor}
	}
	pieceSources := func(v PieceSnapshot) (int, *string) { return len(v.Preferences), v.ConservationSourceURL }
	personSources := func(v PersonSnapshot) (int, *string) {
		require.NotNil(t, v.Profile)

		return len(v.Profile.Preferences), v.StatusSourceURL
	}

	tests := []struct {
		name     string
		isPerson bool
		// base is the URL of every source of the document the author received the projection of, made when no
		// link was hidden; empty for a new entity, of which the author received nothing.
		base string
		// shown is whether the author's view shows the sources of the submitted document, every one the candidate
		// URL, when the candidate is a hidden link's.
		shown bool
	}{
		{name: "typed piece sources", shown: true},
		{name: "typed person sources", isPerson: true, shown: true},
		{name: "received source edited to a hidden URL is shown", base: given, shown: true},
		{name: "received person source edited to a hidden URL is shown", isPerson: true, base: given, shown: true},
		{name: "received source hidden since is withheld", base: candidate},
		{name: "received person source hidden since is withheld", isPerson: true, base: candidate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			received := ElementKeySet{}
			if tt.isPerson {
				if tt.base != "" {
					_, received = EditableDocument(personID, person(tt.base), hiding(t))
				}
				view := func(vis EditorVisibility) PersonSnapshot {
					return personView(t, personID, person(candidate), received, vis, vis.SnapshotVisibility)
				}
				hiddenPrefs, hiddenStatus := personSources(view(hiding(t, candidate)))
				unknownPrefs, unknownStatus := personSources(view(hiding(t)))
				assertSourcesShown(t, tt.shown, hiddenPrefs, hiddenStatus, unknownPrefs, unknownStatus)

				return
			}
			if tt.base != "" {
				_, received = EditableDocument(pieceID, piece(tt.base), hiding(t))
			}
			view := func(vis EditorVisibility) PieceSnapshot {
				return pieceView(piece(candidate), received, vis, vis.SnapshotVisibility)
			}
			hiddenPrefs, hiddenConservation := pieceSources(view(hiding(t, candidate)))
			unknownPrefs, unknownConservation := pieceSources(view(hiding(t)))
			assertSourcesShown(t, tt.shown, hiddenPrefs, hiddenConservation, unknownPrefs, unknownConservation)
		})
	}

	// A received key recorded without its source's URL (by a projection made before the URL was recorded) fails
	// closed: the source counts as received, and is withheld while it is hidden.
	t.Run("received key without its source URL is withheld", func(t *testing.T) {
		received := ElementKeySet{preferenceElementKey(pref(candidate)[0]): true,
			elementKey("source", "conservation"): true}
		vis := hiding(t, candidate)
		prefs, conservation := pieceSources(pieceView(piece(candidate), received, vis, vis.SnapshotVisibility))
		assert.Zero(t, prefs)
		assert.Nil(t, conservation)
	})
}

// assertSourcesShown asserts that the author's view with the hidden URL shows its preference and scalar source as
// the view with an unknown URL does when shown, and withholds both otherwise.
func assertSourcesShown(t *testing.T, shown bool, hiddenPrefs int, hiddenSource *string, unknownPrefs int,
	unknownSource *string,
) {
	t.Helper()
	require.Equal(t, 1, unknownPrefs, "the view with an unknown URL shows the preference")
	require.NotNil(t, unknownSource, "the view with an unknown URL shows the source")
	if shown {
		assert.Equal(t, unknownPrefs, hiddenPrefs, "preferences differ: hidden %d, unknown %d", hiddenPrefs,
			unknownPrefs)
		assert.Equal(t, unknownSource, hiddenSource, "the scalar source differs")

		return
	}
	assert.Zero(t, hiddenPrefs, "a received source hidden since is withheld")
	assert.Nil(t, hiddenSource, "a received scalar source hidden since is null")
}
