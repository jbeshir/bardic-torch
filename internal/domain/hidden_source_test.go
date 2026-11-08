package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visibleSource is a source URL that is no link's.
const visibleSource = "https://example.org/assessment"

// The names of the hidden-source test cases of a hidden link, and of the hidden link respelled.
const (
	caseHiddenLink          = "hidden link"
	caseRespelledHiddenLink = "hidden link respelled"
)

// sourceKeys are the element keys of the scalar sources.
var (
	statusSourceKey       = elementKey("source", "status")
	conservationSourceKey = elementKey("source", "conservation")
)

// sourceCases are the sources of the hidden-source tests, and whether a contributor or a public read is shown each.
var sourceCases = []struct {
	name   string
	source string
	shown  bool
}{
	{name: "visible", source: visibleSource, shown: true},
	{name: caseHiddenLink, source: hiddenLinkURL},
	{name: caseRespelledHiddenLink, source: respelledHiddenLinkURL},
	{name: "does not normalize", source: unnormalizableSource},
}

// hiddenSources are the normalized URLs of the hidden links of the hidden-source tests.
func hiddenSources(t *testing.T) map[string]bool {
	t.Helper()
	hidden, err := NormalizeURL(hiddenLinkURL)
	require.NoError(t, err)

	return map[string]bool{hidden: true}
}

// TestHiddenSourceRule checks each source outside a document's links against the hidden-source rule: a
// preference's source (the preference is dropped), a person's status source and a piece's conservation source in
// snapshots, and a conservation history entry's source (null), each visible, a hidden link, a hidden link
// respelled, and a URL that does not normalize.
func TestHiddenSourceRule(t *testing.T) {
	t.Parallel()

	kinds := []struct {
		name string
		// shows applies the rule to a document or entry holding source, and returns whether it is shown.
		shows func(t *testing.T, source string, hidden map[string]bool) bool
	}{
		{name: "preference", shows: func(_ *testing.T, source string, hidden map[string]bool) bool {
			prefs := ShownPreferences([]DocPreference{preference(PreferenceDimensionRecording, source,
				PreferenceChoiceOralOnly)}, hidden)
			return len(prefs) == 1
		}},
		{name: "status source", shows: func(t *testing.T, source string, hidden map[string]bool) bool {
			d := validPerson()
			d.Status, d.StatusSourceURL = PersonStatusDeceased, &source
			snapshot, err := NewPersonSnapshot(personID, d, SnapshotVisibility{HiddenSources: hidden,
				Persons: map[int64]PersonSummary{personID: {Name: d.Name}}})
			require.NoError(t, err)
			return sourceIs(t, snapshot.StatusSourceURL, source)
		}},
		{name: "conservation source", shows: func(t *testing.T, source string, hidden map[string]bool) bool {
			d := validPiece()
			d.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: &source}
			return sourceIs(t, NewPieceSnapshot(d, SnapshotVisibility{HiddenSources: hidden}).ConservationSourceURL,
				source)
		}},
		{name: "conservation history source", shows: func(t *testing.T, source string, hidden map[string]bool) bool {
			return sourceIs(t, ShownSource(&source, hidden), source)
		}},
	}
	for _, kind := range kinds {
		for _, tt := range sourceCases {
			t.Run(kind.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.shown, kind.shows(t, tt.source, hiddenSources(t)))
			})
		}
	}
	t.Run("null source stays null", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, ShownSource(nil, hiddenSources(t)))
	})
}

// sourceIs is whether got is source, failing if it is neither source nor nil.
func sourceIs(t *testing.T, got *string, source string) bool {
	t.Helper()
	if got == nil {
		return false
	}
	require.Equal(t, source, *got)

	return true
}

// sourcedPerson is a deceased person, whose status needs a source, with source.
func sourcedPerson(source string) PersonDocument {
	d := planPerson()
	d.Status, d.StatusSourceURL = PersonStatusDeceased, &source

	return d
}

// sourcedPiece is an endangered piece, whose status needs a source, with source.
func sourcedPiece(source string) PieceDocument {
	d := validPiece()
	d.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: &source}

	return d
}

