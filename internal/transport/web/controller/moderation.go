package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// The moderator views (D8). They hold hidden aliases, hidden links and user IDs, so only the moderator operations,
// which only interactive moderators reach (D25), serve them.

// ModerationAuthor is the JSON representation of a user as moderator views show them.
type ModerationAuthor struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
	Blocked     bool        `json:"blocked"`
}

func moderationAuthorFromDomain(a domain.ModerationAuthor) ModerationAuthor {
	return ModerationAuthor{ID: a.ID, DisplayName: a.DisplayName, Role: a.Role, Blocked: a.Blocked}
}

// QueueItem is the JSON representation of a pending revision in the moderation queue.
type QueueItem struct {
	ID          int64             `json:"id"`
	EntityType  domain.EntityType `json:"entity_type"`
	Entity      *EditorEntityRef  `json:"entity"`
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	Author      ModerationAuthor  `json:"author"`
	SubmittedAt time.Time         `json:"submitted_at"`
	Stale       bool              `json:"stale"`
	IsNew       bool              `json:"is_new"`
	Flags       []domain.FlagCode `json:"flags"`
}

// ModerationQueue is the JSON representation of one page of the moderation queue, with how many there are.
type ModerationQueue struct {
	Data  []QueueItem `json:"data"`
	Total int64       `json:"total"`
}

func moderationQueueFromDomain(q domain.ModerationQueue) ModerationQueue {
	return ModerationQueue{Data: mapSlice(q.Items, func(i domain.QueueItem) QueueItem {
		return QueueItem{
			ID: i.ID, EntityType: i.EntityType, Entity: optional(i.Entity, editorEntityRefFromDomain), Title: i.Title,
			Summary: i.Summary, Author: moderationAuthorFromDomain(i.Author), SubmittedAt: i.SubmittedAt,
			Stale: i.Stale, IsNew: i.IsNew, Flags: append([]domain.FlagCode{}, i.Flags...),
		}
	}), Total: q.Total}
}

// RebaseConflict is the JSON representation of a conflict of a stale revision with its entity's current document,
// in the editor format (domain.EditorConflicts): each value is the JSON of what base, revision and current hold at
// the path, as a string, or null where the element is missing.
type RebaseConflict struct {
	Path         string  `json:"path"`
	BaseJSON     *string `json:"base_json"`
	RevisionJSON *string `json:"revision_json"`
	CurrentJSON  *string `json:"current_json"`
}

// RebasePreview is the JSON representation of what approving a revision would do with its document.
type RebasePreview struct {
	Status         domain.RebaseStatus      `json:"status"`
	MergedDocument domain.AnyEditorDocument `json:"merged_document"`
	Conflicts      []RebaseConflict         `json:"conflicts"`
}

// ModerationPendingReference is the JSON representation of a pending new term or person a revision references.
type ModerationPendingReference struct {
	RevisionID int64                    `json:"revision_id"`
	EntityType domain.EntityType        `json:"entity_type"`
	Entity     EditorEntityRef          `json:"entity"`
	Document   domain.AnyEditorDocument `json:"document"`
	Flags      []Flag                   `json:"flags"`
}

// ModerationRevision is the JSON representation of a revision as a moderator sees it (D8).
type ModerationRevision struct {
	ID                      int64                        `json:"id"`
	Status                  domain.RevisionStatus        `json:"status"`
	EntityType              domain.EntityType            `json:"entity_type"`
	Entity                  *EditorEntityRef             `json:"entity"`
	Source                  domain.RevisionSource        `json:"source"`
	Author                  *ModerationAuthor            `json:"author"`
	Contributors            []string                     `json:"contributors"`
	Reviewer                *ModerationAuthor            `json:"reviewer"`
	RebasedBy               *ModerationAuthor            `json:"rebased_by"`
	Summary                 string                       `json:"summary"`
	ReviewNote              *string                      `json:"review_note"`
	CreatedAt               time.Time                    `json:"created_at"`
	SubmittedAt             *time.Time                   `json:"submitted_at"`
	ReviewedAt              *time.Time                   `json:"reviewed_at"`
	RebasedAt               *time.Time                   `json:"rebased_at"`
	BaseRevisionID          *int64                       `json:"base_revision_id"`
	SubmittedBaseRevisionID *int64                       `json:"submitted_base_revision_id"`
	CurrentRevisionID       *int64                       `json:"current_revision_id"`
	Stale                   bool                         `json:"stale"`
	Document                domain.AnyEditorDocument     `json:"document"`
	SubmittedDocument       domain.AnyEditorDocument     `json:"submitted_document"`
	BaseDocument            domain.AnyEditorDocument     `json:"base_document"`
	CurrentDocument         domain.AnyEditorDocument     `json:"current_document"`
	References              []EditorReference            `json:"references"`
	LinkStates              []LinkState                  `json:"link_states"`
	Rebase                  RebasePreview                `json:"rebase"`
	PendingReferences       []ModerationPendingReference `json:"pending_references"`
	Flags                   []Flag                       `json:"flags"`
	Duplicates              []DuplicateMatch             `json:"duplicates"`
	ChangedPaths            []string                     `json:"changed_paths"`
}

