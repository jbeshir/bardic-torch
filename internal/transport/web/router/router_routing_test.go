package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routing corpus pins how New answers every kind of request at the HTTP level, independent of the router
// implementation: which route a request reaches, path cleaning redirects, 404 and 405 answers for unrouted
// requests, encoded path segments, and whether authentication runs.

const (
	// routingModerator is the Authorization header the routing fixture authenticates as a moderator.
	routingModerator = "Bearer moderator"
	// routingInvalid is the Authorization header the routing fixture rejects as invalid credentials.
	routingInvalid = "Bearer invalid"

	routingModeratorID = "auth0|moderator"
	routingRevokeID    = "0b0e5a4c-6a43-4f8e-9d3f-1c2b3a4d5e6f"

	allRouteMethods = "DELETE, GET, PATCH, POST, PUT"

	jsonContentType = "application/json"
	unmatched       = "unmatched"

	healthyBody         = `{"status":"ok"}` + "\n"
	noRouteBody         = `{"code":"not_found","message":"no route matches the path"}` + "\n"
	notAllowedBody      = `{"code":"method_not_allowed","message":"method not allowed for this path"}` + "\n"
	authRequiredBody    = `{"code":"unauthenticated","message":"authentication required"}` + "\n"
	invalidCredsBody    = `{"code":"unauthenticated","message":"invalid credentials"}` + "\n"
	userNotFoundBody    = `{"code":"not_found","message":"user not found"}` + "\n"
	tokenNotFoundBody   = `{"code":"not_found","message":"API token not found"}` + "\n"
	blockedTemplate     = "/v1/users/{user_id}/blocked"
	revokeTemplate      = "/v1/tokens/{token_id}"
	blockedRequestBody  = `{"blocked":true}`
	routingTokenPath    = "/v1/tokens/" + routingRevokeID
	routingBlockedPath  = "/v1/users/auth0%7Cuser/blocked"
	routingUnknownV1    = "/v1/unknown"
	routingUnknownRoot  = "/unknown"
	routingHealthz      = "/healthz"
	routingMe           = "/v1/me"
	routingTokens       = "/v1/tokens"
	routingReadyz       = "/readyz"
	routingConnectHost  = "example.com:443"
	routingConnectRedir = "//example.com:443/"

	internalBody    = `{"code":"internal","message":"internal error"}` + "\n"
	invalidSlugBody = `{"code":"invalid_request","message":"invalid slug: must be lowercase ASCII letters and ` +
		`digits, in runs joined by single hyphens, at most 191 characters"}` + "\n"

	routingSlug              = "the-copper-kettle"
	routingCatalogueID       = "42"
	routingPieces            = "/v1/pieces"
	routingPieceTemplate     = "/v1/pieces/{slug}"
	routingPiecePath         = routingPieces + "/" + routingSlug
	routingPieceHistory      = routingPiecePath + "/history"
	routingPieceHistoryTmpl  = "/v1/pieces/{slug}/history"
	routingCollectionTmpl    = "/v1/collections/{collection_id}"
	routingCollectionHistory = "/v1/collections/{collection_id}/history"
	routingRevisionTemplate  = "/v1/revisions/{revision_id}"
	routingRevisionPath      = "/v1/revisions/" + routingCatalogueID
	routingNeedsWork         = "/v1/needs-work"

	routingPieceEditableTmpl = "/v1/pieces/{slug}/editable"
	routingPieceEditable     = routingPiecePath + "/editable"
	routingPiecePending      = routingPiecePath + "/pending"
	routingDrafts            = "/v1/drafts"
	routingDraftTmpl         = "/v1/drafts/{draft_id}"
	routingDraftPath         = routingDrafts + "/" + routingCatalogueID
	routingRevisions         = "/v1/revisions"
	routingWithdrawTmpl      = "/v1/revisions/{revision_id}/withdraw"
	routingWithdrawPath      = routingRevisionPath + "/withdraw"
	routingMyRevisions       = "/v1/me/revisions"
	// routingPlaceholder is the placeholder slug of a pending new piece.
	routingPlaceholder = "pending-piece-12"
)

// catalogueNotFound is the answer of a public catalogue route whose controller asked the fixture for pathValue and
// was told that no public what has it.
func catalogueNotFound(template, what, pathValue string) routingCase {
	return routingCase{
		status: http.StatusNotFound, contentType: jsonContentType,
		respBody: `{"code":"not_found","message":"` + what + ` not found"}` + "\n", route: template, pathValue: pathValue,
	}
}

// catalogueListFailed is the answer of a public catalogue list route whose controller asked the fixture for the
// list.
func catalogueListFailed(template string) routingCase {
	return routingCase{
		status: http.StatusInternalServerError, contentType: jsonContentType, respBody: internalBody, route: template,
	}
}

