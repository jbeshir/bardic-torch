package controller

import (
	"cmp"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

// newEventRevisionBody is a submission of a new event named name.
func newEventRevisionBody(name string) string {
	return `{"entity_type":"event","base_revision_id":null,"summary":"","document":{"schema_version":1,` +
		`"name":"` + name + `","kind":"event","description_md":null},"acknowledge_duplicates":false,` +
		`"acknowledge_stale":false}`
}

// TestRevisionRoutes_Rules checks each rule the revision controllers apply, once, against the spec.
func TestRevisionRoutes_Rules(t *testing.T) {
	t.Parallel()

	now := func() time.Time { return testNow }
	for _, tc := range []struct {
		name     string
		handler  func(*fakerepo.Repo) http.Handler
		seed     func(*fakerepo.Repo)
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
			name: "pending_limit",
			handler: func(r *fakerepo.Repo) http.Handler {
				return RevisionCreate{Command: &command.SubmitRevision{Revisions: r, Now: now}}
			},
			seed:   func(r *fakerepo.Repo) { r.Err = fmt.Errorf("submitting: %w", datasources.ErrPendingLimit) },
			method: http.MethodPost, path: "/v1/revisions", body: newEventRevisionBody("Midwinter Revel"),
			wantCode: http.StatusConflict,
			wantBody: `{"code":"pending_limit_reached","message":"you already have 30 pending revisions"}`,
		},
		{
			name: "editable_showing_unlisted_person_is_for_moderators",
			handler: func(r *fakerepo.Repo) http.Handler {
				return EditableGet{Revisions: r, EntityType: domain.EntityTypePiece}
			},
			seed: func(r *fakerepo.Repo) {
				r.Editable = &datasources.Editable{BaseRevisionID: 42, ShowsUnlistedPerson: true,
					Document: domain.EditorPieceDocument{SchemaVersion: 1, Title: "The Copper Kettle"}}
			},
			method: http.MethodGet, path: "/v1/pieces/the-copper-kettle/editable", pattern: "/v1/pieces/{slug}/editable",
			wantCode: http.StatusForbidden,
			wantBody: `{"code":"moderator_only","message":"this document shows a person who prefers not to have a page; ` +
				`only a moderator may edit it"}`,
		},
		{
			name:    "stale_approval",
			handler: func(r *fakerepo.Repo) http.Handler { return RevisionApprove{Revisions: r, Now: now} },
			seed:    func(r *fakerepo.Repo) { r.Err = fmt.Errorf("approving: %w", datasources.ErrStale) },
			method:  http.MethodPost, path: "/v1/revisions/51/approve", pattern: "/v1/revisions/{revision_id}/approve",
			body: `{"acknowledge_stale":false}`, wantCode: http.StatusConflict,
			wantBody: `{"code":"stale_revision",` +
				`"message":"the entity has changed since the revision's base; acknowledge_stale to overwrite the changes"}`,
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
			if tc.seed != nil {
				tc.seed(repo)
			}
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
