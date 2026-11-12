package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
)

// Dependencies are what the routes are built from.
type Dependencies struct {
	Repository datasources.Repository
	// AuthValidators authenticate requests to authenticated routes; with none, every request is anonymous.
	AuthValidators    []AuthValidator
	ResolveUser       command.Command[string, domain.User]
	UpdateDisplayName command.Command[command.UpdateDisplayNameRequest, domain.User]
	SetUserBlocked    command.Command[command.SetUserBlockedRequest, domain.User]

	// Now is the clock the moderation routes stamp their writes with.
	Now                  func() time.Time
	CreateContactRequest command.Command[command.CreateContactRequestRequest, int64]
	SubmitRevision       command.Command[command.SubmitRevisionRequest, domain.RevisionView]
}

// route is one operation the router serves.
type route struct {
	method string
	// path is a net/http.ServeMux path pattern, such as /v1/users/{user_id}/blocked. It must start but not end in
	// "/", and must not contain a {name...} wildcard; newRouter panics if it does, or if method is empty.
	path    string
	handler http.Handler
	// authenticated routes run behind the auth middleware, which sets the request's principal; handler then
	// applies the route's require* middleware. Other routes answer whatever credentials a request carries.
	authenticated bool
}

// routes returns every route the API serves. Each method and path pair is a separate route.
func routes(deps Dependencies) []route {
	return []route{
		{method: http.MethodGet, path: "/healthz", handler: controller.Healthz{}},
		{method: http.MethodGet, path: "/readyz", handler: controller.Readyz{Pinger: deps.Repository}},

		// Public catalogue reads. The ServeMux routes a request to the most specific matching pattern, whatever the
		// order of this table, so a later authenticated route with a literal segment where these have a wildcard,
		// such as GET /v1/pieces/mine or /v1/revisions/pending, is routed to its own handler and not answered by the
		// public {slug} or {revision_id} route. Two patterns that overlap with neither more specific than the other,
		// such as /v1/pieces/{slug} and /v1/{collection}/mine, make newRouter panic. The literal segment must be
		// sent as is: one sent with an encoded letter, such as /v1/pieces/min%65, would match no route.
		// TestRoutes fails for an authenticated operation a public route answers.
		{method: http.MethodGet, path: "/v1/pieces", handler: controller.PieceListGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/pieces/{slug}", handler: controller.PieceGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/persons",
			handler: controller.PersonListGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/persons/{slug}",
			handler: controller.PersonGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/terms", handler: controller.TermListGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/terms/{slug}", handler: controller.TermGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/collections",
			handler: controller.CollectionListGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}",
			handler: controller.CollectionGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/events", handler: controller.EventListGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/events/{slug}", handler: controller.EventGet{Catalogue: deps.Repository}},
		// History, of the entities addressed by slug and of collections.
		{method: http.MethodGet, path: "/v1/pieces/{slug}/history", handler: controller.HistoryGet{
			Catalogue: deps.Repository, EntityType: domain.SlugEntityTypePiece,
		}},
		{method: http.MethodGet, path: "/v1/persons/{slug}/history", handler: controller.HistoryGet{
			Catalogue: deps.Repository, EntityType: domain.SlugEntityTypePerson,
		}},
		{method: http.MethodGet, path: "/v1/terms/{slug}/history", handler: controller.HistoryGet{
			Catalogue: deps.Repository, EntityType: domain.SlugEntityTypeTerm,
		}},
		{method: http.MethodGet, path: "/v1/events/{slug}/history", handler: controller.HistoryGet{
			Catalogue: deps.Repository, EntityType: domain.SlugEntityTypeEvent,
		}},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}/history",
			handler: controller.CollectionHistoryGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/revisions/{revision_id}",
			handler: controller.RevisionGet{Catalogue: deps.Repository}},
		{method: http.MethodGet, path: "/v1/needs-work",
			handler: controller.NeedsWorkListGet{Catalogue: deps.Repository}},

		{method: http.MethodGet, path: "/v1/me", handler: requireAuth(controller.MeGet{}), authenticated: true},
		{method: http.MethodPatch, path: "/v1/me", authenticated: true, handler: requireAuth(controller.MePatch{
			Command: deps.UpdateDisplayName,
		})},

		// Moderation. User IDs contain "|", which clients send percent-encoded as %7C.
		{method: http.MethodPut, path: "/v1/users/{user_id}/blocked", authenticated: true,
			handler: requireModerator(controller.UserBlockedSet{Command: deps.SetUserBlocked})},

		// Link takedown and the contact form.
		{method: http.MethodGet, path: "/v1/moderation/links", authenticated: true,
			handler: requireModerator(controller.ModerationLinkGet{Moderation: deps.Repository})},
		{method: http.MethodPut, path: "/v1/links/{link_id}/hidden", authenticated: true,
			handler: requireModerator(controller.LinkHiddenSet{Moderation: deps.Repository})},
		{method: http.MethodPost, path: "/v1/contact",
			handler: controller.ContactCreate{Command: deps.CreateContactRequest}},
		{method: http.MethodGet, path: "/v1/moderation/contact-requests", authenticated: true,
			handler: requireModerator(controller.ContactRequestListGet{Moderation: deps.Repository})},
		{method: http.MethodPost, path: "/v1/moderation/contact-requests/{request_id}", authenticated: true,
			handler: requireModerator(controller.ContactRequestClose{Moderation: deps.Repository, Now: deps.Now})},

		// Revisions: the caller's own, editing and submitting, and moderating them.
		{method: http.MethodGet, path: "/v1/me/revisions", authenticated: true,
			handler: requireAuth(controller.MyRevisionListGet{Revisions: deps.Repository})},
		{method: http.MethodGet, path: "/v1/me/revisions/{revision_id}", authenticated: true,
			handler: requireAuth(controller.MyRevisionGet{Revisions: deps.Repository})},
		{method: http.MethodGet, path: "/v1/pieces/{slug}/editable", authenticated: true, handler: requireAuth(
			controller.EditableGet{Revisions: deps.Repository, EntityType: domain.EntityTypePiece})},
		{method: http.MethodGet, path: "/v1/persons/{slug}/editable", authenticated: true, handler: requireAuth(
			controller.EditableGet{Revisions: deps.Repository, EntityType: domain.EntityTypePerson})},
		{method: http.MethodGet, path: "/v1/terms/{slug}/editable", authenticated: true, handler: requireAuth(
			controller.EditableGet{Revisions: deps.Repository, EntityType: domain.EntityTypeTerm})},
		{method: http.MethodGet, path: "/v1/events/{slug}/editable", authenticated: true, handler: requireAuth(
			controller.EditableGet{Revisions: deps.Repository, EntityType: domain.EntityTypeEvent})},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}/editable", authenticated: true,
			handler: requireAuth(controller.EditableGet{Revisions: deps.Repository,
				EntityType: domain.EntityTypeCollection})},
		{method: http.MethodPost, path: "/v1/revisions", authenticated: true,
			handler: requireAuth(controller.RevisionCreate{Command: deps.SubmitRevision})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/withdraw", authenticated: true,
			handler: requireAuth(controller.RevisionWithdraw{Revisions: deps.Repository})},
		{method: http.MethodGet, path: "/v1/moderation/queue", authenticated: true,
			handler: requireModerator(controller.ModerationQueueGet{Revisions: deps.Repository})},
		{method: http.MethodGet, path: "/v1/moderation/revisions/{revision_id}", authenticated: true,
			handler: requireModerator(controller.ModerationRevisionGet{Revisions: deps.Repository})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/approve", authenticated: true,
			handler: requireModerator(controller.RevisionApprove{Revisions: deps.Repository, Now: deps.Now})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/reject", authenticated: true,
			handler: requireModerator(controller.RevisionReject{Revisions: deps.Repository, Now: deps.Now})},
	}
}

// New returns the handler for every route. Each request passes through the request logger, then the security
// headers, then CORS, which answers preflight requests itself, before any authentication.
//
// Authenticated routes pass through the auth middleware and then the require* middleware for their route. The
// health checks and the public catalogue reads are not authenticated: they answer whatever credentials a request
// carries, as they would an anonymous request, so a client with an expired token, or a blocked user, can still
// read the catalogue. Requests no route matches get a JSON 404, or 405 if a route matches the path but not the
// method, without authenticating.
func New(deps Dependencies) http.Handler {
	r := newRouter(routes(deps), authMiddleware(deps.AuthValidators, deps.ResolveUser))
	return requestLogger(r.lookup)(securityHeaders(cors(strings.Join(r.methods, ", "))(r)))
}
