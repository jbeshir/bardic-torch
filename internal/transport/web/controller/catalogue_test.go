package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
)

const invalidSlugBody = `{"code":"invalid_request","message":"invalid slug: must be lowercase ASCII letters and ` +
	`digits, in runs joined by single hyphens, at most 191 characters"}`

// invalidSlugCase is a slug path value, as the router passes it on, decoded, that a catalogue controller rejects
// with wantBody without reading the datasource.
type invalidSlugCase struct {
	name     string
	pathSlug string
	wantBody string
}

var invalidSlugCases = []invalidSlugCase{
	{name: "upper_case", pathSlug: "Copper-kettle", wantBody: invalidSlugBody},
	{name: "double_hyphen", pathSlug: "copper--kettle", wantBody: invalidSlugBody},
	{name: "trailing_hyphen", pathSlug: "copper-", wantBody: invalidSlugBody},
	{name: "underscore", pathSlug: "copper_kettle", wantBody: invalidSlugBody},
	// The router decodes path values before the check, so a space sent as %20 arrives as a space.
	{name: "space", pathSlug: "copper kettle", wantBody: invalidSlugBody},
	{name: "non_ascii", pathSlug: "café", wantBody: invalidSlugBody},
	{name: "too_long", pathSlug: strings.Repeat("a", 192), wantBody: invalidSlugBody},
}

// serveSlug serves a GET of /v1/collection/{slug} with handler, with the slug path value set to pathSlug as the
// router would pass it on. The request is validated against the spec too unless it is one of invalidSlugCases,
// which the spec forbids; the response always is.
func serveSlug(t *testing.T, handler http.Handler, collection, pathSlug string, invalid bool) *httptest.ResponseRecorder {
	t.Helper()

	// The request URL must parse whatever the value holds; the controller reads only the path value.
	return serveGet(t, handler, "/v1/"+collection+"/"+url.PathEscape(pathSlug), map[string]string{slugVar: pathSlug},
		invalid)
}

// serveGet serves a GET of target with handler, with the path values vars set as the router would pass them on,
// decoded. The request is validated against the spec too unless invalid, for requests the spec forbids; the
// response always is.
func serveGet(
	t *testing.T, handler http.Handler, target string, vars map[string]string, invalid bool,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	for name, value := range vars {
		req.SetPathValue(name, value)
	}

	if invalid {
		return openapitest.New(t).ServeResponseOnly(t, handler, req)
	}

	return openapitest.New(t).Serve(t, handler, req)
}

// invalidQueryCase is a query string that a list controller rejects with wantBody without reading the
// datasource.
type invalidQueryCase struct {
	name     string
	query    string
	wantBody string
}

const invalidQueryEncodingBody = `{"code":"invalid_request","message":"invalid encoding of query string"}`

// invalidPageCases are the invalid limit and offset parameters every paginated controller rejects.
var invalidPageCases = []invalidQueryCase{
	{name: "limit_zero", query: "limit=0", wantBody: invalidLimitBody},
	{name: "limit_too_large", query: "limit=101", wantBody: invalidLimitBody},
	{name: "limit_empty", query: "limit=", wantBody: invalidLimitBody},
	{name: "limit_not_integer", query: "limit=ten", wantBody: invalidLimitBody},
	{name: "limit_signed", query: "limit=%2B5", wantBody: invalidLimitBody},
	{name: "limit_fraction", query: "limit=2.5", wantBody: invalidLimitBody},
	{name: "limit_overflow", query: "limit=99999999999999999999", wantBody: invalidLimitBody},
	{
		name: "limit_repeated", query: "limit=5&limit=6",
		wantBody: `{"code":"invalid_request","message":"query parameter limit must be given at most once"}`,
	},
	{name: "offset_negative", query: "offset=-1", wantBody: invalidOffsetBody},
	{name: "offset_too_large", query: "offset=10001", wantBody: invalidOffsetBody},
	{name: "bad_encoding", query: "offset=%zz", wantBody: invalidQueryEncodingBody},
}

const (
	invalidLimitBody  = `{"code":"invalid_request","message":"invalid limit: must be an integer from 1 to 100"}`
	invalidOffsetBody = `{"code":"invalid_request","message":"invalid offset: must be an integer from 0 to 10000"}`
)

// widestPage is the largest limit and offset a request may ask for.
var widestPage = domain.Page{Limit: domain.MaxPageLimit, Offset: domain.MaxPageOffset}

// invalidIDCase is an ID path value, as the router passes it on, decoded, that a controller rejects with wantBody
// without reading the datasource.
type invalidIDCase struct {
	name     string
	pathID   string
	wantBody string
}

// invalidIDCases are the invalid values of the ID path value name.
func invalidIDCases(name string) []invalidIDCase {
	body := `{"code":"invalid_request","message":"invalid ` + name + `: must be an integer from 1 to ` +
		`9007199254740991"}`
	return []invalidIDCase{
		{name: "zero", pathID: "0", wantBody: body},
		{name: "negative", pathID: "-1", wantBody: body},
		{name: "signed", pathID: "+1", wantBody: body},
		{name: "fraction", pathID: "1.5", wantBody: body},
		{name: "not_integer", pathID: "songbook", wantBody: body},
		{name: "above_largest_safe_integer", pathID: "9007199254740992", wantBody: body},
		{name: "overflow", pathID: "9223372036854775808", wantBody: body},
	}
}
