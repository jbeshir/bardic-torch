package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// maxDocumentBodyBytes is the largest body of a revision submission.
const maxDocumentBodyBytes = 1 << 20

// EditableResponse is the JSON body of an entity's editable document.
type EditableResponse struct {
	EntityType        domain.EntityType `json:"entity_type"`
	BaseRevisionID    int64             `json:"base_revision_id"`
	PendingRevisionID *int64            `json:"pending_revision_id"`
	Document          any               `json:"document"`
}

// EditableGet handles GET /v1/{type}/{key}/editable: the editor-format document of a public entity, by slug or,
// for a collection, by collection_id.
type EditableGet struct {
	Revisions  datasources.Revisions
	EntityType domain.EntityType
}

func (c EditableGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	var key string
	if c.EntityType == domain.EntityTypeCollection {
		id, ok := idOrBadRequest(w, r, "collection_id")
		if !ok {
			return
		}
		key = strconv.FormatInt(id, 10)
	} else if key, ok = slugOrBadRequest(w, r); !ok {
		return
	}

	editable, err := c.Revisions.GetEditable(r.Context(), c.EntityType, key, principal.User.ID)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		notFound(w, r, string(c.EntityType)+" not found")
	case err != nil:
		writeInternalError(w, r, "unable to get editable document", err)
	default:
		httpjson.WriteJSON(r.Context(), w, http.StatusOK, EditableResponse{EntityType: c.EntityType,
			BaseRevisionID: editable.BaseRevisionID, PendingRevisionID: editable.PendingRevisionID,
			Document: editable.Document})
	}
}

// RevisionCreateRequest is the JSON body of a revision submission. Document is decoded as the editor document of
// EntityType.
type RevisionCreateRequest struct {
	EntityType            domain.EntityType `json:"entity_type"`
	BaseRevisionID        *int64            `json:"base_revision_id"`
	Summary               string            `json:"summary"`
	Document              json.RawMessage   `json:"document"`
	AcknowledgeDuplicates bool              `json:"acknowledge_duplicates"`
	AcknowledgeStale      bool              `json:"acknowledge_stale"`
}

// RevisionCreate handles POST /v1/revisions.
type RevisionCreate struct {
	Command command.Command[command.SubmitRevisionRequest, domain.RevisionView]
}

func (c RevisionCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	var body RevisionCreateRequest
	if !httpjson.DecodeJSONBodyMax(w, r, &body, maxDocumentBodyBytes) {
		return
	}
	doc, err := decodeEditorDocument(body.EntityType, body.Document)
	if err != nil {
		badRequest(w, r, "entity_type must be an entity type, and document its editor document")
		return
	}

	view, err := c.Command.Execute(r.Context(), command.SubmitRevisionRequest{
		User: principal.User, BaseRevisionID: body.BaseRevisionID, Summary: body.Summary, Document: doc,
		AcknowledgeDuplicates: body.AcknowledgeDuplicates, AcknowledgeStale: body.AcknowledgeStale,
	})
	var duplicate *command.DuplicateError
	switch {
	case errors.As(err, &duplicate):
		httpjson.WriteJSON(r.Context(), w, http.StatusConflict, struct {
			Code       httpjson.ErrorCode `json:"code"`
			Message    string             `json:"message"`
			Duplicates []DuplicateMatch   `json:"possible_duplicates"`
		}{httpjson.CodePossibleDuplicate, "the new entity may duplicate public ones; acknowledge_duplicates to submit",
			mapSlice(duplicate.Matches, duplicateMatchFromDomain)})
	case errors.Is(err, datasources.ErrNotFound):
		notFound(w, r, "base revision not found")
	case err != nil:
		writeRevisionError(w, r, err, "unable to submit revision")
	default:
		writeRevisionView(w, r, http.StatusCreated, view, false)
	}
}

// decodeEditorDocument decodes raw as the editor document of entityType, rejecting unknown fields.
func decodeEditorDocument(entityType domain.EntityType, raw json.RawMessage) (any, error) {
	switch entityType {
	case domain.EntityTypePiece:
		return decodeStrict[domain.EditorPieceDocument](raw)
	case domain.EntityTypePerson:
		return decodeStrict[domain.EditorPersonDocument](raw)
	case domain.EntityTypeTerm:
		return decodeStrict[domain.EditorTermDocument](raw)
	case domain.EntityTypeCollection:
		return decodeStrict[domain.EditorCollectionDocument](raw)
	case domain.EntityTypeEvent:
		return decodeStrict[domain.EditorEventDocument](raw)
	default:
		return nil, fmt.Errorf("unknown entity type [%s]", entityType)
	}
}

