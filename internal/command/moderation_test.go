package command

import (
	"context"
	"fmt"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testLinkURL  = "https://example.org/the-copper-kettle"
	testLinkID   = int64(7)
	testReportID = int64(3)
)

var (
	testModerationRevision = domain.ModerationRevision{
		ID: testRevisionID, Status: domain.RevisionStatusPending, EntityType: domain.EntityTypePiece,
		Entity: &testEntity, Document: testPiece, Duplicates: []domain.DuplicateMatch{testDuplicate},
	}
	testApproved = domain.ModerationRevision{ID: testRevisionID, Status: domain.RevisionStatusApproved}
	testLink     = domain.ModerationLink{LinkID: testLinkID, URL: testLinkURL}
	testReport   = domain.LinkReport{ID: testReportID, Link: testLink, Status: domain.LinkReportStatusResolved}
)

// TestModeratorCommands_RequireAnInteractiveModerator checks that every moderator command refuses a contributor
// and a moderator's API token before it reads or writes anything: the mocks fail the test if they are
// called.
func TestModeratorCommands_RequireAnInteractiveModerator(t *testing.T) {
	t.Parallel()

	for _, p := range []struct {
		name      string
		principal domain.Principal
	}{{"contributor", contributor}, {"moderator_api_token", moderatorToken}} {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			run := map[string]func() error{
				"get": func() error {
					_, err := (&GetModerationRevision{Getter: mocks.NewModerationRevisionGetter(t)}).Execute(
						t.Context(), RevisionRequest{Principal: p.principal, ID: testRevisionID})
					return err
				},
				"queue": func() error {
					_, err := (&ListModerationQueue{Lister: mocks.NewModerationQueueLister(t), Now: fixedClock}).
						Execute(t.Context(), ModerationQueueRequest{Principal: p.principal})
					return err
				},
				"approve": func() error {
					_, err := (&ApproveRevision{Revisions: mocks.NewModerationRevisionGetter(t),
						Approver: mocks.NewRevisionApprover(t), Now: fixedClock}).Execute(t.Context(),
						ApproveRevisionRequest{Principal: p.principal, RevisionID: testRevisionID})
					return err
				},
				"reject": func() error {
					_, err := (&RejectRevision{Revisions: mocks.NewModerationRevisionGetter(t),
						Rejecter: mocks.NewRevisionRejecter(t), Now: fixedClock}).Execute(t.Context(),
						RejectRevisionRequest{Principal: p.principal, RevisionID: testRevisionID})
					return err
				},
				"rebase": func() error {
					_, err := (&RebaseRevision{Resolver: mocks.NewEditorRefResolver(t),
						Revisions: mocks.NewModerationRevisionGetter(t), Rebaser: mocks.NewRevisionRebaser(t),
						Now: fixedClock}).Execute(t.Context(), RebaseRevisionRequest{Principal: p.principal,
						RevisionID: testRevisionID, Document: testEditorPiece})
					return err
				},
				"list_reports": func() error {
					_, err := (&ListLinkReports{Lister: mocks.NewLinkReportLister(t)}).Execute(t.Context(),
						LinkReportListRequest{Principal: p.principal})
					return err
				},
				"resolve_report": func() error {
					_, err := (&ResolveLinkReport{Resolver: mocks.NewLinkReportResolver(t), Now: fixedClock}).
						Execute(t.Context(), ResolveLinkReportRequest{Principal: p.principal, ReportID: testReportID})
					return err
				},
				"hide_link": func() error {
					_, err := (&SetLinkHidden{Setter: mocks.NewLinkHiddenSetter(t), Now: fixedClock}).Execute(
						t.Context(), SetLinkHiddenRequest{Principal: p.principal, LinkID: testLinkID, Hidden: true})
					return err
				},
				"get_link": func() error {
					_, err := (&GetModerationLink{Getter: mocks.NewModerationLinkGetter(t)}).Execute(t.Context(),
						ModerationLinkRequest{Principal: p.principal, URL: testLinkURL})
					return err
				},
				"list_contact_requests": func() error {
					_, err := (&ListContactRequests{Purger: mocks.NewContactRequestPurger(t),
						Lister: mocks.NewContactRequestLister(t), Now: fixedClock}).Execute(t.Context(),
						ContactRequestListRequest{Principal: p.principal})
					return err
				},
				"resolve_contact_request": func() error {
					_, err := (&ResolveContactRequest{Resolver: mocks.NewContactRequestResolver(t),
						Purger: mocks.NewContactRequestPurger(t), Now: fixedClock}).Execute(t.Context(),
						ResolveContactRequestRequest{Principal: p.principal, RequestID: testContactRequestID})
					return err
				},
			}
			for name, f := range run {
				assert.ErrorIs(t, f(), domain.ErrModeratorRequired, name)
			}
		})
	}
}