// publicRows are the public catalogue paths. They never authenticate.
var publicRows = []routeRow{
	{path: routingPieces, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed(routingPieces),
	}},
	{path: routingPiecePath, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound(routingPieceTemplate, "piece", routingSlug),
	}},
	{path: "/v1/persons", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed("/v1/persons"),
	}},
	{path: "/v1/persons/" + routingSlug, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/persons/{slug}", "person", routingSlug),
	}},
	{path: "/v1/terms", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed("/v1/terms"),
	}},
	{path: "/v1/terms/" + routingSlug, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/terms/{slug}", "term", routingSlug),
	}},
	{path: "/v1/collections", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed("/v1/collections"),
	}},
	{path: "/v1/collections/" + routingCatalogueID, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound(routingCollectionTmpl, "collection", routingCatalogueID),
	}},
	{path: "/v1/events", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed("/v1/events"),
	}},
	{path: "/v1/events/" + routingSlug, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/events/{slug}", "event", routingSlug),
	}},
	{path: routingPieceHistory, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound(routingPieceHistoryTmpl, "piece", routingSlug),
	}},
	{path: "/v1/persons/" + routingSlug + "/history", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/persons/{slug}/history", "person", routingSlug),
	}},
	{path: "/v1/terms/" + routingSlug + "/history", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/terms/{slug}/history", "term", routingSlug),
	}},
	{path: "/v1/events/" + routingSlug + "/history", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound("/v1/events/{slug}/history", "event", routingSlug),
	}},
	{path: "/v1/collections/" + routingCatalogueID + "/history", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound(routingCollectionHistory, "collection", routingCatalogueID),
	}},
	{path: routingRevisionPath, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueNotFound(routingRevisionTemplate, "revision", routingCatalogueID),
	}},
	{path: routingNeedsWork, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: catalogueListFailed(routingNeedsWork),
	}},
}

// getWith returns tc as the answer to a GET of target with the Authorization header authorization.
func getWith(tc routingCase, target, authorization string) routingCase {
	tc.method, tc.target, tc.authorization = http.MethodGet, target, authorization
	return tc
}

