package domain

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// The errors planning a submission or an approval refuses with, besides *DocumentError, *UnacknowledgedFlagsError
// and *PendingReferenceFlaggedError.
var (
	// ErrRebaseRequired is an approval of a stale revision whose merge with the current document conflicts.
	ErrRebaseRequired = errors.New("rebase required")
	// ErrHiddenDataConflict is an approval of a revision flagged hidden_alias_collision or hidden_data_conflict:
	// approving it would publish a hidden name or write data that is only invalid because of hidden data.
	ErrHiddenDataConflict = errors.New("hidden data conflict")
	// ErrDuplicateBlocked is an approval of a new person matching a person who prefers no page.
	ErrDuplicateBlocked = errors.New("duplicate of a person who prefers no page")
)

// DocumentError is a document that fails validation, with its problems.
type DocumentError struct {
	Problems []DocumentProblem
}

func (e *DocumentError) Error() string {
	return fmt.Sprintf("invalid document: %d problems", len(e.Problems))
}

// UnacknowledgedFlagsError is an approval or a moderator's submission with flags the moderator did not
// acknowledge, or a hidden_alias_in_summary flag without a clean summary to publish instead.
type UnacknowledgedFlagsError struct {
	Flags []Flag
}

func (e *UnacknowledgedFlagsError) Error() string {
	return fmt.Sprintf("%d unacknowledged flags", len(e.Flags))
}

// PendingReferenceFlaggedError is an approval of a revision referencing a pending new term or person whose own
// revision, RevisionID, is flagged: it must be approved on its own first.
type PendingReferenceFlaggedError struct {
	RevisionID int64
	Flags      []Flag
}

func (e *PendingReferenceFlaggedError) Error() string {
	return fmt.Sprintf("pending reference [%d] is flagged", e.RevisionID)
}

// SubmitState is what planning a submission needs: the document as its author sent it, the full document
// of the draft's own base and what the author can see.
type SubmitState[D Document] struct {
	// ID is the entity's ID, 0 for a new entity.
	ID        int64
	Submitted D
	// Base is the full document of the draft's base revision, nil for a new entity.
	Base *D
	// Received are the keys of the projection the author was given (the draft's received keys).
	Received   ElementKeySet
	Visibility EditorVisibility
	Refs       ReferenceIndex
	// HiddenAliases are the names of every hidden alias, for the flags.
	HiddenAliases []string
	Summary       string
	Today         Date
	// Keys issues the keys of the submission's new performances.
	Keys KeySource
}

// SubmitPlan is the planned outcome of a submission: the full document to store, and the flags, which only
// moderators see.
type SubmitPlan[D Document] struct {
	Document D
	Flags    []Flag
}

// PlanSubmit plans a submission. It restores the submitted document onto the draft's own base, never
// merging it with the current document: a stale base is approval's to handle. An interactive moderator's
// submission is then approved at once and planned with PlanApproval, which validates it. A contributor's (or an
// API token's) is validated in two phases (ValidateSubmission): the problems of the submitted document are the
// only ones returned, as a *DocumentError; those only the restored document has become hidden_data_conflict
// flags, with the restore's collisions and the flags of the restored document's content.
func PlanSubmit[D Document](s SubmitState[D]) (SubmitPlan[D], error) {
	restored, flags := RestoreDocument(s.ID, s.Submitted, s.Base, s.Received, s.Visibility, s.Keys)
	restored = CanonicalDocument(restored)
	if s.Visibility.Role == RoleModerator {
		return SubmitPlan[D]{Document: restored}, nil
	}
	var projection *D
	if s.Base != nil {
		p, _ := EditableDocument(s.ID, *s.Base, s.Visibility)
		projection = &p
	}
	v := ValidationContext[D]{Author: RoleContributor, ID: s.ID, Previous: s.Base, Refs: s.Refs, Summary: s.Summary,
		Today: s.Today}
	problems, conflicts := ValidateSubmission(s.Submitted, restored, projection, v)
	if len(problems) > 0 {
		return SubmitPlan[D]{}, &DocumentError{Problems: problems}
	}
	flags = append(flags, conflicts...)
	flags = append(flags, contentFlags(restored, s.Base, s.Summary, s.Refs, s.Visibility.HiddenLinkURLs,
		NewNameMatcher(s.HiddenAliases, nil))...)

	return SubmitPlan[D]{Document: restored, Flags: uniqueFlags(flags)}, nil
}

