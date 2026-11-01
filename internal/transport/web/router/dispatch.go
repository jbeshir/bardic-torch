package router

import (
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// router serves a route table on a net/http.ServeMux, keeping the routing rules the API has always had:
//   - a path that is not in canonical form is redirected to its cleaned form, for every method;
//   - a route matches only its own method, so HEAD is not routed to GET routes;
//   - literal path segments match only as sent, so an encoded letter (/health%7A) is a different path, while
//     wildcards take one segment, decoded once, so %2F and %7C stay inside the segment's value;
//   - a request no route matches gets a JSON 404 or 405, without authenticating.
//
// The ServeMux only routes requests lookup has matched, so it never writes its own 404, 405 or redirect.
type router struct {
	mux *http.ServeMux
	// methods are the methods of every route, sorted and without duplicates.
	methods []string
}

// newRouter registers every route on a new ServeMux, wrapping authenticated routes in authenticate.
func newRouter(routes []route, authenticate func(http.Handler) http.Handler) *router {
	mux := http.NewServeMux()
	for _, rt := range routes {
		if strings.HasSuffix(rt.path, "/") || strings.Contains(rt.path, "...}") {
			panic(fmt.Sprintf("route %s %s: paths must not end in / or contain {name...}", rt.method, rt.path))
		}

		handler := rt.handler
		if rt.authenticated {
			handler = authenticate(handler)
		}
		mux.Handle(rt.method+" "+rt.path, handler)
	}

	return &router{mux: mux, methods: routeMethods(routes)}
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	escaped := r.URL.EscapedPath()
	if cleaned := cleanPath(escaped); cleaned != escaped {
		redirectToCleanPath(w, r, cleaned)
		return
	}

	if _, ok := rt.lookup(r); ok {
		// ServeHTTP, unlike the handler lookup returns, sets the request's Pattern and PathValue.
		rt.mux.ServeHTTP(w, r)
		return
	}

	rt.serveUnrouted(w, r)
}

// lookup returns the path template of the route r is routed to, or false if no route matches it.
func (rt *router) lookup(r *http.Request) (string, bool) {
	escaped := r.URL.EscapedPath()
	if cleanPath(escaped) != escaped {
		return "", false
	}

	// The ServeMux matches HEAD requests to GET routes, and decodes literal segments before comparing them, so
	// its match is checked against the method and the path as sent.
	_, pattern := rt.mux.Handler(r)
	method, template, ok := strings.Cut(pattern, " ")
	if !ok || method != r.Method || !literalSegmentsMatch(template, escaped) {
		return "", false
	}

	return template, true
}

// literalSegmentsMatch reports whether escapedPath has as many segments as template, and every segment of
// template other than a wildcard equals the corresponding segment of escapedPath.
func literalSegmentsMatch(template, escapedPath string) bool {
	templateSegments := strings.Split(template, "/")
	pathSegments := strings.Split(escapedPath, "/")
	if len(templateSegments) != len(pathSegments) {
		return false
	}

	for i, segment := range templateSegments {
		if !strings.HasPrefix(segment, "{") && segment != pathSegments[i] {
			return false
		}
	}

	return true
}

// serveUnrouted answers a request no route matches. If a route matches the path with another method, the answer
// is 405 with those methods in Allow; otherwise it is 404.
func (rt *router) serveUnrouted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var allowed []string
	for _, method := range rt.methods {
		probe := r.WithContext(ctx)
		probe.Method = method
		if _, ok := rt.lookup(probe); ok {
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
}

// cleanPath returns the canonical form of the escaped path p: rooted, with no empty, . or .. segments, and with
// a trailing slash only if p has one.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}

	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}

	return cleaned
}

// redirectToCleanPath answers 301 with no body, redirecting to the escaped path cleaned with the request's
// query. As the cleaned path is set as the URL's decoded path, the Location escapes its escapes again, so
// /v1/users/a%2Fb//blocked redirects to /v1/users/a%252Fb/blocked.
func redirectToCleanPath(w http.ResponseWriter, r *http.Request, cleaned string) {
	location := *r.URL
	location.Path = cleaned
	w.Header().Set("Location", location.String())
	w.WriteHeader(http.StatusMovedPermanently)
}