func TestGetModerationRevision(t *testing.T) {
	t.Parallel()

	getter := mocks.NewModerationRevisionGetter(t)
	getter.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testModerationRevision, nil).Once()
	got, err := (&GetModerationRevision{Getter: getter}).Execute(t.Context(),
		RevisionRequest{Principal: interactiveMod, ID: testRevisionID})
	require.NoError(t, err)
	assert.Equal(t, testModerationRevision, got)

	getter.EXPECT().GetModerationRevision(mock.Anything, testRevisionID+1).Return(domain.ModerationRevision{},
		datasources.ErrNotFound)
	_, err = (&GetModerationRevision{Getter: getter}).Execute(t.Context(),
		RevisionRequest{Principal: interactiveMod, ID: testRevisionID + 1})
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestListModerationQueue_MeasuresAgesToNow(t *testing.T) {
	t.Parallel()

	query := datasources.ModerationQueueQuery{MinAgeHours: new(int32(24)), Page: domain.Page{Limit: 10}}
	want := query
	want.Now = fixedNow
	queue := domain.ModerationQueue{Items: []domain.QueueItem{{ID: testRevisionID}}, Total: 1}
	lister := mocks.NewModerationQueueLister(t)
	lister.EXPECT().ListModerationQueue(mock.Anything, want).Return(queue, nil).Once()
	cmd := &ListModerationQueue{Lister: lister, Now: fixedClock}

	got, err := cmd.Execute(t.Context(), ModerationQueueRequest{Principal: interactiveMod, Query: query})
	require.NoError(t, err)
	assert.Equal(t, queue, got)

	lister.EXPECT().ListModerationQueue(mock.Anything, want).Return(domain.ModerationQueue{}, errDatasource)
	_, err = cmd.Execute(t.Context(), ModerationQueueRequest{Principal: interactiveMod, Query: query})
	require.ErrorIs(t, err, errDatasource)
}

// newPieceWithReference is the moderation view of a pending new piece whose author's pending new person, named
// personName, it credits.
func newPieceWithReference(personName string) domain.ModerationRevision {
	revision := testModerationRevision
	revision.Entity = &domain.RevisionEntity{Type: domain.EntityTypePiece, ID: 7, Slug: "pending-piece-7", IsNew: true}
	revision.PendingReferences = []domain.ModerationPendingReference{{
		RevisionID: testReferenceRevisionID, EntityType: domain.EntityTypePerson, Entity: testReferenceEntity,
		Document: domain.PersonDocument{Name: personName},
	}}

	return revision
}

const testReferenceRevisionID int64 = 41

var testReferenceEntity = domain.RevisionEntity{Type: domain.EntityTypePerson, ID: 9, Slug: "pending-person-9",
	IsNew: true}