// EntityRef is an entity, by type and ID.
type EntityRef struct {
	Type EntityType
	ID   int64
}

// PendingReference is a pending new term or person a revision references, which is approved alongside it: the
// entity, the revision creating it and that revision's flags.
type PendingReference struct {
	Entity     EntityRef
	RevisionID int64
	Flags      []Flag
}

// ApprovalState is what planning an approval needs, as read under the approval's locks.
type ApprovalState[D Document] struct {
	// ID is the entity's ID.
	ID int64
	// Revision is the revision's full document.
	Revision D
	// Base is the document of the revision's base, nil if it has none (a new entity).
	Base *D
	// Current is the entity's current document, nil for a new entity.
	Current *D
	// BaseRevisionID and CurrentRevisionID are the revision's base and the entity's current revision.
	BaseRevisionID    *int64
	CurrentRevisionID *int64
	// Author is the role the revision was submitted with: a contributor for a pending revision (any API token's
	// too), a moderator for an interactive moderator's submission. The contributor restrictions apply to the first.
	Author Role
	Refs   ReferenceIndex
	// HiddenAliases are the names of every hidden alias, and HiddenLinkURLs the normalized URLs of the hidden links
	// among the document's links.
	HiddenAliases  []string
	HiddenLinkURLs map[string]bool
	Summary        string
	// StoredFlags are the flags stored with the revision at its submission.
	StoredFlags []Flag
	// PendingReferences are the pending new terms and persons the document references, parents first.
	PendingReferences []PendingReference
	// PossibleDuplicate is whether a new entity may duplicate another, and DuplicateBlocked whether it is a new
	// person matching a person who prefers no page, by name or by any alias.
	PossibleDuplicate bool
	DuplicateBlocked  bool
	Today             Date
}

// ApprovalOptions are the moderator's choices in an approval.
type ApprovalOptions struct {
	// AcknowledgedFlags are the flags the moderator acknowledged.
	AcknowledgedFlags []FlagCode
	// Summary, if not nil, is the summary to publish instead of the revision's.
	Summary    *string
	ReviewNote string
}

// ApprovalPlan is what an approval writes.
type ApprovalPlan[D Document] struct {
	// Document is the document to write: the revision's, or its merge with the current document if Rebased.
	Document D
	Rebased  bool
	// Summary is the summary to publish.
	Summary string
	// PendingReferences are the pending entities approved alongside, parents first.
	PendingReferences []EntityRef
	// ConservationChange is the conservation history entry to append, if a piece's status changes; the writer sets
	// its time.
	ConservationChange *ConservationChange
	// TermFacetRebuild is the facet whose closure to rebuild, for a new term or a term given another parent.
	TermFacetRebuild *Facet
}

// submitFlags are the flags only a submission can find, the restore's collisions and the problems only the
// restored document has, which an approval keeps from StoredFlags; the others it finds again in the document it
// approves. A rebase clears them (FlagsAfterRebase).
var submitFlags = map[FlagCode]bool{
	FlagHiddenAliasCollision: true, FlagHiddenLinkCollision: true, FlagHiddenDataConflict: true,
}

