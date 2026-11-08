package command

import (
	"errors"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The principals the revision commands act for, and the actors they act as: an API token has contributor
// powers only, so a moderator's token acts as a contributor.
var (
	moderatorUser       = domain.User{ID: "google-oauth2|moderator", Role: domain.RoleModerator}
	contributorUser     = domain.User{ID: resolvedUserID, Role: domain.RoleContributor}
	interactiveMod      = domain.Principal{User: moderatorUser, Method: domain.AuthMethodAuth0}
	moderatorToken      = domain.Principal{User: moderatorUser, Method: domain.AuthMethodAPIToken}
	contributor         = domain.Principal{User: contributorUser, Method: domain.AuthMethodAuth0}
	interactiveModActor = domain.Actor{UserID: moderatorUser.ID, Role: domain.RoleModerator}
	moderatorTokenActor = domain.Actor{UserID: moderatorUser.ID, Role: domain.RoleContributor}
	contributorActor    = domain.Actor{UserID: resolvedUserID, Role: domain.RoleContributor}
)

// principalCases are every kind of principal, with the actor each acts as.
var principalCases = []struct {
	name      string
	principal domain.Principal
	actor     domain.Actor
}{
	{"contributor", contributor, contributorActor},
	{"interactive_moderator", interactiveMod, interactiveModActor},
	{"moderator_api_token", moderatorToken, moderatorTokenActor},
}

const (
	testDraftID     int64 = 51
	testRevisionID  int64 = 52
	testTermID      int64 = 11
	testTermSlug          = "drinking-songs"
	testSummaryText       = "Tag the piece."
	testPieceTitle        = "The Copper Kettle"
)

var errDatasource = errors.New("connection refused")

// testEditorPiece is a piece in the editor format referencing a term; testPiece is it converted.
var (
	testEditorPiece = domain.EditorPieceDocument{
		Title: testPieceTitle, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
		Terms:           []domain.EditorPieceTerm{{Term: testTermSlug, IsPrimary: true}},
	}
	testPiece = domain.PieceDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Title: testPieceTitle, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
		Terms:           []domain.DocPieceTerm{{TermID: testTermID, IsPrimary: true}},
	}
	testRefs        = domain.RefMap{Terms: map[string]int64{testTermSlug: testTermID}}
	testEntityKey   = domain.EntityKey{Type: domain.EntityTypePiece, Slug: "the-copper-kettle"}
	testEntity      = domain.RevisionEntity{Type: domain.EntityTypePiece, ID: 7, Slug: "the-copper-kettle"}
	testExisting    = domain.Draft{ID: testDraftID, EntityType: domain.EntityTypePiece, Entity: &testEntity, Document: testPiece}
	testNewDraft    = domain.Draft{ID: testDraftID, EntityType: domain.EntityTypePiece, Document: testPiece}
	testRevision    = domain.MyRevision{MyRevisionSummary: domain.MyRevisionSummary{ID: testRevisionID}}
	testDuplicate   = domain.DuplicateMatch{EntityType: domain.EntityTypePiece, Slug: "the-copper-kettle-2", Name: testPieceTitle, Matched: domain.DuplicateFieldTitle}
	testBlockedDupe = domain.DuplicateMatch{EntityType: domain.EntityTypePerson, Slug: "wren", Name: "Wren Placeholder", Matched: domain.DuplicateFieldName, Blocked: true}
)

func TestReadCommands_ActAsTheEffectiveRole(t *testing.T) {
	t.Parallel()

	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			editable := mocks.NewEditableDocumentGetter(t)
			editable.EXPECT().GetEditable(mock.Anything, pc.actor, testEntityKey).
				Return(domain.Editable{Entity: testEntity}, nil)
			gotEditable, err := (&GetEditable{Getter: editable}).Execute(t.Context(),
				EntityRequest{Principal: pc.principal, Key: testEntityKey})
			require.NoError(t, err)
			assert.Equal(t, testEntity, gotEditable.Entity)

			pending := mocks.NewPendingVersionGetter(t)
			pending.EXPECT().GetPendingVersion(mock.Anything, pc.actor, testEntityKey).Return(testRevision, nil)
			gotPending, err := (&GetPendingVersion{Getter: pending}).Execute(t.Context(),
				EntityRequest{Principal: pc.principal, Key: testEntityKey})
			require.NoError(t, err)
			assert.Equal(t, testRevision, gotPending)

			revisions := mocks.NewMyRevisionGetter(t)
			revisions.EXPECT().GetMyRevision(mock.Anything, pc.actor, testRevisionID).Return(testRevision, nil)
			gotRevision, err := (&GetMyRevision{Getter: revisions}).Execute(t.Context(),
				RevisionRequest{Principal: pc.principal, ID: testRevisionID})
			require.NoError(t, err)
			assert.Equal(t, testRevision, gotRevision)

			query := datasources.MyRevisionQuery{Page: domain.Page{Limit: 5}}
			lister := mocks.NewMyRevisionLister(t)
			lister.EXPECT().ListMyRevisions(mock.Anything, pc.actor, query).
				Return(domain.MyRevisionList{Total: 3}, nil)
			gotList, err := (&ListMyRevisions{Lister: lister}).Execute(t.Context(),
				ListMyRevisionsRequest{Principal: pc.principal, Query: query})
			require.NoError(t, err)
			assert.Equal(t, int64(3), gotList.Total)

			discarder := mocks.NewDraftDiscarder(t)
			discarder.EXPECT().DiscardDraft(mock.Anything, pc.actor, testDraftID).Return(nil)
			_, err = (&DiscardDraft{Discarder: discarder}).Execute(t.Context(),
				RevisionRequest{Principal: pc.principal, ID: testDraftID})
			require.NoError(t, err)
		})
	}
}