func TestEditableDocument_HiddenStatusSourceWithheld(t *testing.T) {
	t.Parallel()

	for _, tt := range sourceCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var want *string
			if tt.shown {
				want = &tt.source
			}

			person, keys := EditableDocument(personID, sourcedPerson(tt.source), editorVisibility(t, RoleContributor))
			assert.Equal(t, want, person.StatusSourceURL)
			assert.Equal(t, tt.shown, keys[statusSourceKey], "a source is received exactly when shown")

			piece, keys := EditableDocument(pieceID, sourcedPiece(tt.source), editorVisibility(t, RoleContributor))
			assert.Equal(t, want, piece.Conservation.SourceURL)
			assert.Equal(t, tt.shown, keys[conservationSourceKey], "a source is received exactly when shown")

			noPage := noPagePerson()
			noPage.StatusSourceURL = &tt.source
			identity, _ := EditableDocument(refNoPagePerson, noPage, editorVisibility(t, RoleContributor))
			assert.Equal(t, want, identity.StatusSourceURL, "a no-page person's identity has the rule too")

			person, _ = EditableDocument(personID, sourcedPerson(tt.source), editorVisibility(t, RoleModerator))
			assert.Equal(t, &tt.source, person.StatusSourceURL, "an interactive moderator sees every source")
			piece, _ = EditableDocument(pieceID, sourcedPiece(tt.source), editorVisibility(t, RoleModerator))
			assert.Equal(t, &tt.source, piece.Conservation.SourceURL, "an interactive moderator sees every source")
		})
	}
}

func TestRestore_WithheldStatusSourceRestored(t *testing.T) {
	t.Parallel()

	const typed = "https://example.org/typed-source"
	tests := []struct {
		name   string
		role   Role
		source string
		// submitted is the source the author submits, and received edits the keys they received.
		submitted *string
		received  func(ElementKeySet)
		want      *string
	}{
		{name: "withheld, submitted null: base's back", role: RoleContributor, source: hiddenLinkURL,
			want: ptr(hiddenLinkURL)},
		{name: "withheld respelled, submitted null: base's back", role: RoleContributor, source: respelledHiddenLinkURL,
			want: ptr(respelledHiddenLinkURL)},
		{name: "withheld, submitted URL replaces it", role: RoleContributor, source: hiddenLinkURL,
			submitted: ptr(typed), want: ptr(typed)},
		{name: "received and deleted stays deleted", role: RoleContributor, source: visibleSource},
		{name: "never received, submitted null: base's back", role: RoleContributor, source: visibleSource,
			received: func(keys ElementKeySet) {
				delete(keys, statusSourceKey)
				delete(keys, conservationSourceKey)
			}, want: ptr(visibleSource)},
		{name: "moderator sees it and deletes it", role: RoleModerator, source: hiddenLinkURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vis := editorVisibility(t, tt.role)

			person := sourcedPerson(tt.source)
			sub, keys := EditableDocument(personID, person, vis)
			if tt.received != nil {
				tt.received(keys)
			}
			sub.StatusSourceURL = tt.submitted
			got, flags := RestoreDocument(personID, sub, &person, keys, vis, countingKeys())
			assert.Equal(t, tt.want, got.StatusSourceURL)
			assert.Empty(t, flags)

			piece := sourcedPiece(tt.source)
			pieceSub, keys := EditableDocument(pieceID, piece, vis)
			if tt.received != nil {
				tt.received(keys)
			}
			pieceSub.Conservation.SourceURL = tt.submitted
			gotPiece, flags := RestoreDocument(pieceID, pieceSub, &piece, keys, vis, countingKeys())
			assert.Equal(t, tt.want, gotPiece.Conservation.SourceURL)
			assert.Empty(t, flags)
		})
	}
	t.Run("no-page person's identity", func(t *testing.T) {
		t.Parallel()
		vis := editorVisibility(t, RoleContributor)
		base := noPagePerson()
		base.StatusSourceURL = ptr(respelledHiddenLinkURL)
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		require.Nil(t, sub.StatusSourceURL)
		got, _ := RestoreDocument(refNoPagePerson, sub, &base, keys, vis, countingKeys())
		assert.Equal(t, base.StatusSourceURL, got.StatusSourceURL)
	})
}

