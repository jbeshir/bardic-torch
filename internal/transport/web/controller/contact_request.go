package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// ContactSubmitRequest is the JSON body of a contact form message. Every field but kind is personal data or a
// secret: validation errors name fields, never their values, and nothing of it is logged.
type ContactSubmitRequest struct {
	Kind           domain.ContactRequestKind `json:"kind"`
	Name           *string                   `json:"name"`
	Email          string                    `json:"email"`
	Message        string                    `json:"message"`
	URL            *string                   `json:"url"`
	TurnstileToken string                    `json:"turnstile_token"`
}

// ContactReceived is the JSON body of an accepted contact request. It holds no ID, so that anonymous callers
// cannot enumerate requests.
type ContactReceived struct {
	Status string `json:"status"`
}

// contactReceivedStatus is the status of every accepted contact request.
const contactReceivedStatus = "received"

// ContactSubmit handles POST /v1/contact, the public contact form: 202 once the request is stored, 400
// turnstile_failed for a token that does not pass, and 503 when the form is disabled, Turnstile cannot verify the
// token, or too many requests are open. It logs only the stored request's ID and kind.
type ContactSubmit struct {
	Command command.Command[command.ContactSubmission, int64]
}

func (c ContactSubmit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body ContactSubmitRequest
	if !httpjson.DecodeJSONBodyLimit(w, r, &body, httpjson.MaxContactBodyBytes) {
		return
	}
	message, ok := contactSubmissionOrBadRequest(w, r, body)
	if !ok {
		return
	}

	id, err := c.Command.Execute(ctx, command.ContactSubmission{
		Kind: body.Kind, Name: body.Name, Email: body.Email, Message: message, URL: body.URL,
		TurnstileToken: body.TurnstileToken,
	})
	logger := domain.LoggerFromContext(ctx)
	switch {
	case errors.Is(err, datasources.ErrTurnstileRejected):
		logger.InfoContext(ctx, "contact request refused: turnstile token rejected", "kind", body.Kind)
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeTurnstileFailed,
			"the turnstile check failed: complete it again and resend")
	case errors.Is(err, command.ErrContactUnavailable):
		// The command's errors never hold the request's fields or the token.
		logger.WarnContext(ctx, "contact request refused: unavailable", "kind", body.Kind, "error", err)
		httpjson.WriteError(ctx, w, http.StatusServiceUnavailable, httpjson.CodeUnavailable,
			"the contact form is unavailable; try again later")
	case err != nil:
		writeRevisionError(w, r, "contact request", err)
	default:
		logger.InfoContext(ctx, "contact request received", "contact_request_id", id, "kind", body.Kind)
		httpjson.WriteJSON(ctx, w, http.StatusAccepted, ContactReceived{Status: contactReceivedStatus})
	}
}

// contactSubmissionOrBadRequest checks body's fields and returns its message with CRLF and lone CR replaced by
// LF. If a field is invalid it writes 400 invalid_request, naming the field and never its value, and returns
// false; the caller should then return without writing.
func contactSubmissionOrBadRequest(
	w http.ResponseWriter, r *http.Request, body ContactSubmitRequest,
) (string, bool) {
	if !body.Kind.Valid() {
		return "", invalidContactField(w, r, "invalid kind: not one of its allowed values")
	}
	if !contactSenderOrBadRequest(w, r, body) {
		return "", false
	}

	return contactMessageOrBadRequest(w, r, body)
}

// contactSenderOrBadRequest checks the sender's name and email address as contactSubmissionOrBadRequest does.
func contactSenderOrBadRequest(w http.ResponseWriter, r *http.Request, body ContactSubmitRequest) bool {
	switch {
	case !textOrBadRequest(w, r, "name", body.Name, domain.MaxContactNameLength):
		return false
	case body.Email == "":
		return invalidContactField(w, r, "email is required")
	case !textOrBadRequest(w, r, "email", &body.Email, domain.MaxContactEmailLength):
		return false
	case !domain.ValidContactEmail(body.Email):
		return invalidContactField(w, r, "invalid email: must be an address of the form local@domain, without spaces")
	}

	return true
}

// contactMessageOrBadRequest checks the message, the URL and the Turnstile token as contactSubmissionOrBadRequest
// does, and returns the message with its line breaks normalised.
func contactMessageOrBadRequest(w http.ResponseWriter, r *http.Request, body ContactSubmitRequest) (string, bool) {
	if strings.TrimSpace(body.Message) == "" {
		return "", invalidContactField(w, r, "message is required")
	}
	message, ok := freeTextOrBadRequest(w, r, "message", &body.Message, domain.MaxContactMessageLength)
	if !ok {
		return "", false
	}

	switch {
	case !textOrBadRequest(w, r, "url", body.URL, domain.MaxURLLength):
		return "", false
	case body.URL != nil && !domain.IsValidURL(*body.URL):
		return "", invalidContactField(w, r, "invalid url: must be an http or https URL")
	case body.TurnstileToken == "":
		return "", invalidContactField(w, r, "turnstile_token is required")
	case !textOrBadRequest(w, r, "turnstile_token", &body.TurnstileToken, domain.MaxTurnstileTokenLength):
		return "", false
	}

	return *message, true
}

