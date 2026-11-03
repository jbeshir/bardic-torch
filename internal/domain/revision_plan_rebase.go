package domain

import "slices"

// RebaseState is what planning a moderator's rebase of a pending revision needs (D9): the document the moderator
// resolved the conflicts in, edited from their editable view (the preferences withheld, D5), the revision's
// document, and the entity's current document, which the rebase moves the revision onto, nil for a new entity.
type RebaseState[D Document] struct {
	// ID is the entity's ID.
	ID       int64
	Resolved D
	Revision D
	Current  *D
	// Author is the role the revision was submitted with, for the contributor restrictions.
	Author Role
	// Visibility is what the moderator sees.
	Visibility EditorVisibility
	Refs       ReferenceIndex
	Summary    string
	Today      Date
}

// PlanRebase plans a rebase (D9): the resolved document with what was withheld from the moderator (the
// preferences) restored from the current document, or for a new entity from the revision's, validated against the
// current document with the revision author's role for the contributor restrictions. Problems are a
// *DocumentError, for the moderator.
func PlanRebase[D Document](s RebaseState[D]) (D, error) {
	from := s.Revision
	if s.Current != nil {
		from = *s.Current
	}
	_, received := EditableDocument(s.ID, from, s.Visibility)
	doc, _ := RestoreDocument(s.ID, s.Resolved, &from, received, s.Visibility)
	v := ValidationContext[D]{Author: s.Author, ID: s.ID, Previous: s.Current, Refs: s.Refs, Summary: s.Summary,
		Today: s.Today}
	if problems := validateDocument(doc, v); len(problems) > 0 {
		var zero D
		return zero, &DocumentError{Problems: problems}
	}

	return doc, nil
}

// RevertState is what planning a revert of an entity to an earlier approved document of it needs (D10).
type RevertState[D Document] struct {
	// ID is the entity's ID.
	ID      int64
	Target  D
	Current D
	// Visibility is what the reverting actor sees; its role decides whether the revert is a contributor's.
	Visibility EditorVisibility
	Refs       ReferenceIndex
	// HiddenAliases are the names of every hidden alias, for the flags.
	HiddenAliases []string
	Summary       string
	Today         Date
}

// RevertPlan is a planned revert: the full document of the new revision, the projection of it its author is given
// as their document (D5), the keys of that projection, and for a contributor's revert its flags.
type RevertPlan[D Document] struct {
	Document  D
	Submitted D
	Received  ElementKeySet
	Flags     []Flag
}

// PlanRevert plans a revert (D10): the document is RevertDocument(target, current), based on the current document.
// A contributor's (or an API token's) revert is validated as a submission of its projection would be
// (ValidateSubmission against the current document): its problems are a *DocumentError, and those only the full
// document has become hidden_data_conflict flags, with the flags of its content. A moderator's revert is approved
// at once, and so validated by PlanApproval.
func PlanRevert[D Document](s RevertState[D]) (RevertPlan[D], error) {
	doc := RevertDocument(s.Target, s.Current)
	projection, received := EditableDocument(s.ID, doc, s.Visibility)
	plan := RevertPlan[D]{Document: doc, Submitted: projection, Received: received}
	if s.Visibility.Role == RoleModerator {
		return plan, nil
	}
	current, _ := EditableDocument(s.ID, s.Current, s.Visibility)
	v := ValidationContext[D]{Author: RoleContributor, ID: s.ID, Previous: &s.Current, Refs: s.Refs,
		Summary: s.Summary, Today: s.Today}
	problems, conflicts := ValidateSubmission(projection, doc, &current, v)
	if len(problems) > 0 {
		return RevertPlan[D]{}, &DocumentError{Problems: problems}
	}
	plan.Flags = uniqueFlags(slices.Concat(conflicts, contentFlags(doc, &s.Current, s.Summary, s.Refs,
		s.Visibility.HiddenLinkURLs, NewNameMatcher(s.HiddenAliases, nil))))

	return plan, nil
}
