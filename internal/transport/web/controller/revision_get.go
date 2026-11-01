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

// RevisionSummary is the JSON representation of a public revision as an entity's history lists it.
type RevisionSummary struct {
	ID           int64                 `json:"id"`
	Source       domain.RevisionSource `json:"source"`
	Summary      string                `json:"summary"`
	Author       *RevisionAuthor       `json:"author"`
	Contributors []string              `json:"contributors"`
	CreatedAt    time.Time             `json:"created_at"`
}

// RevisionAuthor is the JSON representation of a revision's author.
type RevisionAuthor struct {
	DisplayName string `json:"display_name"`
}

// EntityRevision is the JSON representation of a public revision of an entity, shown as E, with snapshot views
// S: the variant of the spec's Revision for EntityType.
type EntityRevision[E, S any] struct {
	EntityType domain.EntityType `json:"entity_type"`
	RevisionSummary

	Entity         E        `json:"entity"`
	BaseRevisionID *int64   `json:"base_revision_id"`
	Snapshot       S        `json:"snapshot"`
	BaseSnapshot   *S       `json:"base_snapshot"`
	ChangedFields  []string `json:"changed_fields"`
}

// RevisionGet handles GET /v1/revisions/{revision_id}, a public revision with its snapshot, its base's snapshot
// and the fields that changed. A revision that is not approved, or whose entity is not public, is not found,
// exactly like an unknown one.
type RevisionGet struct {
	Getter datasources.PublicRevisionGetter
}

func (c RevisionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := idOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}

	revision, err := c.Getter.GetPublicRevision(ctx, id)
	if errors.Is(err, datasources.ErrNotFound) {
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "revision not found")
		return
	}
	if err != nil {
		writeInternalError(w, r, "unable to get revision", err)
		return
	}

	body, err := revisionFromDomain(revision)
	if err != nil {
		writeInternalError(w, r, "unable to show revision", err)
		return
	}
	httpjson.WriteJSON(ctx, w, http.StatusOK, body)
}

// errUnknownRevision is returned for a domain.Revision of no type the API shows.
var errUnknownRevision = errors.New("unknown revision type")

func revisionFromDomain(r domain.Revision) (any, error) {
	switch r := r.(type) {
	case domain.PieceRevision:
		return entityRevisionFromDomain(domain.EntityTypePiece, r, pieceSummaryFromDomain, pieceSnapshotFromDomain)
	case domain.PersonRevision:
		return entityRevisionFromDomain(domain.EntityTypePerson, r, personSummaryFromDomain, personSnapshotFromDomain)
	case domain.TermRevision:
		return entityRevisionFromDomain(domain.EntityTypeTerm, r, termSummaryFromDomain, termSnapshotFromDomain)
	case domain.CollectionRevision:
		return entityRevisionFromDomain(domain.EntityTypeCollection, r, collectionRefFromDomain,
			collectionSnapshotFromDomain)
	case domain.EventRevision:
		return entityRevisionFromDomain(domain.EntityTypeEvent, r, eventSummaryFromDomain, eventSnapshotFromDomain)
	default:
		return nil, fmt.Errorf("%w [%T]", errUnknownRevision, r)
	}
}

func entityRevisionFromDomain[E, S, JE, JS any](
	entityType domain.EntityType, r domain.EntityRevision[E, S], entity func(E) JE, snapshot func(S) JS,
) (EntityRevision[JE, JS], error) {
	revision := EntityRevision[JE, JS]{
		EntityType:      entityType,
		RevisionSummary: revisionSummaryFromDomain(r.RevisionSummary),
		Entity:          entity(r.Entity),
		Snapshot:        snapshot(r.Snapshot),
	}
	if r.Base != nil {
		base := snapshot(r.Base.Snapshot)
		revision.BaseRevisionID, revision.BaseSnapshot = &r.Base.ID, &base
	}

	changed, err := changedFields(revision.Snapshot, revision.BaseSnapshot)
	if err != nil {
		return EntityRevision[JE, JS]{}, err
	}
	revision.ChangedFields = changed

	return revision, nil
}

func revisionSummaryFromDomain(r domain.RevisionSummary) RevisionSummary {
	return RevisionSummary{
		ID:      r.ID,
		Source:  r.Source,
		Summary: r.Summary,
		Author: optional(r.Author, func(a domain.RevisionAuthor) RevisionAuthor {
			return RevisionAuthor{DisplayName: a.DisplayName}
		}),
		Contributors: nonNil(r.Contributors),
		CreatedAt:    r.CreatedAt,
	}
}