// TestValidateSubmission_WithheldSourceIsNotPhaseOne checks that a contributor who was given a status or
// conservation source as null, because it is a hidden link, and leaves it so is not told the source is required:
// the source is put back, and the submission plans without a problem or a flag. A source they were given and deleted
// is still required.
func TestValidateSubmission_WithheldSourceIsNotPhaseOne(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	inputs := func(id int64, keys ElementKeySet) SubmitInputs {
		return SubmitInputs{ID: id, Keys: countingKeys(), Received: keys, Visibility: vis, Refs: testRefs(),
			Summary: testSummary, Today: validToday}
	}
	t.Run("status source", func(t *testing.T) {
		t.Parallel()
		base := sourcedPerson(hiddenLinkURL)
		sub, keys := EditableDocument(personID, base, vis)
		require.Nil(t, sub.StatusSourceURL)
		plan, err := PlanSubmit(SubmitState[PersonDocument]{Submitted: sub, Base: &base, SubmitInputs: inputs(personID,
			keys)})
		require.NoError(t, err, "%v", problemsOrNil(err))
		assert.Equal(t, base.StatusSourceURL, plan.Document.StatusSourceURL)
		assert.Empty(t, plan.Flags)
	})
	t.Run("conservation source", func(t *testing.T) {
		t.Parallel()
		base := sourcedPiece(respelledHiddenLinkURL)
		sub, keys := EditableDocument(pieceID, base, vis)
		require.Nil(t, sub.Conservation.SourceURL)
		plan, err := PlanSubmit(SubmitState[PieceDocument]{Submitted: sub, Base: &base, SubmitInputs: inputs(pieceID,
			keys)})
		require.NoError(t, err, "%v", problemsOrNil(err))
		assert.Equal(t, base.Conservation.SourceURL, plan.Document.Conservation.SourceURL)
		assert.Empty(t, plan.Flags)
	})
	t.Run("a visible source deleted is required", func(t *testing.T) {
		t.Parallel()
		base := sourcedPerson(visibleSource)
		sub, keys := EditableDocument(personID, base, vis)
		sub.StatusSourceURL = nil
		_, err := PlanSubmit(SubmitState[PersonDocument]{Submitted: sub, Base: &base, SubmitInputs: inputs(personID,
			keys)})
		assert.Contains(t, problemsOf(t, err), DocumentProblem{Path: "/status_source_url", Code: ProblemSourceRequired})
	})
}

// TestPlanSubmit_SourceFlags checks the hidden_link_attached flags of status and conservation sources: a source
// that is a hidden link, in any spelling, is flagged at its path unless the base has it already.
func TestPlanSubmit_SourceFlags(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	inputs := func(id int64, keys ElementKeySet) SubmitInputs {
		return SubmitInputs{ID: id, Keys: countingKeys(), Received: keys, Visibility: vis, Refs: testRefs(),
			Summary: testSummary, Today: validToday}
	}
	tests := []struct {
		name   string
		source string
	}{
		{name: caseHiddenLink, source: hiddenLinkURL},
		{name: caseRespelledHiddenLink, source: respelledHiddenLinkURL},
		{name: "visible", source: visibleSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hidden := tt.source != visibleSource

			person := sourcedPerson("https://example.org/old-notice")
			sub, keys := EditableDocument(personID, person, vis)
			sub.StatusSourceURL = ptr(tt.source)
			plan, err := PlanSubmit(SubmitState[PersonDocument]{Submitted: sub, Base: &person,
				SubmitInputs: inputs(personID, keys)})
			require.NoError(t, err, "%v", problemsOrNil(err))
			assert.Equal(t, hidden, assert.ObjectsAreEqual(
				[]Flag{{Code: FlagHiddenLinkAttached, Path: "/status_source_url"}}, plan.Flags), "%v", plan.Flags)

			piece := sourcedPiece("https://example.org/old-survey")
			pieceSub, keys := EditableDocument(pieceID, piece, vis)
			pieceSub.Conservation.SourceURL = ptr(tt.source)
			piecePlan, err := PlanSubmit(SubmitState[PieceDocument]{Submitted: pieceSub, Base: &piece,
				SubmitInputs: inputs(pieceID, keys)})
			require.NoError(t, err, "%v", problemsOrNil(err))
			assert.Equal(t, hidden, assert.ObjectsAreEqual(
				[]Flag{{Code: FlagHiddenLinkAttached, Path: "/conservation/source_url"}}, piecePlan.Flags),
				"%v", piecePlan.Flags)
		})
	}
	t.Run("unchanged from base", func(t *testing.T) {
		t.Parallel()
		moderator := editorVisibility(t, RoleModerator)
		base := sourcedPerson(hiddenLinkURL)
		doc := base
		doc.NotesMD = ptr("Edited.")
		flags := contentFlags(doc, &base, testSummary, testRefs(), moderator.HiddenLinkURLs, NewNameMatcher(nil, nil))
		assert.Empty(t, flags, "a hidden source the base has already raises no flag")
	})
}
