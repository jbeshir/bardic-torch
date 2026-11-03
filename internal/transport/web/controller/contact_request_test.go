package controller

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
)

const (
	contactPath = "/v1/contact"
	// contactBody is a valid contact request, with every field.
	contactBody = `{"kind":"correction","name":"Wren Placeholder","email":"wren@example.org","message":` +
		contactMessage + `,"url":` + contactURL + `,"turnstile_token":` + contactTurnstile + `}`
	// The JSON values of contactBody's message, URL and token.
	contactMessage   = `"The second verse is credited to the wrong person."`
	contactURL       = `"https://example.org/the-copper-kettle"`
	contactTurnstile = `"XXXX.DUMMY.TOKEN.XXXX"`
	// receivedBody is the answer to a stored contact request.
	receivedBody        = `{"status":"received"}`
	contactRequestsPath = "/v1/moderation/contact-requests"
	contactRequestBody  = `{"id":12,"kind":"correction","name":"Wren Placeholder","email":"wren@example.org",` +
		`"message":"The second verse is credited to the wrong person.","url":null,"status":"resolved",` +
		`"created_at":"2026-10-31T12:00:00Z","resolved_at":"2026-10-31T12:00:00Z","resolved_by":{"id":` +
		`"auth0|moderator","display_name":"Rowan","role":"moderator","blocked":false},"note":"Credit corrected.",` +
		`"purged":false}`
	purgedContactRequestBody = `{"id":11,"kind":"general","name":null,"email":null,"message":null,"url":null,` +
		`"status":"dismissed","created_at":"2026-10-31T12:00:00Z","resolved_at":"2026-10-31T12:00:00Z",` +
		`"resolved_by":null,"note":null,"purged":true}`
)

var (
	testContactRequest = domain.ContactRequest{ID: 12, Kind: domain.ContactRequestKindCorrection,
		Name: new("Wren Placeholder"), Email: new("wren@example.org"),
		Message: new("The second verse is credited to the wrong person."), Status: domain.ContactRequestStatusResolved,
		CreatedAt: testNow, ResolvedAt: &testNow, ResolvedBy: &domain.ModerationAuthor{ID: "auth0|moderator",
			DisplayName: "Rowan", Role: domain.RoleModerator}, Note: new("Credit corrected.")}
	testPurgedContactRequest = domain.ContactRequest{ID: 11, Kind: domain.ContactRequestKindGeneral,
		Status: domain.ContactRequestStatusDismissed, CreatedAt: testNow, ResolvedAt: &testNow, Purged: true}
)

// contactCase is contactBody with its text from replaced by to, which the controller refuses with message before the
// command runs. schemaValid marks bodies the request schema accepts.
func contactCase(name, from, to, message string, schemaValid bool) revisionCase {
	tc := badBodyCase(revisionCase{method: http.MethodPost, target: contactPath, anonymous: true}, name,
		strings.Replace(contactBody, from, to, 1), message)
	tc.invalidRequest = !schemaValid
	return tc
}

