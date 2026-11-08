package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// The messages of the repertoire's 404s. An entry of a piece that is not public is not found exactly like an
// entry of an unknown piece, and a public repertoire that cannot be shown exactly like an unknown one.
const (
	repertoireEntryNotFound = "repertoire entry"
	repertoireNotFound      = "repertoire not found"
)

// RepertoireEntry is the JSON representation of an entry of the caller's own repertoire.
type RepertoireEntry struct {
	Piece      PieceSummary                `json:"piece"`
	Notes      *string                     `json:"notes"`
	Visibility domain.RepertoireVisibility `json:"visibility"`
	AddedAt    time.Time                   `json:"added_at"`
	UpdatedAt  time.Time                   `json:"updated_at"`
}

func repertoireEntryFromDomain(e domain.RepertoireEntry) RepertoireEntry {
	return RepertoireEntry{
		Piece: pieceSummaryFromDomain(e.Piece), Notes: e.Notes, Visibility: e.Visibility, AddedAt: e.AddedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// RepertoireList is the JSON representation of one page of the caller's own repertoire.
type RepertoireList struct {
	Data  []RepertoireEntry `json:"data"`
	Total int64             `json:"total"`
}

// PublicRepertoireEntry is the JSON representation of a public entry of a user's repertoire: never its notes.
type PublicRepertoireEntry struct {
	Piece   PieceSummary `json:"piece"`
	AddedAt time.Time    `json:"added_at"`
}

// RepertoireOwner is the JSON representation of the owner of a public repertoire: never their user ID.
type RepertoireOwner struct {
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
}

// PublicRepertoire is the JSON representation of one page of a user's public repertoire.
type PublicRepertoire struct {
	Owner RepertoireOwner         `json:"owner"`
	Data  []PublicRepertoireEntry `json:"data"`
	Total int64                   `json:"total"`
}

// RepertoireEntryPutRequest is the JSON body of a repertoire entry: the caller's notes, absent or null for none,
// and the entry's visibility.
type RepertoireEntryPutRequest struct {
	Notes      *string                     `json:"notes"`
	Visibility domain.RepertoireVisibility `json:"visibility"`
}

// MyRepertoireList handles GET /v1/me/repertoire, one page of the caller's repertoire, newest entry first,
// optionally of one visibility.
type MyRepertoireList struct {
	Command command.Command[command.RepertoireListRequest, domain.RepertoireList]
}

func (c MyRepertoireList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	visibility, ok := enumOrBadRequest(w, r, query, "visibility", domain.ParseRepertoireVisibility)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Command.Execute(r.Context(), command.RepertoireListRequest{
		Principal: principal, Visibility: visibility, Page: page,
	})
	if err != nil {
		writeRevisionError(w, r, "repertoire", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, RepertoireList{
		Data: mapSlice(list.Entries, repertoireEntryFromDomain), Total: list.Total,
	})
}

// MyRepertoireEntryGet handles GET /v1/me/repertoire/{slug}, the caller's entry for a public piece.
type MyRepertoireEntryGet struct {
	Command command.Command[command.RepertoireEntryRequest, domain.RepertoireEntry]
}

func (c MyRepertoireEntryGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	entry, err := c.Command.Execute(r.Context(), command.RepertoireEntryRequest{Principal: principal, PieceSlug: slug})
	if err != nil {
		writeRevisionError(w, r, repertoireEntryNotFound, err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, repertoireEntryFromDomain(entry))
}

// MyRepertoireEntryPut handles PUT /v1/me/repertoire/{slug}, adding a public piece to the caller's repertoire
// (201) or replacing its entry's notes and visibility (200). The notes are moderation free text: CRLF and a lone
// CR become LF before they are checked and stored.
type MyRepertoireEntryPut struct {
	Command command.Command[command.PutRepertoireEntryRequest, command.PutRepertoireEntryResult]
}

func (c MyRepertoireEntryPut) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}
	var body RepertoireEntryPutRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if !body.Visibility.Valid() {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid visibility: must be private or public")
		return
	}
	notes, ok := freeTextOrBadRequest(w, r, "notes", body.Notes, domain.MaxRepertoireNotesLength)
	if !ok {
		return
	}

	result, err := c.Command.Execute(ctx, command.PutRepertoireEntryRequest{
		Principal: principal, PieceSlug: slug, Notes: notes, Visibility: body.Visibility,
	})
	if err != nil {
		writeRevisionError(w, r, "piece", err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	httpjson.WriteJSON(ctx, w, status, repertoireEntryFromDomain(result.Entry))
}

// MyRepertoireEntryDelete handles DELETE /v1/me/repertoire/{slug}, removing a public piece from the caller's
// repertoire.
type MyRepertoireEntryDelete struct {
	Command command.Command[command.RepertoireEntryRequest, command.Empty]
}

func (c MyRepertoireEntryDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	_, err := c.Command.Execute(r.Context(), command.RepertoireEntryRequest{Principal: principal, PieceSlug: slug})
	if err != nil {
		writeRevisionError(w, r, repertoireEntryNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublicRepertoireGet handles GET /v1/users/{public_id}/repertoire, one page of the public entries of a user's
// repertoire. An unknown public ID, a blocked user and a user with no public entry are each not found, with one
// body, so that none can be told from another.
type PublicRepertoireGet struct {
	Lister datasources.PublicRepertoireLister
}

func (c PublicRepertoireGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	publicID := r.PathValue("public_id")
	if !domain.IsPublicID(publicID) {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid public_id: must be 16 lowercase letters and digits from 2 to 7")
		return
	}
	page, ok := requestPageOrBadRequest(w, r)
	if !ok {
		return
	}

	repertoire, err := c.Lister.ListPublicRepertoire(ctx, publicID, page)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, repertoireNotFound)
	case err != nil:
		WriteInternalError(w, r, "unable to list public repertoire", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, PublicRepertoire{
			Owner: RepertoireOwner{DisplayName: repertoire.Owner.DisplayName, Role: repertoire.Owner.Role},
			Data: mapSlice(repertoire.Entries, func(e domain.PublicRepertoireEntry) PublicRepertoireEntry {
				return PublicRepertoireEntry{Piece: pieceSummaryFromDomain(e.Piece), AddedAt: e.AddedAt}
			}),
			Total: repertoire.Total,
		})
	}
}