// publicCases send requests to the public catalogue routes. They answer every credentials, even invalid ones,
// as they would an anonymous request, without authenticating; their path values are decoded once, as on the
// authenticated routes.
func publicCases() []routingCase {
	piece := catalogueNotFound(routingPieceTemplate, "piece", routingSlug)
	revision := catalogueNotFound(routingRevisionTemplate, "revision", routingCatalogueID)
	invalidSlug := routingCase{
		status: http.StatusBadRequest, contentType: jsonContentType, respBody: invalidSlugBody,
		route: routingPieceTemplate,
	}

	return []routingCase{
		getWith(piece, routingPiecePath, routingInvalid),
		getWith(piece, routingPiecePath, routingModerator),
		getWith(piece, routingPiecePath+"?x=%7C", routingInvalid),
		getWith(catalogueListFailed(routingPieces), routingPieces+"?q=kettle",
			routingInvalid),
		getWith(catalogueListFailed(routingNeedsWork), routingNeedsWork, routingModerator),
		getWith(catalogueNotFound(routingPieceHistoryTmpl, "piece", routingSlug),
			routingPieceHistory, routingInvalid),
		getWith(revision, routingRevisionPath, routingInvalid),
		getWith(catalogueNotFound(routingCollectionHistory, "collection", routingCatalogueID),
			"/v1/collections/42/history", routingModerator),
		// A wrong method on a public path is a 405, never a 401, whatever the credentials.
		notAllowedAt(http.MethodPost, routingPieces, routingInvalid, http.MethodGet),
		notAllowedAt(http.MethodDelete, routingPiecePath, routingModerator, http.MethodGet),
		notAllowedAt(http.MethodHead, routingPiecePath, routingInvalid, http.MethodGet),
		notAllowedAt(http.MethodPut, routingPieceHistory, routingInvalid, http.MethodGet),
		notAllowedAt(http.MethodDelete, routingRevisionPath, routingInvalid, http.MethodGet),
		notAllowedAt(http.MethodPatch, routingNeedsWork, routingInvalid, http.MethodGet),
		// A literal segment of a public route's shape is a slug.
		getWith(catalogueNotFound(routingPieceTemplate, "piece", "history"),
			"/v1/pieces/history", ""),
		// Path values are decoded once before the controller checks them.
		getWith(piece, "/v1/pieces/the-copper-k%65ttle", ""),
		getWith(catalogueNotFound(routingPieceHistoryTmpl, "piece", routingSlug),
			"/v1/pieces/the-copper-k%65ttle/history", ""),
		getWith(revision, "/v1/revisions/%342", ""),
		getWith(catalogueNotFound(routingCollectionTmpl, "collection", routingCatalogueID),
			"/v1/collections/4%32", ""),
		getWith(invalidSlug, "/v1/pieces/a%2Fb", routingInvalid),
		getWith(invalidSlug, "/v1/pieces/the%20kettle", ""),
		getWith(invalidSlug, "/v1/pieces/the-copper-kettle%257C", ""),
		{
			method: http.MethodGet, target: "/v1/revisions/4%2F2",
			status: http.StatusBadRequest, contentType: jsonContentType,
			respBody: `{"code":"invalid_request","message":"invalid revision_id: must be an integer from 1 to ` +
				`9223372036854775807"}` + "\n",
			route: routingRevisionTemplate,
		},
		// A slug or ID that is exactly %2F (or %2f) is an unrouted 404 for every method, as on the authenticated
		// routes. gorilla/mux routed these to their controllers, which answered 400 for the slug or ID, and answered
		// a wrong method with 405 and Allow: GET.
		notFound(http.MethodGet, "/v1/pieces/%2F", ""),
		notFound(http.MethodGet, "/v1/pieces/%2F", routingInvalid),
		notFound(http.MethodGet, "/v1/pieces/%2F/history", ""),
		notFound(http.MethodGet, "/v1/collections/%2F", ""),
		notFound(http.MethodGet, "/v1/revisions/%2F", routingInvalid),
		notFound(http.MethodGet, "/v1/pieces/%2f", ""),
		notFound(http.MethodGet, "/v1/events/%2f/history", routingInvalid),
		notFound(http.MethodPost, "/v1/pieces/%2F", routingInvalid),
		notFound(http.MethodHead, "/v1/revisions/%2F", ""),
		notFound(http.MethodDelete, "/v1/collections/%2f/history", routingInvalid),
		// Unrouted paths around the public routes, none of which authenticates.
		notFound(http.MethodGet, "/v1/pieces/", ""),
		notFound(http.MethodGet, routingPiecePath+"/", routingInvalid),
		notFound(http.MethodGet, routingPieceHistory+"/", routingInvalid),
		notFound(http.MethodGet, routingPiecePath+"/extra", routingInvalid),
		notFound(http.MethodGet, routingPieceHistory+"/extra", routingInvalid),
		notFound(http.MethodGet, routingNeedsWork+"/", routingInvalid),
		notFound(http.MethodGet, routingNeedsWork+"/x", routingInvalid),
		// POST /v1/revisions submits a draft; there is no list of revisions.
		notAllowedAt(http.MethodGet, routingRevisions, routingInvalid, http.MethodPost),
		notFound(http.MethodGet, "/v1/revisions/", routingInvalid),
		notFound(http.MethodGet, "/v1/p%69eces", ""),
		notFound(http.MethodGet, "/v1/p%69eces/"+routingSlug, routingInvalid),
		notFound(http.MethodGet, routingPiecePath+"/hist%6Fry", routingInvalid),
		notFound(http.MethodGet, "/v1/needs%2Dwork", ""),
		// Non-canonical public paths are redirected like any other, before routing.
		redirect(http.MethodGet, "/v1/pieces//"+routingSlug, routingPiecePath),
		redirect(http.MethodGet, "/v1/pieces/x/../"+routingSlug+"/history?limit=5", routingPieceHistory+"?limit=5"),
		redirect(http.MethodGet, "/v1//needs-work?reason=no_links", routingNeedsWork+"?reason=no_links"),
	}
}

// routingFixture records what the handlers New builds were asked to do for one request.
type routingFixture struct {
	mu sync.Mutex
	// authRan is whether an auth validator was consulted.
	authRan bool
	// pathValue is the user or token ID, slug or catalogue ID a command or datasource was given.
	pathValue string
}

func (f *routingFixture) validate(r *http.Request) (*Identity, error) {
	f.mu.Lock()
	f.authRan = true
	f.mu.Unlock()

	switch r.Header.Get("Authorization") {
	case routingModerator:
		return &Identity{UserID: routingModeratorID, Method: domain.AuthMethodAuth0}, nil
	case routingInvalid:
		return nil, fmt.Errorf("bad signature: %w", ErrInvalidCredentials)
	default:
		return nil, nil
	}
}

func (f *routingFixture) recordPathValue(value string) {
	f.mu.Lock()
	f.pathValue = value
	f.mu.Unlock()
}

func (*routingFixture) Ping(context.Context) error {
	return nil
}

func (f *routingFixture) Execute(_ context.Context, req command.SetUserBlockedRequest) (domain.User, error) {
	f.recordPathValue(req.UserID)
	return domain.User{}, datasources.ErrNotFound
}

func (f *routingFixture) RevokeAPIToken(_ context.Context, _, tokenID string, _ time.Time) error {
	f.recordPathValue(tokenID)
	return datasources.ErrNotFound
}

