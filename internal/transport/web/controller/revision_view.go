package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// RevisionView is the JSON representation of a revision of any status as its author sees it, with entity E and
// snapshot views S: the spec's RevisionView.
type RevisionView[E, S any] struct {
	EntityType domain.EntityType `json:"entity_type"`
	RevisionSummary

	Status         domain.RevisionStatus `json:"status"`
	ReviewNote     *string               `json:"review_note"`
	ReviewedAt     *time.Time            `json:"reviewed_at"`
	Entity         *E                    `json:"entity"`
	BaseRevisionID *int64                `json:"base_revision_id"`
	Snapshot       S                     `json:"snapshot"`
	BaseSnapshot   *S                    `json:"base_snapshot"`
	ChangedFields  []string              `json:"changed_fields"`
}

// ModerationRevisionView is the JSON representation of a revision as a moderator sees it: the spec's
// ModerationRevisionView.
type ModerationRevisionView[E, S any] struct {
	RevisionView[E, S]

	AuthorUserID              *string          `json:"author_user_id"`
	CurrentRevisionID         *int64           `json:"current_revision_id"`
	CurrentSnapshot           *S               `json:"current_snapshot"`
	ChangedFieldsSinceCurrent []string         `json:"changed_fields_since_current"`
	PossibleDuplicates        []DuplicateMatch `json:"possible_duplicates"`
}

// DuplicateMatch is the JSON representation of a public entity a new one may duplicate.
type DuplicateMatch struct {
	EntityType   domain.EntityType `json:"entity_type"`
	Slug         *string           `json:"slug"`
	CollectionID *int64            `json:"collection_id"`
	Name         string            `json:"name"`
}

func duplicateMatchFromDomain(c domain.DuplicateCandidate) DuplicateMatch {
	return DuplicateMatch{EntityType: c.EntityType, Slug: c.Slug, CollectionID: c.CollectionID, Name: c.Name}
}

