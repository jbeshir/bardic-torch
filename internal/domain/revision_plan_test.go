package domain

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The revisions of the approval fixtures, by ID.
const (
	importRevisionID  int64 = 100
	currentRevisionID int64 = 101
	termRevision      int64 = 102
	parentRevision    int64 = 103
	aliasSummary            = "Thanks to Wren Placeholder."
	cleanSummary            = "A clean summary."
)

// pieceApproval is the approval of revision, an edit of base (revision importRevisionID), with current at
// currentID; base == current when currentID is importRevisionID.
func pieceApproval(base, revision, current PieceDocument, currentID int64) ApprovalState[PieceDocument] {
	return ApprovalState[PieceDocument]{
		ID: pieceID, Revision: revision, Base: &base, Current: &current,
		BaseRevisionID: ptr(importRevisionID), CurrentRevisionID: ptr(currentID),
		Author: RoleContributor, Refs: testRefs(), HiddenAliases: []string{legalName}, Summary: testSummary,
		Today: validToday,
	}
}

func withNotes(d PieceDocument) PieceDocument {
	return edit(d, func(d *PieceDocument) { d.NotesMD = ptr("Ours.") })
}

func withTitle(d PieceDocument, title string) PieceDocument {
	return edit(d, func(d *PieceDocument) { d.Title = title })
}

// approvalRow is a row of the stale-and-rebase table (plan §6.2) at the plan's level.
type approvalRow struct {
	id    string
	state func() ApprovalState[PieceDocument]
	opts  ApprovalOptions
	err   error
	check func(t *testing.T, plan ApprovalPlan[PieceDocument], err error)
}

func problemsOf(t *testing.T, err error) []DocumentProblem {
	t.Helper()
	var docErr *DocumentError
	require.ErrorAs(t, err, &docErr)

	return docErr.Problems
}

