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
	Pinger datasources.Pinger
	// AuthValidators authenticate requests to authenticated routes; with none, every request is anonymous.
	AuthValidators    []AuthValidator
	ResolveUser       command.Command[string, domain.User]
	UpdateDisplayName command.Command[command.UpdateDisplayNameRequest, domain.User]
	SetUserBlocked    command.Command[command.SetUserBlockedRequest, domain.User]
	CreateAPIToken    command.Command[command.CreateAPITokenRequest, command.CreateAPITokenResponse]
	APITokenLister    datasources.ActiveAPITokenLister
	APITokenRevoker   datasources.APITokenRevoker
	Now               func() time.Time
}

// route is one operation the router serves.
type route struct {
	method string
	// path is a net/http.ServeMux path pattern, such as /v1/users/{user_id}/blocked. It must not end in "/" or
	// contain a {name...} wildcard: newRouter panics if it does.
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
		{method: http.MethodGet, path: "/readyz", handler: controller.Readyz{Pinger: deps.Pinger}},

		{method: http.MethodGet, path: "/v1/me", handler: requireAuth(controller.MeGet{}), authenticated: true},
		{method: http.MethodPatch, path: "/v1/me", authenticated: true, handler: requireAuth(controller.MePatch{
			Command: deps.UpdateDisplayName,
		})},

		// Moderation. User IDs contain "|", which clients send percent-encoded as %7C.
		{method: http.MethodPut, path: "/v1/users/{user_id}/blocked", authenticated: true,
			handler: requireModerator(controller.UserBlockedSet{Command: deps.SetUserBlocked})},

		// API tokens cannot manage API tokens, so a leaked token cannot be used to mint or revoke others.
		{method: http.MethodPost, path: "/v1/tokens", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenCreate{Command: deps.CreateAPIToken})},
		{method: http.MethodGet, path: "/v1/tokens", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenList{Lister: deps.APITokenLister})},
		{method: http.MethodDelete, path: "/v1/tokens/{token_id}", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenRevoke{Revoker: deps.APITokenRevoker, Now: deps.Now})},
	}
}

// New returns the handler for every route. Each request passes through the request logger, then the security
// headers, then CORS, which answers preflight requests itself, before any authentication.
//
// Authenticated routes pass through the auth middleware and then the require* middleware for their route; the
// health checks are not authenticated. Requests no route matches get a JSON 404, or 405 if a route matches the
// path but not the method, without authenticating.
func New(deps Dependencies) http.Handler {
	r := newRouter(routes(deps), authMiddleware(deps.AuthValidators, deps.ResolveUser))
	return requestLogger(r.lookup)(securityHeaders(cors(strings.Join(r.methods, ", "))(r)))
}
