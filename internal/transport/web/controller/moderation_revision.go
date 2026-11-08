package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// maxQueueAgeHours is the largest age bound of the moderation queue, a year.
const maxQueueAgeHours = 8760

// ModerationQueueList handles GET /v1/moderation/queue, one page of the pending revisions, oldest first.
type ModerationQueueList struct {
	Command command.Command[command.ModerationQueueRequest, domain.ModerationQueue]
}

func (c ModerationQueueList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := moderationQueueQueryOrBadRequest(w, r)
	if !ok {
		return
	}

	queue, err := c.Command.Execute(r.Context(), command.ModerationQueueRequest{Principal: principal, Query: query})
	if err != nil {
		writeRevisionError(w, r, "moderation queue", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, moderationQueueFromDomain(queue))
}

// moderationQueueQueryOrBadRequest returns the queue filters the query parameters ask for. If one is invalid it
// writes 400 invalid_request and returns false; the caller should then return without writing.
func moderationQueueQueryOrBadRequest(w http.ResponseWriter, r *http.Request) (datasources.ModerationQueueQuery, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	entityType, ok := enumOrBadRequest(w, r, query, "entity_type", domain.ParseEntityType)
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	author, set, ok := singleOrBadRequest(w, r, query, "author")
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	var authorID *string
	if set {
		authorID = &author
	}
	minAge, ok := ageOrBadRequest(w, r, query, "min_age_hours")
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	maxAge, ok := ageOrBadRequest(w, r, query, "max_age_hours")
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	includeBlocked, ok := boolOrBadRequest(w, r, query, "include_blocked")
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return datasources.ModerationQueueQuery{}, false
	}

	return datasources.ModerationQueueQuery{
		EntityType: entityType, AuthorUserID: authorID, MinAgeHours: minAge, MaxAgeHours: maxAge,
		IncludeBlocked: includeBlocked != nil && *includeBlocked, Page: page,
	}, true
}

// ageOrBadRequest returns the query parameter name as a number of hours from 0 to maxQueueAgeHours, or nil if it
// is not set. If it is invalid it writes 400 invalid_request and returns false.
func ageOrBadRequest(w http.ResponseWriter, r *http.Request, query url.Values, name string) (*int32, bool) {
	if _, set := query[name]; !set {
		return nil, true
	}
	hours, ok := int32OrBadRequest(w, r, query, name, 0, 0, maxQueueAgeHours)
	if !ok {
		return nil, false
	}

	return &hours, true
}

// ModerationRevisionGet handles GET /v1/moderation/revisions/{revision_id}, a revision of any status but draft as
// moderators see it.
type ModerationRevisionGet struct {
	Command command.Command[command.RevisionRequest, domain.ModerationRevision]
}

func (c ModerationRevisionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), req)
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeModerationRevision(w, r, revision)
}

// RevisionApproveRequest is the JSON body of an approval: an optional review note for the author and summary to
// publish instead of the revision's, and the flags the moderator acknowledges, which must be given, possibly empty.
type RevisionApproveRequest struct {
	ReviewNote        *string            `json:"review_note"`
	Summary           *string            `json:"summary"`
	AcknowledgedFlags *[]domain.FlagCode `json:"acknowledged_flags"`
}

// RevisionApprove handles POST /v1/revisions/{revision_id}/approve, approving a pending revision.
type RevisionApprove struct {
	Command command.Command[command.ApproveRevisionRequest, domain.ModerationRevision]
}

func (c RevisionApprove) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}
	var body RevisionApproveRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	acknowledged, ok := acknowledgedFlagsOrBadRequest(w, r, body.AcknowledgedFlags)
	if !ok {
		return
	}
	reviewNote, ok := freeTextOrBadRequest(w, r, "review_note", body.ReviewNote, domain.MaxModeratorNoteLength)
	if !ok {
		return
	}
	summary, ok := freeTextOrBadRequest(w, r, "summary", body.Summary, domain.MaxSummaryLength)
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), command.ApproveRevisionRequest{
		Principal: req.Principal, RevisionID: req.ID, Options: domain.ApprovalOptions{
			AcknowledgedFlags: acknowledged, Summary: summary, ReviewNote: valueOrEmpty(reviewNote),
		},
	})
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeModerationRevision(w, r, revision)
}

// RevisionRejectRequest is the JSON body of a rejection: an optional review note for the author, and the flags the
// moderator acknowledges, which must be given, possibly empty.
type RevisionRejectRequest struct {
	ReviewNote        *string            `json:"review_note"`
	AcknowledgedFlags *[]domain.FlagCode `json:"acknowledged_flags"`
}