// TestPlanApproval holds every §6.2 row but those the repository alone decides, which its integration tests in
// internal/datasources/mysql hold: a draft base that is not an approved revision
// (TestRepository_SubmitDraft_BaseNotApproved), the entity row gone (TestRepository_ApproveRevision_EntityGone),
// a rebase against a current revision that moved (TestRepository_RebaseRevision_Stale), history order
// (TestRepository_ApproveRevision_HistoryOrder) and a rebase keeping the submitted base
// (TestRepository_RebaseRevision).
func TestPlanApproval(t *testing.T) {
	t.Parallel()

	base := mergeBase()
	notStale := func() ApprovalState[PieceDocument] {
		return pieceApproval(base, withNotes(base), base, importRevisionID)
	}
	staleClean := func() ApprovalState[PieceDocument] {
		return pieceApproval(base, withNotes(base), withTitle(base, theirsTitle), currentRevisionID)
	}
	staleConflict := func() ApprovalState[PieceDocument] {
		return pieceApproval(base, withTitle(base, "Ours"), withTitle(base, theirsTitle), currentRevisionID)
	}
	assertRebased := func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
		t.Helper()
		require.NoError(t, err)
		assert.True(t, plan.Rebased)
		assert.Equal(t, theirsTitle, plan.Document.Title)
		assert.Equal(t, "Ours.", *plan.Document.NotesMD)
	}
	withRefs := func(s ApprovalState[PieceDocument], mutate func(*ReferenceIndex)) ApprovalState[PieceDocument] {
		mutate(&s.Refs)

		return s
	}
	addTerm := func(id int64) func() ApprovalState[PieceDocument] {
		return func() ApprovalState[PieceDocument] {
			rev := edit(base, func(d *PieceDocument) {
				d.Terms = append(slices.Clone(d.Terms), DocPieceTerm{TermID: id})
			})

			return pieceApproval(base, rev, base, importRevisionID)
		}
	}
	pending := func(refs ...PendingReference) func() ApprovalState[PieceDocument] {
		return func() ApprovalState[PieceDocument] {
			s := addTerm(refOwnTerm)()
			s.PendingReferences = refs

			return s
		}
	}
	ownTerm := PendingReference{Entity: EntityRef{Type: EntityTypeTerm, ID: refOwnTerm}, RevisionID: termRevision,
		Flags: []Flag{{Code: FlagReferencesPending, Path: pathTermParent}}}
	parentTerm := PendingReference{Entity: EntityRef{Type: EntityTypeTerm, ID: refOthersTerm}, RevisionID: parentRevision}
	licensingLink := DocPieceLink{DocLink: DocLink{URL: linkNew, Roles: []LinkRole{LinkRoleLicensingSource}}}

	rows := []approvalRow{
		{id: "not_stale", state: notStale, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
			assert.Equal(t, withNotes(base), plan.Document)
		}},
		{id: "disjoint_scalars", state: staleClean, check: assertRebased},
		{id: "conflict", state: staleConflict, err: ErrRebaseRequired},
		{id: "conservation_change", state: func() ApprovalState[PieceDocument] {
			rev := edit(base, func(d *PieceDocument) {
				d.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: ptr(linkA)}
			})

			return pieceApproval(base, rev, base, importRevisionID)
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.Equal(t, &ConservationChange{Status: ConservationStatusEndangered, SourceURL: ptr(linkA),
				Reason: testSummary}, plan.ConservationChange)
		}},
		{id: "no_conservation_change", state: notStale,
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Nil(t, plan.ConservationChange)
			}},
		{id: "import_base_clean", state: staleClean, check: assertRebased},
		{id: "import_base_conflict", state: staleConflict, err: ErrRebaseRequired},
		{id: "import_base_is_current", state: notStale,
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.False(t, plan.Rebased)
			}},
		{id: "new_entity", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Base, s.Current, s.BaseRevisionID, s.CurrentRevisionID = nil, nil, nil, nil

			return s
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
		}},
		{id: "deleted_placeholder_referenced", state: addTerm(unknownID),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				assert.Equal(t, []DocumentProblem{{Path: pathNewTerm, Code: ProblemUnknownReference}}, problemsOf(t, err))
			}},
		{id: "double_approve", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			_, err := CheckTransition(TransitionInput{Role: RoleModerator, From: approved, Action: RevisionActionApprove})
			require.ErrorIs(t, err, ErrNotPending, "the second approval sees the first's status under the lock")
		}},
		{id: "approve_after_withdraw", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			_, err := CheckTransition(TransitionInput{Role: RoleModerator, From: withdrawn, Action: RevisionActionApprove})
			require.ErrorIs(t, err, ErrNotPending)
		}},
		{id: "concurrent_approval_sees_stale", state: staleClean, check: assertRebased},
		{id: "rebase_then_approve", state: func() ApprovalState[PieceDocument] {
			resolved := withTitle(withNotes(base), "Resolved")
			s := pieceApproval(withTitle(base, theirsTitle), resolved, withTitle(base, theirsTitle), currentRevisionID)
			s.BaseRevisionID = ptr(currentRevisionID)

			return s
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
			assert.Equal(t, "Resolved", plan.Document.Title)
		}},
		{id: "moderator_submit_stale_clean", state: func() ApprovalState[PieceDocument] {
			s := staleClean()
			s.Author = RoleModerator

			return s
		}, check: assertRebased},
		{id: "moderator_submit_stale_conflict", state: func() ApprovalState[PieceDocument] {
			s := staleConflict()
			s.Author = RoleModerator

			return s
		}, err: ErrRebaseRequired},
		{id: "term_parent_changed_elsewhere", state: func() ApprovalState[PieceDocument] {
			return withRefs(staleClean(), func(r *ReferenceIndex) {
				r.Terms = withEntries(r.Terms, map[int64]ReferencedTerm{refMood: {ReferencedEntity: public,
					Facet: FacetMood, Name: moodName, ParentID: ptr(refTopic)}})
			})
		}, check: assertRebased},
		{id: "own_pending_term_alongside", state: pending(ownTerm),
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err, "references_pending needs no acknowledgement")
				assert.Equal(t, []EntityRef{ownTerm.Entity}, plan.PendingReferences)
			}},
		{id: "others_pending_term", state: addTerm(refOthersTerm),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				assert.Equal(t, []DocumentProblem{{Path: pathNewTerm, Code: ProblemUnknownReference}}, problemsOf(t, err))
			}},
		{id: "pending_parent_first", state: pending(parentTerm, ownTerm),
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Equal(t, []EntityRef{parentTerm.Entity, ownTerm.Entity}, plan.PendingReferences)
			}},
		{id: "hidden_alias_collision", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.StoredFlags = []Flag{{Code: FlagHiddenAliasCollision, Path: pathSecondAlias}}

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasCollision}}, err: ErrHiddenDataConflict},
		{id: "hidden_data_conflict", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.StoredFlags = []Flag{{Code: FlagHiddenDataConflict, Path: pathSecondAlias + "/name"}}

			return s
		}, err: ErrHiddenDataConflict},
		{id: "summary_flag_without_override", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}},
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var flagsErr *UnacknowledgedFlagsError
				require.ErrorAs(t, err, &flagsErr)
				assert.Equal(t, []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}, flagsErr.Flags)
			}},
		{id: "clean_override", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}, Summary: ptr(cleanSummary)},
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Equal(t, cleanSummary, plan.Summary, "the history shows the override")
			}},
		{id: "override_holds_alias", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}, Summary: ptr("wren_placeholder")},
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var flagsErr *UnacknowledgedFlagsError
				require.ErrorAs(t, err, &flagsErr, "the override is checked too")
			}},
		{id: "new_piece_slug_and_sort_keys", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			assert.Equal(t, "the-wren-s-song", Slugify(validPiece().Title), "the writer numbers a taken slug -2")
			assert.Equal(t, "Wren's Song", SortTitle(validPiece().Title))
			assert.Equal(t, Letter("W"), SortLetter(SortTitle(validPiece().Title)))
		}},
		{id: "clean_merge_fails_validation", state: func() ApprovalState[PieceDocument] {
			withLink := addLink(base, licensingLink)
			rev := edit(withLink, func(d *PieceDocument) { d.LicensingStatus = LicensingStatusKnown })

			return pieceApproval(withLink, rev, base, currentRevisionID)
		}, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			assert.Equal(t, []DocumentProblem{{Path: pathLicensingStatus, Code: ProblemSourceRequired}},
				problemsOf(t, err), "merged cleanly, and not approved")
		}},
		{id: "flagged_pending_reference", state: pending(PendingReference{Entity: ownTerm.Entity,
			RevisionID: termRevision, Flags: []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}}),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var refErr *PendingReferenceFlaggedError
				require.ErrorAs(t, err, &refErr)
				assert.Equal(t, termRevision, refErr.RevisionID)
			}},
		{id: "approved_on_its_own_first", state: pending(), check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err, "once the term is approved it is no pending reference")
		}},
		{id: "pending_event", state: func() ApprovalState[PieceDocument] {
			rev := edit(base, func(d *PieceDocument) {
				d.Links = slices.Clone(d.Links)
				d.Links[0].Performances = []DocPerformance{{EventID: ptr(refPendingEvent), DescriptionVerbatim: "At a feast"}}
			})

			return pieceApproval(base, rev, base, importRevisionID)
		}, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			assert.Equal(t, []DocumentProblem{{Path: pathFirstPerformanceEvent, Code: ProblemUnknownReference}},
				problemsOf(t, err))
		}},
		{id: "unchanged_no_page_credit", state: func() ApprovalState[PieceDocument] {
			credited := edit(base, func(d *PieceDocument) {
				d.Credits = append(slices.Clone(d.Credits), DocCredit{PersonID: ptr(refNoPagePerson),
					Role: CreditRoleTune, Attribution: AttributionNamed, Certainty: CertaintyCertain})
			})
			s := pieceApproval(credited, withNotes(credited), credited, importRevisionID)
			s.Author = RoleModerator

			return s
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.Len(t, plan.Document.Credits, 3, "the credit stays")
		}},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			plan, err := PlanApproval(row.state(), row.opts)
			if row.err != nil {
				require.ErrorIs(t, err, row.err)

				return
			}
			row.check(t, plan, err)
		})
	}
}

