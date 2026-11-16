// Package openapitest checks HTTP traffic in tests against the API's OpenAPI specification, so that every
// handler test also proves the handler honours the contract.
//
// Typical use in a handler test:
//
//	contract := openapitest.New(t)
//	rec := contract.Serve(t, handler, req)
//	assert.Equal(t, http.StatusOK, rec.Code)
//
// Serve fails the test if the request or the response does not conform to the spec, including when the
// route, method, status code or content type is undocumented.
package openapitest

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/jbeshir/bardic-torch/openapi"
)

// Validator checks requests and responses against an OpenAPI specification. It is safe for concurrent use.
type Validator struct {
	doc    *openapi3.T
	router routers.Router
}

// Operation identifies an operation in the spec.
type Operation struct {
	Method string
	// Path is the path template, such as /v1/tokens/{token_id}.
	Path string
}

var embeddedSpecValidator = sync.OnceValues(func() (*Validator, error) {
	return newValidator(openapi.Spec)
})

// New returns a Validator for the API's embedded spec, failing the test if the spec is invalid. The spec is
// loaded once per test binary.
func New(t testing.TB) *Validator {
	t.Helper()

	v, err := embeddedSpecValidator()
	if err != nil {
		t.Fatalf("loading OpenAPI spec: %v", err)
	}

	return v
}

func newValidator(spec []byte) (*Validator, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(spec)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("validating spec: %w", err)
	}

	// Routes otherwise only match requests addressed to the spec's servers; tests send requests to any host.
	doc.Servers = nil

	// kin-openapi's gorillamux router matches the escaped path as the API's router does, so an encoded "/" or
	// "|" stays inside one path parameter and a trailing slash matches no operation. Its legacy router matches
	// the decoded path, and would not find an operation for /v1/users/auth0%7Ca%2Fb/blocked.
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		return nil, fmt.Errorf("building router: %w", err)
	}

	return &Validator{doc: doc, router: router}, nil
}

// Serve serves req with handler and returns the recorded response. It fails t unless the request and the
// response both conform to the spec. req's body is consumed; handler receives a copy of it.
func (v *Validator) Serve(t testing.TB, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	return v.serve(t, handler, req, true)
}

// ServeResponseOnly is Serve without request validation, for tests that deliberately send requests the spec
// does not allow, such as malformed bodies or unsupported content types. The response is still checked.
func (v *Validator) ServeResponseOnly(
	t testing.TB, handler http.Handler, req *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()
	return v.serve(t, handler, req, false)
}

// Public reports whether op is in the spec and documented as needing no authentication: its security is
// explicitly empty (security: []).
func (v *Validator) Public(op Operation) bool {
	item := v.doc.Paths.Value(op.Path)
	if item == nil {
		return false
	}
	operation := item.GetOperation(op.Method)

	return operation != nil && operation.Security != nil && len(*operation.Security) == 0
}

// Operations returns every operation in the spec, sorted by path and then method.
func (v *Validator) Operations() []Operation {
	var ops []Operation
	for path, item := range v.doc.Paths.Map() {
		for method := range item.Operations() {
			ops = append(ops, Operation{Method: method, Path: path})
		}
	}

	slices.SortFunc(ops, func(a, b Operation) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Method, b.Method))
	})
	return ops
}

func (v *Validator) serve(
	t testing.TB, handler http.Handler, req *http.Request, validateRequest bool,
) *httptest.ResponseRecorder {
	t.Helper()
	ctx := req.Context()

	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			t.Fatalf("reading request body: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, withBody(ctx, req, body))

	route, pathParams, err := v.router.FindRoute(req)
	if err != nil {
		t.Errorf("%s %s is not an operation in the OpenAPI spec: %v", req.Method, req.URL.Path, err)
		return rec
	}

	input := &openapi3filter.RequestValidationInput{
		Request:    withBody(ctx, req, body),
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	}

	if validateRequest {
		if err := openapi3filter.ValidateRequest(ctx, input); err != nil {
			t.Errorf("request %s %s does not conform to the OpenAPI spec: %v\nrequest body: %s",
				req.Method, req.URL.Path, err, body)
		}
	}

	checkResponse(t, input, rec)
	return rec
}

func checkResponse(t testing.TB, input *openapi3filter.RequestValidationInput, rec *httptest.ResponseRecorder) {
	t.Helper()
	ctx := input.Request.Context()

	responseInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: input,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			MultiError:            true,
		},
	}
	responseInput.SetBodyBytes(rec.Body.Bytes())

	if err := openapi3filter.ValidateResponse(ctx, responseInput); err != nil {
		t.Errorf("response %d to %s %s does not conform to the OpenAPI spec: %v\nresponse body: %s",
			rec.Code, input.Request.Method, input.Request.URL.Path, err, rec.Body.Bytes())
		return
	}

	// ValidateResponse skips the body when the response is documented without content.
	response := input.Route.Operation.Responses.Status(rec.Code)
	if response != nil && len(response.Value.Content) == 0 && rec.Body.Len() > 0 {
		t.Errorf("response %d to %s %s is documented without a body but has one: %s",
			rec.Code, input.Request.Method, input.Request.URL.Path, rec.Body.Bytes())
	}
}

// withBody returns a copy of req that reads body.
func withBody(ctx context.Context, req *http.Request, body []byte) *http.Request {
	clone := req.Clone(ctx)
	clone.Body = io.NopCloser(bytes.NewReader(body))
	return clone
}
