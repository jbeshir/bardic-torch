package command

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSaveDraft_ConvertsAndSaves(t *testing.T) {
	t.Parallel()

	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			resolver := mocks.NewEditorRefResolver(t)
			resolver.EXPECT().ResolveEditorRefs(mock.Anything, pc.actor, testEditorPiece.References()).
				Return(testRefs, nil)
			saver := mocks.NewDraftSaver(t)
			base := int64(41)
			saver.EXPECT().SaveDraft(mock.Anything, datasources.SaveDraftRequest{
				Actor: pc.actor, EntityType: domain.EntityTypePiece, Entity: &testEntityKey, BaseRevisionID: &base,
				Document: testPiece, Summary: testSummaryText, Now: fixedNow,
			}).Return(datasources.SavedDraft{Draft: testExisting}, nil)

			cmd := &SaveDraft{Resolver: resolver, Saver: saver, Duplicates: mocks.NewDuplicateFinder(t), Now: fixedClock}
			got, err := cmd.Execute(t.Context(), SaveDraftRequest{
				Principal: pc.principal, Entity: &testEntityKey, BaseRevisionID: &base, Document: testEditorPiece,
				Summary: testSummaryText,
			})
			require.NoError(t, err)
			assert.Equal(t, SavedDraft{DraftResult: DraftResult{Draft: testExisting, Duplicates: []domain.DuplicateMatch{}}}, got)
		})
	}
}

func TestSaveDraft_NewEntityListsDuplicates(t *testing.T) {
	t.Parallel()

	resolver := mocks.NewEditorRefResolver(t)
	resolver.EXPECT().ResolveEditorRefs(mock.Anything, contributorActor, mock.Anything).Return(testRefs, nil)
	saver := mocks.NewDraftSaver(t)
	saver.EXPECT().SaveDraft(mock.Anything, mock.MatchedBy(func(r datasources.SaveDraftRequest) bool {
		return r.Entity == nil && r.EntityType == domain.EntityTypePiece
	})).Return(datasources.SavedDraft{Draft: testNewDraft, Created: true}, nil)
	finder := mocks.NewDuplicateFinder(t)
	// A contributor's duplicates never compare hidden aliases (D13).
	finder.EXPECT().FindDuplicates(mock.Anything, domain.NewDuplicateCandidate(testPiece), false).
		Return([]domain.DuplicateMatch{testDuplicate}, nil)

	cmd := &SaveDraft{Resolver: resolver, Saver: saver, Duplicates: finder, Now: fixedClock}
	got, err := cmd.Execute(t.Context(), SaveDraftRequest{Principal: contributor, Document: testEditorPiece})
	require.NoError(t, err)
	assert.True(t, got.Created)
	assert.Equal(t, []domain.DuplicateMatch{testDuplicate}, got.Duplicates)
}