// errRoutingList is what the fixture's catalogue lists fail with, so a list route answers 500 once its controller
// has run.
var errRoutingList = errors.New("routing fixture lists nothing")

// The catalogue getters record the slug or ID they are given and find nothing.

func (f *routingFixture) GetPublicPiece(_ context.Context, slug string) (domain.Piece, error) {
	f.recordPathValue(slug)
	return domain.Piece{}, datasources.ErrNotFound
}

func (f *routingFixture) GetPublicPerson(_ context.Context, slug string) (domain.Person, error) {
	f.recordPathValue(slug)
	return domain.Person{}, datasources.ErrNotFound
}

func (f *routingFixture) GetPublicTerm(_ context.Context, slug string, _ domain.Page) (domain.Term, error) {
	f.recordPathValue(slug)
	return domain.Term{}, datasources.ErrNotFound
}

func (f *routingFixture) GetPublicEvent(_ context.Context, slug string) (domain.Event, error) {
	f.recordPathValue(slug)
	return domain.Event{}, datasources.ErrNotFound
}

func (f *routingFixture) GetPublicCollection(_ context.Context, id int64) (domain.Collection, error) {
	f.recordPathValue(strconv.FormatInt(id, 10))
	return domain.Collection{}, datasources.ErrNotFound
}

func (f *routingFixture) GetPublicRevision(_ context.Context, id int64) (domain.Revision, error) {
	f.recordPathValue(strconv.FormatInt(id, 10))
	return nil, datasources.ErrNotFound
}

func (f *routingFixture) ListPublicHistory(
	_ context.Context, _ domain.SlugEntityType, slug string, _ domain.Page,
) (domain.RevisionHistory, error) {
	f.recordPathValue(slug)
	return domain.RevisionHistory{}, datasources.ErrNotFound
}

func (f *routingFixture) ListPublicCollectionHistory(
	_ context.Context, id int64, _ domain.Page,
) (domain.RevisionHistory, error) {
	f.recordPathValue(strconv.FormatInt(id, 10))
	return domain.RevisionHistory{}, datasources.ErrNotFound
}

func (*routingFixture) ListPublicPieces(context.Context, datasources.PieceQuery) (domain.PieceList, error) {
	return domain.PieceList{}, errRoutingList
}

func (*routingFixture) ListPublicPersons(context.Context, datasources.PersonQuery) (domain.PersonList, error) {
	return domain.PersonList{}, errRoutingList
}

func (*routingFixture) ListPublicTerms(context.Context, *domain.Facet) ([]domain.TermListItem, error) {
	return nil, errRoutingList
}

func (*routingFixture) ListPublicCollections(
	context.Context, datasources.CollectionQuery,
) (domain.CollectionList, error) {
	return domain.CollectionList{}, errRoutingList
}

func (*routingFixture) ListPublicEvents(context.Context, *domain.EventKind, domain.Page) (domain.EventList, error) {
	return domain.EventList{}, errRoutingList
}

func (*routingFixture) ListPublicNeedsWork(
	context.Context, *domain.NeedsWorkReason, domain.Page,
) (domain.NeedsWorkList, error) {
	return domain.NeedsWorkList{}, errRoutingList
}

// The revision commands record the slug or ID they are given and find nothing.

type routingEditable struct{ f *routingFixture }

func (c routingEditable) Execute(_ context.Context, req command.EntityRequest) (domain.Editable, error) {
	c.f.recordPathValue(req.Key.Slug)
	return domain.Editable{}, datasources.ErrNotFound
}

type routingPending struct{ f *routingFixture }

func (c routingPending) Execute(_ context.Context, req command.EntityRequest) (domain.MyRevision, error) {
	c.f.recordPathValue(req.Key.Slug)
	return domain.MyRevision{}, datasources.ErrNotFound
}

type routingRevision struct{ f *routingFixture }

func (c routingRevision) Execute(_ context.Context, req command.RevisionRequest) (domain.MyRevision, error) {
	c.f.recordPathValue(strconv.FormatInt(req.ID, 10))
	return domain.MyRevision{}, datasources.ErrNotFound
}

type routingDraft struct{ f *routingFixture }

func (c routingDraft) Execute(_ context.Context, req command.RevisionRequest) (command.DraftResult, error) {
	c.f.recordPathValue(strconv.FormatInt(req.ID, 10))
	return command.DraftResult{}, datasources.ErrNotFound
}

