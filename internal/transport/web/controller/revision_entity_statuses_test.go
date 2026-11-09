package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// TestEditableAndPendingGet_StatusesOfEveryEntityType serves the answers of the editable and pending reads of
// each entity type through its own operation of the spec: the 400 of an invalid slug or collection ID, and the 404
// of an unknown entity. TestEditableGet_ServeHTTP and TestPendingGet_ServeHTTP serve the other statuses, with
// events.
func TestEditableAndPendingGet_StatusesOfEveryEntityType(t *testing.T) {
	t.Parallel()

	slugTypes := []domain.EntityType{domain.EntityTypePiece, domain.EntityTypePerson, domain.EntityTypeTerm}
	type entityCase struct {
		name       string
		entityType domain.EntityType
		suffix     string
		pathValues map[string]string
		target     string
		badTarget  string
		badValues  map[string]string
		badBody    string
		notFound   string
	}
	var cases []entityCase
	for _, suffix := range []string{"editable", "pending"} {
		message := func(entityType domain.EntityType) string {
			if suffix == "pending" {
				return "pending revision not found"
			}
			return string(entityType) + " not found"
		}
		for _, entityType := range slugTypes {
			// The path collection of a type is its name in the plural.
			plural := string(entityType) + "s"
			cases = append(cases, entityCase{
				name: suffix + "_" + string(entityType), entityType: entityType, suffix: suffix,
				pathValues: map[string]string{slugVar: testPieceSlug},
				target:     "/v1/" + plural + "/" + testPieceSlug + "/" + suffix,
				badTarget:  "/v1/" + plural + "/Bad%20Slug/" + suffix,
				badValues:  map[string]string{slugVar: "Bad Slug"}, badBody: invalidSlugBody,
				notFound: message(entityType),
			})
		}
		cases = append(cases, entityCase{
			name: suffix + "_collection", entityType: domain.EntityTypeCollection, suffix: suffix,
			pathValues: map[string]string{collectionIDVar: "12"}, target: "/v1/collections/12/" + suffix,
			badTarget: "/v1/collections/0/" + suffix, badValues: map[string]string{collectionIDVar: "0"},
			badBody: invalidID("collection_id"), notFound: message(domain.EntityTypeCollection),
		})
	}

	for _, tc := range cases {
		handler := func(t *testing.T, rc revisionCase) http.Handler {
			missing := commandFunc[command.EntityRequest, domain.Editable](
				func(context.Context, command.EntityRequest) (domain.Editable, error) {
					if rc.notCalled {
						t.Error("command called unexpectedly")
					}
					return domain.Editable{}, rc.err
				})
			if tc.suffix == "editable" {
				return EditableGet{EntityType: tc.entityType, Command: missing}
			}
			return PendingGet{EntityType: tc.entityType, Command: commandFunc[command.EntityRequest, domain.MyRevision](
				func(ctx context.Context, req command.EntityRequest) (domain.MyRevision, error) {
					_, err := missing(ctx, req)
					return domain.MyRevision{}, err
				})}
		}
		runRevisionCases(t, []revisionCase{
			{name: tc.name + "_invalid", method: http.MethodGet, target: tc.badTarget, pathValues: tc.badValues,
				invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest, wantBody: tc.badBody},
			{name: tc.name + "_not_found", method: http.MethodGet, target: tc.target, pathValues: tc.pathValues,
				err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
				wantBody: `{"code":"not_found","message":"` + tc.notFound + `"}`},
		}, handler)
	}
}
