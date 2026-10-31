package router

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
)

// New returns the handler for every route. Each request passes through the request logger, then the security
// headers, then CORS, which answers preflight requests itself.
//
// Health checks are registered directly on the root router so that no authentication applies to them; they
// answer whatever credentials a request carries.
func New(pinger datasources.Pinger) http.Handler {
	// Encoded paths keep percent-encoded characters, such as %2F, inside a single path variable.
	routes := mux.NewRouter().UseEncodedPath()

	routes.Handle("/healthz", controller.Healthz{}).Methods(http.MethodGet)
	routes.Handle("/readyz", controller.Readyz{Pinger: pinger}).Methods(http.MethodGet)

	return requestLogger(routes)(securityHeaders(cors(routeMethods(routes))(routes)))
}
