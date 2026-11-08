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
	require.Empty(t, resolved.Preferences, "the moderator's editable view withholds the preferences")
	state := func(current *PersonDocument) RebaseState[PersonDocument] {
		return RebaseState[PersonDocument]{ID: personID, Resolved: resolved, Revision: person, Current: current,
			Author: RoleContributor, Visibility: vis, Refs: testRefs(), Summary: testSummary, Today: validToday}
	}

	t.Run("PreferencesFromCurrent", func(t *testing.T) {
		t.Parallel()
		current := planPerson()
		current.Preferences = current.Preferences[:1]
		doc, err := PlanRebase(state(&current))
		require.NoError(t, err)
		assert.Equal(t, current.Preferences, doc.Preferences)
		assert.Equal(t, ptr("Resolved."), doc.NotesMD)
		assert.Equal(t, person.Aliases, doc.Aliases, "the hidden alias, which the moderator sees, is kept")
	})
	t.Run("NewEntityPreferencesFromTheRevision", func(t *testing.T) {
		t.Parallel()
		s := state(nil)
		s.Resolved.Links, s.Revision.Links = nil, nil // a new entity's references are all checked as added
		doc, err := PlanRebase(s)
		require.NoError(t, err, "%v", problemsOrNil(err))
		assert.Equal(t, person.Preferences, doc.Preferences)
	})
	t.Run("SourcedFieldCheckedAgainstCurrent", func(t *testing.T) {
		t.Parallel()
		s := state(&person)
		s.Resolved.Status, s.Resolved.StatusSourceURL = PersonStatusDeceased, nil
		_, err := PlanRebase(s)
		assert.True(t, hasProblem(t, err, ProblemSourceRequired), "%v", err)
	})
}

func TestPlanRevert(t *testing.T) {
	t.Parallel()

	current := planPerson()
	target := planPerson()
	target.NotesMD = ptr("The old notes.")
	target.Preferences = nil
	state := func(role Role) RevertState[PersonDocument] {
		return RevertState[PersonDocument]{ID: personID, Target: target, Current: current,
			Visibility: editorVisibility(t, role), Refs: testRefs(), HiddenAliases: []string{legalName},
			Summary: testSummary, Today: validToday}
	}

	t.Run("Contributor", func(t *testing.T) {
		t.Parallel()
		plan, err := PlanRevert(state(RoleContributor))
		require.NoError(t, err)
		assert.Equal(t, RevertDocument(target, current), plan.Document)
		assert.Equal(t, current.Preferences, plan.Document.Preferences)
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
		s := RevertState[PieceDocument]{ID: pieceID, Target: old, Current: piece,
			Visibility: editorVisibility(t, RoleContributor), Refs: testRefs(), Today: validToday}
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
