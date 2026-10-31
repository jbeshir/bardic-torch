package app

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
)

// HTTPDependencies are the datasources the HTTP handler is built from.
type HTTPDependencies struct {
	Pinger datasources.Pinger
}

// NewHTTPHandler builds the production HTTP handler from deps. Setup and tests both use it, so tests exercise
// the same wiring as production.
func NewHTTPHandler(deps HTTPDependencies) http.Handler {
	return router.New(deps.Pinger)
}
