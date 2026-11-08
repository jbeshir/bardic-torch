package app

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/mock"
)

// The repertoire operations' auth matrix rows. The caller's own repertoire is theirs with either credential
// (authenticatedUser); a public repertoire, addressed by public ID, answers whatever credentials a request carries.

// matrixPublicID is the public ID of every user the matrix resolves, and of the public repertoire it reads.
const matrixPublicID = "k3vq7xw2mbnc4zra"

const repertoireEntryRoute = "/v1/me/repertoire/{slug}"

var matrixRepertoireEntry = domain.RepertoireEntry{
	Piece: matrixPiece.PieceSummary, Visibility: domain.RepertoireVisibilityPrivate, AddedAt: handlerNow,
	UpdatedAt: handlerNow,
}

// repertoireMatrix are the rows of the repertoire operations, added to authMatrix.
func repertoireMatrix() map[openapitest.Operation]matrixRow {
	entryParams := map[string]string{slugParam: matrixSlug}

	return map[openapitest.Operation]matrixRow{
		{Method: http.MethodGet, Path: "/v1/me/repertoire"}: {
			access:        authenticatedUser,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.repertoire.EXPECT().ListRepertoire(mock.Anything,
					datasources.RepertoireQuery{UserID: userID, Page: matrixPage}).Return(domain.RepertoireList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: repertoireEntryRoute}: {
			access:        authenticatedUser,
			pathParams:    entryParams,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.repertoireEntries.EXPECT().GetRepertoireEntry(mock.Anything, userID, matrixSlug).
					Return(matrixRepertoireEntry, nil)
			},
		},
		{Method: http.MethodPut, Path: repertoireEntryRoute}: {
			access:        authenticatedUser,
			pathParams:    entryParams,
			body:          `{"notes":null,"visibility":"private"}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.repertoirePutter.EXPECT().PutRepertoireEntry(mock.Anything, datasources.RepertoireEntryWrite{
					UserID: userID, PieceSlug: matrixSlug, Visibility: domain.RepertoireVisibilityPrivate,
					MaxEntries: domain.MaxRepertoireEntries, Now: handlerNow,
				}).Return(matrixRepertoireEntry, true, nil)
			},
		},
		{Method: http.MethodDelete, Path: repertoireEntryRoute}: {
			access:        authenticatedUser,
			pathParams:    entryParams,
			successStatus: http.StatusNoContent,
			expectSuccess: func(m handlerMocks, userID string) {
				m.repertoireDeleter.EXPECT().DeleteRepertoireEntry(mock.Anything, userID, matrixSlug).Return(nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/users/{public_id}/repertoire"}: {
			access:        public,
			pathParams:    map[string]string{"public_id": matrixPublicID},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.publicRepertoire.EXPECT().ListPublicRepertoire(mock.Anything, matrixPublicID, matrixPage).
					Return(domain.PublicRepertoire{
						Owner: domain.RepertoireOwner{DisplayName: matrixDisplayName, Role: domain.RoleContributor},
						Total: 1,
					}, nil)
			},
		},
	}
}