// termApproval is the approval of a term document of entity id, whose current document is current.
func termApproval(id int64, revision, current TermDocument) ApprovalState[TermDocument] {
	return ApprovalState[TermDocument]{ID: id, Revision: revision, Base: &current, Current: &current,
		BaseRevisionID: ptr(currentRevisionID), CurrentRevisionID: ptr(currentRevisionID), Author: RoleContributor,
		Refs: testRefs(), Summary: testSummary, Today: validToday}
}

func TestPlanApproval_Terms(t *testing.T) {
	t.Parallel()

	root := TermDocument{SchemaVersion: DocumentSchemaVersion, Facet: FacetMood, Name: moodName, DisplayName: moodName}
	tests := []struct {
		id      string
		state   ApprovalState[TermDocument]
		problem *DocumentProblem
		rebuild bool
	}{
		{id: "cycle", state: termApproval(refMood, termReferencing(refMoodGrandchild, 0), root),
			problem: &DocumentProblem{Path: pathTermParent, Code: ProblemCycle}},
		{id: "too_deep", state: termApproval(termID, termReferencing(refMoodLeaf, 0), validTerm()),
			problem: &DocumentProblem{Path: pathTermParent, Code: ProblemTooDeep}},
		{id: "name_taken_since_submit", state: func() ApprovalState[TermDocument] {
			s := termApproval(0, root, root)
			s.Base, s.Current, s.BaseRevisionID, s.CurrentRevisionID = nil, nil, nil, nil

			return s
		}(), problem: &DocumentProblem{Path: pathName, Code: ProblemDuplicate}},
		{id: "NewParentRebuildsFacet", state: termApproval(termID, termReferencing(refMoodChild, 0), validTerm()),
			rebuild: true},
		{id: "SameParentNoRebuild", state: termApproval(termID, func() TermDocument {
			d := validTerm()
			d.DisplayName = "Wistful Indeed"

			return d
		}(), validTerm())},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			plan, err := PlanApproval(tt.state, ApprovalOptions{})
			if tt.problem != nil {
				assert.Contains(t, problemsOf(t, err), *tt.problem)

				return
			}
			require.NoError(t, err)
			if tt.rebuild {
				assert.Equal(t, ptr(FacetMood), plan.TermFacetRebuild)
			} else {
				assert.Nil(t, plan.TermFacetRebuild)
			}
		})
	}
}

