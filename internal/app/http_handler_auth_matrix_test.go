package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// principal is who a request is from, by the credentials it carries.
type principal struct {
	name          string
	authorization string
	// resolves is the user the auth middleware looks up for the request; empty for anonymous requests.
	resolves string
}

// Every principal the auth matrix sends requests as. The "API auth" names avoid gosec's G101, which flags
// identifiers containing "token".
var (
	anonymous = principal{name: "anonymous"}
	// unrecognisedCredentials carry no prefix any validator handles, so the request is anonymous.
	unrecognisedCredentials = principal{name: "unrecognised_credentials", authorization: "Bearer " + contributorID}
	invalidCredentials      = principal{name: "invalid_credentials", authorization: "Bearer auth0|" + invalidSecret}
	// unverifiableCredentials cannot be checked, as when the validator cannot reach Auth0.
	unverifiableCredentials = principal{
		name: "unverifiable_credentials", authorization: "Bearer auth0|" + unverifiableSecret,
	}
	auth0Contributor = principal{
		name: "auth0_contributor", authorization: "Bearer auth0|" + contributorID, resolves: contributorID,
	}
	auth0Moderator = principal{
		name: "auth0_moderator", authorization: "Bearer auth0|" + moderatorID, resolves: moderatorID,
	}
	apiAuthContributor = principal{
		name:          "api_token_contributor",
		authorization: "Bearer " + domain.APITokenPrefix + contributorID,
		resolves:      contributorID,
	}
	apiAuthModerator = principal{
		name:          "api_token_moderator",
		authorization: "Bearer " + domain.APITokenPrefix + moderatorID,
		resolves:      moderatorID,
	}
	blockedAuth0Contributor = principal{
		name: "auth0_blocked_contributor", authorization: "Bearer auth0|" + blockedID, resolves: blockedID,
	}
	blockedAPIAuthContributor = principal{
		name:          "api_token_blocked_contributor",
		authorization: "Bearer " + domain.APITokenPrefix + blockedID,
		resolves:      blockedID,
	}
	// unresolvable authenticates as a user whose lookup fails.
	unresolvable = principal{
		name: "unresolvable_user", authorization: "Bearer auth0|" + unresolvableID, resolves: unresolvableID,
	}
)

var principals = []principal{
	anonymous,
	unrecognisedCredentials,
	invalidCredentials,
	unverifiableCredentials,
	auth0Contributor,
	auth0Moderator,
	apiAuthContributor,
	apiAuthModerator,
	blockedAuth0Contributor,
	blockedAPIAuthContributor,
	unresolvable,
}

// outcome is the response a principal gets: the operation's success, or an error status and code.
type outcome struct {
	status int
	code   httpjson.ErrorCode
}

var (
	allowed           = outcome{}
	unauthenticated   = outcome{status: http.StatusUnauthorized, code: httpjson.CodeUnauthenticated}
	userBlocked       = outcome{status: http.StatusForbidden, code: httpjson.CodeUserBlocked}
	apiAuthNotAllowed = outcome{status: http.StatusForbidden, code: httpjson.CodeAPIAuthNotAllowed}
	moderatorRequired = outcome{status: http.StatusForbidden, code: httpjson.CodeModeratorRequired}
	apiAuthRequired   = outcome{status: http.StatusForbidden, code: httpjson.CodeAPIAuthRequired}
	internalError     = outcome{status: http.StatusInternalServerError, code: httpjson.CodeInternal}
)

// access is who may use an operation: whether its requests are authenticated, and each principal's outcome.
type access struct {
	authenticated bool
	outcomes      map[principal]outcome
}

