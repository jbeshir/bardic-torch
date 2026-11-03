package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.ModerationQueueLister    = (*Repository)(nil)
	_ datasources.ModerationRevisionGetter = (*Repository)(nil)
	_ datasources.ModerationLinkGetter     = (*Repository)(nil)
)

// moderationViewer is who moderator views are built for: an interactive moderator, who sees every entity, every
// pending new entity among them.
var moderationViewer = domain.Actor{Role: domain.RoleModerator}

// ListModerationQueue lists one page of the pending revisions, oldest first.
func (r *Repository) ListModerationQueue(
	ctx context.Context, query datasources.ModerationQueueQuery,
) (domain.ModerationQueue, error) {
	params := queries.ListModerationQueueParams{AuthorUserID: nullString(query.AuthorUserID),
		IncludeBlocked: query.IncludeBlocked, Limit: query.Page.Limit, Offset: query.Page.Offset}
	if query.EntityType != nil {
		params.EntityType = queries.NullRevisionsEntityType{
			RevisionsEntityType: queries.RevisionsEntityType(*query.EntityType), Valid: true,
		}
	}
	if query.MinAgeHours != nil {
		params.SubmittedBefore = nullTime(query.Now.Add(-time.Duration(*query.MinAgeHours) * time.Hour))
	}
	if query.MaxAgeHours != nil {
		params.SubmittedAfter = nullTime(query.Now.Add(-time.Duration(*query.MaxAgeHours) * time.Hour))
	}

	var queue domain.ModerationQueue
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "moderation queue", c.q.ListModerationQueue, params)
		queue.Items = make([]domain.QueueItem, 0, len(rows))
		for _, row := range rows {
			item, err := c.queueItem(ctx, row)
			if err != nil {
				return err
			}
			queue.Items = append(queue.Items, item)
		}
		queue.Total = fetch(ctx, c, "moderation queue count", c.q.CountModerationQueue,
			queries.CountModerationQueueParams{EntityType: params.EntityType, AuthorUserID: params.AuthorUserID,
				SubmittedBefore: params.SubmittedBefore, SubmittedAfter: params.SubmittedAfter,
				IncludeBlocked: params.IncludeBlocked})
		return c.err
	})
	if err != nil {
		return domain.ModerationQueue{}, fmt.Errorf("listing the moderation queue: %w", err)
	}

	return queue, nil
}

func (c *catalogueReader) queueItem(ctx context.Context, row queries.ListModerationQueueRow) (domain.QueueItem, error) {
	entityType := enumOf[domain.EntityType](c, row.EntityType)
	kind, err := kindFor(entityType)
	if err != nil {
		return domain.QueueItem{}, err
	}
	doc, err := kind.decode(row.SnapshotJson)
	if err != nil {
		return domain.QueueItem{}, fmt.Errorf("revision [%d] snapshot: %w", row.ID, err)
	}
	flags, err := decodeFlags(row.FlagsJson)
	if err != nil {
		return domain.QueueItem{}, fmt.Errorf("revision [%d]: %w", row.ID, err)
	}
	author, err := moderationAuthor(row.AuthorID, row.AuthorDisplayName, row.AuthorRole, row.AuthorBlockedAt)
	if err != nil {
		return domain.QueueItem{}, err
	}
	entity, current, err := c.entityAndCurrent(ctx, entityType, row.EntityID)
	if err != nil {
		return domain.QueueItem{}, err
	}

	return domain.QueueItem{
		ID: c.signedID(row.ID), EntityType: entityType, Entity: entity, Title: documentTitle(doc),
		Summary: row.Summary, Author: author, SubmittedAt: row.SubmittedAt.Time,
		Stale: !equalIDs(int64Ptr(row.BaseRevisionID), current), IsNew: current == nil, Flags: flagCodes(flags),
	}, nil
}

// flagCodes returns the codes of flags, each once, in order.
func flagCodes(flags []domain.Flag) []domain.FlagCode {
	codes := []domain.FlagCode{}
	for _, f := range flags {
		if !slices.Contains(codes, f.Code) {
			codes = append(codes, f.Code)
		}
	}

	return codes
}