// FlagsAfterRebase are the flags a pending revision keeps when a moderator rebases it, of stored, the flags stored
// with it: all but those only a submission can find (submitFlags). The rebased document is the one the
// moderator resolved from their full view, and the rebase validated it against the full current document, so the
// submission's findings no longer describe it. The other flags are found again at approval. It is never nil.
func FlagsAfterRebase(stored []Flag) []Flag {
	kept := make([]Flag, 0, len(stored))
	for _, f := range stored {
		if !submitFlags[f.Code] {
			kept = append(kept, f)
		}
	}

	return kept
}

// PlanApproval plans an approval. A stale revision is merged with the current document, and a
// conflict is ErrRebaseRequired. Then the flags gate it: a hidden-data flag is ErrHiddenDataConflict, a no-page
// duplicate ErrDuplicateBlocked, a flagged pending reference *PendingReferenceFlaggedError, and any flag but
// references_pending that is not acknowledged an *UnacknowledgedFlagsError, as is a hidden_alias_in_summary flag
// without a clean replacement summary and a review note holding a hidden alias. Last, the document is validated
// against the current one, with the author's role for the contributor restrictions; problems are a
// *DocumentError, for the moderator.
func PlanApproval[D Document](s ApprovalState[D], opts ApprovalOptions) (ApprovalPlan[D], error) {
	doc, rebased, err := approvalDocument(s)
	if err != nil {
		return ApprovalPlan[D]{}, err
	}
	matcher := NewNameMatcher(s.HiddenAliases, nil)
	flags := approvalFlags(s, doc, matcher)
	if slices.ContainsFunc(flags, func(f Flag) bool {
		return f.Code == FlagHiddenAliasCollision || f.Code == FlagHiddenDataConflict
	}) {
		return ApprovalPlan[D]{}, ErrHiddenDataConflict
	}
	if s.DuplicateBlocked {
		return ApprovalPlan[D]{}, ErrDuplicateBlocked
	}
	for _, ref := range s.PendingReferences {
		if slices.ContainsFunc(ref.Flags, func(f Flag) bool { return f.Code != FlagReferencesPending }) {
			return ApprovalPlan[D]{}, &PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: ref.Flags}
		}
	}
	summary, err := approvalSummary(flags, s.Summary, opts, matcher)
	if err != nil {
		return ApprovalPlan[D]{}, err
	}
	v := ValidationContext[D]{Author: s.Author, ID: s.ID, Previous: s.Current, Refs: s.Refs, Summary: summary,
		Today: s.Today}
	if problems := validateDocument(doc, v); len(problems) > 0 {
		return ApprovalPlan[D]{}, &DocumentError{Problems: problems}
	}
	plan := ApprovalPlan[D]{Document: doc, Rebased: rebased, Summary: summary}
	for _, ref := range s.PendingReferences {
		plan.PendingReferences = append(plan.PendingReferences, ref.Entity)
	}
	plan.ConservationChange, plan.TermFacetRebuild = documentChanges(doc, s.Current, summary)

	return plan, nil
}

// ApprovalFlags returns the flags approving the revision of s gates on, as PlanApproval finds them: the
// flags only its submission could find, kept from StoredFlags; those of the document approval would write (the
// revision's clean merge with the current document if it is stale, else the revision's own); and
// possible_duplicate. Moderator views show them before the moderator approves.
func ApprovalFlags[D Document](s ApprovalState[D]) []Flag {
	// A merge that conflicts leaves the revision's own document, whose flags are the ones to show.
	doc, _, _ := approvalDocument(s)

	return approvalFlags(s, doc, NewNameMatcher(s.HiddenAliases, nil))
}

// approvalFlags are the flags of approving the revision of s with the document doc, each once.
func approvalFlags[D Document](s ApprovalState[D], doc D, matcher NameMatcher) []Flag {
	flags := slices.DeleteFunc(slices.Clone(s.StoredFlags), func(f Flag) bool { return !submitFlags[f.Code] })
	flags = append(flags, contentFlags(doc, s.Current, s.Summary, s.Refs, s.HiddenLinkURLs, matcher)...)
	if s.PossibleDuplicate {
		flags = append(flags, Flag{Code: FlagPossibleDuplicate})
	}

	return uniqueFlags(flags)
}