func decodeStrict[T any](raw json.RawMessage) (any, error) {
	var doc T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding document: %w", err)
	}

	return doc, nil
}

// writeRevisionError writes the answer to a revision change that failed with err: the revision rule it broke, or
// 500.
func writeRevisionError(w http.ResponseWriter, r *http.Request, err error, msg string) {
	var invalid *datasources.ValidationError
	switch {
	case errors.As(err, &invalid):
		httpjson.WriteJSON(r.Context(), w, http.StatusUnprocessableEntity, struct {
			Code     httpjson.ErrorCode `json:"code"`
			Message  string             `json:"message"`
			Problems []domain.Problem   `json:"problems"`
		}{httpjson.CodeValidationFailed, "the document does not validate", invalid.Problems})
	case errors.Is(err, datasources.ErrNotFound):
		notFound(w, r, "revision not found")
	case errors.Is(err, domain.ErrRevisionNotPending):
		httpjson.WriteError(r.Context(), w, http.StatusConflict, httpjson.CodeRevisionNotPending,
			"the revision is not pending")
	case errors.Is(err, datasources.ErrPendingLimit):
		httpjson.WriteError(r.Context(), w, http.StatusConflict, httpjson.CodePendingLimitReached,
			fmt.Sprintf("you already have %d pending revisions", datasources.MaxPendingRevisions))
	case errors.Is(err, datasources.ErrStale):
		httpjson.WriteError(r.Context(), w, http.StatusConflict, httpjson.CodeStaleRevision,
			"the entity has changed since the revision's base; acknowledge_stale to overwrite the changes")
	default:
		writeInternalError(w, r, msg, err)
	}
}

// writeChanged writes the answer to a withdrawal, approval or rejection: 204, or the error err is.
func writeChanged(w http.ResponseWriter, r *http.Request, err error, msg string) {
	if err != nil {
		writeRevisionError(w, r, err, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevisionWithdraw handles POST /v1/revisions/{revision_id}/withdraw, by the revision's author.
type RevisionWithdraw struct {
	Revisions datasources.Revisions
}

func (c RevisionWithdraw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "revision_id")
	principal, authed := principalOrUnauthenticated(w, r)
	if !ok || !authed {
		return
	}
	writeChanged(w, r, c.Revisions.WithdrawRevision(r.Context(), id, principal.User.ID), "unable to withdraw revision")
}

// RevisionApproveRequest is the JSON body of a moderator's approval.
type RevisionApproveRequest struct {
	AcknowledgeStale bool `json:"acknowledge_stale"`
}

// RevisionApprove handles POST /v1/revisions/{revision_id}/approve.
type RevisionApprove struct {
	Revisions datasources.Revisions
	Now       func() time.Time
}

func (c RevisionApprove) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "revision_id")
	principal, authed := principalOrUnauthenticated(w, r)
	if !ok || !authed {
		return
	}
	var body RevisionApproveRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	writeChanged(w, r, c.Revisions.ApproveRevision(r.Context(), id, principal.User.ID, body.AcknowledgeStale, c.Now()),
		"unable to approve revision")
}

// RevisionRejectRequest is the JSON body of a moderator's rejection.
type RevisionRejectRequest struct {
	Note string `json:"note"`
}

// RevisionReject handles POST /v1/revisions/{revision_id}/reject.
type RevisionReject struct {
	Revisions datasources.Revisions
	Now       func() time.Time
}

func (c RevisionReject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "revision_id")
	principal, authed := principalOrUnauthenticated(w, r)
	if !ok || !authed {
		return
	}
	var body RevisionRejectRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	note, noteOK := optionalText(&body.Note, domain.MaxNoteLength)
	if note == nil || !noteOK {
		badRequest(w, r, "note is required, at most 2000 characters")
		return
	}
	writeChanged(w, r, c.Revisions.RejectRevision(r.Context(), id, principal.User.ID, *note, c.Now()),
		"unable to reject revision")
}