func TestSaveDraft_DocumentProblems(t *testing.T) {
	t.Parallel()

	editor := testEditorPiece
	editor.Kind = "ballad"
	editor.Credits = []domain.EditorCredit{{Person: new("no-such-person"), Role: domain.CreditRoleLyrics,
		Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}
	resolver := mocks.NewEditorRefResolver(t)
	resolver.EXPECT().ResolveEditorRefs(mock.Anything, contributorActor, editor.References()).Return(testRefs, nil)

	// The saver is not called.
	cmd := &SaveDraft{Resolver: resolver, Saver: mocks.NewDraftSaver(t), Duplicates: mocks.NewDuplicateFinder(t),
		Now: fixedClock}
	_, err := cmd.Execute(t.Context(), SaveDraftRequest{Principal: contributor, Document: editor})

	var documentErr *domain.DocumentError
	require.ErrorAs(t, err, &documentErr)
	assert.ElementsMatch(t, []domain.DocumentProblem{
		{Path: "/credits/0/person", Code: domain.ProblemUnknownReference},
		{Path: "/kind", Code: domain.ProblemOutOfRange},
	}, documentErr.Problems)
}

func TestSaveDraft_Errors(t *testing.T) {
	t.Parallel()

	resolver := mocks.NewEditorRefResolver(t)
	resolver.EXPECT().ResolveEditorRefs(mock.Anything, contributorActor, mock.Anything).Return(domain.RefMap{}, errDatasource).Once()
	cmd := &SaveDraft{Resolver: resolver, Saver: mocks.NewDraftSaver(t), Duplicates: mocks.NewDuplicateFinder(t),
		Now: fixedClock}
	_, err := cmd.Execute(t.Context(), SaveDraftRequest{Principal: contributor, Document: testEditorPiece})
	require.ErrorIs(t, err, errDatasource)

	for _, want := range []error{datasources.ErrNotFound, datasources.ErrDraftLimitReached, datasources.ErrUnavailable,
		&domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/base_revision_id", Code: domain.ProblemUnknownReference}}}} {
		resolver.EXPECT().ResolveEditorRefs(mock.Anything, contributorActor, mock.Anything).Return(testRefs, nil).Once()
		saver := mocks.NewDraftSaver(t)
		saver.EXPECT().SaveDraft(mock.Anything, mock.Anything).Return(datasources.SavedDraft{}, want)
		cmd.Saver = saver
		_, err := cmd.Execute(t.Context(), SaveDraftRequest{Principal: contributor, Document: testEditorPiece})
		require.ErrorIs(t, err, want)
	}
}

func TestUpdateDraft(t *testing.T) {
	t.Parallel()

	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			resolver := mocks.NewEditorRefResolver(t)
			resolver.EXPECT().ResolveEditorRefs(mock.Anything, pc.actor, testEditorPiece.References()).
				Return(testRefs, nil)
			updater := mocks.NewDraftUpdater(t)
			updater.EXPECT().UpdateDraft(mock.Anything, datasources.UpdateDraftRequest{
				Actor: pc.actor, ID: testDraftID, Document: testPiece, Summary: testSummaryText, Now: fixedNow,
			}).Return(testExisting, nil)

			cmd := &UpdateDraft{Resolver: resolver, Updater: updater, Duplicates: mocks.NewDuplicateFinder(t),
				Now: fixedClock}
			got, err := cmd.Execute(t.Context(), UpdateDraftRequest{
				Principal: pc.principal, ID: testDraftID, Document: testEditorPiece, Summary: testSummaryText,
			})
			require.NoError(t, err)
			assert.Equal(t, DraftResult{Draft: testExisting, Duplicates: []domain.DuplicateMatch{}}, got)
		})
	}

	for _, want := range []error{datasources.ErrNotFound, domain.ErrNotDraft, datasources.ErrDocumentType} {
		resolver := mocks.NewEditorRefResolver(t)
		resolver.EXPECT().ResolveEditorRefs(mock.Anything, contributorActor, mock.Anything).Return(testRefs, nil)
		updater := mocks.NewDraftUpdater(t)
		updater.EXPECT().UpdateDraft(mock.Anything, mock.Anything).Return(domain.Draft{}, want)
		cmd := &UpdateDraft{Resolver: resolver, Updater: updater, Duplicates: mocks.NewDuplicateFinder(t),
			Now: fixedClock}
		_, err := cmd.Execute(t.Context(), UpdateDraftRequest{Principal: contributor, ID: testDraftID,
			Document: testEditorPiece})
		require.ErrorIs(t, err, want)
	}
}

// submitMocks are SubmitRevision's datasources.
type submitMocks struct {
	drafts    *mocks.DraftGetter
	finder    *mocks.DuplicateFinder
	submitter *mocks.RevisionSubmitter
	revisions *mocks.MyRevisionGetter
	cmd       *SubmitRevision
}

func newSubmitMocks(t *testing.T) submitMocks {
	m := submitMocks{drafts: mocks.NewDraftGetter(t), finder: mocks.NewDuplicateFinder(t),
		submitter: mocks.NewRevisionSubmitter(t), revisions: mocks.NewMyRevisionGetter(t)}
	m.cmd = &SubmitRevision{Drafts: m.drafts, Duplicates: m.finder, Submitter: m.submitter, Revisions: m.revisions,
		Now: fixedClock}
	return m
}

func TestSubmitRevision_ActsAsTheEffectiveRole(t *testing.T) {
	t.Parallel()

	acknowledged := []domain.FlagCode{domain.FlagHiddenAliasInText}
	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			m := newSubmitMocks(t)
			m.drafts.EXPECT().GetDraft(mock.Anything, pc.actor, testDraftID).Return(testExisting, nil)
			// Only an interactive moderator's acknowledgements are passed on: an API token's submission is a
			// contributor's, which is pending (T52).
			var wantAcknowledged []domain.FlagCode
			if pc.actor.Role == domain.RoleModerator {
				wantAcknowledged = acknowledged
			}
			m.submitter.EXPECT().SubmitDraft(mock.Anything, datasources.SubmitRequest{
				Actor: pc.actor, DraftID: testDraftID,
				AcknowledgedFlags: wantAcknowledged, Now: fixedNow,
			}).Return(datasources.SubmitResult{RevisionID: testDraftID, Status: domain.RevisionStatusPending}, nil)
			m.revisions.EXPECT().GetMyRevision(mock.Anything, pc.actor, testDraftID).Return(testRevision, nil)

			got, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{
				Principal: pc.principal, DraftID: testDraftID, AcknowledgedFlags: acknowledged,
			})
			require.NoError(t, err)
			assert.Equal(t, SubmittedRevision{Revision: testRevision}, got)
		})
	}
}

