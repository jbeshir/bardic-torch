package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// DocumentError is the JSON body of a 422 invalid_document response: the problems of a document, each at its path
// in the editor format. Neither the message nor a problem ever quotes a value.
type DocumentError struct {
	Code     httpjson.ErrorCode `json:"code"`
	Message  string             `json:"message"`
	Problems []DocumentProblem  `json:"problems"`
}

// DocumentProblem is the JSON representation of a problem of a document.
type DocumentProblem struct {
	Path    string             `json:"path"`
	Problem domain.ProblemCode `json:"problem"`
}

// DuplicateError is the JSON body of a 409 possible_duplicate response: the entities a new one may duplicate.
type DuplicateError struct {
	Code       httpjson.ErrorCode `json:"code"`
	Message    string             `json:"message"`
	Duplicates []DuplicateMatch   `json:"duplicates"`
}

// FlagsError is the JSON body of a response refusing a revision for its flags, which only moderators see.
type FlagsError struct {
	Code    httpjson.ErrorCode `json:"code"`
	Message string             `json:"message"`
	Flags   []Flag             `json:"flags"`
}

// Flag is the JSON representation of a flag on a revision (D19), with the path of what raised it, if anything in
// particular did.
type Flag struct {
	Code domain.FlagCode `json:"code"`
	Path *string         `json:"path"`
}

func flagFromDomain(f domain.Flag) Flag {
	flag := Flag{Code: f.Code}
	if f.Path != "" {
		flag.Path = &f.Path
	}

	return flag
}

// The messages of the revision writes' error responses. None quotes a value.
const (
	notDraftMessage          = "the revision is not a draft"
	invalidDocumentMessage   = "the document is invalid"
	duplicateBlockedMessage  = "a person with this name has asked not to have a page"
	possibleDuplicateMessage = "the new entity may duplicate an existing one: acknowledge_duplicates confirms it"
	unavailableMessage       = "the write could not be completed in time; try again"
)

// revisionErrorAnswer is the response to an error of a revision command.
type revisionErrorAnswer struct {
	status  int
	code    httpjson.ErrorCode
	message string
}

// revisionErrorAnswers are the responses to the revision commands' sentinel errors.
var revisionErrorAnswers = []struct {
	err    error
	answer revisionErrorAnswer
}{
	{domain.ErrNotDraft, revisionErrorAnswer{http.StatusConflict, httpjson.CodeNotDraft, notDraftMessage}},
	{domain.ErrNotPending, revisionErrorAnswer{http.StatusConflict, httpjson.CodeNotPending,
		"the revision is not pending"}},
	{datasources.ErrDraftLimitReached, revisionErrorAnswer{http.StatusConflict, httpjson.CodeDraftLimitReached,
		fmt.Sprintf("a user may hold at most %d drafts", domain.MaxDrafts)}},
	{datasources.ErrPendingLimitReached, revisionErrorAnswer{http.StatusConflict, httpjson.CodePendingLimitReached,
		fmt.Sprintf("a user may have at most %d pending revisions", domain.MaxOpenPendingRevisions)}},
	{datasources.ErrEntityGone, revisionErrorAnswer{http.StatusConflict, httpjson.CodeEntityGone,
		"the entity, or an entity it references, no longer exists"}},
	{datasources.ErrAlreadyCurrent, revisionErrorAnswer{http.StatusConflict, httpjson.CodeAlreadyCurrent,
		"the revision is already the entity's current revision"}},
	{datasources.ErrDocumentType, revisionErrorAnswer{http.StatusBadRequest, httpjson.CodeInvalidRequest,
		"invalid entity_type: not the draft's entity type"}},
	{domain.ErrRebaseRequired, revisionErrorAnswer{http.StatusConflict, httpjson.CodeRebaseRequired,
		"the entity changed since the base, and the changes conflict: edit the current version"}},
	{domain.ErrHiddenDataConflict, revisionErrorAnswer{http.StatusConflict, httpjson.CodeHiddenDataConflict,
		"the revision conflicts with hidden data"}},
	{domain.ErrDuplicateBlocked, revisionErrorAnswer{http.StatusUnprocessableEntity, httpjson.CodeDuplicateBlocked,
		duplicateBlockedMessage}},
	{datasources.ErrUnavailable, revisionErrorAnswer{http.StatusServiceUnavailable, httpjson.CodeUnavailable,
		unavailableMessage}},
}

// writeRevisionError writes the response for err, an error of a revision command about what (such as "draft"):
// its status and code, or 500 internal for an unexpected error, which it logs. Another user's draft or revision is
// not found, exactly like an unknown one.
func writeRevisionError(w http.ResponseWriter, r *http.Request, what string, err error) {
	ctx := r.Context()
	if errors.Is(err, datasources.ErrNotFound) || errors.Is(err, domain.ErrNotApproved) {
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, what+" not found")
		return
	}
	for _, a := range revisionErrorAnswers {
		if errors.Is(err, a.err) {
			httpjson.WriteError(ctx, w, a.answer.status, a.answer.code, a.answer.message)
			return
		}
	}
	if body, ok := revisionErrorBody(err); ok {
		httpjson.WriteJSON(ctx, w, body.status, body.body)
		return
	}
	writeInternalError(w, r, "unable to handle "+what, err)
}

// revisionErrorResponse is a status and JSON body.
type revisionErrorResponse struct {
	status int
	body   any
}

// revisionErrorBody returns the status and body of the response to err if it is one of the typed errors whose
// response has more than a code and message.
func revisionErrorBody(err error) (revisionErrorResponse, bool) {
	var (
		documentErr  *domain.DocumentError
		duplicateErr *command.PossibleDuplicateError
		flagsErr     *domain.UnacknowledgedFlagsError
		pendingErr   *domain.PendingReferenceFlaggedError
	)
	switch {
	case errors.As(err, &documentErr):
		return revisionErrorResponse{http.StatusUnprocessableEntity, DocumentError{
			Code: httpjson.CodeInvalidDocument, Message: invalidDocumentMessage,
			Problems: mapSlice(documentErr.Problems, func(p domain.DocumentProblem) DocumentProblem {
				return DocumentProblem{Path: p.Path, Problem: p.Code}
			}),
		}}, true
	case errors.As(err, &duplicateErr):
		return revisionErrorResponse{http.StatusConflict, DuplicateError{
			Code: httpjson.CodePossibleDuplicate, Message: possibleDuplicateMessage,
			Duplicates: mapSlice(duplicateErr.Duplicates, duplicateMatchFromDomain),
		}}, true
	case errors.As(err, &flagsErr):
		return revisionErrorResponse{http.StatusConflict, FlagsError{
			Code: httpjson.CodeUnacknowledgedFlags, Message: "the revision has flags that are not acknowledged",
			Flags: mapSlice(flagsErr.Flags, flagFromDomain),
		}}, true
	case errors.As(err, &pendingErr):
		return revisionErrorResponse{http.StatusConflict, FlagsError{
			Code: httpjson.CodePendingReferenceFlagged,
			Message: fmt.Sprintf("the referenced pending revision %d has flags: approve it on its own first",
				pendingErr.RevisionID),
			Flags: mapSlice(pendingErr.Flags, flagFromDomain),
		}}, true
	default:
		return revisionErrorResponse{}, false
	}
}
