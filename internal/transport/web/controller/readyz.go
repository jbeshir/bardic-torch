package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// readyzPingTimeout bounds how long a readiness check waits for the database.
const readyzPingTimeout = 2 * time.Second

// Readyz handles GET /readyz, reporting whether the database is reachable.
type Readyz struct {
	Pinger datasources.Pinger
}

func (c Readyz) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pingCtx, cancel := context.WithTimeout(ctx, readyzPingTimeout)
	defer cancel()

	if err := c.Pinger.Ping(pingCtx); err != nil {
		domain.LoggerFromContext(ctx).WarnContext(ctx, "readiness check failed", "error", err)
		httpjson.WriteError(ctx, w, http.StatusServiceUnavailable, httpjson.CodeUnavailable, "database unavailable")
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, healthy)
}
