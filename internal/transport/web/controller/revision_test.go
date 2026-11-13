package controller

import (
	"cmp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

// newEventRevisionBody is a submission of a new event named name.
func newEventRevisionBody(name string) string {
	return `{"entity_type":"event","base_revision_id":null,"summary":"","document":{"schema_version":1,` +
		`"name":"` + name + `"},"acknowledge_duplicates":false,` +
		`"acknowledge_stale":false}`
}

// TestRevisionRoutes_Rules checks each rule the revision controllers apply, once, against the spec.
func TestRevisionRoutes_Rules(t *testing.T) {
	t.Parallel()

	now := func() time.Time { return testNow }
	for _, tc := range []struct {
		name     string
		handler  func(*fakerepo.Repo) http.Handler
		method   string
		path     string
		pattern  string
		body     string
		wantCode int
		wantBody string
	}{
		{
			name: "invalid_document_lists_problems",
			handler: func(r *fakerepo.Repo) http.Handler {
				return RevisionCreate{Command: &command.SubmitRevision{Revisions: r, Now: now}}
			},
			method: http.MethodPost, path: "/v1/revisions", body: newEventRevisionBody(" "),
			wantCode: http.StatusUnprocessableEntity,
			wantBody: `{"code":"validation_failed","message":"the document does not validate",` +
				`"problems":[{"path":"name","code":"required"}]}`,
		},
		{
			name:    "rejection_needs_a_note",
			handler: func(r *fakerepo.Repo) http.Handler { return RevisionReject{Revisions: r, Now: now} },
			method:  http.MethodPost, path: "/v1/revisions/51/reject", pattern: "/v1/revisions/{revision_id}/reject",
			body: `{"note":"  "}`, wantCode: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"note is required, at most 2000 characters"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakerepo.Repo{}
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			mux := http.NewServeMux()
			mux.Handle(tc.method+" "+cmp.Or(tc.pattern, tc.path), tc.handler(repo))
			// The blank note breaks the request schema on purpose, so only responses are checked.
			rec := openapitest.New(t).ServeResponseOnly(t, mux, asPrincipal(req))

			assert.Equal(t, tc.wantCode, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