func TestApproveRevision_PassesTheDuplicateChecks(t *testing.T) {
	t.Parallel()

	options := domain.ApprovalOptions{AcknowledgedFlags: []domain.FlagCode{domain.FlagPossibleDuplicate},
		ReviewNote: "Thanks."}
	revision := newPieceWithReference("Ash Example")
	reference := domain.NewDuplicateCandidate(revision.PendingReferences[0].Document)
	public := domain.DuplicateMatch{EntityType: domain.EntityTypePerson, Slug: "ash-example", Name: "Ash Example"}
	itself := domain.DuplicateMatch{EntityType: domain.EntityTypePerson, Slug: testReferenceEntity.Slug,
		Name: "Ash Example"}
	revisions := mocks.NewModerationRevisionGetter(t)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(revision, nil).Once()
	finder := mocks.NewDuplicateFinder(t)
	// A pending reference is checked as moderators check it, its own placeholder left out.
	finder.EXPECT().FindDuplicates(mock.Anything, reference, true).Return([]domain.DuplicateMatch{itself, public}, nil)
	approver := mocks.NewRevisionApprover(t)
	approver.EXPECT().ApproveRevision(mock.Anything, datasources.ApproveRequest{
		Actor: interactiveModActor, RevisionID: testRevisionID, Options: options,
		Duplicates: map[int64]datasources.DuplicateCheck{
			testRevisionID: {Candidate: domain.NewDuplicateCandidate(testPiece),
				Matches: []domain.DuplicateMatch{testDuplicate}},
			testReferenceRevisionID: {Candidate: reference, Matches: []domain.DuplicateMatch{public}},
		},
		Now: fixedNow,
	}).Return(nil)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testApproved, nil).Once()

	cmd := &ApproveRevision{Revisions: revisions, Duplicates: finder, Approver: approver, Now: fixedClock}
	got, err := cmd.Execute(t.Context(), ApproveRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID,
		Options: options})
	require.NoError(t, err)
	assert.Equal(t, testApproved, got)
}

// TestApproveRevision_RechecksAChangedDocument covers the version check of the duplicate check: an approval refused
// because a document changed since its check is checked again on the new documents, and the new checks are used.
func TestApproveRevision_RechecksAChangedDocument(t *testing.T) {
	t.Parallel()

	before, after := newPieceWithReference("Ash Example"), newPieceWithReference("Ash Renamed")
	revisions := mocks.NewModerationRevisionGetter(t)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(before, nil).Once()
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(after, nil).Once()
	finder := mocks.NewDuplicateFinder(t)
	finder.EXPECT().FindDuplicates(mock.Anything, mock.Anything, true).Return([]domain.DuplicateMatch{}, nil).Twice()
	approver := mocks.NewRevisionApprover(t)
	var checked []domain.DuplicateCandidate
	approver.EXPECT().ApproveRevision(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req datasources.ApproveRequest) error {
			checked = append(checked, req.Duplicates[testReferenceRevisionID].Candidate)
			if len(checked) == 1 {
				return fmt.Errorf("approving: %w", datasources.ErrDocumentChanged)
			}
			return nil
		}).Twice()
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testApproved, nil).Once()

	cmd := &ApproveRevision{Revisions: revisions, Duplicates: finder, Approver: approver, Now: fixedClock}
	_, err := cmd.Execute(t.Context(), ApproveRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID})
	require.NoError(t, err)
	assert.Equal(t, []domain.DuplicateCandidate{
		domain.NewDuplicateCandidate(before.PendingReferences[0].Document),
		domain.NewDuplicateCandidate(after.PendingReferences[0].Document),
	}, checked)
}

// TestApproveRevision_DocumentKeepsChanging requires an approval whose document changes at every try to give up
// as unavailable after the rechecks.
func TestApproveRevision_DocumentKeepsChanging(t *testing.T) {
	t.Parallel()

	revisions := mocks.NewModerationRevisionGetter(t)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testModerationRevision, nil).
		Times(maxDuplicateRechecks + 1)
	approver := mocks.NewRevisionApprover(t)
	approver.EXPECT().ApproveRevision(mock.Anything, mock.Anything).Return(datasources.ErrDocumentChanged).
		Times(maxDuplicateRechecks + 1)

	cmd := &ApproveRevision{Revisions: revisions, Duplicates: mocks.NewDuplicateFinder(t), Approver: approver,
		Now: fixedClock}
	_, err := cmd.Execute(t.Context(), ApproveRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID})
	require.ErrorIs(t, err, datasources.ErrUnavailable)
	require.ErrorIs(t, err, datasources.ErrDocumentChanged)
}