// RevisionReject handles POST /v1/revisions/{revision_id}/reject, rejecting a pending revision.
type RevisionReject struct {
	Command command.Command[command.RejectRevisionRequest, domain.ModerationRevision]
}

func (c RevisionReject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}
	var body RevisionRejectRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	acknowledged, ok := acknowledgedFlagsOrBadRequest(w, r, body.AcknowledgedFlags)
	if !ok {
		return
	}
	reviewNote, ok := freeTextOrBadRequest(w, r, "review_note", body.ReviewNote, domain.MaxModeratorNoteLength)
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), command.RejectRevisionRequest{
		Principal: req.Principal, RevisionID: req.ID, ReviewNote: valueOrEmpty(reviewNote),
		AcknowledgedFlags: acknowledged,
	})
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeModerationRevision(w, r, revision)
}

// RevisionRebaseRequest is the JSON body of a rebase: the document the moderator resolved against the entity's
// current revision CurrentRevisionID (null for a new entity), of EntityType, and an optional replacement summary.
type RevisionRebaseRequest struct {
	EntityType        domain.EntityType `json:"entity_type"`
	CurrentRevisionID *int64            `json:"current_revision_id"`
	Document          json.RawMessage   `json:"document"`
	Summary           *string           `json:"summary"`
}

// RevisionRebase handles POST /v1/revisions/{revision_id}/rebase, rebasing a pending revision onto its entity's
// current revision.
type RevisionRebase struct {
	Command command.Command[command.RebaseRevisionRequest, domain.ModerationRevision]
}

func (c RevisionRebase) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}
	var body RevisionRebaseRequest
	if !httpjson.DecodeJSONBodyLimit(w, r, &body, httpjson.MaxDocumentBodyBytes) {
		return
	}
	summary, ok := freeTextOrBadRequest(w, r, "summary", body.Summary, domain.MaxSummaryLength)
	if !ok {
		return
	}
	doc, sent, ok := editorDocumentToDomain(r.Context(), w, body.EntityType, body.Document)
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), command.RebaseRevisionRequest{
		Principal: req.Principal, RevisionID: req.ID, CurrentRevisionID: body.CurrentRevisionID, Document: doc,
		Sent: sent, Summary: summary,
	})
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeModerationRevision(w, r, revision)
}

// acknowledgedFlagsOrBadRequest returns the acknowledged flags of a moderator's request, which must be given. If
// they are missing or one is not a flag code, it writes 400 invalid_request and returns false; the caller should
// then return without writing.
func acknowledgedFlagsOrBadRequest(
	w http.ResponseWriter, r *http.Request, codes *[]domain.FlagCode,
) ([]domain.FlagCode, bool) {
	if codes == nil {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"acknowledged_flags is required")
		return nil, false
	}

	return *codes, flagCodesOrBadRequest(w, r, *codes)
}

// textOrBadRequest checks that the optional free-text field name has at most maxLength characters and no control
// character (domain.IsControlCharacter: a C0 or C1 control other than tab and line feed, or a bidirectional
// override, which could rewrite how a moderator's screen shows the text after it). Otherwise it writes 400
// invalid_request, naming the field but never quoting its value, and returns false; the caller should then return
// without writing.
func textOrBadRequest(w http.ResponseWriter, r *http.Request, name string, text *string, maxLength int) bool {
	switch {
	case text == nil:
		return true
	case utf8.RuneCountInString(*text) > maxLength:
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("invalid %s: must be at most %d characters", name, maxLength))
		return false
	case strings.ContainsFunc(*text, domain.IsControlCharacter):
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("invalid %s: must not contain a control character", name))
		return false
	}

	return true
}

// freeTextOrBadRequest is textOrBadRequest for free text that may span lines. It replaces CRLF and lone CR in
// *text with LF before it checks the length and the control characters, and returns the normalised text (nil for
// nil), which the caller must use in place of *text; text itself is not changed. It returns false after writing
// the 400.
func freeTextOrBadRequest(
	w http.ResponseWriter, r *http.Request, name string, text *string, maxLength int,
) (*string, bool) {
	if text == nil {
		return nil, true
	}
	normalised := domain.WithLineBreaksNormalized(*text)
	if !textOrBadRequest(w, r, name, &normalised, maxLength) {
		return nil, false
	}

	return &normalised, true
}

// valueOrEmpty returns *s, or "" for nil.
func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
