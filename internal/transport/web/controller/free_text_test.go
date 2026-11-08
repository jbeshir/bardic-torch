package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freeTextField is a free-text field of a write, and how to send it to and read it from its controller.
type freeTextField struct {
	name   string
	method string
	// target is the request's path, whose path variables are pathValues.
	target     string
	pathValues map[string]string
	// body is the JSON request body that holds the JSON string literal value in the field.
	body func(value string) string
	// handler is the controller with a command that passes the field's value it receives to got.
	handler func(got func(*string)) http.Handler
}

// freeTextFields are the free-text fields of moderation and the contact form, which the controller checks: a
// control character or a bidirectional formatting character in them is 400.
func freeTextFields() []freeTextField {
	revisionID := map[string]string{revisionIDVar: "51"}

	return []freeTextField{
		{name: "approve review_note", method: http.MethodPost, target: "/v1/revisions/51/approve",
			pathValues: revisionID,
			body:       func(v string) string { return `{"review_note":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionApprove{Command: commandFunc[command.ApproveRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.ApproveRevisionRequest) (domain.ModerationRevision, error) {
						got(&req.Options.ReviewNote)
						return testModerationRevision, nil
					})}
			}},
		{name: "approve summary", method: http.MethodPost, target: "/v1/revisions/51/approve",
			pathValues: revisionID,
			body:       func(v string) string { return `{"summary":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionApprove{Command: commandFunc[command.ApproveRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.ApproveRevisionRequest) (domain.ModerationRevision, error) {
						got(req.Options.Summary)
						return testModerationRevision, nil
					})}
			}},
		{name: "reject review_note", method: http.MethodPost, target: "/v1/revisions/51/reject",
			pathValues: revisionID,
			body:       func(v string) string { return `{"review_note":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionReject{Command: commandFunc[command.RejectRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.RejectRevisionRequest) (domain.ModerationRevision, error) {
						got(&req.ReviewNote)
						return testModerationRevision, nil
					})}
			}},
		{name: "rebase summary", method: http.MethodPost, target: "/v1/revisions/51/rebase",
			pathValues: revisionID,
			body: func(v string) string {
				return `{"entity_type":"event","current_revision_id":46,"document":` + testEditorEventJSON +
					`,"summary":` + v + `}`
			},
			handler: func(got func(*string)) http.Handler {
				return RevisionRebase{Command: commandFunc[command.RebaseRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.RebaseRevisionRequest) (domain.ModerationRevision, error) {
						got(req.Summary)
						return testModerationRevision, nil
					})}
			}},
		{name: "contact message", method: http.MethodPost, target: contactPath,
			body: func(v string) string {
				return strings.Replace(contactBody, contactMessage, v, 1)
			},
			handler: func(got func(*string)) http.Handler {
				return ContactSubmit{Command: commandFunc[command.ContactSubmission, int64](
					func(_ context.Context, req command.ContactSubmission) (int64, error) {
						got(&req.Message)
						return 12, nil
					})}
			}},
		{name: "contact request note", method: http.MethodPost, target: "/v1/moderation/contact-requests/12",
			pathValues: map[string]string{requestIDVar: "12"},
			body:       func(v string) string { return `{"status":"resolved","note":` + v + `}` },
			handler: func(got func(*string)) http.Handler {
				return ContactRequestResolve{Command: commandFunc[command.ResolveContactRequestRequest, domain.ContactRequest](
					func(_ context.Context, req command.ResolveContactRequestRequest) (domain.ContactRequest, error) {
						got(req.Note)
						return testContactRequest, nil
					})}
			}},
		{name: "link report message", method: http.MethodPost, target: "/v1/link-reports",
			body: func(v string) string {
				return `{"url":"https://example.org/the-copper-kettle","reason":"broken","message":` + v + `}`
			},
			handler: func(got func(*string)) http.Handler {
				return LinkReportCreate{Command: commandFunc[command.ReportLinkRequest, command.ReportedLink](
					func(_ context.Context, req command.ReportLinkRequest) (command.ReportedLink, error) {
						got(req.Message)
						return command.ReportedLink{Receipt: domain.LinkReportReceipt{ID: 3, Status: domain.LinkReportStatusOpen,
							CreatedAt: testNow}, Created: true}, nil
					})}
			}},
		{name: "link report note", method: http.MethodPost, target: "/v1/moderation/link-reports/3",
			pathValues: map[string]string{reportIDVar: "3"},
			body:       func(v string) string { return `{"status":"resolved","note":` + v + `}` },
			handler: func(got func(*string)) http.Handler {
				return LinkReportResolve{Command: commandFunc[command.ResolveLinkReportRequest, domain.LinkReport](
					func(_ context.Context, req command.ResolveLinkReportRequest) (domain.LinkReport, error) {
						got(req.Note)
						return testLinkReport, nil
					})}
			}},
	}
}

// summaryFields are the summaries of drafts and reverts. They are checked with the document, by the command, so
// that their other characters are 422 invalid_document, but their line breaks are normalised as free text's are.
func summaryFields() []freeTextField {
	sentSummary := `"Describe the revel."`

	return []freeTextField{
		{name: "create draft summary", method: http.MethodPost, target: draftsTarget,
			body: func(v string) string { return strings.Replace(createDraftBody, sentSummary, v, 1) },
			handler: func(got func(*string)) http.Handler {
				return DraftCreate{Command: commandFunc[command.SaveDraftRequest, command.SavedDraft](
					func(_ context.Context, req command.SaveDraftRequest) (command.SavedDraft, error) {
						got(&req.Summary)
						return command.SavedDraft{DraftResult: command.DraftResult{Draft: testDraft}, Created: true}, nil
					})}
			}},
		{name: "update draft summary", method: http.MethodPut, target: "/v1/drafts/51",
			pathValues: map[string]string{draftIDVar: "51"},
			body:       func(v string) string { return strings.Replace(updateDraftBody, sentSummary, v, 1) },
			handler: func(got func(*string)) http.Handler {
				return DraftUpdate{Command: commandFunc[command.UpdateDraftRequest, command.DraftResult](
					func(_ context.Context, req command.UpdateDraftRequest) (command.DraftResult, error) {
						got(&req.Summary)
						return command.DraftResult{Draft: testDraft}, nil
					})}
			}},
		{name: "revert summary", method: http.MethodPost, target: "/v1/revisions/51/revert",
			pathValues: map[string]string{revisionIDVar: "51"},
			body:       func(v string) string { return `{"summary":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionRevert{Command: commandFunc[command.RevertRequest, command.SubmittedRevision](
					func(_ context.Context, req command.RevertRequest) (command.SubmittedRevision, error) {
						got(&req.Summary)
						return command.SubmittedRevision{Revision: testMyRevision}, nil
					})}
			}},
	}
}

// serveFreeText sends value, a JSON string literal, in f's field through the contract validator, and returns the
// response status and the value the command received, which is nil if the command was not called.
func serveFreeText(t *testing.T, f freeTextField, value string) (int, *string) {
	t.Helper()

	return serveFreeTextChecking(t, f, value, true)
}

// serveFreeTextChecking is serveFreeText, checking the request against the spec only if validateRequest; the
// response is always checked.
func serveFreeTextChecking(t *testing.T, f freeTextField, value string, validateRequest bool) (int, *string) {
	t.Helper()

	var received *string
	called := false
	handler := f.handler(func(v *string) {
		called = true
		if v != nil {
			received = new(*v)
		}
	})
	req := httptest.NewRequestWithContext(t.Context(), f.method, f.target, strings.NewReader(f.body(value)))
	req.Header.Set("Content-Type", "application/json")
	for name, v := range f.pathValues {
		req.SetPathValue(name, v)
	}
	validator := openapitest.New(t)
	serve := validator.ServeResponseOnly
	if validateRequest {
		serve = validator.Serve
	}
	rec := serve(t, handler, asPrincipal(req))

	if !called {
		assert.Nil(t, received)
		return rec.Code, nil
	}
	require.NotNil(t, received, rec.Body.String())

	return rec.Code, received
}

// twoLines is "one" and "two" on separate lines, as stored.
const twoLines = "one\ntwo"

func TestFreeTextFields_LineBreaksAreNormalisedToLF(t *testing.T) {
	t.Parallel()

	lineBreaks := []struct{ name, sent, want string }{
		{"crlf", `"one\r\ntwo"`, twoLines},
		{"lone_cr", `"one\rtwo"`, twoLines},
		{"lf", `"one\ntwo"`, twoLines},
		{"mixed", `"a\r\nb\rc\nd\r\n\r\ne"`, "a\nb\nc\nd\n\ne"},
	}
	for _, f := range append(freeTextFields(), summaryFields()...) {
		for _, lb := range lineBreaks {
			t.Run(f.name+"/"+lb.name, func(t *testing.T) {
				t.Parallel()

				status, got := serveFreeText(t, f, lb.sent)

				assert.Less(t, status, http.StatusBadRequest)
				require.NotNil(t, got)
				assert.Equal(t, lb.want, *got)
			})
		}
	}
}

func TestFreeTextFields_OtherControlCharactersAreRefused(t *testing.T) {
	t.Parallel()

	for _, f := range freeTextFields() {
		for name, sent := range map[string]string{
			"bell":          `"one\u0007two"`,
			"escape":        `"Hello\u001b[2J"`,
			"bidi_override": `"one\u202etwo"`,
			"c1_control":    `"one\u0085two"`,
			"cr_and_bell":   `"one\r\u0007two"`,
		} {
			t.Run(f.name+"/"+name, func(t *testing.T) {
				t.Parallel()

				status, got := serveFreeText(t, f, sent)

				assert.Equal(t, http.StatusBadRequest, status)
				assert.Nil(t, got, "the command must not run")
			})
		}
	}
}

func TestFreeTextFields_LengthCountsLineBreaksOnce(t *testing.T) {
	t.Parallel()

	// A CRLF is one character once normalised, so a text of exactly the limit passes however its lines end. The
	// spec's maxLength is the limit on the stored text, so the request, twice as long as sent, is outside it.
	f := freeTextFields()[0]
	text := strings.Repeat(`\r\n`, domain.MaxModeratorNoteLength)

	status, got := serveFreeTextChecking(t, f, `"`+text+`"`, false)

	assert.Equal(t, http.StatusOK, status)
	require.NotNil(t, got)
	assert.Equal(t, strings.Repeat("\n", domain.MaxModeratorNoteLength), *got)
}

func TestWithLineBreaksNormalized_DoesNotChangeItsInput(t *testing.T) {
	t.Parallel()

	in := "a\r\nb\rc"

	assert.Equal(t, "a\nb\nc", domain.WithLineBreaksNormalized(in))
	assert.Equal(t, "a\r\nb\rc", in)
	assert.Equal(t, "plain", domain.WithLineBreaksNormalized("plain"))
	assert.Empty(t, domain.WithLineBreaksNormalized(""))
}

func TestSummaryFields_OtherCharactersAreLeftToTheDocumentChecks(t *testing.T) {
	t.Parallel()

	// The command checks the summary with the document (422 invalid_document); the controller only replaces the
	// line breaks.
	for _, f := range summaryFields() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			status, got := serveFreeText(t, f, `"one\r\u0007two\u202e"`)

			assert.Less(t, status, http.StatusBadRequest)
			require.NotNil(t, got)
			assert.Equal(t, "one\n\u0007two\u202e", *got)
		})
	}
}
