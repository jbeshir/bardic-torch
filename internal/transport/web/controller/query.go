package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// queryOrBadRequest returns the request's query parameters. If the query string is not validly encoded it
// writes 400 invalid_request and returns false; the caller should then return without writing.
func queryOrBadRequest(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid encoding of query string")
		return nil, false
	}

	return query, true
}

// singleOrBadRequest returns the value of the query parameter name and whether it is set. If it is given more
// than once it writes 400 invalid_request and returns false; the caller should then return without writing.
func singleOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values, name string) (string, bool, bool) {
	values := query[name]
	switch len(values) {
	case 0:
		return "", false, true
	case 1:
		return values[0], true, true
	default:
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"query parameter "+name+" must be given at most once")
		return "", false, false
	}
}

// isDecimal returns whether s is written in decimal digits only, without a sign: ParseInt also accepts "+1".
func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// parseDecimal returns s as an integer if it is written in decimal digits only and is from lowest to highest.
func parseDecimal(s string, lowest, highest int64) (int64, bool) {
	if !isDecimal(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < lowest || n > highest {
		return 0, false
	}

	return n, true
}

// int32OrBadRequest returns the query parameter name as an integer from lowest to highest, or fallback if it is
// not set. If it is invalid it writes 400 invalid_request and returns false; the caller should then return
// without writing.
func int32OrBadRequest(
	w http.ResponseWriter, r *http.Request, query url.Values, name string, fallback, lowest, highest int32,
) (int32, bool) {
	value, set, ok := singleOrBadRequest(w, r, query, name)
	if !ok {
		return 0, false
	}
	if !set {
		return fallback, true
	}

	n, err := strconv.ParseInt(value, 10, 32)
	if err != nil || !isDecimal(value) || n < int64(lowest) || n > int64(highest) {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("invalid %s: must be an integer from %d to %d", name, lowest, highest))
		return 0, false
	}

	return int32(n), true
}

// pageOrBadRequest returns the page the limit and offset query parameters ask for. If either is invalid it
// writes 400 invalid_request and returns false; the caller should then return without writing.
func pageOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values) (domain.Page, bool) {
	limit, ok := int32OrBadRequest(w, r, query, "limit", domain.DefaultPageLimit, 1, domain.MaxPageLimit)
	if !ok {
		return domain.Page{}, false
	}
	offset, ok := int32OrBadRequest(w, r, query, "offset", 0, 0, domain.MaxPageOffset)
	if !ok {
		return domain.Page{}, false
	}

	return domain.Page{Limit: limit, Offset: offset}, true
}

// enumOrBadRequest returns the query parameter name parsed by parse, or nil if it is not set. If it is invalid
// it writes 400 invalid_request and returns false; the caller should then return without writing.
func enumOrBadRequest[E any](
	w http.ResponseWriter, r *http.Request, query url.Values, name string, parse func(string) (E, error),
) (*E, bool) {
	value, set, ok := singleOrBadRequest(w, r, query, name)
	if !ok || !set {
		return nil, ok
	}

	parsed, err := parse(value)
	if err != nil {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid "+name+": not one of its allowed values")
		return nil, false
	}

	return &parsed, true
}

// listOrBadRequest returns the values of the repeatable query parameter name, each checked by parse, in request
// order. If it is given more than maxItems times, or a value is invalid, it writes 400 invalid_request, naming
// the parameter and ending with invalid for an invalid value, and returns false; the caller should then return
// without writing.
func listOrBadRequest[E any](
	w http.ResponseWriter, r *http.Request, query url.Values, name string, maxItems int,
	parse func(string) (E, error), invalid string,
) ([]E, bool) {
	values := query[name]
	if len(values) > maxItems {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("query parameter %s must be given at most %d times", name, maxItems))
		return nil, false
	}

	var parsed []E
	for _, value := range values {
		p, err := parse(value)
		if err != nil {
			httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
				"invalid "+name+": "+invalid)
			return nil, false
		}
		parsed = append(parsed, p)
	}

	return parsed, true
}

// enumListOrBadRequest returns the values of the repeatable query parameter name, parsed by parse, in request
// order. If a value is invalid or repeated it writes 400 invalid_request and returns false; the caller should
// then return without writing. Distinct valid values are at most as many as the enum has, which is the spec's
// maxItems.
func enumListOrBadRequest[E comparable](
	w http.ResponseWriter, r *http.Request, query url.Values, name string, parse func(string) (E, error),
) ([]E, bool) {
	values, ok := listOrBadRequest(w, r, query, name, len(query[name]), parse, "not one of its allowed values")
	if !ok {
		return nil, false
	}

	seen := make(map[E]bool, len(values))
	for _, value := range values {
		if seen[value] {
			httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
				"query parameter "+name+" must not repeat a value")
			return nil, false
		}
		seen[value] = true
	}

	return values, true
}

// boolOrBadRequest returns the query parameter name, true or false, or nil if it is not set. If it is invalid it
// writes 400 invalid_request and returns false; the caller should then return without writing.
func boolOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values, name string) (*bool, bool) {
	value, set, ok := singleOrBadRequest(w, r, query, name)
	if !ok || !set {
		return nil, ok
	}

	switch value {
	case "true", "false":
		b := value == "true"
		return &b, true
	default:
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid "+name+": must be true or false")
		return nil, false
	}
}

// searchOrBadRequest returns the search query parameter q, or nil if it is not set. If it is invalid it writes
// 400 invalid_request and returns false; the caller should then return without writing.
func searchOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values) (*domain.SearchQuery, bool) {
	value, set, ok := singleOrBadRequest(w, r, query, "q")
	if !ok || !set {
		return nil, ok
	}

	search, err := domain.ParseSearchQuery(value)
	if err != nil {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "invalid q: "+err.Error())
		return nil, false
	}

	return &search, true
}

// sortOrBadRequest returns the sort query parameter parsed by parse, which defaults to relevance if the request
// has a search and to byKey otherwise. Relevance without a search is invalid. If the parameter is invalid it
// writes 400 invalid_request and returns false; the caller should then return without writing.
func sortOrBadRequest[S comparable](
	w http.ResponseWriter, r *http.Request, query url.Values, search *domain.SearchQuery,
	parse func(string) (S, error), byKey, relevance S,
) (S, bool) {
	var zero S
	sort, ok := enumOrBadRequest(w, r, query, "sort", parse)
	switch {
	case !ok:
		return zero, false
	case sort == nil && search == nil:
		return byKey, true
	case sort == nil:
		return relevance, true
	case *sort == relevance && search == nil:
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid sort: relevance needs a search query q")
		return zero, false
	default:
		return *sort, true
	}
}

// writeSearchError logs a failed search and writes 500 internal. A search the database stopped for exceeding
// its execution time limit is logged as a warning: an expensive request rather than a fault.
func writeSearchError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	if errors.Is(err, datasources.ErrQueryTimeout) {
		ctx := r.Context()
		domain.LoggerFromContext(ctx).WarnContext(ctx, msg, "error", err)
		httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
		return
	}

	WriteInternalError(w, r, msg, err)
}

// slugParamOrBadRequest returns the query parameter name, which must be a slug, or nil if it is not set. If it
// is invalid it writes 400 invalid_request and returns false; the caller should then return without writing.
func slugParamOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values, name string) (*string, bool) {
	value, set, ok := singleOrBadRequest(w, r, query, name)
	if !ok || !set {
		return nil, ok
	}
	if !domain.IsSlug(value) {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid "+name+": "+slugRule)
		return nil, false
	}

	return &value, true
}
