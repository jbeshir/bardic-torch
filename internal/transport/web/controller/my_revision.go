package controller

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// MyRevisionSummary is the JSON representation of one of the caller's own revisions, drafts included, as their
// list of revisions shows it.
type MyRevisionSummary struct {
	ID         int64                 `json:"id"`
	Status     domain.RevisionStatus `json:"status"`
	EntityType domain.EntityType     `json:"entity_type"`
	// Entity is nil for a new entity's draft, or once a new entity's placeholder is deleted.
	Entity                  *EditorEntityRef      `json:"entity"`
	Title                   string                `json:"title"`
	Summary                 string                `json:"summary"`
	Source                  domain.RevisionSource `json:"source"`
	Author                  AuthorRef             `json:"author"`
	CreatedAt               time.Time             `json:"created_at"`
	SubmittedAt             *time.Time            `json:"submitted_at"`
	ReviewedAt              *time.Time            `json:"reviewed_at"`
	ReviewNote              *string               `json:"review_note"`
	BaseRevisionID          *int64                `json:"base_revision_id"`
	SubmittedBaseRevisionID *int64                `json:"submitted_base_revision_id"`
}

// AuthorRef is the JSON representation of a revision's author as they see themselves.
type AuthorRef struct {
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
}

// MyRevision is the JSON representation of one of the caller's own revisions with its views: the author's
// view of what they submitted and of the base they edited, or for an approved revision the public views.
type MyRevision struct {
	MyRevisionSummary

	Snapshot          any               `json:"snapshot"`
	BaseSnapshot      any               `json:"base_snapshot"`
	ChangedFields     []string          `json:"changed_fields"`
	PendingReferences []EditorReference `json:"pending_references"`
}

func myRevisionSummaryFromDomain(r domain.MyRevisionSummary) MyRevisionSummary {
	return MyRevisionSummary{
		ID: r.ID, Status: r.Status, EntityType: r.EntityType, Entity: optional(r.Entity, editorEntityRefFromDomain),
		Title: r.Title, Summary: r.Summary, Source: r.Source,
		Author:    AuthorRef{DisplayName: r.Author.DisplayName, Role: r.Author.Role},
		CreatedAt: r.CreatedAt, SubmittedAt: r.SubmittedAt, ReviewedAt: r.ReviewedAt, ReviewNote: r.ReviewNote,
		BaseRevisionID: r.BaseRevisionID, SubmittedBaseRevisionID: r.SubmittedBaseRevisionID,
	}
}

func myRevisionFromDomain(r domain.MyRevision) (MyRevision, error) {
	snapshot, base, changed, err := snapshotViews(r.Snapshot, r.BaseSnapshot)
	if err != nil {
		return MyRevision{}, err
	}

	return MyRevision{
		MyRevisionSummary: myRevisionSummaryFromDomain(r.MyRevisionSummary),
		Snapshot:          snapshot, BaseSnapshot: base, ChangedFields: changed,
		PendingReferences: mapSlice(r.PendingReferences, editorReferenceFromDomain),
	}, nil
}

// writeMyRevision writes revision as the response with status, or the error converting it.
func writeMyRevision(w http.ResponseWriter, r *http.Request, status int, revision domain.MyRevision) {
	body, err := myRevisionFromDomain(revision)
	if err != nil {
		WriteInternalError(w, r, "unable to show revision", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, status, body)
}
