package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// maxContactBodyBytes is the largest body of a contact request.
const maxContactBodyBytes = 64 << 10

// badRequest writes 400 invalid_request with msg.
func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest, msg)
}

// optionalText returns s trimmed, or nil if it is empty, and whether it has at most maxLen characters.
func optionalText(s *string, maxLen int) (*string, bool) {
	if s == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil, true
	}

	return &trimmed, utf8.RuneCountInString(trimmed) <= maxLen
}

// notFound writes 404 not_found with msg.
func notFound(w http.ResponseWriter, r *http.Request, msg string) {
	httpjson.WriteError(r.Context(), w, http.StatusNotFound, httpjson.CodeNotFound, msg)
}

// ClosingRequest is the JSON body of a moderator's closing of a contact request.
type ClosingRequest struct {
	Status domain.ReportStatus `json:"status"`
	Note   *string             `json:"note"`
}

// closing decodes the body of a closing request and returns a datasources.Closing for the moderator. It
// writes 400 and returns false if the body is invalid.
func closing(w http.ResponseWriter, r *http.Request, idName string, now time.Time) (datasources.Closing, bool) {
	id, ok := idOrBadRequest(w, r, idName)
	principal, authed := principalOrUnauthenticated(w, r)
	var body ClosingRequest
	if !ok || !authed || !httpjson.DecodeJSONBody(w, r, &body) {
		return datasources.Closing{}, false
	}
	note, noteOK := optionalText(body.Note, domain.MaxNoteLength)
	if !body.Status.Closes() || !noteOK {
		badRequest(w, r, "status must be resolved or dismissed, and note at most 2000 characters")
		return datasources.Closing{}, false
	}

	return datasources.Closing{ModeratorUserID: principal.User.ID, ID: id, Status: body.Status, Note: note,
		Now: now}, true
}

// reportListQuery reads the status (default open) and page of a moderation list. It writes 400 and returns false
// if either is invalid.
func reportListQuery(w http.ResponseWriter, r *http.Request) (domain.ReportStatus, domain.Page, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return "", domain.Page{}, false
	}
	status, ok := enumOrBadRequest(w, r, query, "status", func(s string) (domain.ReportStatus, error) {
		if v := domain.ReportStatus(s); v.Valid() {
			return v, nil
		}
		return "", errors.New("unknown status")
	})
	if !ok {
		return "", domain.Page{}, false
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return "", domain.Page{}, false
	}
	if status == nil {
		open := domain.ReportStatusOpen
		status = &open
	}

	return *status, page, true
}

type resolutionJSON struct {
	ResolvedBy string    `json:"resolved_by"`
	ResolvedAt time.Time `json:"resolved_at"`
	Note       *string   `json:"note"`
}

func resolutionFromDomain(res *domain.Resolution) *resolutionJSON {
	if res == nil {
		return nil
	}

	return &resolutionJSON{ResolvedBy: res.ResolvedBy, ResolvedAt: res.ResolvedAt, Note: res.Note}
}

