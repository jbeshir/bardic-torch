package router

import (
	"net/http"
	"slices"

	"github.com/gorilla/mux"
)

// cors allows cross-origin requests from any origin, answering every preflight (OPTIONS) request with 204
// before it reaches authentication. allowedMethods is the Access-Control-Allow-Methods value.
func cors(allowedMethods string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// routeMethods returns the methods of every route registered on routes, sorted and without duplicates.
func routeMethods(routes *mux.Router) []string {
	var methods []string
	_ = routes.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		routeMethods, err := route.GetMethods()
		if err == nil {
			methods = append(methods, routeMethods...)
		}
		return nil
	})

	slices.Sort(methods)
	return slices.Compact(methods)
}