func TestReadCommands_PassErrorsOn(t *testing.T) {
	t.Parallel()

	for _, want := range []error{datasources.ErrNotFound, domain.ErrNotDraft, errDatasource} {
		editable := mocks.NewEditableDocumentGetter(t)
		editable.EXPECT().GetEditable(mock.Anything, contributorActor, testEntityKey).Return(domain.Editable{}, want)
		_, err := (&GetEditable{Getter: editable}).Execute(t.Context(),
			EntityRequest{Principal: contributor, Key: testEntityKey})
		require.ErrorIs(t, err, want)

		pending := mocks.NewPendingVersionGetter(t)
		pending.EXPECT().GetPendingVersion(mock.Anything, contributorActor, testEntityKey).Return(domain.MyRevision{}, want)
		_, err = (&GetPendingVersion{Getter: pending}).Execute(t.Context(),
			EntityRequest{Principal: contributor, Key: testEntityKey})
		require.ErrorIs(t, err, want)

		revisions := mocks.NewMyRevisionGetter(t)
		revisions.EXPECT().GetMyRevision(mock.Anything, contributorActor, testRevisionID).Return(domain.MyRevision{}, want)
		_, err = (&GetMyRevision{Getter: revisions}).Execute(t.Context(),
			RevisionRequest{Principal: contributor, ID: testRevisionID})
		require.ErrorIs(t, err, want)

		lister := mocks.NewMyRevisionLister(t)
		lister.EXPECT().ListMyRevisions(mock.Anything, contributorActor, datasources.MyRevisionQuery{}).
			Return(domain.MyRevisionList{}, want)
		_, err = (&ListMyRevisions{Lister: lister}).Execute(t.Context(), ListMyRevisionsRequest{Principal: contributor})
		require.ErrorIs(t, err, want)

		discarder := mocks.NewDraftDiscarder(t)
		discarder.EXPECT().DiscardDraft(mock.Anything, contributorActor, testDraftID).Return(want)
		_, err = (&DiscardDraft{Discarder: discarder}).Execute(t.Context(),
			RevisionRequest{Principal: contributor, ID: testDraftID})
		require.ErrorIs(t, err, want)

		drafts := mocks.NewDraftGetter(t)
		drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(domain.Draft{}, want)
		_, err = (&GetDraft{Getter: drafts, Duplicates: mocks.NewDuplicateFinder(t)}).Execute(t.Context(),
			RevisionRequest{Principal: contributor, ID: testDraftID})
		require.ErrorIs(t, err, want)
	}
}

func TestGetDraft_Duplicates(t *testing.T) {
	t.Parallel()

	for _, pc := range principalCases {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()

			// A draft of an existing entity has no duplicates, and none are looked for.
			drafts := mocks.NewDraftGetter(t)
			drafts.EXPECT().GetDraft(mock.Anything, pc.actor, testDraftID).Return(testExisting, nil).Once()
			cmd := &GetDraft{Getter: drafts, Duplicates: mocks.NewDuplicateFinder(t)}
			got, err := cmd.Execute(t.Context(), RevisionRequest{Principal: pc.principal, ID: testDraftID})
			require.NoError(t, err)
			assert.Equal(t, DraftResult{Draft: testExisting, Duplicates: []domain.DuplicateMatch{}}, got)

			// A new entity's: only an interactive moderator's include hidden aliases and pending entities.
			drafts.EXPECT().GetDraft(mock.Anything, pc.actor, testDraftID).Return(testNewDraft, nil).Once()
			finder := mocks.NewDuplicateFinder(t)
			finder.EXPECT().FindDuplicates(mock.Anything, domain.NewDuplicateCandidate(testPiece),
				pc.actor.Role == domain.RoleModerator).Return([]domain.DuplicateMatch{testDuplicate}, nil)
			cmd.Duplicates = finder
			got, err = cmd.Execute(t.Context(), RevisionRequest{Principal: pc.principal, ID: testDraftID})
			require.NoError(t, err)
			assert.Equal(t, DraftResult{Draft: testNewDraft, Duplicates: []domain.DuplicateMatch{testDuplicate}}, got)
		})
	}

	drafts := mocks.NewDraftGetter(t)
	drafts.EXPECT().GetDraft(mock.Anything, contributorActor, testDraftID).Return(testNewDraft, nil)
	finder := mocks.NewDuplicateFinder(t)
	finder.EXPECT().FindDuplicates(mock.Anything, mock.Anything, false).Return(nil, errDatasource)
	_, err := (&GetDraft{Getter: drafts, Duplicates: finder}).Execute(t.Context(),
		RevisionRequest{Principal: contributor, ID: testDraftID})
	require.ErrorIs(t, err, errDatasource)
}