func TestContactSubmit_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: contactPath, anonymous: true, body: contactBody}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusAccepted, receivedBody
	minimal := base
	minimal.name, minimal.wantStatus, minimal.wantBody = "minimal", http.StatusAccepted, receivedBody
	minimal.body = `{"kind":"general","email":"a@b","message":"Hello.","turnstile_token":"t"}`
	// A 5000-character message of 4-byte characters, escaped, is well over 16 KiB but under 64 KiB.
	longMessage := base
	longMessage.name, longMessage.wantStatus, longMessage.wantBody = "long_multibyte_message", http.StatusAccepted,
		receivedBody
	longMessage.body = `{"kind":"takedown","email":"wren@example.org","message":"` +
		strings.Repeat(`🎶`, domain.MaxContactMessageLength) + `","turnstile_token":"t"}`
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"kind":"general","message":"` + strings.Repeat("a", httpjson.MaxContactBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	malformed := badBodyCase(base, "malformed", `{"kind":"general","email":"wren@example.org`,
		"request body is not valid JSON")
	unknownField := badBodyCase(base, "unknown_field", `{"kind":"general","phone":"0"}`,
		"request body has an unknown field")

	cases := append([]revisionCase{ok, minimal, longMessage, tooLarge, notJSON, malformed, unknownField,
		contactCase("unknown_kind", `"correction"`, `"complaint"`, "invalid kind: not one of its allowed values", false),
		contactCase("name_too_long", `"Wren Placeholder"`, `"`+strings.Repeat("ł", 256)+`"`,
			"invalid name: must be at most 255 characters", false),
		contactCase("no_email", `"wren@example.org"`, `""`, "email is required", false),
		contactCase("email_too_long", `"wren@example.org"`, `"`+strings.Repeat("a", 250)+`@a.org"`,
			"invalid email: must be at most 254 characters", false),
		contactCase("email_without_at", `"wren@example.org"`, `"wren.example.org"`,
			"invalid email: must be an address of the form local@domain, without spaces", true),
		contactCase("email_with_space", `"wren@example.org"`, `"wren placeholder@example.org"`,
			"invalid email: must be an address of the form local@domain, without spaces", true),
		contactCase("no_message", contactMessage, `""`, "message is required", false),
		contactCase("blank_message", contactMessage, `"  \n "`, "message is required", true),
		contactCase("message_too_long", contactMessage, `"`+strings.Repeat("a", 5001)+`"`,
			"invalid message: must be at most 5000 characters", false),
		contactCase("url_too_long", contactURL, `"https://example.org/`+strings.Repeat("a", 2040)+`"`,
			"invalid url: must be at most 2048 characters", false),
		contactCase("url_not_http", contactURL, `"ftp://example.org/kettle"`, "invalid url: must be an http or https URL",
			true),
		contactCase("no_token", contactTurnstile, `""`, "turnstile_token is required", false),
		contactCase("token_too_long", contactTurnstile, `"`+strings.Repeat("a", 2049)+`"`,
			"invalid turnstile_token: must be at most 2048 characters", false),
	}, errorCases(base,
		revisionCase{name: "turnstile_failed", err: datasources.ErrTurnstileRejected,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"turnstile_failed","message":"the turnstile check failed: complete it again and resend"}`},
		revisionCase{name: "unavailable_cloudflare", err: command.ErrContactUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"code":"unavailable","message":"the contact form is unavailable; try again later"}`},
		revisionCase{name: "database_busy", err: datasources.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable, wantBody: unavailableBody},
		internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ContactSubmit{Command: commandFunc[command.ContactSubmission, int64](
			func(_ context.Context, req command.ContactSubmission) (int64, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, command.ContactSubmission{Kind: domain.ContactRequestKindCorrection,
						Name: new("Wren Placeholder"), Email: "wren@example.org",
						Message: "The second verse is credited to the wrong person.",
						URL:     new("https://example.org/the-copper-kettle"), TurnstileToken: "XXXX.DUMMY.TOKEN.XXXX"},
						req)
				}
				return 12, tc.err
			})}
	})
}

func TestContactRequestsList_ServeHTTP(t *testing.T) {
	t.Parallel()

	list := domain.ContactRequestList{Requests: []domain.ContactRequest{testContactRequest, testPurgedContactRequest},
		Total: 2}
	get := func(name, query string, status int, body string) revisionCase {
		tc := revisionCase{name: name, method: http.MethodGet, target: contactRequestsPath + query,
			wantStatus: status, wantBody: body}
		if status == http.StatusBadRequest {
			tc.invalidRequest, tc.notCalled = true, true
		}
		return tc
	}
	anonymous := get(anonymousCase, "", http.StatusUnauthorized, unauthenticatedBody)
	anonymous.anonymous, anonymous.notCalled = true, true
	forbidden := get(moderatorRequiredCase.name, "", http.StatusForbidden, moderatorRequiredBy)
	forbidden.err = domain.ErrModeratorRequired
	internal := get(internalCase.name, "", http.StatusInternalServerError, internalErrorBody)
	internal.err = errCommand
	cases := []revisionCase{
		get("ok", "?status=open&kind=takedown&limit=5&offset=10", http.StatusOK,
			`{"data":[`+contactRequestBody+`,`+purgedContactRequestBody+`],"total":2}`),
		anonymous, forbidden, internal,
		get("unknown_status", "?status=closed", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid status: not one of its allowed values"}`),
		get("unknown_kind", "?kind=complaint", http.StatusBadRequest, invalidKindBody),
		get("bad_limit", "?limit=0", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid limit: must be an integer from 1 to 100"}`),
	}
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ContactRequestsList{Command: commandFunc[command.ContactRequestListRequest, domain.ContactRequestList](
			func(_ context.Context, req command.ContactRequestListRequest) (domain.ContactRequestList, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					status, kind := domain.ContactRequestStatusOpen, domain.ContactRequestKindTakedown
					assert.Equal(t, datasources.ContactRequestQuery{Status: &status, Kind: &kind,
						Page: domain.Page{Limit: 5, Offset: 10}}, req.Query)
				}
				return list, tc.err
			})}
	})
}

func TestContactRequestResolve_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: contactRequestsPath + "/12",
		pathValues: map[string]string{"request_id": "12"}, body: `{"status":"resolved","note":"Credit corrected."}`}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, contactRequestBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{"request_id": "0"}, true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID("request_id")
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"note":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{ok, anonymous, badID, tooLarge, notJSON,
		badBodyCase(base, "reopen", `{"status":"open"}`, "invalid status: must be resolved or dismissed"),
		badBodyCase(base, "note_too_long", `{"status":"dismissed","note":"`+strings.Repeat("a", 2001)+`"}`,
			"invalid note: must be at most 2000 characters"),
	}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"contact request not found"}`},
		revisionCase{name: "not_open", err: datasources.ErrContactRequestNotOpen, wantStatus: http.StatusConflict,
			wantBody: `{"code":"conflict","message":"the contact request is not open"}`},
		moderatorRequiredCase, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ContactRequestResolve{Command: commandFunc[command.ResolveContactRequestRequest, domain.ContactRequest](
			func(_ context.Context, req command.ResolveContactRequestRequest) (domain.ContactRequest, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, command.ResolveContactRequestRequest{Principal: req.Principal, RequestID: 12,
						Status: domain.ContactRequestStatusResolved, Note: new("Credit corrected.")}, req)
				}
				return testContactRequest, tc.err
			})}
	})
}