// moderationAuthor returns a user as moderator views show them.
func moderationAuthor(
	id, displayName string, role queries.UsersRole, blockedAt sql.NullTime,
) (domain.ModerationAuthor, error) {
	parsed, err := domain.ParseRole(string(role))
	if err != nil {
		return domain.ModerationAuthor{}, err
	}

	return domain.ModerationAuthor{ID: id, DisplayName: displayName, Role: parsed, Blocked: blockedAt.Valid}, nil
}

// optionalModerationAuthor is moderationAuthor of a user a revision may have, nil for none.
func optionalModerationAuthor(
	id, displayName sql.NullString, role queries.NullUsersRole, blockedAt sql.NullTime,
) (*domain.ModerationAuthor, error) {
	if !id.Valid {
		return nil, nil
	}
	author, err := moderationAuthor(id.String, displayName.String, role.UsersRole, blockedAt)
	if err != nil {
		return nil, err
	}

	return &author, nil
}

// entityAndCurrent returns a revision's entity as its author or a moderator addresses it, and its current revision
// (nil for a pending new entity); both are nil if it has none or its row is gone (a deleted placeholder).
func (c *catalogueReader) entityAndCurrent(
	ctx context.Context, entityType domain.EntityType, entityID sql.NullInt64,
) (*domain.RevisionEntity, *int64, error) {
	if !entityID.Valid {
		return nil, nil, nil
	}
	row, err := c.entityRow(ctx, entityType, entityID.Int64, false)
	if errors.Is(err, datasources.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	return &domain.RevisionEntity{Type: entityType, ID: entityID.Int64, Slug: row.slug,
		IsNew: row.currentRevision == nil}, row.currentRevision, nil
}

// GetModerationRevision returns a revision of any status but draft as a moderator sees it (D8).
func (r *Repository) GetModerationRevision(ctx context.Context, id int64) (domain.ModerationRevision, error) {
	key, ok := positiveID(id)
	if !ok {
		return domain.ModerationRevision{}, fmt.Errorf("getting moderation revision [%d]: %w", id,
			datasources.ErrNotFound)
	}
	var revision domain.ModerationRevision
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := c.q.GetModerationRevision(ctx, key)
		if err != nil {
			return lookupError("revision", err)
		}
		revision, err = c.moderationRevision(ctx, row)
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return domain.ModerationRevision{}, fmt.Errorf("getting moderation revision [%d]: %w", id, err)
	}

	return revision, nil
}

// moderationDocuments are the documents of a moderation view: the revision's, what its author submitted, its
// base's and its entity's current one, each nil for none.
type moderationDocuments struct {
	doc, submitted, base, current domain.AnyDocument
}

// all returns the documents that are not nil.
func (d moderationDocuments) all() []domain.AnyDocument {
	return slices.DeleteFunc([]domain.AnyDocument{d.doc, d.submitted, d.base, d.current},
		func(doc domain.AnyDocument) bool { return doc == nil })
}

func (c *catalogueReader) moderationRevision(
	ctx context.Context, row queries.GetModerationRevisionRow,
) (domain.ModerationRevision, error) {
	entityType := enumOf[domain.EntityType](c, row.EntityType)
	kind, err := kindFor(entityType)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	entity, current, err := c.entityAndCurrent(ctx, entityType, row.EntityID)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	revision, err := c.moderationRevisionRow(ctx, row)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	revision.EntityType, revision.Entity, revision.CurrentRevisionID = entityType, entity, current
	revision.Stale = revision.Status == domain.RevisionStatusPending && !equalIDs(revision.BaseRevisionID, current)
	docs, err := c.moderationDocuments(ctx, kind, row, current)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	revision.Document, revision.SubmittedDocument, revision.BaseDocument, revision.CurrentDocument = docs.doc,
		docs.submitted, docs.base, docs.current
	revision.Rebase = rebasePreview(kind, revision.Stale, docs)

	id := row.EntityID.Int64
	pending := c.authorPendingReferences(ctx, kind, domain.EntityRef{Type: entityType, ID: id},
		row.AuthorUserID.String, docs.doc)
	if revision.PendingReferences, err = c.moderationPendingReferences(ctx, pending); err != nil {
		return domain.ModerationRevision{}, err
	}
	revision.Duplicates = c.moderationDuplicates(ctx, revision)
	if revision.Flags, err = c.moderationFlags(ctx, kind, row, revision, docs, pending); err != nil {
		return domain.ModerationRevision{}, err
	}
	revision.ChangedPaths = []string{}
	for _, path := range kind.changedPaths(docs.base, docs.doc) {
		revision.ChangedPaths = append(revision.ChangedPaths, string(path))
	}

	revision.References = c.moderationReferences(ctx, kind, id, revision, docs, pending)
	revision.LinkStates = c.documentLinkStates(ctx, moderationViewer, id, kind, docs.doc)

	return revision, nil
}