// editorDocuments converts a moderation view's documents to the editor format with the slugs of its references,
// keeping the first error.
type editorDocuments struct {
	refs []domain.EditorReference
	err  error
}

// convert returns doc in the editor format, nil for none.
func (e *editorDocuments) convert(doc domain.AnyDocument) domain.AnyEditorDocument {
	if doc == nil || e.err != nil {
		return nil
	}
	editor, err := editorDocumentFromDomain(doc, e.refs)
	if err != nil {
		e.err = err
	}

	return editor
}

func moderationRevisionFromDomain(r domain.ModerationRevision) (ModerationRevision, error) {
	docs := editorDocuments{refs: r.References}
	slugs := domain.NewSlugMap(r.References)
	conflicts, err := domain.EditorConflicts(r.EntityType, r.Rebase.Conflicts, slugs)
	if err != nil {
		return ModerationRevision{}, fmt.Errorf("revision [%d] conflicts: %w", r.ID, err)
	}
	changed, err := domain.EditorFieldPaths(r.EntityType, r.ChangedPaths, slugs)
	if err != nil {
		return ModerationRevision{}, fmt.Errorf("revision [%d] changed paths: %w", r.ID, err)
	}
	view := ModerationRevision{
		ID: r.ID, Status: r.Status, EntityType: r.EntityType, Entity: optional(r.Entity, editorEntityRefFromDomain),
		Source: r.Source, Author: optional(r.Author, moderationAuthorFromDomain),
		Contributors: append([]string{}, r.Contributors...), Reviewer: optional(r.Reviewer, moderationAuthorFromDomain),
		RebasedBy: optional(r.RebasedBy, moderationAuthorFromDomain),
		Summary:   r.Summary, ReviewNote: r.ReviewNote, CreatedAt: r.CreatedAt, SubmittedAt: r.SubmittedAt,
		ReviewedAt: r.ReviewedAt, RebasedAt: r.RebasedAt, BaseRevisionID: r.BaseRevisionID,
		SubmittedBaseRevisionID: r.SubmittedBaseRevisionID, CurrentRevisionID: r.CurrentRevisionID, Stale: r.Stale,
		Document: docs.convert(r.Document), SubmittedDocument: docs.convert(r.SubmittedDocument),
		BaseDocument: docs.convert(r.BaseDocument), CurrentDocument: docs.convert(r.CurrentDocument),
		References: mapSlice(r.References, editorReferenceFromDomain),
		LinkStates: mapSlice(r.LinkStates, linkStateFromDomain),
		Rebase: RebasePreview{Status: r.Rebase.Status, MergedDocument: docs.convert(r.Rebase.MergedDocument),
			Conflicts: mapSlice(conflicts, rebaseConflictFromDomain)},
		PendingReferences: mapSlice(r.PendingReferences,
			func(p domain.ModerationPendingReference) ModerationPendingReference {
				return ModerationPendingReference{RevisionID: p.RevisionID, EntityType: p.EntityType,
					Entity: editorEntityRefFromDomain(p.Entity), Document: docs.convert(p.Document),
					Flags: mapSlice(p.Flags, flagFromDomain)}
			}),
		Flags: mapSlice(r.Flags, flagFromDomain), Duplicates: mapSlice(r.Duplicates, duplicateMatchFromDomain),
		ChangedPaths: mapSlice(changed, func(p domain.FieldPath) string { return string(p) }),
	}
	if docs.err != nil {
		return ModerationRevision{}, fmt.Errorf("revision [%d]: %w", r.ID, docs.err)
	}

	return view, nil
}

