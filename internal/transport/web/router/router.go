package router

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
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
// the require* middleware for its route.
func New(deps Dependencies) http.Handler {
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

	return requestLogger(routes)(securityHeaders(cors(routeMethods(routes))(routes)))
}