func TestApproveRevision_Errors(t *testing.T) {
	t.Parallel()

	nameTaken := &domain.DocumentError{Problems: []domain.DocumentProblem{
		{Path: "/name", Code: domain.ProblemDuplicate},
	}}
	term := domain.ModerationRevision{ID: testRevisionID, Status: domain.RevisionStatusPending,
		EntityType: domain.EntityTypeTerm}
	cases := []struct {
		name     string
		revision domain.ModerationRevision
		err      error
		want     error
	}{
		{"not_pending", testModerationRevision, domain.ErrNotPending, domain.ErrNotPending},
		{"rebase_required", testModerationRevision, domain.ErrRebaseRequired, domain.ErrRebaseRequired},
		{"hidden_data_conflict", testModerationRevision, domain.ErrHiddenDataConflict, domain.ErrHiddenDataConflict},
		{"entity_gone", testModerationRevision, datasources.ErrEntityGone, datasources.ErrEntityGone},
		{"unavailable", testModerationRevision, datasources.ErrUnavailable, datasources.ErrUnavailable},
		// A term's name taken in its facet since it was submitted is duplicate_name; another entity's
		// duplicate name is a document problem.
		{"term_name_taken", term, nameTaken, ErrDuplicateName},
		{"piece_name_problem", testModerationRevision, nameTaken, nameTaken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			revisions := mocks.NewModerationRevisionGetter(t)
			revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(tc.revision, nil).Once()
			approver := mocks.NewRevisionApprover(t)
			approver.EXPECT().ApproveRevision(mock.Anything, mock.Anything).Return(tc.err)

			cmd := &ApproveRevision{Revisions: revisions, Duplicates: mocks.NewDuplicateFinder(t), Approver: approver,
				Now: fixedClock}
			_, err := cmd.Execute(t.Context(), ApproveRevisionRequest{Principal: interactiveMod,
				RevisionID: testRevisionID})
			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("not_found", func(t *testing.T) {
		t.Parallel()

		revisions := mocks.NewModerationRevisionGetter(t)
		revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).
			Return(domain.ModerationRevision{}, datasources.ErrNotFound)
		cmd := &ApproveRevision{Revisions: revisions, Duplicates: mocks.NewDuplicateFinder(t),
			Approver: mocks.NewRevisionApprover(t), Now: fixedClock}
		_, err := cmd.Execute(t.Context(), ApproveRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID})
		require.ErrorIs(t, err, datasources.ErrNotFound)
	})
}

func TestRejectRevision(t *testing.T) {
	t.Parallel()

	acknowledged := []domain.FlagCode{domain.FlagHiddenAliasInReviewNote}
	request := datasources.RejectRequest{Actor: interactiveModActor, RevisionID: testRevisionID,
		ReviewNote: "Already catalogued.", AcknowledgedFlags: acknowledged, Now: fixedNow}
	revisions := mocks.NewModerationRevisionGetter(t)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testApproved, nil).Once()
	rejecter := mocks.NewRevisionRejecter(t)
	rejecter.EXPECT().RejectRevision(mock.Anything, request).Return(nil).Once()
	cmd := &RejectRevision{Revisions: revisions, Rejecter: rejecter, Now: fixedClock}
	req := RejectRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID,
		ReviewNote: "Already catalogued.", AcknowledgedFlags: acknowledged}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testApproved, got)

	flagged := &domain.UnacknowledgedFlagsError{Flags: []domain.Flag{{Code: domain.FlagHiddenAliasInReviewNote}}}
	for _, want := range []error{datasources.ErrNotFound, domain.ErrNotPending, flagged} {
		rejecter.EXPECT().RejectRevision(mock.Anything, request).Return(want).Once()
		_, err := cmd.Execute(t.Context(), req)
		require.ErrorIs(t, err, want)
	}
}

