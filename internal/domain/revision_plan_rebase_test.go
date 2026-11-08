package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planPerson is fullPerson with only what TestPlanSubmit's person keeps, which validates against testRefs.
func planPerson() PersonDocument {
	person := fullPerson()
	person.Relations, person.AffiliationIDs = person.Relations[:1], person.AffiliationIDs[:1]
	person.Links = person.Links[:1]

	return person
}

func hasProblem(t *testing.T, err error, code ProblemCode) bool {
	t.Helper()
	for _, p := range problemsOf(t, err) {
		if p.Code == code {
			return true
		}
	}

	return false
}

func TestPlanRebase(t *testing.T) {
	t.Parallel()

	person := planPerson()
	vis := editorVisibility(t, RoleModerator)
	resolved, _ := EditableDocument(personID, person, vis)
	resolved.NotesMD = ptr("Resolved.")
	state := func(current *PersonDocument) RebaseState[PersonDocument] {
		return RebaseState[PersonDocument]{Resolved: resolved, Revision: person, Current: current,
			RebaseInputs: RebaseInputs{ID: personID, Keys: countingKeys(), Sent: AllEditorFieldsSent, Author: RoleContributor, Visibility: vis, Refs: testRefs(),
				Summary: testSummary, Today: validToday}}
	}

	t.Run("ResolvedKept", func(t *testing.T) {
		t.Parallel()
		current := planPerson()
		doc, err := PlanRebase(state(&current))
		require.NoError(t, err)
		assert.Equal(t, ptr("Resolved."), doc.NotesMD)
		assert.Equal(t, person.Aliases, doc.Aliases, "the hidden alias, which the moderator sees, is kept")
	})
	t.Run("SourcedFieldCheckedAgainstCurrent", func(t *testing.T) {
		t.Parallel()
		s := state(&person)
		s.Resolved.Status, s.Resolved.StatusSourceURL = PersonStatusDeceased, nil
		_, err := PlanRebase(s)
		assert.True(t, hasProblem(t, err, ProblemSourceRequired), "%v", err)
	})
}

// rebasePreferences are two preferences of a rebase's current document, which the resolved document changes.
func rebasePreferences() []DocPreference {
	return []DocPreference{
		preference(PreferenceDimensionPerformancePermission, preferenceSource, PreferenceChoiceAskFirst),
		preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceAskBeforeRecording),
	}
}

func TestPlanRebase_PreferencesFromDocument(t *testing.T) {
	t.Parallel()

	current := planPerson()
	current.Preferences = rebasePreferences()
	vis := editorVisibility(t, RoleModerator)
	resolved, _ := EditableDocument(personID, current, vis)
	require.Equal(t, current.Preferences, resolved.Preferences, "the moderator's editable view shows the preferences")
	resolved.Preferences = []DocPreference{
		preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceNoRecording,
			PreferenceChoiceOralOnly),
	}
	doc, err := PlanRebase(RebaseState[PersonDocument]{Resolved: resolved, Revision: planPerson(), Current: &current,
		RebaseInputs: RebaseInputs{ID: personID, Keys: countingKeys(), Sent: AllEditorFieldsSent, Author: RoleContributor, Visibility: vis, Refs: testRefs(),
			Summary: testSummary, Today: validToday}})
	require.NoError(t, err, "%v", problemsOrNil(err))
	assert.Equal(t, []DocPreference{
		preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceOralOnly,
			PreferenceChoiceNoRecording),
	}, doc.Preferences, "the document's preferences, the one it removed gone, in canonical order")
}

func TestPlanRebase_AbsentPreferencesKeepCurrent(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleModerator)
	revision := planPerson()
	revision.Preferences = rebasePreferences()[:1]
	current := planPerson()
	current.Preferences = rebasePreferences()
	resolved := planPerson()
	resolved.Preferences = nil // a client built before preferences existed sends none
	state := func(current *PersonDocument) RebaseState[PersonDocument] {
		return RebaseState[PersonDocument]{Resolved: resolved, Revision: revision, Current: current,
			RebaseInputs: RebaseInputs{ID: personID, Keys: countingKeys(), Sent: EditorFieldsSent{}, Author: RoleContributor, Visibility: vis, Refs: testRefs(),
				Summary: testSummary, Today: validToday}}
	}

	doc, err := PlanRebase(state(&current))
	require.NoError(t, err, "%v", problemsOrNil(err))
	assert.Equal(t, current.Preferences, doc.Preferences, "the current document's")

	s := state(nil)
	s.Resolved.Links, s.Revision.Links = nil, nil // a new entity's references are all checked as added
	doc, err = PlanRebase(s)
	require.NoError(t, err, "%v", problemsOrNil(err))
	assert.Equal(t, revision.Preferences, doc.Preferences, "for a new entity, the revision's")
}

func TestPlanRevert(t *testing.T) {
	t.Parallel()

	current := planPerson()
	target := planPerson()
	target.NotesMD = ptr("The old notes.")
	target.Preferences = nil
	state := func(role Role) RevertState[PersonDocument] {
		return RevertState[PersonDocument]{Target: target, Current: current, RevertInputs: RevertInputs{ID: personID,
			Visibility: editorVisibility(t, role), Refs: testRefs(), HiddenAliases: []string{legalName},
			Summary: testSummary, Today: validToday}}
	}

	t.Run("Contributor", func(t *testing.T) {
		t.Parallel()
		plan, err := PlanRevert(state(RoleContributor))
		require.NoError(t, err)
		assert.Equal(t, RevertDocument(target, current), plan.Document)
		assert.Equal(t, target.Preferences, plan.Document.Preferences, "the target's preferences")
		projection, received := EditableDocument(personID, plan.Document, editorVisibility(t, RoleContributor))
		assert.Equal(t, projection, plan.Submitted, "the author's document is the projection")
		assert.Equal(t, received, plan.Received)
		assert.NotContains(t, plan.Submitted.Aliases, hiddenLegalName)
		assert.Empty(t, plan.Flags)
	})
	t.Run("Moderator", func(t *testing.T) {
		t.Parallel()
		plan, err := PlanRevert(state(RoleModerator))
		require.NoError(t, err)
		assert.Equal(t, RevertDocument(target, current), plan.Document)
		assert.Empty(t, plan.Flags, "a moderator's revert is approved, and so validated, by PlanApproval")
	})
	t.Run("SummaryFlagged", func(t *testing.T) {
		t.Parallel()
		s := state(RoleContributor)
		s.Summary = "Back to what " + legalName + " wrote."
		plan, err := PlanRevert(s)
		require.NoError(t, err)
		assert.Contains(t, plan.Flags, Flag{Code: FlagHiddenAliasInSummary, Path: pathSummaryFlag})
	})
	t.Run("ReasonRequired", func(t *testing.T) {
		t.Parallel()
		piece := fullPiece()
		old := fullPiece()
		old.Conservation = DocConservation{Status: ConservationStatusEndangered,
			SourceURL: ptr("https://example.org/wren/endangered")}
		s := RevertState[PieceDocument]{Target: old, Current: piece, RevertInputs: RevertInputs{ID: pieceID,
			Visibility: editorVisibility(t, RoleContributor), Refs: testRefs(), Today: validToday}}
		_, err := PlanRevert(s)
		assert.True(t, hasProblem(t, err, ProblemReasonRequired), "%v", err)

		s.Summary = "Endangered after all."
		_, err = PlanRevert(s)
		require.NoError(t, err)
	})
}

func problemsOrNil(err error) []DocumentProblem {
	var d *DocumentError
	if errors.As(err, &d) {
		return d.Problems
	}

	return nil
}
