package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freeTextField is a free-text field of a write, and how to send it to and read it from its controller.
type freeTextField struct {
	name       string
	pathValues map[string]string
	// body is the JSON request body that holds the JSON string literal value in the field.
	body func(value string) string
	// handler is the controller with a command that passes the field's value it receives to got.
	handler func(got func(*string)) http.Handler
}

func freeTextFields() []freeTextField {
	revisionID := map[string]string{revisionIDVar: "51"}

	return []freeTextField{
		{name: "approve review_note", pathValues: revisionID,
			body: func(v string) string { return `{"review_note":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionApprove{Command: commandFunc[command.ApproveRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.ApproveRevisionRequest) (domain.ModerationRevision, error) {
						got(&req.Options.ReviewNote)
						return testModerationRevision, nil
					})}
			}},
		{name: "approve summary", pathValues: revisionID,
			body: func(v string) string { return `{"summary":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionApprove{Command: commandFunc[command.ApproveRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.ApproveRevisionRequest) (domain.ModerationRevision, error) {
						got(req.Options.Summary)
						return testModerationRevision, nil
					})}
			}},
		{name: "reject review_note", pathValues: revisionID,
			body: func(v string) string { return `{"review_note":` + v + `,"acknowledged_flags":[]}` },
			handler: func(got func(*string)) http.Handler {
				return RevisionReject{Command: commandFunc[command.RejectRevisionRequest, domain.ModerationRevision](
					func(_ context.Context, req command.RejectRevisionRequest) (domain.ModerationRevision, error) {
						got(&req.ReviewNote)
						return testModerationRevision, nil
					})}
			}},
		{name: "rebase summary", pathValues: revisionID,
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
		{name: "contact message",
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
		{name: "contact request note", pathValues: map[string]string{requestIDVar: "12"},
			body: func(v string) string { return `{"status":"resolved","note":` + v + `}` },
			handler: func(got func(*string)) http.Handler {
				return ContactRequestResolve{Command: commandFunc[command.ResolveContactRequestRequest, domain.ContactRequest](
					func(_ context.Context, req command.ResolveContactRequestRequest) (domain.ContactRequest, error) {
						got(req.Note)
						return testContactRequest, nil
					})}
			}},
		{name: "link report message",
			body: func(v string) string {
				return `{"url":"https://example.org/the-copper-kettle","reason":"broken","message":` + v + `}`
			},
			handler: func(got func(*string)) http.Handler {
				return LinkReportCreate{Command: commandFunc[command.ReportLinkRequest, command.ReportedLink](
					func(_ context.Context, req command.ReportLinkRequest) (command.ReportedLink, error) {
						got(req.Message)
						return command.ReportedLink{Created: true}, nil
					})}
			}},
		{name: "link report note", pathValues: map[string]string{reportIDVar: "3"},
			body: func(v string) string { return `{"status":"resolved","note":` + v + `}` },
			handler: func(got func(*string)) http.Handler {
				return LinkReportResolve{Command: commandFunc[command.ResolveLinkReportRequest, domain.LinkReport](
					func(_ context.Context, req command.ResolveLinkReportRequest) (domain.LinkReport, error) {
						got(req.Note)
						return domain.LinkReport{}, nil
					})}
			}},
	}
}

// serveFreeText sends value, a JSON string literal, in f's field and returns the response status and the value
// the command received, which is nil if the command was not called.
func serveFreeText(t *testing.T, f freeTextField, value string) (int, *string) {
	t.Helper()

	var received *string
	called := false
	handler := f.handler(func(v *string) {
		called = true
		if v != nil {
			received = new(*v)
		}
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(f.body(value)))
	req.Header.Set("Content-Type", "application/json")
	for name, v := range f.pathValues {
		req.SetPathValue(name, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, asPrincipal(req))

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
	for _, f := range freeTextFields() {
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

	// A CRLF is one character once normalised, so a text of exactly the limit passes however its lines end.
	f := freeTextFields()[0]
	text := strings.Repeat(`\r\n`, domain.MaxModeratorNoteLength)

	status, got := serveFreeText(t, f, `"`+text+`"`)

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