func (f *routingFixture) dependencies() Dependencies {
	moderator := testUser
	moderator.ID, moderator.Role = routingModeratorID, domain.RoleModerator
	return Dependencies{
		Pinger:                        f,
		AuthValidators:                []AuthValidator{f.validate},
		ResolveUser:                   resolveTo(moderator),
		SetUserBlocked:                f,
		APITokenRevoker:               f,
		PublicPieceLister:             f,
		PublicPieceGetter:             f,
		PublicPersonLister:            f,
		PublicPersonGetter:            f,
		PublicTermLister:              f,
		PublicTermGetter:              f,
		PublicCollectionLister:        f,
		PublicCollectionGetter:        f,
		PublicEventLister:             f,
		PublicEventGetter:             f,
		PublicHistoryLister:           f,
		PublicCollectionHistoryLister: f,
		PublicRevisionGetter:          f,
		PublicNeedsWorkLister:         f,
		GetEditable:                   routingEditable{f},
		GetPendingVersion:             routingPending{f},
		WithdrawRevision:              routingRevision{f},
		GetDraft:                      routingDraft{f},
		Now:                           func() time.Time { return testNow },
	}
}

// routingCase is one request and everything observable about its answer. Empty strings are asserted as absent.
type routingCase struct {
	method        string
	target        string
	authorization string
	body          string

	status      int
	allow       string
	location    string
	contentType string
	respBody    string
	authRan     bool
	// route is the request log's route attribute.
	route string
	// pathValue is the ID the controller passed on, if it got that far.
	pathValue string
}

// routeRow is one path of the route table, used to generate a case for every method.
type routeRow struct {
	path string
	// allow is the Allow header of a 405 answer for the path.
	allow string
	// routed are the answers to anonymous requests for each method the path is routed for.
	routed map[string]routingCase
}

var everyMethod = []string{
	http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions,
	http.MethodPatch, http.MethodPost, http.MethodPut,
}

func anonymousAuthRequired(template string) routingCase {
	return routingCase{
		status: http.StatusUnauthorized, contentType: jsonContentType, respBody: authRequiredBody,
		authRan: true, route: template,
	}
}

var routeRows = []routeRow{
	{path: routingHealthz, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: {status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingHealthz},
	}},
	{path: routingReadyz, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: {status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingReadyz},
	}},
	{path: routingMe, allow: "GET, PATCH", routed: map[string]routingCase{
		http.MethodGet:   anonymousAuthRequired(routingMe),
		http.MethodPatch: anonymousAuthRequired(routingMe),
	}},
	{path: routingBlockedPath, allow: "PUT", routed: map[string]routingCase{
		http.MethodPut: anonymousAuthRequired(blockedTemplate),
	}},
	{path: routingTokens, allow: "GET, POST", routed: map[string]routingCase{
		http.MethodGet:  anonymousAuthRequired(routingTokens),
		http.MethodPost: anonymousAuthRequired(routingTokens),
	}},
	// gorilla/mux answered a wrong method here with every method in Allow, and logged the route /v1: the last /v1
	// route cleared the subrouter's method mismatch. That contradicted the spec's rule that Allow lists the
	// supported methods, and is fixed.
	{path: routingTokenPath, allow: "DELETE", routed: map[string]routingCase{
		http.MethodDelete: anonymousAuthRequired(revokeTemplate),
	}},
	{path: routingPieceEditable, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired(routingPieceEditableTmpl),
	}},
	{path: "/v1/persons/" + routingSlug + "/editable", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/persons/{slug}/editable"),
	}},
	{path: "/v1/terms/" + routingSlug + "/editable", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/terms/{slug}/editable"),
	}},
	{path: "/v1/events/" + routingSlug + "/editable", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/events/{slug}/editable"),
	}},
	{path: "/v1/collections/" + routingCatalogueID + "/editable", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/collections/{collection_id}/editable"),
	}},
	{path: routingPiecePending, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/pieces/{slug}/pending"),
	}},
	{path: "/v1/persons/" + routingSlug + "/pending", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/persons/{slug}/pending"),
	}},
	{path: "/v1/terms/" + routingSlug + "/pending", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/terms/{slug}/pending"),
	}},
	{path: "/v1/events/" + routingSlug + "/pending", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/events/{slug}/pending"),
	}},
	{path: "/v1/collections/" + routingCatalogueID + "/pending", allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/collections/{collection_id}/pending"),
	}},
	{path: routingDrafts, allow: http.MethodPost, routed: map[string]routingCase{
		http.MethodPost: anonymousAuthRequired(routingDrafts),
	}},
	{path: routingDraftPath, allow: "DELETE, GET, PUT", routed: map[string]routingCase{
		http.MethodDelete: anonymousAuthRequired(routingDraftTmpl),
		http.MethodGet:    anonymousAuthRequired(routingDraftTmpl),
		http.MethodPut:    anonymousAuthRequired(routingDraftTmpl),
	}},
	{path: routingRevisions, allow: http.MethodPost, routed: map[string]routingCase{
		http.MethodPost: anonymousAuthRequired(routingRevisions),
	}},
	{path: routingWithdrawPath, allow: http.MethodPost, routed: map[string]routingCase{
		http.MethodPost: anonymousAuthRequired(routingWithdrawTmpl),
	}},
	{path: routingRevisionPath + "/revert", allow: http.MethodPost, routed: map[string]routingCase{
		http.MethodPost: anonymousAuthRequired("/v1/revisions/{revision_id}/revert"),
	}},
	{path: routingMyRevisions, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired(routingMyRevisions),
	}},
	{path: routingMyRevisions + "/" + routingCatalogueID, allow: http.MethodGet, routed: map[string]routingCase{
		http.MethodGet: anonymousAuthRequired("/v1/me/revisions/{revision_id}"),
	}},
}