func TestSubmitRevision_Replay(t *testing.T) {
	t.Parallel()

	m := newSubmitMocks(t)
	// A draft already submitted is no longer a draft, and has no duplicates to look for.
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(domain.Draft{}, domain.ErrNotDraft)
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
		return assert.Equal(t, datasources.DuplicateCheck{}, r.Duplicates)
	})).Return(datasources.SubmitResult{RevisionID: testDraftID, Status: domain.RevisionStatusPending, Replayed: true},
		nil)
	m.revisions.EXPECT().GetMyRevision(mock.Anything, contributorActor, testDraftID).Return(testRevision, nil)

	got, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.NoError(t, err)
	assert.Equal(t, SubmittedRevision{Revision: testRevision, Replayed: true}, got)
}

func TestSubmitRevision_Duplicates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		principal   domain.Principal
		matches     []domain.DuplicateMatch
		acknowledge bool
		wantErr     error
		wantDupErr  bool
	}{
		{name: "contributor_unacknowledged", principal: contributor, matches: []domain.DuplicateMatch{testDuplicate},
			wantDupErr: true},
		{name: "contributor_acknowledged", principal: contributor, matches: []domain.DuplicateMatch{testDuplicate},
			acknowledge: true},
		{name: "api_token_moderator_unacknowledged", principal: moderatorToken,
			matches: []domain.DuplicateMatch{testDuplicate}, wantDupErr: true},
		// Moderators need no acknowledgement: possible_duplicate is one of their flags (D13).
		{name: "moderator", principal: interactiveMod, matches: []domain.DuplicateMatch{testDuplicate}},
		{name: "blocked", principal: contributor, matches: []domain.DuplicateMatch{testDuplicate, testBlockedDupe},
			acknowledge: true, wantErr: domain.ErrDuplicateBlocked},
		{name: "blocked_moderator", principal: interactiveMod, matches: []domain.DuplicateMatch{testBlockedDupe},
			wantErr: domain.ErrDuplicateBlocked},
		{name: "none", principal: contributor, matches: []domain.DuplicateMatch{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actor := tc.principal.Actor()
			m := newSubmitMocks(t)
			m.drafts.EXPECT().GetDraft(mock.Anything, actor, testDraftID).Return(testNewDraft, nil)
			m.finder.EXPECT().FindDuplicates(mock.Anything, domain.NewDuplicateCandidate(testPiece),
				actor.Role == domain.RoleModerator).Return(tc.matches, nil)
			refused := tc.wantErr != nil || tc.wantDupErr
			if !refused {
				m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
					return assert.Equal(t, datasources.DuplicateCheck{Candidate: domain.NewDuplicateCandidate(testPiece),
						Matches: tc.matches}, r.Duplicates)
				})).Return(datasources.SubmitResult{RevisionID: testDraftID}, nil)
				m.revisions.EXPECT().GetMyRevision(mock.Anything, actor, testDraftID).Return(testRevision, nil)
			}

			_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{
				Principal: tc.principal, DraftID: testDraftID, AcknowledgeDuplicates: tc.acknowledge,
			})
			switch {
			case tc.wantDupErr:
				var dupErr *PossibleDuplicateError
				require.ErrorAs(t, err, &dupErr)
				assert.Equal(t, tc.matches, dupErr.Duplicates)
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			default:
				require.NoError(t, err)
			}
		})
	}
}