func TestRebaseRevision(t *testing.T) {
	t.Parallel()

	current := int64(46)
	summary := "Merge the two edits."
	resolver := mocks.NewEditorRefResolver(t)
	resolver.EXPECT().ResolveEditorRefs(mock.Anything, interactiveModActor, testEditorPiece.References()).
		Return(testRefs, nil)
	revisions := mocks.NewModerationRevisionGetter(t)
	revisions.EXPECT().GetModerationRevision(mock.Anything, testRevisionID).Return(testModerationRevision, nil).Once()
	rebaser := mocks.NewRevisionRebaser(t)
	request := datasources.RebaseRequest{Actor: interactiveModActor, RevisionID: testRevisionID,
		CurrentRevisionID: &current, Document: testPiece, Summary: &summary, Now: fixedNow}
	rebaser.EXPECT().RebaseRevision(mock.Anything, request).Return(nil).Once()
	cmd := &RebaseRevision{Resolver: resolver, Revisions: revisions, Rebaser: rebaser, Now: fixedClock}
	req := RebaseRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID, CurrentRevisionID: &current,
		Document: testEditorPiece, Summary: &summary}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testModerationRevision, got)

	for _, want := range []error{datasources.ErrStale, domain.ErrNotPending, datasources.ErrEntityGone} {
		rebaser.EXPECT().RebaseRevision(mock.Anything, request).Return(want).Once()
		_, err := cmd.Execute(t.Context(), req)
		require.ErrorIs(t, err, want)
	}
}

func TestRebaseRevision_UnresolvedReference(t *testing.T) {
	t.Parallel()

	resolver := mocks.NewEditorRefResolver(t)
	resolver.EXPECT().ResolveEditorRefs(mock.Anything, interactiveModActor, testEditorPiece.References()).
		Return(domain.RefMap{}, nil)
	// The rebaser is not called.
	cmd := &RebaseRevision{Resolver: resolver, Revisions: mocks.NewModerationRevisionGetter(t),
		Rebaser: mocks.NewRevisionRebaser(t), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), RebaseRevisionRequest{Principal: interactiveMod, RevisionID: testRevisionID,
		Document: testEditorPiece})

	var documentErr *domain.DocumentError
	require.ErrorAs(t, err, &documentErr)
	assert.Equal(t, []domain.DocumentProblem{{Path: "/terms/0/term", Code: domain.ProblemUnknownReference}},
		documentErr.Problems)
}

// TestReportLink checks that any principal reports as their user, whatever their credential.
func TestReportLink(t *testing.T) {
	t.Parallel()

	message := "The video is gone."
	receipt := domain.LinkReportReceipt{ID: testReportID, Status: domain.LinkReportStatusOpen, CreatedAt: fixedNow}
	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			reporter := mocks.NewLinkReporter(t)
			report := datasources.NewLinkReport{ReporterUserID: pc.principal.User.ID, URL: testLinkURL,
				Reason: domain.LinkReportReasonBroken, Message: &message, Now: fixedNow}
			reporter.EXPECT().InsertLinkReport(mock.Anything, report).Return(receipt, false, nil).Once()
			cmd := &ReportLink{Reporter: reporter, Now: fixedClock}
			req := ReportLinkRequest{Principal: pc.principal, URL: testLinkURL, Reason: domain.LinkReportReasonBroken,
				Message: &message}

			got, err := cmd.Execute(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, ReportedLink{Receipt: receipt}, got)

			for _, want := range []error{datasources.ErrNotFound, datasources.ErrReportLimitReached} {
				reporter.EXPECT().InsertLinkReport(mock.Anything, report).Return(domain.LinkReportReceipt{}, false,
					want).Once()
				_, err := cmd.Execute(t.Context(), req)
				require.ErrorIs(t, err, want)
			}
		})
	}
}

