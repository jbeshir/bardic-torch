package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// HealthStatus is the JSON body of a successful health check.
type HealthStatus struct {
	Status string `json:"status"`
}

var healthy = HealthStatus{Status: "ok"}

// Healthz handles GET /healthz, reporting that the process is serving requests.
type Healthz struct{}

func (Healthz) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, healthy)
}
