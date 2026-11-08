package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sources of the hidden-source preference tests: the hidden link respelled (scheme and host case, a tracking
// parameter), which normalizes to it, and a source that does not normalize.
const (
	respelledHiddenLinkURL = "HTTPS://EXAMPLE.org/hidden-recording?utm_source=x"
	unnormalizableSource   = "https://exa mple.org/prefs"
)

func TestEditableDocument_PreferencesVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role Role
		want []DocPreference
	}{
		// An API token's principal, moderator or not, edits as a contributor (EditorVisibility.Role).
		{name: "contributor or API token", role: RoleContributor, want: shownPreferences()},
		{name: "interactive moderator", role: RoleModerator, want: testPreferences()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vis := editorVisibility(t, tt.role)
			piece, pieceKeys := EditableDocument(pieceID, fullPiece(), vis)
			assert.Equal(t, tt.want, piece.Preferences)
			person, personKeys := EditableDocument(personID, fullPerson(), vis)
			assert.Equal(t, tt.want, person.Preferences)
			for _, p := range tt.want {
				assert.True(t, pieceKeys[preferenceElementKey(p)], "a shown preference is received")
				assert.True(t, personKeys[preferenceElementKey(p)], "a shown preference is received")
			}
		})
	}
	t.Run("no-page person withheld whole", func(t *testing.T) {
		t.Parallel()
		doc, keys := EditableDocument(refNoPagePerson, noPagePerson(), editorVisibility(t, RoleContributor))
		assert.Empty(t, doc.Preferences)
		for _, p := range testPreferences() {
			assert.False(t, keys[preferenceElementKey(p)])
		}
	})
}

func TestEditableDocument_PreferenceWithHiddenSourceWithheld(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "hidden link", source: hiddenLinkURL},
		{name: "hidden link respelled", source: respelledHiddenLinkURL},
		{name: "source that does not normalize", source: unnormalizableSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			withheld := preference(PreferenceDimensionAttribution, tt.source, PreferenceChoiceSCAName)
			person := fullPerson()
			person.Preferences = []DocPreference{shownPreferences()[0], withheld}

			doc, keys := EditableDocument(personID, person, editorVisibility(t, RoleContributor))
			assert.Equal(t, shownPreferences(), doc.Preferences)
			assert.False(t, keys[preferenceElementKey(withheld)], "a withheld preference is not received")

			doc, _ = EditableDocument(personID, person, editorVisibility(t, RoleModerator))
			assert.Equal(t, person.Preferences, doc.Preferences, "an interactive moderator sees every preference")
		})
	}
}

func TestRestore_Preferences(t *testing.T) {
	t.Parallel()

	recording := shownPreferences()[0]
	edited := preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceNoRecording)
	licensing := preference(PreferenceDimensionLicensing, preferenceSource, PreferenceChoiceCCBY)
	hiddenSource := testPreferences()[0]
	tests := []struct {
		name      string
		role      Role
		submitted []DocPreference
		// received edits the keys the author received of the base's projection.
		received func(ElementKeySet) ElementKeySet
		want     []DocPreference
	}{
		{name: "received and kept", role: RoleContributor, submitted: []DocPreference{recording},
			want: testPreferences()},
		{name: "received and edited", role: RoleContributor, submitted: []DocPreference{edited},
			want: []DocPreference{hiddenSource, edited}},
		{name: "received and deleted stays deleted", role: RoleContributor, submitted: []DocPreference{},
			want: []DocPreference{hiddenSource}},
		{name: "never received is restored", role: RoleContributor, submitted: []DocPreference{},
			received: EditorFieldsSent{}.ReceivedKeys, want: testPreferences()},
		{name: "added beside the base's", role: RoleContributor, submitted: []DocPreference{licensing, recording},
			want: []DocPreference{licensing, hiddenSource, recording}},
		{name: "moderator deletes any", role: RoleModerator, submitted: []DocPreference{recording},
			want: []DocPreference{recording}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vis := editorVisibility(t, tt.role)
			base := fullPerson()
			sub, keys := EditableDocument(personID, base, vis)
			if tt.received != nil {
				keys = tt.received(keys)
			}
			sub.Preferences = tt.submitted
			got, flags := RestoreDocument(personID, sub, &base, keys, vis)
			assert.Equal(t, tt.want, CanonicalisePreferences(got.Preferences))
			assert.Empty(t, flags)
		})
	}
	t.Run("no-page profile whole", func(t *testing.T) {
		t.Parallel()
		vis := editorVisibility(t, RoleContributor)
		base := noPagePerson()
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		sub.Preferences = []DocPreference{licensing}
		got, _ := RestoreDocument(refNoPagePerson, sub, &base, keys, vis)
		assert.Equal(t, base.Preferences, got.Preferences, "the base's profile, what was sent ignored")
	})
}

