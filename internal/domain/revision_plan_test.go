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
// internal/datasources/mysql hold: R19 (a draft base that is not an approved revision,
// TestRepository_SubmitDraft_BaseNotApproved), R22 (the entity row gone, TestRepository_ApproveRevision_EntityGone),
// R26 (a rebase against a current revision that moved, TestRepository_RebaseRevision_Stale), R46 (history order,
// TestRepository_ApproveRevision_HistoryOrder) and R49 (a rebase keeps the submitted base,
// TestRepository_RebaseRevision).
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
		{id: "R01_NotStale", state: notStale, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
			assert.Equal(t, withNotes(base), plan.Document)
		}},
		{id: "R02_DisjointScalars", state: staleClean, check: assertRebased},
		{id: "R03_Conflict", state: staleConflict, err: ErrRebaseRequired},
		{id: "R06_ConservationChange", state: func() ApprovalState[PieceDocument] {
			rev := edit(base, func(d *PieceDocument) {
				d.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: ptr(linkA)}
			})

			return pieceApproval(base, rev, base, importRevisionID)
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.Equal(t, &ConservationChange{Status: ConservationStatusEndangered, SourceURL: ptr(linkA),
				Reason: testSummary}, plan.ConservationChange)
		}},
		{id: "R06b_NoConservationChange", state: notStale,
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Nil(t, plan.ConservationChange)
			}},
		{id: "R17_ImportBaseClean", state: staleClean, check: assertRebased},
		{id: "R17_ImportBaseConflict", state: staleConflict, err: ErrRebaseRequired},
		{id: "R18_ImportBaseIsCurrent", state: notStale,
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.False(t, plan.Rebased)
			}},
		{id: "R20_NewEntity", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Base, s.Current, s.BaseRevisionID, s.CurrentRevisionID = nil, nil, nil, nil

			return s
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
		}},
		{id: "R21_DeletedPlaceholderReferenced", state: addTerm(unknownID),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				assert.Equal(t, []DocumentProblem{{Path: pathNewTerm, Code: ProblemUnknownReference}}, problemsOf(t, err))
			}},
		{id: "R23_DoubleApprove", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			_, err := CheckTransition(TransitionInput{Role: RoleModerator, From: approved, Action: RevisionActionApprove})
			require.ErrorIs(t, err, ErrNotPending, "the second approval sees the first's status under the lock")
		}},
		{id: "R24_ApproveAfterWithdraw", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			_, err := CheckTransition(TransitionInput{Role: RoleModerator, From: withdrawn, Action: RevisionActionApprove})
			require.ErrorIs(t, err, ErrNotPending)
		}},
		{id: "R25_ConcurrentApprovalSeesStale", state: staleClean, check: assertRebased},
		{id: "R27_RebaseThenApprove", state: func() ApprovalState[PieceDocument] {
			resolved := withTitle(withNotes(base), "Resolved")
			s := pieceApproval(withTitle(base, theirsTitle), resolved, withTitle(base, theirsTitle), currentRevisionID)
			s.BaseRevisionID = ptr(currentRevisionID)

			return s
		}, check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err)
			assert.False(t, plan.Rebased)
			assert.Equal(t, "Resolved", plan.Document.Title)
		}},
		{id: "R28_ModeratorSubmitStaleClean", state: func() ApprovalState[PieceDocument] {
			s := staleClean()
			s.Author = RoleModerator

			return s
		}, check: assertRebased},
		{id: "R29_ModeratorSubmitStaleConflict", state: func() ApprovalState[PieceDocument] {
			s := staleConflict()
			s.Author = RoleModerator

			return s
		}, err: ErrRebaseRequired},
		{id: "R33_TermParentChangedElsewhere", state: func() ApprovalState[PieceDocument] {
			return withRefs(staleClean(), func(r *ReferenceIndex) {
				r.Terms = withEntries(r.Terms, map[int64]ReferencedTerm{refMood: {ReferencedEntity: public,
					Facet: FacetMood, Name: moodName, ParentID: ptr(refTopic)}})
			})
		}, check: assertRebased},
		{id: "R34_OwnPendingTermAlongside", state: pending(ownTerm),
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err, "references_pending needs no acknowledgement")
				assert.Equal(t, []EntityRef{ownTerm.Entity}, plan.PendingReferences)
			}},
		{id: "R35_OthersPendingTerm", state: addTerm(refOthersTerm),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				assert.Equal(t, []DocumentProblem{{Path: pathNewTerm, Code: ProblemUnknownReference}}, problemsOf(t, err))
			}},
		{id: "R36_PendingParentFirst", state: pending(parentTerm, ownTerm),
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Equal(t, []EntityRef{parentTerm.Entity, ownTerm.Entity}, plan.PendingReferences)
			}},
		{id: "R41_HiddenAliasCollision", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.StoredFlags = []Flag{{Code: FlagHiddenAliasCollision, Path: pathSecondAlias}}

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasCollision}}, err: ErrHiddenDataConflict},
		{id: "R41b_HiddenDataConflict", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.StoredFlags = []Flag{{Code: FlagHiddenDataConflict, Path: pathSecondAlias + "/name"}}

			return s
		}, err: ErrHiddenDataConflict},
		{id: "R42_SummaryFlagWithoutOverride", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}},
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var flagsErr *UnacknowledgedFlagsError
				require.ErrorAs(t, err, &flagsErr)
				assert.Equal(t, []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}, flagsErr.Flags)
			}},
		{id: "R43_CleanOverride", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}, Summary: ptr(cleanSummary)},
			check: func(t *testing.T, plan ApprovalPlan[PieceDocument], err error) {
				require.NoError(t, err)
				assert.Equal(t, cleanSummary, plan.Summary, "the history shows the override")
			}},
		{id: "R43b_OverrideHoldsAlias", state: func() ApprovalState[PieceDocument] {
			s := notStale()
			s.Summary = aliasSummary

			return s
		}, opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagHiddenAliasInSummary}, Summary: ptr("wren_placeholder")},
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var flagsErr *UnacknowledgedFlagsError
				require.ErrorAs(t, err, &flagsErr, "the override is checked too")
			}},
		{id: "R44_NewPieceSlugAndSortKeys", state: notStale, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], _ error) {
			assert.Equal(t, "the-wren-s-song", Slugify(validPiece().Title), "the writer numbers a taken slug -2")
			assert.Equal(t, "Wren's Song", SortTitle(validPiece().Title))
			assert.Equal(t, Letter("W"), SortLetter(SortTitle(validPiece().Title)))
		}},
		{id: "R47_CleanMergeFailsValidation", state: func() ApprovalState[PieceDocument] {
			withLink := addLink(base, licensingLink)
			rev := edit(withLink, func(d *PieceDocument) { d.LicensingStatus = LicensingStatusKnown })

			return pieceApproval(withLink, rev, base, currentRevisionID)
		}, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			assert.Equal(t, []DocumentProblem{{Path: pathLicensingStatus, Code: ProblemSourceRequired}},
				problemsOf(t, err), "SF15: merged cleanly, and not approved")
		}},
		{id: "R50_FlaggedPendingReference", state: pending(PendingReference{Entity: ownTerm.Entity,
			RevisionID: termRevision, Flags: []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}}),
			check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
				var refErr *PendingReferenceFlaggedError
				require.ErrorAs(t, err, &refErr)
				assert.Equal(t, termRevision, refErr.RevisionID)
			}},
		{id: "R50b_ApprovedOnItsOwnFirst", state: pending(), check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			require.NoError(t, err, "once the term is approved it is no pending reference")
		}},
		{id: "R51_PendingEvent", state: func() ApprovalState[PieceDocument] {
			rev := edit(base, func(d *PieceDocument) {
				d.Links = slices.Clone(d.Links)
				d.Links[0].Performances = []DocPerformance{{EventID: ptr(refPendingEvent), DescriptionVerbatim: "At a feast"}}
			})

			return pieceApproval(base, rev, base, importRevisionID)
		}, check: func(t *testing.T, _ ApprovalPlan[PieceDocument], err error) {
			assert.Equal(t, []DocumentProblem{{Path: pathFirstPerformanceEvent, Code: ProblemUnknownReference}},
				problemsOf(t, err))
		}},
		{id: "R52_UnchangedNoPageCredit", state: func() ApprovalState[PieceDocument] {
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
		{id: "R39_Cycle", state: termApproval(refMood, termReferencing(refMoodGrandchild, 0), root),
			problem: &DocumentProblem{Path: pathTermParent, Code: ProblemCycle}},
		{id: "R40_TooDeep", state: termApproval(termID, termReferencing(refMoodLeaf, 0), validTerm()),
			problem: &DocumentProblem{Path: pathTermParent, Code: ProblemTooDeep}},
		{id: "R45_NameTakenSinceSubmit", state: func() ApprovalState[TermDocument] {
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
		{name: "StoredContentFlagsAreFoundAgain", state: state(func(s *ApprovalState[PieceDocument]) {
			s.StoredFlags = []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}
		})},
		{name: "W24_HiddenAliasInText", state: state(func(s *ApprovalState[PieceDocument]) {
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
		{name: "W26_PossibleDuplicate", state: state(func(s *ApprovalState[PieceDocument]) {
			s.PossibleDuplicate = true
		}), flags: []Flag{{Code: FlagPossibleDuplicate}}},
		{name: "W26_PossibleDuplicateAcknowledged", state: state(func(s *ApprovalState[PieceDocument]) {
			s.PossibleDuplicate = true
		}), opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagPossibleDuplicate}}},
		{name: "W25_DuplicateBlocked", state: state(func(s *ApprovalState[PieceDocument]) {
			s.DuplicateBlocked = true
		}), opts: ApprovalOptions{AcknowledgedFlags: []FlagCode{FlagPossibleDuplicate}}, err: ErrDuplicateBlocked},
		{name: "W23_ModeratorSummary", state: state(func(s *ApprovalState[PieceDocument]) {
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

	t.Run("W03_Restored", func(t *testing.T) {
		t.Parallel()
		plan, err := submitPerson(t, func(*PersonDocument) {})
		require.NoError(t, err)
		assert.Equal(t, person, plan.Document)
		assert.Empty(t, plan.Flags)
	})
	t.Run("W06_PublicLegalAlias", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), DocPersonAlias{Name: otherLegalName,
				Kind: PersonAliasKindLegal, IsPublic: true})
		})
		assert.Equal(t, []DocumentProblem{{Path: "/aliases/2/is_public", Code: ProblemAliasVisibility}}, problemsOf(t, err))
	})
	t.Run("W07_HiddenLegalAlias", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = append(slices.Clone(d.Aliases), DocPersonAlias{Name: otherLegalName,
				Kind: PersonAliasKindLegal})
		})
		require.NoError(t, err)
	})
	t.Run("W22_SummaryHoldsHiddenAlias", func(t *testing.T) {
		t.Parallel()
		s := submitState(t, personID, personEditor, &person, personKeys)
		s.Summary = aliasSummary
		plan, err := PlanSubmit(s)
		require.NoError(t, err, "the contributor is told nothing")
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasInSummary, Path: pathSummary}}, plan.Flags)
	})
	t.Run("W24_NewPersonNamedAsHiddenAlias", func(t *testing.T) {
		t.Parallel()
		d := validPerson()
		d.Name = legalName
		plan, err := PlanSubmit(submitState(t, 0, d, nil, ElementKeySet{}))
		require.NoError(t, err)
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasInText, Path: pathName}}, plan.Flags)
	})
	t.Run("W27_RenameToHiddenAlias", func(t *testing.T) {
		t.Parallel()
		hiddenPlan, hiddenErr := submitPerson(t, func(d *PersonDocument) { d.Name = legalName })
		freshPlan, freshErr := submitPerson(t, func(d *PersonDocument) { d.Name = "Ash Fresh-Name" })
		require.NoError(t, hiddenErr)
		require.NoError(t, freshErr)
		assert.Contains(t, hiddenPlan.Flags, Flag{Code: FlagHiddenDataConflict, Path: pathSecondAlias + "/name"})
		assert.Empty(t, freshPlan.Flags)
	})
	t.Run("W28_CapsCountVisibleElements", func(t *testing.T) {
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
	t.Run("W30_EditorPaths", func(t *testing.T) {
		t.Parallel()
		_, err := submitPerson(t, func(d *PersonDocument) {
			d.Aliases = slices.Clone(d.Aliases)
			d.Aliases[1].Name = ""
		})
		assert.Equal(t, []DocumentProblem{{Path: pathSecondAlias + "/name", Code: ProblemBlank}}, problemsOf(t, err))
	})
	t.Run("W29_HiddenLicensingSource", func(t *testing.T) {
		t.Parallel()
		base := addLink(validPiece(), DocPieceLink{DocLink: DocLink{URL: hiddenLinkURL,
			Roles: []LinkRole{LinkRoleLicensingSource}}})
		editor, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
		editor.LicensingStatus = LicensingStatusKnown
		_, err := PlanSubmit(submitState(t, pieceID, editor, &base, keys))
		assert.Equal(t, []DocumentProblem{{Path: pathLicensingStatus, Code: ProblemSourceRequired}}, problemsOf(t, err))
	})
	t.Run("W31_RestoredNoPageCredit", func(t *testing.T) {
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
	t.Run("W10_LinkCollisionFlag", func(t *testing.T) {
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
	t.Run("W37_ModeratorPreferencesRestored", func(t *testing.T) {
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
	assert.Equal(t, withNotes(base), plan.Document, "R48: planned on the draft's own base, whatever is current")
}