// everyRouteAndMethod sends every method, anonymously, to every route: routed methods reach the route, OPTIONS
// is answered by CORS and every other method, HEAD included, gets 405.
func everyRouteAndMethod() []routingCase {
	var cases []routingCase
	for _, row := range slices.Concat(routeRows, publicRows) {
		for _, method := range everyMethod {
			tc, routed := row.routed[method]
			switch {
			case method == http.MethodOptions:
				tc = routingCase{status: http.StatusNoContent, route: unmatched}
			case !routed:
				tc = methodNotAllowed(row.allow)
			}
			tc.method, tc.target = method, row.path
			cases = append(cases, tc)
		}
	}
	return cases
}

func notFound(method, target, authorization string) routingCase {
	return routingCase{
		method: method, target: target, authorization: authorization,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: noRouteBody, route: unmatched,
	}
}

func methodNotAllowed(allow string) routingCase {
	return routingCase{
		status: http.StatusMethodNotAllowed, allow: allow, contentType: jsonContentType, respBody: notAllowedBody,
		route: unmatched,
	}
}

func notAllowedAt(method, target, authorization, allow string) routingCase {
	tc := methodNotAllowed(allow)
	tc.method, tc.target, tc.authorization = method, target, authorization
	return tc
}

func redirect(method, target, location string) routingCase {
	return routingCase{
		method: method, target: target, status: http.StatusMovedPermanently, location: location, route: unmatched,
	}
}

func blockAsModerator(target, wantUserID string) routingCase {
	return routingCase{
		method: http.MethodPut, target: target, authorization: routingModerator, body: blockedRequestBody,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: userNotFoundBody, authRan: true,
		route: blockedTemplate, pathValue: wantUserID,
	}
}

func revokeAsModerator(target string) routingCase {
	return routingCase{
		method: http.MethodDelete, target: target, authorization: routingModerator,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: tokenNotFoundBody, authRan: true,
		route: revokeTemplate,
	}
}

// unroutedCases are requests no route matches. None of them authenticates, so invalid credentials do not
// turn them into 401s.
func unroutedCases() []routingCase {
	return []routingCase{
		notFound(http.MethodGet, routingUnknownRoot, ""),
		notFound(http.MethodPost, routingUnknownRoot, routingInvalid),
		notFound(http.MethodGet, "/v1", ""),
		notFound(http.MethodGet, "/v1/", ""),
		notFound(http.MethodGet, routingUnknownV1, ""),
		notFound(http.MethodGet, routingUnknownV1, routingInvalid),
		notFound(http.MethodDelete, routingUnknownV1, routingInvalid),
		notFound(http.MethodGet, "/v1me", routingInvalid),
		notFound(http.MethodGet, "/v1/tokens/"+routingRevokeID+"/extra", routingInvalid),
		notAllowedAt(http.MethodDelete, routingMe, routingInvalid, "GET, PATCH"),
		notAllowedAt(http.MethodHead, routingMe, routingInvalid, "GET, PATCH"),
		notAllowedAt(http.MethodPut, routingTokens, routingInvalid, "GET, POST"),
		notAllowedAt(http.MethodGet, routingTokenPath, routingInvalid, "DELETE"),
		notAllowedAt(http.MethodGet, routingBlockedPath, routingInvalid, "PUT"),
		notAllowedAt(http.MethodPost, routingHealthz, routingInvalid, "GET"),
		notAllowedAt(http.MethodConnect, routingHealthz, "", "GET"),
		notAllowedAt("PROPFIND", routingHealthz, "", "GET"),
		// Trailing slashes are not cleaned away, and no route has one.
		notFound(http.MethodGet, "/healthz/", ""),
		notFound(http.MethodGet, "/v1/me/", routingInvalid),
		notFound(http.MethodGet, "/v1/tokens/", routingInvalid),
		notFound(http.MethodPost, "/v1/tokens/", routingInvalid),
		notFound(http.MethodDelete, "/v1/tokens/", routingInvalid),
		// Literal path segments match only as sent: an encoded letter is a different path.
		notFound(http.MethodGet, "/health%7A", ""),
		notFound(http.MethodGet, "/%68ealthz", ""),
		notFound(http.MethodGet, "/v1/%6De", routingInvalid),
		notFound(http.MethodGet, "/%761/me", routingInvalid),
		notFound(http.MethodPut, "/v1/users/auth0%7Cuser/bl%6Fcked", routingInvalid),
		notFound(http.MethodDelete, "/v1/tok%65ns/"+routingRevokeID, routingInvalid),
		// Around the revision routes.
		notFound(http.MethodGet, routingMyRevisions+"/", routingInvalid),
		notFound(http.MethodPost, routingDrafts+"/", routingInvalid),
		notFound(http.MethodGet, routingPieceEditable+"/", routingInvalid),
		notFound(http.MethodGet, routingPiecePath+"/edit%61ble", routingInvalid),
		notFound(http.MethodPost, routingWithdrawPath+"/extra", routingInvalid),
		notAllowedAt(http.MethodPost, routingPieceEditable, routingInvalid, http.MethodGet),
		notAllowedAt(http.MethodPatch, routingDraftPath, routingInvalid, "DELETE, GET, PUT"),
		notAllowedAt(http.MethodGet, routingWithdrawPath, routingInvalid, http.MethodPost),
	}
}