// writeRevisionView writes the author's or, with moderation, the moderator's view of a revision.
func writeRevisionView(w http.ResponseWriter, r *http.Request, status int, v domain.RevisionView, moderation bool) {
	var (
		body any
		err  error
	)
	switch v := v.(type) {
	case domain.PieceRevisionView:
		body, err = revisionViewFromDomain(domain.EntityTypePiece, v, pieceSummaryFromDomain, pieceSnapshotFromDomain,
			moderation)
	case domain.PersonRevisionView:
		body, err = revisionViewFromDomain(domain.EntityTypePerson, v, personSummaryFromDomain,
			personSnapshotFromDomain, moderation)
	case domain.TermRevisionView:
		body, err = revisionViewFromDomain(domain.EntityTypeTerm, v, termSummaryFromDomain, termSnapshotFromDomain,
			moderation)
	case domain.CollectionRevisionView:
		body, err = revisionViewFromDomain(domain.EntityTypeCollection, v, collectionRefFromDomain,
			collectionSnapshotFromDomain, moderation)
	case domain.EventRevisionView:
		body, err = revisionViewFromDomain(domain.EntityTypeEvent, v, eventSummaryFromDomain, eventSnapshotFromDomain,
			moderation)
	default:
		err = fmt.Errorf("%w [%T]", errUnknownRevision, v)
	}
	if err != nil {
		writeInternalError(w, r, "unable to show revision", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, status, body)
}

func revisionViewFromDomain[E, S, JE, JS any](
	entityType domain.EntityType, v domain.EntityRevisionView[E, S], entity func(E) JE, snapshot func(S) JS,
	moderation bool,
) (any, error) {
	view := RevisionView[JE, JS]{
		EntityType:      entityType,
		RevisionSummary: revisionSummaryFromDomain(v.RevisionSummary),
		Status:          v.Status,
		ReviewNote:      v.ReviewNote,
		ReviewedAt:      v.ReviewedAt,
		Entity:          optional(v.Entity, entity),
		Snapshot:        snapshot(v.Snapshot),
	}
	if v.Base != nil {
		base := snapshot(v.Base.Snapshot)
		view.BaseRevisionID, view.BaseSnapshot = &v.Base.ID, &base
	}
	var err error
	if view.ChangedFields, err = changedFields(view.Snapshot, view.BaseSnapshot); err != nil {
		return nil, err
	}
	if !moderation {
		return view, nil
	}

	m := ModerationRevisionView[JE, JS]{RevisionView: view, AuthorUserID: v.AuthorUserID,
		PossibleDuplicates: mapSlice(v.PossibleDuplicates, duplicateMatchFromDomain)}
	if v.Current != nil {
		current := snapshot(v.Current.Snapshot)
		m.CurrentRevisionID, m.CurrentSnapshot = &v.Current.ID, &current
		if m.ChangedFieldsSinceCurrent, err = changedFields(view.Snapshot, &current); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// RevisionListItem is the JSON representation of a revision in the author's list or the moderation queue.
type RevisionListItem struct {
	ID         int64                 `json:"id"`
	EntityType domain.EntityType     `json:"entity_type"`
	Entity     *RevisionEntity       `json:"entity"`
	Status     domain.RevisionStatus `json:"status"`
	Summary    string                `json:"summary"`
	CreatedAt  time.Time             `json:"created_at"`
	ReviewedAt *time.Time            `json:"reviewed_at"`
	IsStale    bool                  `json:"is_stale"`
	Author     *ModerationAuthor     `json:"author"`
}

// RevisionEntity is the JSON representation of the entity a listed revision edits.
type RevisionEntity struct {
	Slug         *string `json:"slug"`
	CollectionID *int64  `json:"collection_id"`
	Name         string  `json:"name"`
}

// ModerationAuthor is the JSON representation of a revision's author as moderators see them.
type ModerationAuthor struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// writeRevisionList writes a page of revisions.
func writeRevisionList(w http.ResponseWriter, r *http.Request, list domain.RevisionList) {
	data := mapSlice(list.Revisions, func(i domain.RevisionListItem) RevisionListItem {
		return RevisionListItem{
			ID: i.ID, EntityType: i.EntityType, Status: i.Status, Summary: i.Summary, CreatedAt: i.CreatedAt,
			ReviewedAt: i.ReviewedAt, IsStale: i.IsStale,
			Entity: optional(i.Entity, func(e domain.RevisionEntity) RevisionEntity {
				return RevisionEntity{Slug: e.Slug, CollectionID: e.CollectionID, Name: e.Name}
			}),
			Author: optional(i.Author, func(a domain.RevisionAuthorAccount) ModerationAuthor {
				return ModerationAuthor{UserID: a.UserID, DisplayName: a.DisplayName}
			}),
		}
	})
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, struct {
		Data  []RevisionListItem `json:"data"`
		Total int64              `json:"total"`
	}{data, list.Total})
}

// MyRevisionListGet handles GET /v1/me/revisions.
type MyRevisionListGet struct {
	Revisions datasources.Revisions
}

func (c MyRevisionListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	status, ok := enumOrBadRequest(w, r, query, "status", func(s string) (domain.RevisionStatus, error) {
		if v := domain.RevisionStatus(s); v.Valid() {
			return v, nil
		}
		return "", errors.New("unknown status")
	})
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Revisions.ListMyRevisions(r.Context(), principal.User.ID, status, page)
	if err != nil {
		writeInternalError(w, r, "unable to list revisions", err)
		return
	}
	writeRevisionList(w, r, list)
}

// MyRevisionGet handles GET /v1/me/revisions/{revision_id}.
type MyRevisionGet struct {
	Revisions datasources.Revisions
}

func (c MyRevisionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "revision_id")
	principal, authed := principalOrUnauthenticated(w, r)
	if !ok || !authed {
		return
	}
	view, err := c.Revisions.GetMyRevision(r.Context(), principal.User.ID, id)
	writeRevisionGet(w, r, view, err, false)
}

// writeRevisionGet writes the view of a revision that was read, or the error reading it failed with.
func writeRevisionGet(w http.ResponseWriter, r *http.Request, view domain.RevisionView, err error, moderation bool) {
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		notFound(w, r, "revision not found")
	case err != nil:
		writeInternalError(w, r, "unable to get revision", err)
	default:
		writeRevisionView(w, r, http.StatusOK, view, moderation)
	}
}

// ModerationQueueGet handles GET /v1/moderation/queue.
type ModerationQueueGet struct {
	Revisions datasources.Revisions
}

func (c ModerationQueueGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Revisions.ListModerationQueue(r.Context(), page)
	if err != nil {
		writeInternalError(w, r, "unable to list the moderation queue", err)
		return
	}
	writeRevisionList(w, r, list)
}

// ModerationRevisionGet handles GET /v1/moderation/revisions/{revision_id}.
type ModerationRevisionGet struct {
	Revisions datasources.Revisions
}

func (c ModerationRevisionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}
	view, err := c.Revisions.GetModerationRevision(r.Context(), id)
	writeRevisionGet(w, r, view, err, true)
}