// The kinds of access operations require, one per authorisation middleware. Each lists every principal.
var (
	// public operations are not authenticated, so they answer whatever credentials a request carries.
	public = access{outcomes: map[principal]outcome{
		anonymous:                 allowed,
		unrecognisedCredentials:   allowed,
		invalidCredentials:        allowed,
		unverifiableCredentials:   allowed,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        allowed,
		apiAuthModerator:          allowed,
		blockedAuth0Contributor:   allowed,
		blockedAPIAuthContributor: allowed,
		unresolvable:              allowed,
	}}
	// authenticatedUser operations are behind requireAuth.
	authenticatedUser = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        allowed,
		apiAuthModerator:          allowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
	// interactiveUser operations are behind requireNonAPIToken.
	interactiveUser = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        apiAuthNotAllowed,
		apiAuthModerator:          apiAuthNotAllowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
	// interactiveModerator operations are behind requireInteractiveModerator: moderators only, and not with an API
	// token.
	interactiveModerator = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          moderatorRequired,
		auth0Moderator:            allowed,
		apiAuthContributor:        moderatorRequired,
		apiAuthModerator:          apiAuthNotAllowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
)

// apiTokenUser operations are behind requireAPIToken: API tokens only, of any role.
var apiTokenUser = access{authenticated: true, outcomes: map[principal]outcome{
	anonymous:                 unauthenticated,
	unrecognisedCredentials:   unauthenticated,
	invalidCredentials:        unauthenticated,
	unverifiableCredentials:   internalError,
	auth0Contributor:          apiAuthRequired,
	auth0Moderator:            apiAuthRequired,
	apiAuthContributor:        allowed,
	apiAuthModerator:          allowed,
	blockedAuth0Contributor:   userBlocked,
	blockedAPIAuthContributor: userBlocked,
	unresolvable:              internalError,
}}

// matrixRow is an operation's access, and a request to it that succeeds when allowed.
type matrixRow struct {
	access access
	// pathParams are the path parameter values, percent-encoded into the path.
	pathParams map[string]string
	// query is the query string, with its "?", of a request that succeeds.
	query string
	body  string
	// headers are request headers beyond Content-Type and Authorization.
	headers       map[string]string
	successStatus int
	// expectSuccess sets up the datasources for a successful request by userID.
	expectSuccess func(m handlerMocks, userID string)
}

// blockBody is the body of a request to block a user.
const blockBody = `{"blocked":true}`

// blockTargetID is the user the moderation operation acts on.
const blockTargetID = "google-oauth2|123"

const revokeTargetID = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"

// pieceRoute is the route of a public piece.
const pieceRoute = "/v1/pieces/{slug}"

// matrixSlug is the slug of the piece, person, term and event the catalogue operations read.
const matrixSlug = "the-copper-kettle"

// matrixPiece, matrixPerson, matrixTerm, matrixCollection and matrixEvent are what the catalogue operations
// read: the least a valid response needs.
var (
	matrixPiece = domain.Piece{
		PieceSummary: domain.PieceSummary{
			Slug: matrixSlug, Title: "The Copper Kettle", Kind: domain.PieceKindSong,
			ConservationStatus: domain.ConservationStatusUnassessed,
		},
		LicensingStatus: domain.LicensingStatusUnknown,
	}
	matrixPerson = domain.Person{
		PersonSummary: domain.PersonSummary{
			Slug: matrixSlug, Name: "Wren of the Vale", Kind: domain.PersonKindCommunityBard, IsStub: true,
		},
		Status: domain.PersonStatusPrefersNoPage,
	}
	matrixTerm = domain.Term{
		TermSummary: domain.TermSummary{Slug: matrixSlug, Facet: domain.FacetTopic, DisplayName: "Kettles"},
		Name:        "Kettles",
	}
	matrixCollection = domain.Collection{
		CollectionSummary: domain.CollectionSummary{
			ID: matrixCollectionID, Name: "The Northmarch Songbook", Kind: domain.CollectionKindSongbookPDF,
		},
		Enumerable: domain.CollectionEnumerableUnknown,
	}
	matrixEvent = domain.Event{
		EventSummary: domain.EventSummary{Slug: matrixSlug, Name: "Midwinter Revel", Kind: domain.EventKindEvent},
	}
)