// writeClosed writes the answer to closing a request or setting a link hidden: 204, or the error err is.
func writeClosed(w http.ResponseWriter, r *http.Request, err error, what string) {
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		notFound(w, r, what+" not found")
	case errors.Is(err, datasources.ErrNotOpen):
		httpjson.WriteError(r.Context(), w, http.StatusConflict, httpjson.CodeConflict, "the "+what+" is not open")
	case err != nil:
		writeInternalError(w, r, "unable to close "+what, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ModerationLinkGet handles GET /v1/moderation/links?url=.
type ModerationLinkGet struct {
	Moderation datasources.Moderation
}

func (c ModerationLinkGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	raw, set, ok := singleOrBadRequest(w, r, query, "url")
	if !ok {
		return
	}
	if !set || raw == "" {
		badRequest(w, r, "url is required")
		return
	}
	normalized, err := domain.NormalizeURL(raw)
	if err != nil {
		notFound(w, r, "link not found")
		return
	}

	link, err := c.Moderation.GetModerationLink(r.Context(), normalized)
	if errors.Is(err, datasources.ErrNotFound) {
		notFound(w, r, "link not found")
		return
	} else if err != nil {
		writeInternalError(w, r, "unable to get link", err)
		return
	}

	type attachment struct {
		EntityType domain.EntityType `json:"entity_type"`
		Slug       string            `json:"slug"`
		Title      string            `json:"title"`
		Roles      []domain.LinkRole `json:"roles"`
	}
	attachments := make([]attachment, 0, len(link.Attachments))
	for _, a := range link.Attachments {
		attachments = append(attachments, attachment{a.EntityType, a.Slug, a.Title, a.Roles})
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, struct {
		ID          uint64          `json:"id"`
		URL         string          `json:"url"`
		Title       *string         `json:"title"`
		IsHidden    bool            `json:"is_hidden"`
		Liveness    domain.Liveness `json:"liveness"`
		Attachments []attachment    `json:"attachments"`
	}{link.ID, link.URL, link.Title, link.IsHidden, link.Liveness, attachments})
}

// LinkHiddenSetRequest is the JSON body of a request to hide or restore a link. Hidden is a pointer so that a
// missing value is rejected rather than read as false, which would restore the link.
type LinkHiddenSetRequest struct {
	Hidden *bool `json:"hidden"`
}

// LinkHiddenSet handles PUT /v1/links/{link_id}/hidden.
type LinkHiddenSet struct {
	Moderation datasources.Moderation
}

func (c LinkHiddenSet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "link_id")
	if !ok {
		return
	}
	var body LinkHiddenSetRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.Hidden == nil {
		badRequest(w, r, "hidden is required")
		return
	}

	err := c.Moderation.SetLinkHidden(r.Context(), id, *body.Hidden)
	if err == nil {
		domain.LoggerFromContext(r.Context()).InfoContext(r.Context(), "set link hidden",
			"link_id", id, "hidden", *body.Hidden)
	}
	writeClosed(w, r, err, "link")
}

// ContactCreateRequest is the JSON body of a contact request. Its fields are personal data and are never logged.
type ContactCreateRequest struct {
	Kind           domain.ContactKind `json:"kind"`
	Name           *string            `json:"name"`
	Email          string             `json:"email"`
	Message        string             `json:"message"`
	URL            *string            `json:"url"`
	TurnstileToken string             `json:"turnstile_token"`
}

// request returns the command request for the body, or false if a field is missing or out of range.
func (b ContactCreateRequest) request() (command.CreateContactRequestRequest, bool) {
	name, nameOK := optionalText(b.Name, domain.MaxContactNameLength)
	pageURL, urlOK := optionalText(b.URL, domain.MaxURLLength)
	message, messageOK := optionalText(&b.Message, domain.MaxContactMessageLength)
	if !b.Kind.Valid() || !nameOK || !urlOK || !messageOK || message == nil ||
		len(b.Email) > domain.MaxContactEmailLength || !domain.ValidContactEmail(b.Email) ||
		b.TurnstileToken == "" || len(b.TurnstileToken) > domain.MaxTurnstileTokenLength {
		return command.CreateContactRequestRequest{}, false
	}

	return command.CreateContactRequestRequest{Kind: b.Kind, Name: name, Email: b.Email, Message: *message, URL: pageURL,
		Token: b.TurnstileToken}, true
}

// ContactCreate handles POST /v1/contact. The request needs no account.
type ContactCreate struct {
	Command command.Command[command.CreateContactRequestRequest, int64]
}

func (c ContactCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body ContactCreateRequest
	if !httpjson.DecodeJSONBodyMax(w, r, &body, maxContactBodyBytes) {
		return
	}
	req, ok := body.request()
	if !ok {
		badRequest(w, r, "kind, email, message and turnstile_token are required and within their limits")
		return
	}

	id, err := c.Command.Execute(r.Context(), req)
	switch {
	case errors.Is(err, command.ErrTurnstileFailed):
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeTurnstileFailed,
			"the Turnstile check failed; reload the form and try again")
	case err != nil:
		writeInternalError(w, r, "unable to create contact request", err)
	default:
		domain.LoggerFromContext(r.Context()).InfoContext(r.Context(), "contact request received",
			"contact_request_id", id, "kind", body.Kind)
		w.WriteHeader(http.StatusAccepted)
	}
}

// ContactRequestListGet handles GET /v1/moderation/contact-requests.
type ContactRequestListGet struct {
	Moderation datasources.Moderation
}

func (c ContactRequestListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, page, ok := reportListQuery(w, r)
	if !ok {
		return
	}
	list, err := c.Moderation.ListContactRequests(r.Context(), status, page)
	if err != nil {
		writeInternalError(w, r, "unable to list contact requests", err)
		return
	}

	type item struct {
		ID         uint64              `json:"id"`
		Kind       domain.ContactKind  `json:"kind"`
		Name       *string             `json:"name"`
		Email      *string             `json:"email"`
		Message    *string             `json:"message"`
		URL        *string             `json:"url"`
		Status     domain.ReportStatus `json:"status"`
		CreatedAt  time.Time           `json:"created_at"`
		Resolution *resolutionJSON     `json:"resolution"`
	}
	data := make([]item, 0, len(list.Requests))
	for _, q := range list.Requests {
		data = append(data, item{q.ID, q.Kind, q.Name, q.Email, q.Message, q.URL, q.Status, q.CreatedAt,
			resolutionFromDomain(q.Resolution)})
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, struct {
		Data  []item `json:"data"`
		Total int64  `json:"total"`
	}{data, list.Total})
}

// ContactRequestClose handles POST /v1/moderation/contact-requests/{request_id}.
type ContactRequestClose struct {
	Moderation datasources.Moderation
	Now        func() time.Time
}

func (c ContactRequestClose) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	closed, ok := closing(w, r, "request_id", c.Now())
	if !ok {
		return
	}
	writeClosed(w, r, c.Moderation.CloseContactRequest(r.Context(), closed), "contact request")
}