// cleaningCases are paths that are not in canonical form. They are redirected to the cleaned path, keeping the
// query, for every method and before any routing or authentication.
func cleaningCases() []routingCase {

	return []routingCase{
		redirect(http.MethodGet, "//healthz", routingHealthz),
		redirect(http.MethodGet, "//healthz?a=1&b=%2F", "/healthz?a=1&b=%2F"),
		redirect(http.MethodPost, "//healthz", routingHealthz),
		redirect(http.MethodHead, "//healthz", routingHealthz),
		redirect(http.MethodGet, "/./healthz", routingHealthz),
		redirect(http.MethodGet, "/v1/../healthz?x=1", "/healthz?x=1"),
		redirect(http.MethodGet, "/v1//me", routingMe),
		redirect(http.MethodGet, "/v1//me?q=%7C", "/v1/me?q=%7C"),
		redirect(http.MethodPatch, "/v1/./me", routingMe),
		redirect(http.MethodGet, "/v1/me//", "/v1/me/"),
		redirect(http.MethodGet, "/v1//unknown", routingUnknownV1),
		redirect(http.MethodGet, "/unknown/../..", "/"),
		redirect(http.MethodPut, "/v1/users//blocked", "/v1/users/blocked"),
		// The cleaned path is set as the URL's decoded path, so its escapes are escaped again.
		redirect(http.MethodPut, "/v1/users/a%2Fb//blocked?x=1", "/v1/users/a%252Fb/blocked?x=1"),
		redirect(http.MethodGet, "*", "/%2A"),
		// A CONNECT request in authority form has an empty path, which cleans to "/".
		redirect(http.MethodConnect, routingConnectHost, routingConnectRedir),
		redirect(http.MethodPut, "/v1/users//blocked/", "/v1/users/blocked/"),
		// gorilla/mux logged this redirect with the route its uncleaned path matched, user_id ".". The answer is
		// unchanged, but the log now says unmatched, as for every other redirect: no route serves it.
		{
			method: http.MethodPut, target: "/v1/users/./blocked", authorization: routingInvalid,
			status: http.StatusMovedPermanently, location: "/v1/users/blocked", route: unmatched,
		},
	}
}