// matrixCollectionID is the ID of the collection the catalogue operations read.
const matrixCollectionID = 12

// matrixRevisionID is the ID of the revision the catalogue operations read, matrixRevision.
const matrixRevisionID = 42

var matrixRevision = domain.EventRevision{
	RevisionSummary: domain.RevisionSummary{ID: matrixRevisionID, Source: domain.RevisionSourceImport},
	Entity:          matrixEvent.EventSummary,
	Snapshot:        domain.EventSnapshot{Name: matrixEvent.Name, Kind: matrixEvent.Kind},
}

// matrixPage is the page list operations read when a request sets no limit or offset.
var matrixPage = domain.Page{Limit: domain.DefaultPageLimit}

// authMatrix is the access every operation in the OpenAPI spec requires. TestRouter_AuthMatrix fails for an
// operation without a row, so each new endpoint adds one here.
var authMatrix = withRows(revisionMatrix(), moderationMatrix(), contactMatrix(), repertoireMatrix(),
	map[openapitest.Operation]matrixRow{
		{Method: http.MethodGet, Path: healthzPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(handlerMocks, string) {},
		},
		{Method: http.MethodGet, Path: readyzPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.pinger.EXPECT().Ping(mock.Anything).Return(nil)
			},
		},
		{Method: http.MethodGet, Path: mePath}: {
			access:        authenticatedUser,
			successStatus: http.StatusOK,
			expectSuccess: func(handlerMocks, string) {},
		},
		{Method: http.MethodPatch, Path: mePath}: {
			access:        authenticatedUser,
			body:          `{"display_name":"  Morwen "}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.displayNames.EXPECT().UpdateDisplayName(mock.Anything, userID, "Morwen").Return(nil)
				m.users.EXPECT().GetUser(mock.Anything, userID).Return(storedUser(userID), nil).Once()
			},
		},
		{Method: http.MethodPut, Path: "/v1/users/{user_id}/blocked"}: {
			access:        interactiveModerator,
			pathParams:    map[string]string{"user_id": blockTargetID},
			body:          blockBody,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.blocker.EXPECT().BlockUser(mock.Anything, moderatorID, blockTargetID, handlerNow).Return(nil)
				m.users.EXPECT().GetUser(mock.Anything, blockTargetID).Return(storedUser(blockTargetID), nil)
			},
		},
		{Method: http.MethodPost, Path: tokensPath}: {
			access:        interactiveUser,
			body:          `{"name":"CI runner"}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.counter.EXPECT().CountUserActiveAPITokens(mock.Anything, userID).Return(0, nil)
				m.creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{Method: http.MethodGet, Path: tokensPath}: {
			access:        interactiveUser,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.lister.EXPECT().ListUserActiveAPITokens(mock.Anything, userID).Return(nil, nil)
			},
		},
		{Method: http.MethodDelete, Path: "/v1/tokens/{token_id}"}: {
			access:        interactiveUser,
			pathParams:    map[string]string{"token_id": revokeTargetID},
			successStatus: http.StatusNoContent,
			expectSuccess: func(m handlerMocks, userID string) {
				m.revoker.EXPECT().RevokeAPIToken(mock.Anything, userID, revokeTargetID, handlerNow).Return(nil)
			},
		},
		{Method: http.MethodGet, Path: pieceRoute}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.pieces.EXPECT().GetPublicPiece(mock.Anything, matrixSlug).Return(matrixPiece, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/persons/{slug}"}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.persons.EXPECT().GetPublicPerson(mock.Anything, matrixSlug).Return(matrixPerson, nil)
			},
		},
		{Method: http.MethodGet, Path: piecesPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.pieceLister.EXPECT().ListPublicPieces(mock.Anything,
					datasources.PieceQuery{Sort: domain.PieceSortTitle, Page: matrixPage}).Return(domain.PieceList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: personsPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.personLister.EXPECT().ListPublicPersons(mock.Anything,
					datasources.PersonQuery{Sort: domain.PersonSortName, Page: matrixPage}).Return(domain.PersonList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: collectionsPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.collectionLister.EXPECT().ListPublicCollections(mock.Anything,
					datasources.CollectionQuery{Page: matrixPage}).Return(domain.CollectionList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: termsPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.termLister.EXPECT().ListPublicTerms(mock.Anything, (*domain.Facet)(nil)).Return(nil, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/terms/{slug}"}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.terms.EXPECT().GetPublicTerm(mock.Anything, matrixSlug, matrixPage).Return(matrixTerm, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/collections/{collection_id}"}: {
			access:        public,
			pathParams:    map[string]string{collectionIDParam: strconv.Itoa(matrixCollectionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.collections.EXPECT().GetPublicCollection(mock.Anything, int64(matrixCollectionID)).
					Return(matrixCollection, nil)
			},
		},
		{Method: http.MethodGet, Path: eventsPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.eventLister.EXPECT().ListPublicEvents(mock.Anything, (*domain.EventKind)(nil), matrixPage).
					Return(domain.EventList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/events/{slug}"}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.events.EXPECT().GetPublicEvent(mock.Anything, matrixSlug).Return(matrixEvent, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/pieces/{slug}/history"}:  historyRow(domain.SlugEntityTypePiece),
		{Method: http.MethodGet, Path: "/v1/persons/{slug}/history"}: historyRow(domain.SlugEntityTypePerson),
		{Method: http.MethodGet, Path: "/v1/terms/{slug}/history"}:   historyRow(domain.SlugEntityTypeTerm),
		{Method: http.MethodGet, Path: "/v1/events/{slug}/history"}:  historyRow(domain.SlugEntityTypeEvent),
		{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/history"}: {
			access:        public,
			pathParams:    map[string]string{collectionIDParam: strconv.Itoa(matrixCollectionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.collectionHistory.EXPECT().ListPublicCollectionHistory(mock.Anything, int64(matrixCollectionID),
					matrixPage).Return(domain.RevisionHistory{Entity: domain.HistoryEntity{
					Type: domain.EntityTypeCollection, CollectionID: new(int64(matrixCollectionID)), Name: "The Northmarch Songbook",
				}}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/persons/{slug}/performances"}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.personPerformances.EXPECT().ListPublicPersonPerformances(mock.Anything, matrixSlug, matrixPage).
					Return(domain.PersonPerformanceList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/citations"}: {
			access:        public,
			pathParams:    map[string]string{collectionIDParam: strconv.Itoa(matrixCollectionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.collectionCitations.EXPECT().ListPublicCollectionCitations(mock.Anything, int64(matrixCollectionID),
					matrixPage).Return(domain.CitationList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/events/{slug}/performances"}: {
			access:        public,
			pathParams:    map[string]string{slugParam: matrixSlug},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.eventPerformances.EXPECT().ListPublicEventPerformances(mock.Anything, matrixSlug, matrixPage).
					Return(domain.EventPerformanceList{}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/revisions/{revision_id}"}: {
			access:        public,
			pathParams:    map[string]string{revisionIDParam: strconv.Itoa(matrixRevisionID)},
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.revisions.EXPECT().GetPublicRevision(mock.Anything, int64(matrixRevisionID)).Return(matrixRevision, nil)
			},
		},
		// Listing the MCP server's tools reaches it without calling any operation.
		{Method: http.MethodPost, Path: "/v1/mcp"}: {
			access:        apiTokenUser,
			body:          `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
			headers:       map[string]string{"Accept": "application/json, text/event-stream"},
			successStatus: http.StatusOK,
			expectSuccess: func(handlerMocks, string) {},
		},
		{Method: http.MethodGet, Path: needsWorkPath}: {
			access:        public,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.needsWork.EXPECT().ListPublicNeedsWork(mock.Anything, (*domain.NeedsWorkReason)(nil), matrixPage).
					Return(domain.NeedsWorkList{}, nil)
			},
		},
	})

// historyRow is the row of the history of the public entity of entityType with slug matrixSlug.
func historyRow(entityType domain.SlugEntityType) matrixRow {
	return matrixRow{
		access:        public,
		pathParams:    map[string]string{slugParam: matrixSlug},
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, _ string) {
			m.history.EXPECT().ListPublicHistory(mock.Anything, entityType, matrixSlug, matrixPage).
				Return(domain.RevisionHistory{Entity: domain.HistoryEntity{
					Type: entityType.EntityType(), Slug: new(matrixSlug), Name: "Matrix History Piece",
				}}, nil)
		},
	}
}

// TestRouter_AuthMatrix sends a request as every principal to every operation in the OpenAPI spec, through the
// production handler, and checks the status, the error code and the contract. It also checks that preflight
// requests to every operation get 204 without authenticating, whatever their credentials.
func TestRouter_AuthMatrix(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)
	operations := contract.Operations()
	for op := range authMatrix {
		assert.Contains(t, operations, op, "authMatrix has a row for an operation the spec does not have")
	}

	for _, op := range operations {
		row, ok := authMatrix[op]
		if !assert.True(t, ok, "%s %s has no row in authMatrix", op.Method, op.Path) {
			continue
		}
		// A row's access must agree with the spec's security, so that a route registered without the auth its
		// spec declares, with a row written to match the route, still fails.
		assert.Equal(t, !contract.Public(op), row.access.authenticated,
			"%s %s: the row's access is authenticated exactly when the spec declares security", op.Method, op.Path)

		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			path := expandPath(op.Path, row.pathParams) + row.query
			for _, p := range principals {
				want, ok := row.access.outcomes[p]
				if !assert.True(t, ok, "access has no outcome for %s", p.name) {
					continue
				}

				t.Run(p.name, func(t *testing.T) {
					t.Parallel()
					assertOutcome(t, op.Method, path, row, p, want)
				})
			}

			t.Run("preflight", func(t *testing.T) {
				t.Parallel()
				assertPreflightSkipsAuth(t, op.Method, path)
			})
		})
	}
}

// expandPath fills a path template's parameters with their percent-encoded values.
func expandPath(template string, params map[string]string) string {
	path := template
	for name, value := range params {
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))
	}
	return path
}

func assertOutcome(t *testing.T, method, path string, row matrixRow, p principal, want outcome) {
	t.Helper()

	m := newHandlerMocks(t)
	if row.access.authenticated && p.resolves != "" {
		m.expectUser(p.resolves)
	}
	wantStatus := want.status
	if want == allowed {
		wantStatus = row.successStatus
		row.expectSuccess(m, p.resolves)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(row.body))
	if row.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.authorization != "" {
		req.Header.Set("Authorization", p.authorization)
	}
	for name, value := range row.headers {
		req.Header.Set(name, value)
	}
	rec := m.validate.Serve(t, m.handler, req)

	assert.Equal(t, wantStatus, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body errorResponse
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	assert.Equal(t, want.code, body.Code)
}

// assertPreflightSkipsAuth checks that a preflight request for method on path gets 204 as every principal. The
// mocks fail the test if a request reaches authentication or a controller. Preflight requests are answered by
// CORS for every path, so they are not operations in the spec.
func assertPreflightSkipsAuth(t *testing.T, method, path string) {
	t.Helper()

	m := newHandlerMocks(t)
	wantAllowedMethods := allowedMethods(m.validate)
	for _, p := range principals {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, path, nil)
		req.Header.Set("Access-Control-Request-Method", method)
		if p.authorization != "" {
			req.Header.Set("Authorization", p.authorization)
		}
		rec := httptest.NewRecorder()
		m.handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code, p.name)
		assert.Equal(t, wantAllowedMethods, rec.Header().Get("Access-Control-Allow-Methods"), p.name)
		assert.Empty(t, rec.Body.String(), p.name)
	}
}
