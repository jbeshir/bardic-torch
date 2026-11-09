package controller

import (
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// TestDrafts_PreferenceProblemsMatchSpec serves a 422 invalid_document with each problem a preference or a
// licence version can give through createDraft, so that the spec's problem codes are checked against them.
func TestDrafts_PreferenceProblemsMatchSpec(t *testing.T) {
	t.Parallel()

	problems := []domain.DocumentProblem{
		{Path: "/preferences/0/dimension", Code: domain.ProblemDimensionNotAllowed},
		{Path: "/preferences/1/value/choices", Code: domain.ProblemChoiceConflict},
		{Path: "/licence_version", Code: domain.ProblemOutOfRange},
		{Path: "/links/0/performances/0/key", Code: domain.ProblemOutOfRange},
	}
	wantBodies := []string{
		`{"path":"/preferences/0/dimension","problem":"dimension_not_allowed"}`,
		`{"path":"/preferences/1/value/choices","problem":"choice_conflict"}`,
		`{"path":"/licence_version","problem":"out_of_range"}`,
		`{"path":"/links/0/performances/0/key","problem":"out_of_range"}`,
	}
	cases := make([]revisionCase, 0, len(problems))
	for i, p := range problems {
		cases = append(cases, revisionCase{name: string(p.Code) + p.Path, method: http.MethodPost,
			target: draftsTarget, body: createDraftBody, err: &domain.DocumentError{Problems: []domain.DocumentProblem{p}},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody: `{"code":"invalid_document","message":"the document is invalid","problems":[` + wantBodies[i] +
				`]}`})
	}
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return DraftCreate{Command: returning[command.SaveDraftRequest](t, true, command.SavedDraft{}, tc.err)}
	})
}