func linkStateFromDomain(s domain.LinkState) LinkState {
	return LinkState{URL: s.URL, LinkID: s.LinkID, IsHidden: s.IsHidden, Liveness: s.Liveness}
}

func rebaseConflictFromDomain(c domain.RebaseConflict) RebaseConflict {
	return RebaseConflict{Path: string(c.Path), BaseJSON: rawJSONString(c.Base),
		RevisionJSON: rawJSONString(c.Revision), CurrentJSON: rawJSONString(c.Current)}
}

// rawJSONString returns raw as a string, nil for no value or JSON null.
func rawJSONString(raw []byte) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s := string(raw)

	return &s
}

// writeModerationRevision writes 200 with the revision as moderators see it.
func writeModerationRevision(w http.ResponseWriter, r *http.Request, revision domain.ModerationRevision) {
	view, err := moderationRevisionFromDomain(revision)
	if err != nil {
		WriteInternalError(w, r, "unable to show moderation revision", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, view)
}

// ModerationLinkAttachment is the JSON representation of an entity a link is attached to, as moderators see it.
type ModerationLinkAttachment struct {
	EntityType domain.EntityType `json:"entity_type"`
	Slug       string            `json:"slug"`
	Title      string            `json:"title"`
	Public     bool              `json:"public"`
	Roles      []domain.LinkRole `json:"roles"`
}

// ModerationLink is the JSON representation of a link as moderators see it, hidden or not.
type ModerationLink struct {
	LinkID          int64                      `json:"link_id"`
	URL             string                     `json:"url"`
	Title           *string                    `json:"title"`
	Description     *string                    `json:"description"`
	Platform        *string                    `json:"platform"`
	PlaceType       *domain.LinkPlaceType      `json:"place_type"`
	Collection      *CollectionRef             `json:"collection"`
	Locator         *string                    `json:"locator"`
	Liveness        domain.Liveness            `json:"liveness"`
	IsHidden        bool                       `json:"is_hidden"`
	Attachments     []ModerationLinkAttachment `json:"attachments"`
	OpenReportCount int64                      `json:"open_report_count"`
}

func moderationLinkFromDomain(l domain.ModerationLink) ModerationLink {
	return ModerationLink{
		LinkID: l.LinkID, URL: l.URL, Title: l.Title, Description: l.Description, Platform: l.Platform,
		PlaceType: l.PlaceType, Collection: optional(l.Collection, collectionRefFromDomain), Locator: l.Locator,
		Liveness: l.Liveness, IsHidden: l.IsHidden,
		Attachments: mapSlice(l.Attachments, func(a domain.ModerationLinkAttachment) ModerationLinkAttachment {
			return ModerationLinkAttachment{EntityType: a.EntityType, Slug: a.Slug, Title: a.Title, Public: a.Public,
				Roles: append([]domain.LinkRole{}, a.Roles...)}
		}),
		OpenReportCount: l.OpenReportCount,
	}
}

// LinkReport is the JSON representation of a link report as moderators see it.
type LinkReport struct {
	ID         int64                   `json:"id"`
	Link       ModerationLink          `json:"link"`
	Reason     domain.LinkReportReason `json:"reason"`
	Message    *string                 `json:"message"`
	Reporter   ModerationAuthor        `json:"reporter"`
	Status     domain.LinkReportStatus `json:"status"`
	CreatedAt  time.Time               `json:"created_at"`
	ResolvedAt *time.Time              `json:"resolved_at"`
	ResolvedBy *ModerationAuthor       `json:"resolved_by"`
	Note       *string                 `json:"note"`
}

// LinkReportList is the JSON representation of one page of link reports, with how many there are.
type LinkReportList struct {
	Data  []LinkReport `json:"data"`
	Total int64        `json:"total"`
}

func linkReportFromDomain(r domain.LinkReport) LinkReport {
	return LinkReport{
		ID: r.ID, Link: moderationLinkFromDomain(r.Link), Reason: r.Reason, Message: r.Message,
		Reporter: moderationAuthorFromDomain(r.Reporter), Status: r.Status, CreatedAt: r.CreatedAt,
		ResolvedAt: r.ResolvedAt, ResolvedBy: optional(r.ResolvedBy, moderationAuthorFromDomain), Note: r.Note,
	}
}
