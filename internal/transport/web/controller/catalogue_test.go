package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/v1/"+collection+"/"+url.PathEscape(pathSlug), nil)
	req.SetPathValue("slug", pathSlug)

	if invalid {
		return openapitest.New(t).ServeResponseOnly(t, handler, req)
	}

	return openapitest.New(t).Serve(t, handler, req)
}