// approvalDocument is the document an approval writes: the revision's if it is not stale, or its clean merge with
// the current document, and whether it was merged. A revision is stale when its base is not the current revision.
func approvalDocument[D Document](s ApprovalState[D]) (D, bool, error) {
	if EqualPointers(s.BaseRevisionID, s.CurrentRevisionID) {
		return s.Revision, false, nil
	}
	if s.Base == nil || s.Current == nil {
		return s.Revision, false, ErrRebaseRequired
	}
	merged, conflicts := MergeDocuments(*s.Base, s.Revision, *s.Current)
	if len(conflicts) > 0 {
		return s.Revision, false, ErrRebaseRequired
	}

	return CanonicalDocument(merged), true, nil
}

// approvalSummary checks flags against the moderator's acknowledgements and returns the summary to publish: the
// replacement summary if there is one, which must hold no hidden alias, and the revision's otherwise.
func approvalSummary(flags []Flag, summary string, opts ApprovalOptions, matcher NameMatcher) (string, error) {
	var unacknowledged []Flag
	for _, f := range flags {
		if f.Code != FlagReferencesPending && !slices.Contains(opts.AcknowledgedFlags, f.Code) {
			unacknowledged = append(unacknowledged, f)
		}
	}
	if slices.ContainsFunc(flags, func(f Flag) bool { return f.Code == FlagHiddenAliasInSummary }) &&
		(opts.Summary == nil || len(matcher.Find(*opts.Summary)) > 0) &&
		!slices.ContainsFunc(unacknowledged, func(f Flag) bool { return f.Code == FlagHiddenAliasInSummary }) {
		unacknowledged = append(unacknowledged, Flag{Code: FlagHiddenAliasInSummary, Path: pathSummaryFlag})
	}
	if len(unacknowledged) > 0 {
		return "", &UnacknowledgedFlagsError{Flags: unacknowledged}
	}
	if err := CheckReviewNote(opts.ReviewNote, matcher, opts.AcknowledgedFlags); err != nil {
		return "", err
	}
	if opts.Summary != nil {
		return *opts.Summary, nil
	}

	return summary, nil
}

// pathSummaryFlag is the path of a flag on a revision's summary.
const pathSummaryFlag = "/summary"

// CheckReviewNote checks a moderator's review note, which the revision's author reads: one holding a hidden
// alias is an *UnacknowledgedFlagsError with the hidden_alias_in_review_note flag, unless acknowledged.
func CheckReviewNote(note string, matcher NameMatcher, acknowledged []FlagCode) error {
	if len(matcher.Find(note)) > 0 && !slices.Contains(acknowledged, FlagHiddenAliasInReviewNote) {
		return &UnacknowledgedFlagsError{Flags: []Flag{{Code: FlagHiddenAliasInReviewNote, Path: "/review_note"}}}
	}

	return nil
}

// documentChanges are what approving doc over current changes besides the entity's rows: a piece's conservation
// history, and a term's facet closure.
func documentChanges[D Document](doc D, current *D, summary string) (*ConservationChange, *Facet) {
	switch d := any(doc).(type) {
	case PieceDocument:
		prev := DefaultPieceDocument()
		if current != nil {
			prev = any(*current).(PieceDocument)
		}
		if d.Conservation.Status != prev.Conservation.Status {
			return &ConservationChange{Status: d.Conservation.Status, SourceURL: d.Conservation.SourceURL,
				Reason: summary}, nil
		}
	case TermDocument:
		if current == nil || !EqualPointers(any(*current).(TermDocument).ParentID, d.ParentID) {
			return nil, &d.Facet
		}
	}

	return nil, nil
}