func TestListLinkReports(t *testing.T) {
	t.Parallel()

	status := domain.LinkReportStatusOpen
	query := datasources.LinkReportQuery{Status: &status, Page: domain.Page{Limit: 5}}
	list := domain.LinkReportList{Reports: []domain.LinkReport{testReport}, Total: 1}
	lister := mocks.NewLinkReportLister(t)
	lister.EXPECT().ListLinkReports(mock.Anything, query).Return(list, nil).Once()
	cmd := &ListLinkReports{Lister: lister}

	got, err := cmd.Execute(t.Context(), LinkReportListRequest{Principal: interactiveMod, Query: query})
	require.NoError(t, err)
	assert.Equal(t, list, got)

	lister.EXPECT().ListLinkReports(mock.Anything, query).Return(domain.LinkReportList{}, errDatasource)
	_, err = cmd.Execute(t.Context(), LinkReportListRequest{Principal: interactiveMod, Query: query})
	require.ErrorIs(t, err, errDatasource)
}

func TestResolveLinkReport(t *testing.T) {
	t.Parallel()

	note := "Replaced."
	request := datasources.ResolveLinkReportRequest{ModeratorUserID: moderatorUser.ID, ReportID: testReportID,
		Status: domain.LinkReportStatusResolved, Note: &note, HideLink: true, Now: fixedNow}
	resolver := mocks.NewLinkReportResolver(t)
	resolver.EXPECT().ResolveLinkReport(mock.Anything, request).Return(testReport, nil).Once()
	cmd := &ResolveLinkReport{Resolver: resolver, Now: fixedClock}
	req := ResolveLinkReportRequest{Principal: interactiveMod, ReportID: testReportID,
		Status: domain.LinkReportStatusResolved, Note: &note, HideLink: true}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testReport, got)

	for _, want := range []error{datasources.ErrNotFound, datasources.ErrReportNotOpen, datasources.ErrUnavailable} {
		resolver.EXPECT().ResolveLinkReport(mock.Anything, request).Return(domain.LinkReport{}, want).Once()
		_, err := cmd.Execute(t.Context(), req)
		require.ErrorIs(t, err, want)
	}
}

func TestSetLinkHidden(t *testing.T) {
	t.Parallel()

	request := datasources.SetLinkHiddenRequest{ModeratorUserID: moderatorUser.ID, LinkID: testLinkID, Hidden: true,
		Now: fixedNow}
	setter := mocks.NewLinkHiddenSetter(t)
	setter.EXPECT().SetLinkHidden(mock.Anything, request).Return(testLink, nil).Once()
	cmd := &SetLinkHidden{Setter: setter, Now: fixedClock}
	req := SetLinkHiddenRequest{Principal: interactiveMod, LinkID: testLinkID, Hidden: true}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testLink, got)

	setter.EXPECT().SetLinkHidden(mock.Anything, request).Return(domain.ModerationLink{}, datasources.ErrNotFound)
	_, err = cmd.Execute(t.Context(), req)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestGetModerationLink(t *testing.T) {
	t.Parallel()

	getter := mocks.NewModerationLinkGetter(t)
	getter.EXPECT().GetModerationLink(mock.Anything, testLinkURL).Return(testLink, nil).Once()
	cmd := &GetModerationLink{Getter: getter}

	got, err := cmd.Execute(t.Context(), ModerationLinkRequest{Principal: interactiveMod, URL: testLinkURL})
	require.NoError(t, err)
	assert.Equal(t, testLink, got)

	getter.EXPECT().GetModerationLink(mock.Anything, testLinkURL).Return(domain.ModerationLink{},
		datasources.ErrNotFound)
	_, err = cmd.Execute(t.Context(), ModerationLinkRequest{Principal: interactiveMod, URL: testLinkURL})
	require.ErrorIs(t, err, datasources.ErrNotFound)
}