// moderationReferences returns the entities every document of a moderation view references, as a moderator sees
// them: the documents of docs, the merged document of the rebase preview and the pending references' documents.
func (c *catalogueReader) moderationReferences(
	ctx context.Context, kind documentKind, id int64, revision domain.ModerationRevision, docs moderationDocuments,
	pending []pendingNewEntity,
) []domain.EditorReference {
	var refs domain.SnapshotReferences
	for _, doc := range docs.all() {
		kind.addReferences(id, doc, &refs)
	}
	if revision.Rebase.MergedDocument != nil {
		kind.addReferences(id, revision.Rebase.MergedDocument, &refs)
	}
	for _, p := range pending {
		documentKinds[p.entity.Type].addReferences(p.entity.ID, p.doc, &refs)
	}

	return c.editorReferences(ctx, moderationViewer, refs, c.referencedPending(ctx, moderationViewer))
}

// moderationRevisionRow returns the fields of a moderation view that are the revision row's own.
func (c *catalogueReader) moderationRevisionRow(
	ctx context.Context, row queries.GetModerationRevisionRow,
) (domain.ModerationRevision, error) {
	author, err := optionalModerationAuthor(row.AuthorUserID, row.AuthorDisplayName, row.AuthorRole,
		row.AuthorBlockedAt)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	reviewer, err := optionalModerationAuthor(row.ReviewerUserID, row.ReviewerDisplayName, row.ReviewerRole,
		row.ReviewerBlockedAt)
	if err != nil {
		return domain.ModerationRevision{}, err
	}
	contributors := c.contributors(ctx, []uint64{row.ID})[row.ID]
	if contributors == nil {
		contributors = []string{}
	}

	return domain.ModerationRevision{
		ID: c.signedID(row.ID), Status: enumOf[domain.RevisionStatus](c, row.Status),
		Source: enumOf[domain.RevisionSource](c, row.Source), Author: author, Contributors: contributors,
		Reviewer: reviewer, Summary: row.Summary, ReviewNote: stringPtr(row.ReviewNote), CreatedAt: row.CreatedAt,
		SubmittedAt: timePtr(row.SubmittedAt), ReviewedAt: timePtr(row.ReviewedAt), RebasedAt: timePtr(row.RebasedAt),
		BaseRevisionID: int64Ptr(row.BaseRevisionID), SubmittedBaseRevisionID: int64Ptr(row.SubmittedBaseRevisionID),
	}, nil
}

// moderationDocuments loads the documents of the revision row, whose entity's current revision is current.
func (c *catalogueReader) moderationDocuments(
	ctx context.Context, kind documentKind, row queries.GetModerationRevisionRow, current *int64,
) (moderationDocuments, error) {
	var docs moderationDocuments
	var err error
	if docs.doc, err = kind.decode(row.SnapshotJson); err != nil {
		return moderationDocuments{}, fmt.Errorf("revision [%d] snapshot: %w", row.ID, err)
	}
	if len(row.SubmittedSnapshotJson) > 0 {
		if docs.submitted, err = kind.decode(row.SubmittedSnapshotJson); err != nil {
			return moderationDocuments{}, fmt.Errorf("revision [%d] submitted snapshot: %w", row.ID, err)
		}
	}
	entity := domain.RevisionEntity{Type: enumOf[domain.EntityType](c, row.EntityType), ID: row.EntityID.Int64}
	if row.BaseRevisionID.Valid {
		if docs.base, err = c.approvedDocument(ctx, kind, entity, row.BaseRevisionID.Int64); err != nil {
			return moderationDocuments{}, err
		}
	}
	if current != nil {
		if docs.current, err = c.approvedDocument(ctx, kind, entity, *current); err != nil {
			return moderationDocuments{}, err
		}
	}

	return docs, nil
}