// encodedIDCases send IDs with percent-encoded characters. Each path segment is decoded once: %2F stays inside
// its segment, and %257C reaches the controller as %7C.
func encodedIDCases() []routingCase {
	return []routingCase{
		blockAsModerator("/v1/users/auth0%7Cuser/blocked", "auth0|user"),
		blockAsModerator("/v1/users/auth0%7ca%2Fb/blocked", "auth0|a/b"),
		blockAsModerator("/v1/users/auth0%257Cuser/blocked", "auth0%7Cuser"),
		blockAsModerator("/v1/users/a%2E%2E/blocked", "a.."),
		blockAsModerator("/v1/users/%2E%2E/blocked", ".."),
		blockAsModerator("/v1/users/%2Fx/blocked", "/x"),
		// An ID that is exactly %2F decodes to "/", which ServeMux reads as an extra path segment, so no route
		// matches. gorilla/mux routed these to their controllers, so a moderator got 404 user or token not found
		// after authenticating, and anonymous or invalid-credential requests got 401. No user or token ID is "/",
		// and none of these requests could be authorised to act on one; all of them now get the unrouted 404,
		// without authenticating.
		notFound(http.MethodPut, "/v1/users/%2F/blocked", routingModerator),
		notFound(http.MethodDelete, "/v1/tokens/%2F", routingModerator),
		notFound(http.MethodDelete, "/v1/tokens/%2F", ""),
		notFound(http.MethodPut, "/v1/users/%2F/blocked", routingInvalid),
		revokeAsModerator("/v1/tokens/a%2Fb"),
		// Revision routes decode their slug or ID once, like the others; a placeholder slug is a slug.
		asModerator(http.MethodGet, "/v1/pieces/pending-piece-1%32/editable", routingPieceEditableTmpl,
			`{"code":"not_found","message":"piece not found"}`, routingPlaceholder),
		asModerator(http.MethodGet, "/v1/pieces/"+routingPlaceholder+"/pending", "/v1/pieces/{slug}/pending",
			`{"code":"not_found","message":"pending revision not found"}`, routingPlaceholder),
		asModerator(http.MethodPost, "/v1/revisions/4%32/withdraw", routingWithdrawTmpl,
			`{"code":"not_found","message":"revision not found"}`, routingCatalogueID),
		asModerator(http.MethodGet, routingDrafts+"/%342", routingDraftTmpl,
			`{"code":"not_found","message":"draft not found"}`, routingCatalogueID),
		notFound(http.MethodGet, "/v1/pieces/%2F/editable", routingModerator),
		notFound(http.MethodDelete, routingDrafts+"/%2F", routingModerator),
		notFound(http.MethodPost, "/v1/revisions/%2F/withdraw", ""),
		{
			method: http.MethodDelete, target: routingTokenPath, authorization: routingModerator,
			status: http.StatusNotFound, contentType: jsonContentType, respBody: tokenNotFoundBody, authRan: true,
			route: revokeTemplate, pathValue: routingRevokeID,
		},
		{
			method: http.MethodGet, target: routingMe, authorization: routingInvalid,
			status: http.StatusUnauthorized, contentType: jsonContentType, respBody: invalidCredsBody, authRan: true,
			route: routingMe,
		},
		{
			method: http.MethodGet, target: "/healthz?x=%7C", authorization: routingInvalid,
			status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingHealthz,
		},
	}
}

// TestRouter_Routing sends the routing corpus through New and checks every observable part of each answer.
func TestRouter_Routing(t *testing.T) {
	t.Parallel()

	var cases []routingCase
	cases = append(cases, everyRouteAndMethod()...)
	cases = append(cases, unroutedCases()...)
	cases = append(cases, cleaningCases()...)
	cases = append(cases, encodedIDCases()...)
	cases = append(cases, publicCases()...)

	for _, tc := range cases {
		name := tc.method + " " + tc.target
		if tc.authorization != "" {
			name += " as " + strings.TrimPrefix(tc.authorization, "Bearer ")
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec, fixture, route := serveRouting(t, tc)

			assert.Equal(t, tc.status, rec.Code, "status")
			assert.Equal(t, tc.allow, rec.Header().Get("Allow"), "Allow")
			assert.Equal(t, tc.location, rec.Header().Get("Location"), "Location")
			assert.Equal(t, tc.contentType, rec.Header().Get("Content-Type"), "Content-Type")
			assert.Equal(t, allRouteMethods, rec.Header().Get("Access-Control-Allow-Methods"),
				"Access-Control-Allow-Methods")
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "security headers")
			assert.Equal(t, tc.respBody, rec.Body.String(), "body")
			assert.Equal(t, tc.authRan, fixture.authRan, "whether authentication ran")
			assert.Equal(t, tc.pathValue, fixture.pathValue, "path value passed on")
			assert.Equal(t, tc.route, route, "logged route")
		})
	}
}

// serveRouting serves tc's request through New with a routing fixture, returning the response, the fixture and
// the route attribute of the request's completion log.
func serveRouting(t *testing.T, tc routingCase) (*httptest.ResponseRecorder, *routingFixture, string) {
	t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	req := httptest.NewRequestWithContext(domain.ContextWithLogger(t.Context(), logger), tc.method, tc.target,
		strings.NewReader(tc.body))
	if tc.body != "" {
		req.Header.Set("Content-Type", jsonContentType)
	}
	if tc.authorization != "" {
		req.Header.Set("Authorization", tc.authorization)
	}

	fixture := &routingFixture{}
	rec := httptest.NewRecorder()
	New(fixture.dependencies()).ServeHTTP(rec, req)

	var route any
	for line := range strings.Lines(logs.String()) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == "request completed" {
			route = entry["route"]
		}
	}
	routeString, ok := route.(string)
	require.True(t, ok, "the request completion log has a route")

	return rec, fixture, routeString
}

// asModerator is a request as a moderator to route, which answers 404 with body after its controller passed
// pathValue on.
func asModerator(method, target, route, body, pathValue string) routingCase {
	return routingCase{
		method: method, target: target, authorization: routingModerator,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: body + "\n", authRan: true,
		route: route, pathValue: pathValue,
	}
}
