package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Dependencies are what the routes are built from.
type Dependencies struct {
	Pinger datasources.Pinger
	// AuthValidators authenticate /v1 requests; with none, every request is anonymous.
	AuthValidators    []AuthValidator
	ResolveUser       command.Command[string, domain.User]
	UpdateDisplayName command.Command[command.UpdateDisplayNameRequest, domain.User]
	SetUserBlocked    command.Command[command.SetUserBlockedRequest, domain.User]
	CreateAPIToken    command.Command[command.CreateAPITokenRequest, command.CreateAPITokenResponse]
	APITokenLister    datasources.ActiveAPITokenLister
	APITokenRevoker   datasources.APITokenRevoker
	Now               func() time.Time
}

// New returns the handler for every route. Each request passes through the request logger, then the security
// headers, then CORS, which answers preflight requests itself, before any authentication.
//
// Health checks are registered directly on the root router so that no authentication applies to them; they
// answer whatever credentials a request carries. Every /v1 route passes through the auth middleware, and then
// the require* middleware for its route. Requests no route matches get a JSON 404, or 405 if a route matches
// the path but not the method, without authenticating.
func New(deps Dependencies) http.Handler {
	routes := newRoutes(deps)
	return requestLogger(routes)(securityHeaders(cors(strings.Join(routeMethods(routes), ", "))(routes)))
}

// newRoutes registers every route, behind its authentication and authorisation, on a new router.
func newRoutes(deps Dependencies) *mux.Router {
	// Encoded paths keep percent-encoded characters, such as %2F, inside a single path variable.
	routes := mux.NewRouter().UseEncodedPath()

	routes.Handle("/healthz", controller.Healthz{}).Methods(http.MethodGet)
	routes.Handle("/readyz", controller.Readyz{Pinger: deps.Pinger}).Methods(http.MethodGet)

	v1 := routes.PathPrefix("/v1").Subrouter()
	v1.Use(authMiddleware(deps.AuthValidators, deps.ResolveUser))

	v1.Handle("/me", requireAuth(controller.MeGet{})).Methods(http.MethodGet)
	v1.Handle("/me", requireAuth(controller.MePatch{
		Command: deps.UpdateDisplayName,
	})).Methods(http.MethodPatch)

	// Moderation. User IDs contain "|", which clients send percent-encoded as %7C.
	v1.Handle("/users/{user_id}/blocked", requireModerator(controller.UserBlockedSet{
		Command: deps.SetUserBlocked,
	})).Methods(http.MethodPut)

	// API tokens cannot manage API tokens, so a leaked token cannot be used to mint or revoke others.
	v1.Handle("/tokens", requireNonAPIToken(controller.APITokenCreate{
		Command: deps.CreateAPIToken,
	})).Methods(http.MethodPost)
	v1.Handle("/tokens", requireNonAPIToken(controller.APITokenList{
		Lister: deps.APITokenLister,
	})).Methods(http.MethodGet)
	v1.Handle("/tokens/{token_id}", requireNonAPIToken(controller.APITokenRevoke{
		Revoker: deps.APITokenRevoker,
		Now:     deps.Now,
	})).Methods(http.MethodDelete)

	// Both routers need the handler, as the /v1 subrouter answers for every path under /v1. It runs without the
	// subrouter's middleware, so requests no route matches are not authenticated.
	unrouted := unroutedHandler(routes, routeMethods(routes))
	for _, r := range []*mux.Router{routes, v1} {
		r.NotFoundHandler = unrouted
		r.MethodNotAllowedHandler = unrouted
	}

	return routes
}

// unroutedHandler answers requests no route of routes matches. If a route matches the path with another of
// methods, the answer is 405 with those methods in Allow; otherwise it is 404.
//
// It checks the methods itself because mux reports a wrong method on a /v1 path as not found whenever a later
// /v1 route does not match the path.
func unroutedHandler(routes *mux.Router, methods []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var allowed []string
		for _, method := range methods {
			probe := r.WithContext(ctx)
			probe.Method = method
			var match mux.RouteMatch
			if routes.Match(probe, &match) && match.MatchErr == nil {
				allowed = append(allowed, method)
			}
		}

		if len(allowed) == 0 {
			httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "no route matches the path")
			return
		}

		w.Header().Set("Allow", strings.Join(allowed, ", "))
		httpjson.WriteError(ctx, w, http.StatusMethodNotAllowed, httpjson.CodeMethodNotAllowed,
			"method not allowed for this path")
	})
}