// rebasePreview is what approving a revision would do with its document (D9): merge a stale revision that has a
// base with the current document, cleanly or with conflicts. A revision that is not stale, or has no base or
// current document to merge with, needs no rebase.
func rebasePreview(kind documentKind, stale bool, docs moderationDocuments) domain.RebasePreview {
	preview := domain.RebasePreview{Status: domain.RebaseStatusNotNeeded, Conflicts: []domain.RebaseConflict{}}
	if !stale || docs.base == nil || docs.current == nil {
		return preview
	}
	merged, conflicts := kind.merge(docs.base, docs.doc, docs.current)
	if len(conflicts) > 0 {
		preview.Status, preview.Conflicts = domain.RebaseStatusConflicts, conflicts
		return preview
	}
	preview.Status, preview.MergedDocument = domain.RebaseStatusClean, merged

	return preview
}

// authorPendingReferences returns the pending new terms and persons of the user authorID (none for an imported
// revision) that doc, the document of the entity own of kind, references, directly or through one another's
// documents, each after those its own document references: those approving it would approve alongside (D15).
func (c *catalogueReader) authorPendingReferences(
	ctx context.Context, kind documentKind, own domain.EntityRef, authorID string, doc domain.AnyDocument,
) []pendingNewEntity {
	if authorID == "" {
		return nil
	}

	return pendingReferenceOrder(own, referencesOf(kind, own.ID, doc), c.pendingCandidates(ctx, authorID))
}

// moderationPendingReferences returns the pending references, each with the flags stored on its revision.
func (c *catalogueReader) moderationPendingReferences(
	ctx context.Context, pending []pendingNewEntity,
) ([]domain.ModerationPendingReference, error) {
	out := make([]domain.ModerationPendingReference, 0, len(pending))
	for _, p := range pending {
		row, err := c.q.GetRevision(ctx, p.revisionID)
		if err != nil {
			return nil, fmt.Errorf("fetching pending reference [%d]: %w", p.revisionID, err)
		}
		flags, err := decodeFlags(row.FlagsJson)
		if err != nil {
			return nil, fmt.Errorf("pending reference [%d]: %w", p.revisionID, err)
		}
		if flags == nil {
			flags = []domain.Flag{}
		}
		out = append(out, domain.ModerationPendingReference{
			RevisionID: c.signedID(p.revisionID), EntityType: p.entity.Type,
			Entity:   domain.RevisionEntity{Type: p.entity.Type, ID: p.entity.ID, Slug: p.slug, IsNew: true},
			Document: p.doc, Flags: flags,
		})
	}

	return out, nil
}

// moderationDuplicates returns the entities a pending new entity may duplicate, hidden aliases and the other
// pending new entities compared (D13), as approving it would find them; none for any other revision.
func (c *catalogueReader) moderationDuplicates(
	ctx context.Context, revision domain.ModerationRevision,
) []domain.DuplicateMatch {
	if revision.Status != domain.RevisionStatusPending || revision.Entity == nil || !revision.Entity.IsNew {
		return []domain.DuplicateMatch{}
	}
	own := *revision.Entity
	matches := c.findDuplicates(ctx, domain.NewDuplicateCandidate(revision.Document), true)

	return slices.DeleteFunc(append([]domain.DuplicateMatch{}, matches...), func(m domain.DuplicateMatch) bool {
		return m.EntityType == own.Type && (own.Type == domain.EntityTypeCollection && m.CollectionID == own.ID ||
			own.Type != domain.EntityTypeCollection && m.Slug == own.Slug)
	})
}

