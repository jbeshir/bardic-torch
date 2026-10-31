package controller

import (
	"net/http"
	"net/url"

	"github.com/gorilla/mux"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// principalOrUnauthenticated returns the request's principal. If the request is anonymous it writes 401
// unauthenticated and returns false; the caller should then return without writing. The router puts require*
// middleware in front of these controllers, so this only answers requests that reach one some other way.
func principalOrUnauthenticated(w http.ResponseWriter, r *http.Request) (domain.Principal, bool) {
	principal, ok := domain.PrincipalFromContext(r.Context())
	if !ok {
		httpjson.WriteError(r.Context(), w, http.StatusUnauthorized, httpjson.CodeUnauthenticated,
			"authentication required")
	}

	return principal, ok
}

// writeInternalError logs err with msg and writes 500 internal, keeping the details out of the response.
func writeInternalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	ctx := r.Context()
	domain.LoggerFromContext(ctx).ErrorContext(ctx, msg, "error", err)
	httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
}

// pathVarOrBadRequest returns the request's path variable name, percent-decoded: the router keeps paths encoded,
// so that values containing "/" stay in one segment, and every controller decodes them here. If the value is not
// validly encoded it writes 400 invalid_request and returns false; the caller should then return without
// writing. net/http rejects such request URIs before routing, so this only answers requests that reach a
// controller some other way.
func pathVarOrBadRequest(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value, err := url.PathUnescape(mux.Vars(r)[name])
	if err != nil {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid encoding of path parameter "+name)
		return "", false
	}

	return value, true
}