// TestSubmitRevision_RechecksAChangedDraft covers the version check of the duplicate check: a submission refused
// because the draft changed since its check is checked again on the new draft, and the new check is used; one whose
// draft keeps changing gives up as unavailable.
func TestSubmitRevision_RechecksAChangedDraft(t *testing.T) {
	t.Parallel()

	renamed := testNewDraft
	renamedPiece := testPiece
	renamedPiece.Title = "The Tin Kettle"
	renamed.Document = renamedPiece
	m := newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testNewDraft, nil).Once()
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(renamed, nil).Once()
	m.finder.EXPECT().FindDuplicates(mock.Anything, domain.NewDuplicateCandidate(testPiece), false).
		Return([]domain.DuplicateMatch{}, nil).Once()
	m.finder.EXPECT().FindDuplicates(mock.Anything, domain.NewDuplicateCandidate(renamedPiece), false).
		Return([]domain.DuplicateMatch{}, nil).Once()
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
		return r.Duplicates.Candidate.Equal(domain.NewDuplicateCandidate(testPiece))
	})).Return(datasources.SubmitResult{}, datasources.ErrDocumentChanged).Once()
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
		return r.Duplicates.Candidate.Equal(domain.NewDuplicateCandidate(renamedPiece))
	})).Return(datasources.SubmitResult{RevisionID: testDraftID}, nil).Once()
	m.revisions.EXPECT().GetMyRevision(mock.Anything, contributorActor, testDraftID).Return(testRevision, nil)

	_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.NoError(t, err)

	m = newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testExisting, nil).
		Times(maxDuplicateRechecks + 1)
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.Anything).
		Return(datasources.SubmitResult{}, datasources.ErrDocumentChanged).Times(maxDuplicateRechecks + 1)
	_, err = m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.ErrorIs(t, err, datasources.ErrUnavailable)
}

func TestSubmitRevision_Errors(t *testing.T) {
	t.Parallel()

	for _, want := range []error{
		datasources.ErrNotFound, domain.ErrNotDraft, datasources.ErrPendingLimitReached, datasources.ErrEntityGone,
		datasources.ErrUnavailable, domain.ErrRebaseRequired, domain.ErrHiddenDataConflict,
		&domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/title", Code: domain.ProblemBlank}}},
		&domain.UnacknowledgedFlagsError{Flags: []domain.Flag{{Code: domain.FlagHiddenAliasInText}}},
		&domain.PendingReferenceFlaggedError{RevisionID: 3},
	} {
		m := newSubmitMocks(t)
		m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testExisting, nil)
		m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.Anything).Return(datasources.SubmitResult{}, want)
		_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
		require.ErrorIs(t, err, want)
	}

	// Another user's draft, or none: the submission is not tried.
	m := newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(domain.Draft{}, datasources.ErrNotFound)
	_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.ErrorIs(t, err, datasources.ErrNotFound)

	// The revision cannot be read back.
	m = newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testExisting, nil)
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.Anything).Return(datasources.SubmitResult{RevisionID: testDraftID}, nil)
	m.revisions.EXPECT().GetMyRevision(mock.Anything, contributorActor, testDraftID).Return(domain.MyRevision{}, errDatasource)
	_, err = m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.ErrorIs(t, err, errDatasource)
}

// TestSubmitRevision_ContributorNeverSeesFlags checks that a contributor's submission returns only their view of
// the revision, which has no flags (D19): the flags it stored are the moderators'.
func TestSubmitRevision_ContributorNeverSeesFlags(t *testing.T) {
	t.Parallel()

	m := newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testExisting, nil)
	// A contributor's acknowledgements are dropped: contributors never see flags, so they cannot acknowledge any.
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
		return r.AcknowledgedFlags == nil
	})).Return(datasources.SubmitResult{RevisionID: testDraftID, Status: domain.RevisionStatusPending}, nil)
	m.revisions.EXPECT().GetMyRevision(mock.Anything, contributorActor, testDraftID).Return(testRevision, nil)

	got, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID,
		AcknowledgedFlags: []domain.FlagCode{domain.FlagHiddenAliasCollision}})
	require.NoError(t, err)
	// SubmittedRevision holds the author's view and the replay mark, and nothing else: no field carries flags.
	assert.Equal(t, SubmittedRevision{Revision: testRevision}, got)
}

func TestWithdrawRevision(t *testing.T) {
	t.Parallel()

	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			withdrawer := mocks.NewRevisionWithdrawer(t)
			withdrawer.EXPECT().WithdrawRevision(mock.Anything, pc.actor, testRevisionID).Return(nil)
			revisions := mocks.NewMyRevisionGetter(t)
			revisions.EXPECT().GetMyRevision(mock.Anything, pc.actor, testRevisionID).Return(testRevision, nil)
			got, err := (&WithdrawRevision{Withdrawer: withdrawer, Revisions: revisions}).Execute(t.Context(),
				RevisionRequest{Principal: pc.principal, ID: testRevisionID})
			require.NoError(t, err)
			assert.Equal(t, testRevision, got)
		})
	}

	for _, want := range []error{datasources.ErrNotFound, domain.ErrNotPending, datasources.ErrUnavailable} {
		withdrawer := mocks.NewRevisionWithdrawer(t)
		withdrawer.EXPECT().WithdrawRevision(mock.Anything, contributorActor, testRevisionID).Return(want)
		_, err := (&WithdrawRevision{Withdrawer: withdrawer, Revisions: mocks.NewMyRevisionGetter(t)}).Execute(
			t.Context(), RevisionRequest{Principal: contributor, ID: testRevisionID})
		require.ErrorIs(t, err, want)
	}
}

