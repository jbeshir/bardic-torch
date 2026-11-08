package app

import (
	"maps"
	"net/http"
	"strconv"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/mock"
)

// The revision operations' auth matrix rows. Each operation is the caller's own (authenticatedUser): an API token
// acts with contributor powers, which the commands' tests check, so every authenticated principal that is not
// blocked succeeds here.

// matrixEventDocument is the document the revision operations read and write: the least a valid one needs.
var matrixEventDocument = domain.EventDocument{
	SchemaVersion: domain.DocumentSchemaVersion, Name: matrixEvent.Name, Kind: matrixEvent.Kind,
}

// matrixEditorEvent is matrixEventDocument in the editor format, as a request body's document.
const matrixEditorEvent = `{"name":"Midwinter Revel","kind":"event","description_md":null}`

// matrixDocuments are the documents of each type the editable rows read: the least a valid one needs.
var matrixDocuments = map[domain.EntityType]domain.AnyDocument{
	domain.EntityTypePiece: domain.PieceDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Title: matrixPiece.Title, Kind: matrixPiece.Kind,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
	},
	domain.EntityTypePerson: domain.PersonDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Name: matrixPerson.Name, Kind: matrixPerson.Kind,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown,
	},
	domain.EntityTypeTerm: domain.TermDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Facet: matrixTerm.Facet, Name: matrixTerm.Name,
		DisplayName: matrixTerm.DisplayName,
	},
	domain.EntityTypeCollection: domain.CollectionDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Name: matrixCollection.Name, Kind: matrixCollection.Kind,
		Enumerable: matrixCollection.Enumerable,
	},
	domain.EntityTypeEvent: matrixEventDocument,
}

// matrixEventEntity is the event the revision operations act on.
var matrixEventEntity = domain.RevisionEntity{Type: domain.EntityTypeEvent, ID: 7, Slug: matrixSlug}

var matrixDraft = domain.Draft{
	ID: matrixRevisionID, EntityType: domain.EntityTypeEvent, Entity: &matrixEventEntity,
	BaseRevisionID: new(int64(matrixRevisionID - 1)), Document: matrixEventDocument, CreatedAt: handlerNow,
	UpdatedAt: handlerNow,
}

var matrixMyRevision = domain.MyRevision{
	MyRevisionSummary: domain.MyRevisionSummary{
		ID: matrixRevisionID, Status: domain.RevisionStatusPending, EntityType: domain.EntityTypeEvent,
		Entity: &matrixEventEntity, Title: matrixEvent.Name, Source: domain.RevisionSourceUser,
		Author: domain.AuthorRef{DisplayName: "Morwen", Role: domain.RoleContributor}, CreatedAt: handlerNow,
		SubmittedAt: &handlerNow,
	},
	Snapshot: matrixRevision.Snapshot,
}

// matrixActor matches the actor a request by userID acts as, whatever its effective role.
func matrixActor(userID string) any {
	return mock.MatchedBy(func(a domain.Actor) bool { return a.UserID == userID })
}