func TestRestore_PreferenceWithHiddenSourceRestored(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	for _, source := range []string{hiddenLinkURL, respelledHiddenLinkURL, unnormalizableSource} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			base := fullPiece()
			withheld := preference(PreferenceDimensionAttribution, source, PreferenceChoiceSCAName)
			base.Preferences = []DocPreference{withheld, shownPreferences()[0]}
			sub, keys := EditableDocument(pieceID, base, vis)
			require.Equal(t, shownPreferences(), sub.Preferences)

			got, flags := RestoreDocument(pieceID, sub, &base, keys, vis)
			assert.Equal(t, base.Preferences, got.Preferences, "the withheld preference is put back")
			assert.Empty(t, flags)

			sub.Preferences = []DocPreference{
				preference(PreferenceDimensionAttribution, preferenceSource, PreferenceChoiceStageName),
			}
			got, flags = RestoreDocument(pieceID, sub, &base, keys, vis)
			assert.Equal(t, sub.Preferences, got.Preferences,
				"a submitted preference of the withheld one's dimension replaces it, and the received one deleted stays so")
			assert.Empty(t, flags)
		})
	}
}

// TestSubmit_AbsentPreferencesKeepBase checks that a draft whose client never sent the preferences (its received
// keys without them, and none in its document) keeps every base preference when submitted, as a contributor and as
// an interactive moderator.
func TestSubmit_AbsentPreferencesKeepBase(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleContributor, RoleModerator} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			vis := editorVisibility(t, role)
			base := planPerson()
			base.Preferences = testPreferences()
			sub, keys := EditableDocument(personID, base, vis)
			sub.Preferences = nil
			sub.NotesMD = ptr("Edited by an older client.")
			plan, err := PlanSubmit(SubmitState[PersonDocument]{ID: personID, Submitted: sub, Base: &base,
				Received: EditorFieldsSent{}.ReceivedKeys(keys), Visibility: vis, Refs: testRefs(),
				Summary: testSummary, Today: validToday})
			require.NoError(t, err, "%v", problemsOrNil(err))
			assert.Equal(t, base.Preferences, plan.Document.Preferences)
			assert.Equal(t, ptr("Edited by an older client."), plan.Document.NotesMD)
		})
	}
}

func TestEditorFieldsSent_ReceivedKeys(t *testing.T) {
	t.Parallel()

	received := ElementKeySet{preferenceElementKey(shownPreferences()[0]): true, profileKey: true,
		linkElementKey(songbookPageURL): true}
	assert.Equal(t, received, AllEditorFieldsSent.ReceivedKeys(received))
	assert.Equal(t, ElementKeySet{profileKey: true, linkElementKey(songbookPageURL): true},
		EditorFieldsSent{}.ReceivedKeys(received), "only the preferences' keys are given up")
	assert.Len(t, received, 3, "received is not changed")
}

func TestPlanSubmit_PreferenceFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// edit changes the submitted document, the projection of a base with the shown preferences only.
		edit func(*PersonDocument)
		want []Flag
	}{
		{name: "hidden alias in notes", edit: func(d *PersonDocument) {
			d.Preferences = []DocPreference{noted(shownPreferences()[0], "Ask "+legalName+" first.")}
		}, want: []Flag{{Code: FlagHiddenAliasInText, Path: "/preferences/0/value/notes"}}},
		{name: "source is a hidden link", edit: func(d *PersonDocument) {
			d.Preferences = []DocPreference{
				preference(PreferenceDimensionRecording, hiddenLinkURL, PreferenceChoiceAskBeforeRecording),
			}
		}, want: []Flag{{Code: FlagHiddenLinkAttached, Path: "/preferences/0/source_url"}}},
		{name: "source is a hidden link respelled", edit: func(d *PersonDocument) {
			d.Preferences = []DocPreference{
				preference(PreferenceDimensionRecording, respelledHiddenLinkURL, PreferenceChoiceAskBeforeRecording),
			}
		}, want: []Flag{{Code: FlagHiddenLinkAttached, Path: "/preferences/0/source_url"}}},
		{name: "unchanged", edit: func(*PersonDocument) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vis := editorVisibility(t, RoleContributor)
			base := planPerson()
			base.Preferences = shownPreferences()
			sub, keys := EditableDocument(personID, base, vis)
			tt.edit(&sub)
			plan, err := PlanSubmit(SubmitState[PersonDocument]{ID: personID, Submitted: sub, Base: &base,
				Received: keys, Visibility: vis, Refs: testRefs(), HiddenAliases: []string{legalName},
				Summary: testSummary, Today: validToday})
			require.NoError(t, err, "%v", problemsOrNil(err))
			assert.Equal(t, tt.want, plan.Flags)
		})
	}
	t.Run("hidden source unchanged from base", func(t *testing.T) {
		t.Parallel()
		vis := editorVisibility(t, RoleContributor)
		base := planPerson()
		base.Preferences = testPreferences()
		sub, keys := EditableDocument(personID, base, vis)
		plan, err := PlanSubmit(SubmitState[PersonDocument]{ID: personID, Submitted: sub, Base: &base,
			Received: keys, Visibility: vis, Refs: testRefs(), Summary: testSummary, Today: validToday})
		require.NoError(t, err, "%v", problemsOrNil(err))
		assert.Equal(t, testPreferences(), plan.Document.Preferences, "restored")
		assert.Empty(t, plan.Flags, "a hidden source the base has already raises no flag")
	})
}