func TestPlanApproval_FlagsGate(t *testing.T) {
	t.Parallel()

	base := mergeBase()
	hidden, err := NormalizeURL(hiddenLinkURL)
	require.NoError(t, err)
	state := func(mutate func(*ApprovalState[PieceDocument])) ApprovalState[PieceDocument] {
		s := pieceApproval(base, withNotes(base), base, importRevisionID)
		mutate(&s)

		return s
	}
	tests := []struct {
		name  string
		state ApprovalState[PieceDocument]
		opts  ApprovalOptions
		flags []Flag
		err   error
	}{
		{name: "HiddenLinkCollisionNeedsAcknowledgement", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = []Flag{{Code: FlagHiddenLinkCollision, Path: pathThirdLink}}
		}), flags: []Flag{{Code: FlagHiddenLinkCollision, Path: pathThirdLink}}},
		{name: "HiddenLinkCollisionAcknowledged", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = []Flag{{Code: FlagHiddenLinkCollision, Path: pathThirdLink}}
		}), opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenLinkCollision}}},
		{name: "HiddenDataConflictRefuses", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = []Flag{{Code: FlagHiddenDataConflict, Path: pathNotes}}
		}), err: ErrHiddenDataConflict},
		{name: "HiddenDataConflictClearedByRebase", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = FlagsAfterRebase([]Flag{{Code: FlagHiddenDataConflict, Path: pathNotes}})
		})},
		{name: "StoredContentFlagsAreFoundAgain", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}
		})},
		{name: "hidden_alias_in_text", state: state(func(s *ApprovalState[PieceDocument]) {
			s.Revision = edit(base, func(d *PieceDocument) { d.NotesMD = ptr("Sung by Wren Placeholder.") })
		}), flags: []Flag{{Code: FlagHiddenAliasInText, Path: pathNotes}}},
		{name: "UnchangedTextNotFlagged", state: state(func(s *ApprovalState[PieceDocument]) {
			noted := edit(base, func(d *PieceDocument) { d.NotesMD = ptr("Sung by Wren Placeholder.") })
			s.Base, s.Current, s.Revision = &noted, &noted, withTitle(noted, "Retitled")
		})},
		{name: "HiddenLinkAttached", state: state(func(s *ApprovalState[PieceDocument]) {
			s.Revision = addLink(base, pieceLink(hiddenLinkURL, "Hidden"))
			s.HiddenLinkURLs = map[string]bool{hidden: true}
		}), flags: []Flag{{Code: FlagHiddenLinkAttached, Path: "/links/3"}}},
		{name: "possible_duplicate", state: state(func(s *ApprovalState[PieceDocument]) {
			s.PossibleDuplicate = true
		}), flags: []Flag{{Code: FlagPossibleDuplicate}}},
		{name: "possible_duplicate_acknowledged", state: state(func(s *ApprovalState[PieceDocument]) {
			s.PossibleDuplicate = true
		}), opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagPossibleDuplicate}}},
		{name: "duplicate_blocked", state: state(func(s *ApprovalState[PieceDocument]) {
			s.DuplicateBlocked = true
		}), opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagPossibleDuplicate}}, err: ErrDuplicateBlocked},
		{name: "moderator_summary", state: state(func(s *ApprovalState[PieceDocument]) {
			s.Author, s.Summary = RoleModerator, aliasSummary
		}), flags: []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}},
		{name: "ReviewNoteHoldsAlias", state: state(func(*ApprovalState[PieceDocument]) {}),
			opts:  ApprovalOptions{ReviewNote: "Ask wren_placeholder first."},
			flags: []Flag{{Code: FlagHiddenAliasInReviewNote, Path: "/review_note"}}},
		{name: "ReviewNoteAcknowledged", state: state(func(*ApprovalState[PieceDocument]) {}),
			opts: ApprovalOptions{ReviewNote: "Ask wren_placeholder first.",
				AcknowledgedFlags: []FlagCode{FlagHiddenAliasInReviewNote}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := PlanApproval(tt.state, tt.opts)
			switch {
			case tt.err != nil:
				require.ErrorIs(t, err, tt.err)
			case tt.flags != nil:
				var flagsErr *UnacknowledgedFlagsError
				require.ErrorAs(t, err, &flagsErr)
				assert.Equal(t, tt.flags, flagsErr.Flags)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckReviewNote(t *testing.T) {
	t.Parallel()

	matcher := NewNameMatcher([]string{legalName}, nil)
	require.NoError(t, CheckReviewNote("Thanks.", matcher, nil))
	var flagsErr *UnacknowledgedFlagsError
	require.ErrorAs(t, CheckReviewNote("WrenPlaceholder said no.", matcher, nil), &flagsErr)
	require.NoError(t, CheckReviewNote("WrenPlaceholder said no.", matcher,
		[]FlagCode{FlagHiddenAliasInReviewNote}))
}

// submitState is a contributor's submission of submitted, edited from base's projection.
func submitState[D Document](t *testing.T, id int64, submitted D, base *D, received ElementKeySet) SubmitState[D] {
	t.Helper()

	return SubmitState[D]{ID: id, Submitted: submitted, Base: base, Received: received,
		Visibility: editorVisibility(t, RoleContributor), Refs: testRefs(), HiddenAliases: []string{legalName},
		Summary: testSummary, Today: validToday}
}

func TestPlanSubmit(t *testing.T) {
	t.Parallel()

	person := fullPerson()
	person.Relations, person.AffiliationIDs = person.Relations[:1], person.AffiliationIDs[:1]
	person.Links = person.Links[:1]
	personEditor, personKeys := EditableDocument(personID, person, editorVisibility(t, RoleContributor))
	submitPerson := func(t *testing.T, mutate func(*PersonDocument)) (SubmitPlan[PersonDocument], error) {
		t.Helper()
		sub := personEditor
		mutate(&sub)

		return PlanSubmit(submitState(t, personID, sub, &person, personKeys))
	}

	t.Run("restored", func(t *testing.T) {
		t.Parallel()
		plan, err := submitPerson(t, func(*PersonDocument) {})
		require.NoError(t, err)
		assert.Equal(t, person, plan.Document)
		assert.Empty(t, plan.Flags)
	})
	t.Run("public_legal_alias", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), DocPersonAlias{Name: otherLegalName,
				Kind: PersonAliasKindLegal, IsPublic: true})
		})
		assert.Equal(t, []DocumentProblem{{Path: "/aliases/2/is_public", Code: ProblemAliasVisibility}}, problemsOf(t, err))
	})
	t.Run("hidden_legal_alias", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), DocPersonAlias{Name: otherLegalName,
				Kind: PersonAliasKindLegal})
		})
		require.NoError(t, err)
	})
	t.Run("summary_holds_hidden_alias", func(t *testing.T) {
		t.Parallel()
		s := submitState(t, personID, personEditor, &person, personKeys)
		s.Summary = aliasSummary
		plan, err := PlanSubmit(s)
		require.NoError(t, err, "the contributor is told nothing")
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}, plan.Flags)
	})
	t.Run("new_person_named_as_hidden_alias", func(t *testing.T) {
		t.Parallel()
		d := validPerson()
		d.Name = legalName
		plan, err := PlanSubmit(submitState(t, 0, d, nil, ElementKeySet{}))
		require.NoError(t, err)
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasInText, Path: pathName}}, plan.Flags)
	})
	t.Run("rename_to_hidden_alias", func(t *testing.T) {
		t.Parallel()
		hiddenPlan, hiddenErr := submitPerson(t, func(d *PersonDocument) { d.Name = legalName })
		freshPlan, freshErr := submitPerson(t, func(d *PersonDocument) { d.Name = "Ash Fresh-Name" })
		require.NoError(t, hiddenErr)
		require.NoError(t, freshErr)
		assert.Contains(t, hiddenPlan.Flags, Flag{Code: FlagHiddenDataConflict, Path: pathSecondAlias + "/name"})
		assert.Empty(t, freshPlan.Flags)
	})
	t.Run("caps_count_visible_elements", func(t *testing.T) {
		t.Parallel()
		many := person
		many.Aliases = []DocPersonAlias{hiddenLegalName, {Name: otherLegalName, Kind: PersonAliasKindLegal}}
		for k := range 98 {
			many.Aliases = append(many.Aliases, DocPersonAlias{Name: fmt.Sprintf("Morwen %03d", k),
				Kind: PersonAliasKindNickname, IsPublic: true})
		}
		editor, keys := EditableDocument(personID, many, editorVisibility(t, RoleContributor))
		editor.Aliases = append(slices.Clone(editor.Aliases), DocPersonAlias{Name: "Morwen Extra",
			Kind: PersonAliasKindNickname, IsPublic: true})
		plan, err := PlanSubmit(submitState(t, personID, editor, &many, keys))
		require.NoError(t, err, "99 visible aliases")
		assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: pathAliases}}, plan.Flags)
	})
	t.Run("editor_paths", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = slices.Clone(d.Aliases)
			d.Aliases[1].Name = ""
		})
		assert.Equal(t, []DocumentProblem{{Path: pathSecondAlias + "/name", Code: ProblemBlank}}, problemsOf(t, err))
	})
	t.Run("hidden_licensing_source", func(t *testing.T) {
		t.Parallel()
		base := addLink(validPiece(), DocPieceLink{DocLink: DocLink{URL: hiddenLinkURL,
			Roles: []LinkRole{LinkRoleLicensingSource}}})
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		editor.LicensingStatus = LicensingStatusKnown
		_, err := PlanSubmit(submitState(t, pieceID, editor, &base, keys))
		assert.Equal(t, []DocumentProblem{{Path: pathLicensingStatus, Code: ProblemSourceRequired}}, problemsOf(t, err))
	})
	t.Run("restored_no_page_credit", func(t *testing.T) {
		t.Parallel()
		base := edit(validPiece(), func(d *PieceDocument) {
			d.Credits = append(slices.Clone(d.Credits), DocCredit{PersonID: ptr(refNoPagePerson),
				Role: CreditRoleTune, Attribution: AttributionNamed, Certainty: CertaintyCertain})
		})
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		plan, err := PlanSubmit(submitState(t, pieceID, withNotes(editor), &base, keys))
		require.NoError(t, err)
		assert.Equal(t, base.Credits, plan.Document.Credits)
		assert.Empty(t, plan.Flags)
	})
	t.Run("EditedPerformanceWithWithheldPerformer", func(t *testing.T) {
		t.Parallel()
		base := edit(validPiece(), func(d *PieceDocument) {
			d.Links = slices.Clone(d.Links)
			d.Links[0].Performances = []DocPerformance{{Year: ptr(int16(2001)), DescriptionVerbatim: "Live",
				PerformerIDs: []int64{refPerson, refNoPagePerson}}}
		})
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		plan, err := PlanSubmit(submitState(t, pieceID, editor, &base, keys))
		require.NoError(t, err)
		assert.Equal(t, base, plan.Document, "unchanged: the withheld performer is restored")
		assert.Empty(t, plan.Flags)
		editor.Links = slices.Clone(editor.Links)
		editor.Links[0].Performances = slices.Clone(editor.Links[0].Performances)
		editor.Links[0].Performances[0].Year = ptr(int16(2002))
		plan, err = PlanSubmit(submitState(t, pieceID, editor, &base, keys))
		require.NoError(t, err, "the contributor is told nothing")
		assert.Equal(t, editor.Links[0].Performances, plan.Document.Links[0].Performances, "kept as submitted")
		assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: pathFirstLinkPerformances}}, plan.Flags)
	})
	t.Run("link_collision_flag", func(t *testing.T) {
		t.Parallel()
		base := addLink(validPiece(), pieceLink(hiddenLinkURL, "Hidden"))
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		plan, err := PlanSubmit(submitState(t, pieceID, addLink(editor, pieceLink(hiddenLinkURL, "Typed")), &base, keys))
		require.NoError(t, err)
		assert.Equal(t, []Flag{{Code: FlagHiddenLinkCollision, Path: "/links/1"}}, plan.Flags)
	})
	t.Run("ReferencesPending", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		editor.Terms = append(slices.Clone(editor.Terms), DocPieceTerm{TermID: refOwnTerm})
		plan, err := PlanSubmit(submitState(t, pieceID, editor, &base, keys))
		require.NoError(t, err)
		assert.Equal(t, []Flag{{Code: FlagReferencesPending, Path: pathNewTerm}}, plan.Flags)
	})
	t.Run("moderator_preferences_restored", func(t *testing.T) {
		t.Parallel()
		base := fullPiece()
		vis := editorVisibility(t, RoleModerator)
		editor, keys := EditableDocument(pieceID, base, vis)
		s := submitState(t, pieceID, editor, &base, keys)
		s.Visibility, s.Summary = vis, aliasSummary
		plan, err := PlanSubmit(s)
		require.NoError(t, err)
		assert.Equal(t, base, plan.Document)
		assert.Empty(t, plan.Flags, "a moderator's flags are PlanApproval's")
	})
}