// moderationFlags returns the flags of a moderation view: for a pending revision whose entity exists, the flags
// approving it gates on (domain.ApprovalFlags), its pending references being pending and its possible duplicates
// revision.Duplicates; for any other, the flags stored on it.
func (c *catalogueReader) moderationFlags(
	ctx context.Context, kind documentKind, row queries.GetModerationRevisionRow, revision domain.ModerationRevision,
	docs moderationDocuments, pending []pendingNewEntity,
) ([]domain.Flag, error) {
	stored, err := decodeFlags(row.FlagsJson)
	if err != nil {
		return nil, fmt.Errorf("revision [%d]: %w", row.ID, err)
	}
	if revision.Status != domain.RevisionStatusPending || revision.Entity == nil {
		if stored == nil {
			stored = []domain.Flag{}
		}
		return stored, nil
	}
	in := approvalInput{
		revisionID: row.ID, authorID: row.AuthorUserID.String, author: domain.RoleContributor,
		entity: domain.EntityRef{Type: revision.EntityType, ID: revision.Entity.ID}, kind: kind, doc: docs.doc,
		baseRevisionID: revision.BaseRevisionID, summary: row.Summary, storedFlags: stored,
	}
	hidden := fetchAll(ctx, c, "hidden aliases", c.q.ListHiddenAliasNames)
	state := c.approvalState(ctx, in, ownPendingRefs(pending), hidden, docs.doc, docs.base, docs.current)
	state.currentRevisionID = revision.CurrentRevisionID
	state.possibleDuplicate = len(revision.Duplicates) > 0
	if c.err != nil {
		return nil, c.err
	}
	flags := kind.approvalFlags(state)
	if flags == nil {
		flags = []domain.Flag{}
	}

	return flags, nil
}

// GetModerationLink returns the link whose normalized URL is url's as a moderator sees it. The URL is not named
// in errors: it may be a hidden link's.
func (r *Repository) GetModerationLink(ctx context.Context, url string) (domain.ModerationLink, error) {
	normalized, err := domain.NormalizeURL(url)
	if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("getting moderation link: %w", datasources.ErrNotFound)
	}
	var link domain.ModerationLink
	err = r.readOnly(ctx, func(c *catalogueReader) error {
		id, err := c.q.GetLinkIDByNormalizedURL(ctx, normalized)
		if err != nil {
			return lookupError("link", err)
		}
		link, err = c.moderationLink(ctx, id)
		return err
	})
	if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("getting moderation link: %w", err)
	}

	return link, nil
}

// moderationLink returns the link id as a moderator sees it.
func (c *catalogueReader) moderationLink(ctx context.Context, id uint64) (domain.ModerationLink, error) {
	row, err := c.q.GetModerationLink(ctx, id)
	if err != nil {
		return domain.ModerationLink{}, lookupError("link", err)
	}
	link := domain.ModerationLink{
		LinkID: c.signedID(row.ID), URL: row.Url, Title: stringPtr(row.Title),
		Description: stringPtr(row.Description), Platform: stringPtr(row.Platform),
		PlaceType: nullEnumOf[domain.LinkPlaceType](c, row.PlaceType.LinksPlaceType, row.PlaceType.Valid),
		Locator:   stringPtr(row.Locator), Liveness: enumOf[domain.Liveness](c, row.Liveness),
		IsHidden: row.IsHidden, Attachments: c.linkAttachments(ctx, row.ID), OpenReportCount: row.OpenReportCount,
	}
	if row.CollectionID.Valid {
		link.Collection = &domain.CollectionRef{ID: row.CollectionID.Int64, Name: row.CollectionName.String}
	}

	return link, c.err
}

// linkAttachments returns the entities the link id is attached to, pieces first, then persons, each by ID, with
// the link's roles on each in attachment order.
func (c *catalogueReader) linkAttachments(ctx context.Context, id uint64) []domain.ModerationLinkAttachment {
	out := []domain.ModerationLinkAttachment{}
	index := map[domain.EntityRef]int{}
	for _, row := range fetch(ctx, c, "link attachments", c.q.ListModerationLinkAttachments, id) {
		key := domain.EntityRef{Type: domain.EntityTypePiece, ID: row.PieceID.Int64}
		attachment := domain.ModerationLinkAttachment{EntityType: domain.EntityTypePiece, Slug: row.PieceSlug.String,
			Title: row.PieceTitle.String, Public: row.PiecePublic}
		if !row.PieceID.Valid {
			key = domain.EntityRef{Type: domain.EntityTypePerson, ID: row.PersonID.Int64}
			attachment = domain.ModerationLinkAttachment{EntityType: domain.EntityTypePerson,
				Slug: row.PersonSlug.String, Title: row.PersonName.String, Public: row.PersonPublic}
		}
		i, seen := index[key]
		if !seen {
			i = len(out)
			index[key] = i
			out = append(out, attachment)
		}
		out[i].Roles = append(out[i].Roles, enumOf[domain.LinkRole](c, row.Role))
	}

	return out
}