// invalidContactField writes 400 invalid_request with message, which names a field and never its value, and
// returns false.
func invalidContactField(w http.ResponseWriter, r *http.Request, message string) bool {
	httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest, message)
	return false
}

// ContactRequest is the JSON representation of a contact request, as moderators see it.
type ContactRequest struct {
	ID         int64                       `json:"id"`
	Kind       domain.ContactRequestKind   `json:"kind"`
	Name       *string                     `json:"name"`
	Email      *string                     `json:"email"`
	Message    *string                     `json:"message"`
	URL        *string                     `json:"url"`
	Status     domain.ContactRequestStatus `json:"status"`
	CreatedAt  time.Time                   `json:"created_at"`
	ResolvedAt *time.Time                  `json:"resolved_at"`
	ResolvedBy *ModerationAuthor           `json:"resolved_by"`
	Note       *string                     `json:"note"`
	Purged     bool                        `json:"purged"`
}

// ContactRequestList is the JSON representation of one page of contact requests, with how many there are.
type ContactRequestList struct {
	Data  []ContactRequest `json:"data"`
	Total int64            `json:"total"`
}

func contactRequestFromDomain(c domain.ContactRequest) ContactRequest {
	return ContactRequest{
		ID: c.ID, Kind: c.Kind, Name: c.Name, Email: c.Email, Message: c.Message, URL: c.URL, Status: c.Status,
		CreatedAt: c.CreatedAt, ResolvedAt: c.ResolvedAt,
		ResolvedBy: optional(c.ResolvedBy, moderationAuthorFromDomain), Note: c.Note, Purged: c.Purged,
	}
}

// ContactRequestsList handles GET /v1/moderation/contact-requests, one page of the contact requests, newest
// first, optionally of one status and one kind.
type ContactRequestsList struct {
	Command command.Command[command.ContactRequestListRequest, domain.ContactRequestList]
}

func (c ContactRequestsList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := contactRequestQueryOrBadRequest(w, r)
	if !ok {
		return
	}

	list, err := c.Command.Execute(r.Context(), command.ContactRequestListRequest{Principal: principal, Query: query})
	if err != nil {
		writeRevisionError(w, r, "contact requests", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, ContactRequestList{
		Data: mapSlice(list.Requests, contactRequestFromDomain), Total: list.Total,
	})
}

// contactRequestQueryOrBadRequest returns the inbox filters and page of the request's query string. If one is
// invalid it writes 400 invalid_request and returns false; the caller should then return without writing.
func contactRequestQueryOrBadRequest(w http.ResponseWriter, r *http.Request) (datasources.ContactRequestQuery, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return datasources.ContactRequestQuery{}, false
	}
	status, ok := enumOrBadRequest(w, r, query, "status", domain.ParseContactRequestStatus)
	if !ok {
		return datasources.ContactRequestQuery{}, false
	}
	kind, ok := enumOrBadRequest(w, r, query, "kind", domain.ParseContactRequestKind)
	if !ok {
		return datasources.ContactRequestQuery{}, false
	}
	page, ok := pageOrBadRequest(w, r, query)

	return datasources.ContactRequestQuery{Status: status, Kind: kind, Page: page}, ok
}

// ContactRequestResolveRequest is the JSON body of a contact request's resolution: resolved or dismissed, and an
// optional note.
type ContactRequestResolveRequest struct {
	Status domain.ContactRequestStatus `json:"status"`
	Note   *string                     `json:"note"`
}

// ContactRequestResolve handles POST /v1/moderation/contact-requests/{request_id}, resolving or dismissing an open
// contact request.
type ContactRequestResolve struct {
	Command command.Command[command.ResolveContactRequestRequest, domain.ContactRequest]
}

func (c ContactRequestResolve) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	requestID, ok := idOrBadRequest(w, r, "request_id")
	if !ok {
		return
	}
	var body ContactRequestResolveRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.Status != domain.ContactRequestStatusResolved && body.Status != domain.ContactRequestStatusDismissed {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid status: must be resolved or dismissed")
		return
	}
	note, ok := freeTextOrBadRequest(w, r, "note", body.Note, domain.MaxModeratorNoteLength)
	if !ok {
		return
	}

	request, err := c.Command.Execute(ctx, command.ResolveContactRequestRequest{
		Principal: principal, RequestID: requestID, Status: body.Status, Note: note,
	})
	if err != nil {
		writeRevisionError(w, r, "contact request", err)
		return
	}
	domain.LoggerFromContext(ctx).InfoContext(ctx, "resolved contact request",
		"contact_request_id", request.ID, "kind", request.Kind, "status", request.Status)
	httpjson.WriteJSON(ctx, w, http.StatusOK, contactRequestFromDomain(request))
}