// revisionMatrix are the rows of the revision operations, added to authMatrix.
func revisionMatrix() map[openapitest.Operation]matrixRow {
	rows := map[openapitest.Operation]matrixRow{
		{Method: http.MethodPost, Path: draftsPath}: {
			access: authenticatedUser,
			body: `{"entity_type":"event","entity":{"slug":"` + matrixSlug + `","collection_id":null},` +
				`"base_revision_id":null,"document":` + matrixEditorEvent + `,"summary":""}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.refs.EXPECT().ResolveEditorRefs(mock.Anything, matrixActor(userID), domain.EditorRefs{}).
					Return(domain.RefMap{}, nil)
				m.draftSaver.EXPECT().SaveDraft(mock.Anything, mock.MatchedBy(func(r datasources.SaveDraftRequest) bool {
					return r.Actor.UserID == userID && r.Document == matrixEventDocument
				})).Return(datasources.SavedDraft{Draft: matrixDraft, Created: true}, nil)
			},
		},
		{Method: http.MethodGet, Path: draftPath}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{draftIDParam: strconv.Itoa(matrixRevisionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.drafts.EXPECT().GetDraft(mock.Anything, matrixActor(userID), int64(matrixRevisionID)).
					Return(matrixDraft, nil)
			},
		},
		{Method: http.MethodPut, Path: draftPath}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{draftIDParam: strconv.Itoa(matrixRevisionID)},
			body:          `{"entity_type":"event","document":` + matrixEditorEvent + `,"summary":""}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.refs.EXPECT().ResolveEditorRefs(mock.Anything, matrixActor(userID), domain.EditorRefs{}).
					Return(domain.RefMap{}, nil)
				m.draftUpdater.EXPECT().UpdateDraft(mock.Anything, mock.MatchedBy(func(r datasources.UpdateDraftRequest) bool {
					return r.Actor.UserID == userID && r.ID == matrixRevisionID
				})).Return(matrixDraft, nil)
			},
		},
		{Method: http.MethodDelete, Path: draftPath}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{draftIDParam: strconv.Itoa(matrixRevisionID)},
			successStatus: http.StatusNoContent,
			expectSuccess: func(m handlerMocks, userID string) {
				m.draftDiscarder.EXPECT().DiscardDraft(mock.Anything, matrixActor(userID), int64(matrixRevisionID)).
					Return(nil)
			},
		},
		{Method: http.MethodPost, Path: revisionsPath}: {
			access:        authenticatedUser,
			body:          `{"draft_id":42,"acknowledge_duplicates":false,"acknowledged_flags":[]}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.drafts.EXPECT().GetDraft(mock.Anything, matrixActor(userID), int64(matrixRevisionID)).
					Return(matrixDraft, nil)
				m.submitter.EXPECT().SubmitDraft(mock.Anything, mock.MatchedBy(func(r datasources.SubmitRequest) bool {
					return r.Actor.UserID == userID && r.DraftID == matrixRevisionID
				})).Return(datasources.SubmitResult{RevisionID: matrixRevisionID, Status: domain.RevisionStatusPending},
					nil)
				expectMatrixRevision(m, userID)
			},
		},
		{Method: http.MethodPost, Path: withdrawRoute}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{revisionIDParam: strconv.Itoa(matrixRevisionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.withdrawer.EXPECT().WithdrawRevision(mock.Anything, matrixActor(userID), int64(matrixRevisionID)).
					Return(nil)
				expectMatrixRevision(m, userID)
			},
		},
		{Method: http.MethodPost, Path: revertRoute}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{revisionIDParam: strconv.Itoa(matrixRevisionID - 1)},
			body:          `{"summary":null,"acknowledged_flags":[]}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.reverter.EXPECT().RevertTo(mock.Anything, mock.MatchedBy(func(r datasources.RevertRequest) bool {
					return r.Actor.UserID == userID && r.RevisionID == matrixRevisionID-1
				})).Return(datasources.SubmitResult{RevisionID: matrixRevisionID, Status: domain.RevisionStatusPending},
					nil)
				expectMatrixRevision(m, userID)
			},
		},
		{Method: http.MethodGet, Path: myRevisionsPath}: {
			access:        authenticatedUser,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.myRevisionLister.EXPECT().ListMyRevisions(mock.Anything, matrixActor(userID),
					datasources.MyRevisionQuery{Page: matrixPage}).Return(domain.MyRevisionList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: myRevisionPath}: {
			access:        authenticatedUser,
			pathParams:    map[string]string{revisionIDParam: strconv.Itoa(matrixRevisionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				expectMatrixRevision(m, userID)
			},
		},
	}
	for _, entity := range []struct {
		collection, param, value string
		entityType               domain.EntityType
	}{
		{"pieces", slugParam, matrixSlug, domain.EntityTypePiece},
		{"persons", slugParam, matrixSlug, domain.EntityTypePerson},
		{"terms", slugParam, matrixSlug, domain.EntityTypeTerm},
		{"events", slugParam, matrixSlug, domain.EntityTypeEvent},
		{"collections", collectionIDParam, strconv.Itoa(matrixCollectionID), domain.EntityTypeCollection},
	} {
		key := domain.EntityKey{Type: entity.entityType, Slug: matrixSlug}
		if entity.entityType == domain.EntityTypeCollection {
			key = domain.EntityKey{Type: entity.entityType, CollectionID: matrixCollectionID}
		}
		prefix := "/v1/" + entity.collection + "/{" + entity.param + "}"
		params := map[string]string{entity.param: entity.value}
		rows[openapitest.Operation{Method: http.MethodGet, Path: prefix + "/editable"}] = matrixRow{
			access: authenticatedUser, pathParams: params, successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.editable.EXPECT().GetEditable(mock.Anything, matrixActor(userID), key).Return(domain.Editable{
					Entity:        domain.RevisionEntity{Type: key.Type, ID: key.CollectionID, Slug: key.Slug},
					StartingPoint: domain.StartingPointCurrent, Document: matrixDocuments[key.Type],
				}, nil)
			},
		}
		rows[openapitest.Operation{Method: http.MethodGet, Path: prefix + "/pending"}] = matrixRow{
			access: authenticatedUser, pathParams: params, successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.pending.EXPECT().GetPendingVersion(mock.Anything, matrixActor(userID), key).
					Return(matrixMyRevision, nil)
			},
		}
	}

	return rows
}

// expectMatrixRevision expects the caller userID's revision matrixRevisionID to be read.
func expectMatrixRevision(m handlerMocks, userID string) {
	m.myRevisions.EXPECT().GetMyRevision(mock.Anything, matrixActor(userID), int64(matrixRevisionID)).
		Return(matrixMyRevision, nil)
}

// withRows returns the rows of every map in rows, which have no operation in common.
func withRows(rows ...map[openapitest.Operation]matrixRow) map[openapitest.Operation]matrixRow {
	all := map[openapitest.Operation]matrixRow{}
	for _, r := range rows {
		maps.Copy(all, r)
	}

	return all
}

// The path parameters and templates the revision rows share.
const (
	revisionIDParam   = "revision_id"
	collectionIDParam = "collection_id"
	draftPath         = "/v1/drafts/{draft_id}"
	withdrawRoute     = "/v1/revisions/{revision_id}/withdraw"
	revertRoute       = "/v1/revisions/{revision_id}/revert"
)