func TestRevertTo(t *testing.T) {
	t.Parallel()

	acknowledged := []domain.FlagCode{domain.FlagHiddenAliasInSummary}
	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			var wantAcknowledged []domain.FlagCode
			if pc.actor.Role == domain.RoleModerator {
				wantAcknowledged = acknowledged
			}
			reverter := mocks.NewRevisionReverter(t)
			reverter.EXPECT().RevertTo(mock.Anything, datasources.RevertRequest{
				Actor: pc.actor, RevisionID: testRevisionID, Summary: testSummaryText,
				AcknowledgedFlags: wantAcknowledged, Now: fixedNow,
			}).Return(datasources.SubmitResult{RevisionID: testRevisionID + 1, Replayed: true}, nil)
			revisions := mocks.NewMyRevisionGetter(t)
			revisions.EXPECT().GetMyRevision(mock.Anything, pc.actor, testRevisionID+1).Return(testRevision, nil)

			got, err := (&RevertTo{Reverter: reverter, Revisions: revisions, Now: fixedClock}).Execute(t.Context(),
				RevertRequest{Principal: pc.principal, RevisionID: testRevisionID, Summary: testSummaryText,
					AcknowledgedFlags: acknowledged})
			require.NoError(t, err)
			assert.Equal(t, SubmittedRevision{Revision: testRevision, Replayed: true}, got)
		})
	}

	for _, want := range []error{datasources.ErrNotFound, datasources.ErrAlreadyCurrent,
		datasources.ErrPendingLimitReached, datasources.ErrUnavailable, &domain.DocumentError{}} {
		reverter := mocks.NewRevisionReverter(t)
		reverter.EXPECT().RevertTo(mock.Anything, mock.Anything).Return(datasources.SubmitResult{}, want)
		_, err := (&RevertTo{Reverter: reverter, Revisions: mocks.NewMyRevisionGetter(t), Now: fixedClock}).Execute(
			t.Context(), RevertRequest{Principal: contributor, RevisionID: testRevisionID})
		require.ErrorIs(t, err, want)
	}
}

// termNameTaken is the error an approval's write gives a term whose name another term of its facet has taken.
var termNameTaken = &domain.DocumentError{Problems: []domain.DocumentProblem{
	{Path: "/name", Code: domain.ProblemDuplicate},
}}

// TestModeratorWrites_TermNameTakenIsDuplicateName checks that an interactive moderator's own submission and revert,
// which are approved at once, refuse a term whose name is taken with ErrDuplicateName, as approving does.
func TestModeratorWrites_TermNameTakenIsDuplicateName(t *testing.T) {
	t.Parallel()

	t.Run("submit", func(t *testing.T) {
		t.Parallel()
		m := newSubmitMocks(t)
		m.drafts.EXPECT().GetDraft(mock.Anything, interactiveModActor, testDraftID).Return(testExisting, nil)
		m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.Anything).Return(datasources.SubmitResult{}, termNameTaken)
		_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: interactiveMod, DraftID: testDraftID})
		require.ErrorIs(t, err, ErrDuplicateName)
	})

	t.Run("revert", func(t *testing.T) {
		t.Parallel()
		reverter := mocks.NewRevisionReverter(t)
		reverter.EXPECT().RevertTo(mock.Anything, mock.Anything).Return(datasources.SubmitResult{}, termNameTaken)
		_, err := (&RevertTo{Reverter: reverter, Revisions: mocks.NewMyRevisionGetter(t), Now: fixedClock}).Execute(
			t.Context(), RevertRequest{Principal: interactiveMod, RevisionID: testRevisionID})
		require.ErrorIs(t, err, ErrDuplicateName)
	})
}

// TestSubmitRevision_ContributorTermNameTakenIsInvalidDocument checks that a contributor's submission, which is not
// approved, keeps a taken term name as a document problem (422), not ErrDuplicateName.
func TestSubmitRevision_ContributorTermNameTakenIsInvalidDocument(t *testing.T) {
	t.Parallel()

	m := newSubmitMocks(t)
	m.drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testExisting, nil)
	m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.Anything).Return(datasources.SubmitResult{}, termNameTaken)
	_, err := m.cmd.Execute(t.Context(), SubmitRevisionRequest{Principal: contributor, DraftID: testDraftID})
	require.ErrorIs(t, err, termNameTaken)
	require.NotErrorIs(t, err, ErrDuplicateName)
}