// contentFlags are the flags of a revision's content, its document doc compared with prev (nil for a new entity):
// its summary holding a hidden alias, each string it changed holding one, each hidden link it attaches (as a link
// or as a preference's source) and each pending new entity it references.
func contentFlags[D Document](doc D, prev *D, summary string, refs ReferenceIndex, hiddenLinks map[string]bool,
	matcher NameMatcher,
) []Flag {
	var flags []Flag
	if len(matcher.Find(summary)) > 0 {
		flags = append(flags, Flag{Code: FlagHiddenAliasInSummary, Path: pathSummaryFlag})
	}
	before := map[string]bool{}
	var prevLinks []string
	if prev != nil {
		walkStrings(reflect.ValueOf(withoutHiddenAliases(*prev)), "", documentPaths, func(_ string, v reflect.Value) {
			before[v.String()] = true
		})
		prevLinks = linkURLs(*prev)
	}
	walkStrings(reflect.ValueOf(withoutHiddenAliases(doc)), "", documentPaths, func(path string, v reflect.Value) {
		if _, enum := v.Interface().(validator); !enum && !before[v.String()] && len(matcher.Find(v.String())) > 0 {
			flags = append(flags, Flag{Code: FlagHiddenAliasInText, Path: path})
		}
	})
	attached := elementKeys(linkKey)(prevLinks)
	for i, url := range linkURLs(doc) {
		if key := linkKey(url); hiddenLinks[key] && !slices.Contains(attached, key) {
			flags = append(flags, Flag{Code: FlagHiddenLinkAttached, Path: fmt.Sprintf("/links/%d", i)})
		}
	}
	flags = append(flags, preferenceSourceFlags(doc, prev, hiddenLinks)...)
	for _, r := range documentReferences(doc) {
		if refs.ownPending(r) {
			flags = append(flags, Flag{Code: FlagReferencesPending, Path: r.Path})
		}
	}

	return flags
}

// ownPending is whether the reference r is to a pending new term or person of the revision's author.
func (i ReferenceIndex) ownPending(r DocReference) bool {
	return r.Type == EntityTypePerson && i.Persons[r.ID].OwnPending ||
		r.Type == EntityTypeTerm && i.Terms[r.ID].OwnPending
}

// withoutHiddenAliases is doc without a person's hidden aliases, which are hidden alias names themselves.
func withoutHiddenAliases[D Document](doc D) D {
	if d, ok := any(doc).(PersonDocument); ok {
		d.Aliases = slices.DeleteFunc(slices.Clone(d.Aliases), func(a DocPersonAlias) bool { return !a.IsPublic })

		return any(d).(D)
	}

	return doc
}

// uniqueFlags is flags with each flag once, in order.
func uniqueFlags(flags []Flag) []Flag {
	var out []Flag
	for _, f := range flags {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}

	return out
}

// preferenceSourceFlags are the hidden_link_attached flags of doc's preferences whose source is a hidden link,
// unless prev (nil for a new entity) has the preference of that dimension with that source already.
func preferenceSourceFlags[D Document](doc D, prev *D, hiddenLinks map[string]bool) []Flag {
	before := map[PreferenceDimension]string{}
	if prev != nil {
		for _, p := range documentPreferences(*prev) {
			before[p.Dimension] = linkKey(p.SourceURL)
		}
	}
	var flags []Flag
	for i, p := range documentPreferences(doc) {
		key := linkKey(p.SourceURL)
		if hiddenLinks[key] && before[p.Dimension] != key {
			flags = append(flags, Flag{Code: FlagHiddenLinkAttached, Path: fmt.Sprintf("/preferences/%d/source_url", i)})
		}
	}

	return flags
}

// documentPreferences are a document's preferences: none for a document type without them.
func documentPreferences[D Document](doc D) []DocPreference {
	switch d := any(doc).(type) {
	case PieceDocument:
		return d.Preferences
	case PersonDocument:
		return d.Preferences
	}

	return nil
}