func TestPlanSubmit_StaleBaseNoMerge(t *testing.T) {
	t.Parallel()

	base := validPiece()
	editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
	plan, err := PlanSubmit(submitState(t, pieceID, withNotes(editor), &base, keys))
	require.NoError(t, err)
	assert.Equal(t, withNotes(base), plan.Document, "planned on the draft's own base, whatever is current")
}

func TestFlagsAfterRebase(t *testing.T) {
	t.Parallel()

	stored := []Flag{
		{Code: FlagHiddenAliasCollision, Path: "/aliases/2"}, {Code: FlagHiddenAliasInText, Path: pathNotes},
		{Code: FlagHiddenLinkCollision, Path: pathThirdLink}, {Code: FlagHiddenDataConflict, Path: pathName},
		{Code: FlagPossibleDuplicate}, {Code: FlagReferencesPending, Path: pathNewTerm},
	}
	kept := FlagsAfterRebase(stored)
	assert.Equal(t, []Flag{{Code: FlagHiddenAliasInText, Path: pathNotes}, {Code: FlagPossibleDuplicate},
		{Code: FlagReferencesPending, Path: pathNewTerm}}, kept, "only the submission's restore flags are cleared")
	assert.Len(t, stored, 6, "stored is not changed")
	assert.NotNil(t, FlagsAfterRebase(nil), "stored as an empty list")
}
