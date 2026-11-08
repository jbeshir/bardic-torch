package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.RepertoireEntryPutter  = (*Repository)(nil)
	_ datasources.RepertoireEntryDeleter = (*Repository)(nil)
	_ datasources.RepertoireEntryGetter  = (*Repository)(nil)
	_ datasources.RepertoireLister       = (*Repository)(nil)
	_ datasources.PublicRepertoireLister = (*Repository)(nil)
)

// PutRepertoireEntry adds a public piece to a user's repertoire, or replaces its entry's notes and visibility,
// keeping when it was added. Under the user's users row lock (lockUser), which refuses a user blocked since the
// request was authenticated and serialises the user's repertoire writes, it counts the entries of a new one
// against entry.MaxEntries, then writes, then reads the entry back.
func (r *Repository) PutRepertoireEntry(
	ctx context.Context, entry datasources.RepertoireEntryWrite,
) (domain.RepertoireEntry, bool, error) {
	var (
		stored  domain.RepertoireEntry
		created bool
	)
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		stored, created, err = r.txReader(tx).putRepertoireEntry(ctx, entry)
		return err
	})
	if err != nil {
		return domain.RepertoireEntry{}, false, databaseError("putting repertoire entry", err)
	}

	return stored, created, nil
}

// putRepertoireEntry is PutRepertoireEntry's work in the reader's transaction.
func (c *catalogueReader) putRepertoireEntry(
	ctx context.Context, entry datasources.RepertoireEntryWrite,
) (domain.RepertoireEntry, bool, error) {
	pieceID, err := c.lockRepertoirePiece(ctx, entry.UserID, entry.PieceSlug)
	if err != nil {
		return domain.RepertoireEntry{}, false, err
	}
	exists, err := c.q.RepertoireEntryExists(ctx, queries.RepertoireEntryExistsParams{
		UserID: entry.UserID, PieceID: pieceID,
	})
	if err != nil {
		return domain.RepertoireEntry{}, false, fmt.Errorf("fetching repertoire entry: %w", err)
	}
	if err := c.writeRepertoireEntry(ctx, entry, pieceID, !exists); err != nil {
		return domain.RepertoireEntry{}, false, err
	}
	stored := c.repertoireEntry(ctx, entry.UserID, entry.PieceSlug)

	return stored, !exists, c.err
}

// lockRepertoirePiece locks the users row of userID (lockUser) and returns the ID of the public piece with slug,
// or datasources.ErrNotFound if there is none.
func (c *catalogueReader) lockRepertoirePiece(ctx context.Context, userID, slug string) (uint64, error) {
	if _, err := c.lockUser(ctx, userID); err != nil {
		return 0, err
	}
	pieceID, err := c.q.GetPublicPieceID(ctx, slug)
	if err != nil {
		return 0, lookupError("piece", err)
	}

	return pieceID, nil
}

// writeRepertoireEntry inserts entry for the piece pieceID if created, refusing it if the repertoire is full, and
// otherwise updates the existing entry's notes, visibility and update time.
func (c *catalogueReader) writeRepertoireEntry(
	ctx context.Context, entry datasources.RepertoireEntryWrite, pieceID uint64, created bool,
) error {
	notes, visibility := nullString(entry.Notes), queries.RepertoireVisibility(entry.Visibility)
	if !created {
		err := c.q.UpdateRepertoireEntry(ctx, queries.UpdateRepertoireEntryParams{
			Notes: notes, Visibility: visibility, UpdatedAt: entry.Now, UserID: entry.UserID, PieceID: pieceID,
		})
		if err != nil {
			return fmt.Errorf("updating repertoire entry: %w", err)
		}
		return nil
	}

	count, err := c.q.CountRepertoireEntries(ctx, entry.UserID)
	if err != nil {
		return fmt.Errorf("counting repertoire entries: %w", err)
	}
	if count >= int64(entry.MaxEntries) {
		return datasources.ErrRepertoireLimitReached
	}
	err = c.q.InsertRepertoireEntry(ctx, queries.InsertRepertoireEntryParams{
		UserID: entry.UserID, PieceID: pieceID, Notes: notes, Visibility: visibility, AddedAt: entry.Now,
		UpdatedAt: entry.Now,
	})
	if err != nil {
		return fmt.Errorf("inserting repertoire entry: %w", err)
	}

	return nil
}

// DeleteRepertoireEntry removes a public piece from a user's repertoire, under the user's users row lock
// (lockUser), returning datasources.ErrNotFound if it is not there or no public piece has the slug.
func (r *Repository) DeleteRepertoireEntry(ctx context.Context, userID, pieceSlug string) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		pieceID, err := c.lockRepertoirePiece(ctx, userID, pieceSlug)
		if err != nil {
			return err
		}
		deleted, err := c.q.DeleteRepertoireEntry(ctx, queries.DeleteRepertoireEntryParams{
			UserID: userID, PieceID: pieceID,
		})
		if err != nil {
			return fmt.Errorf("deleting repertoire entry: %w", err)
		}
		if deleted == 0 {
			return datasources.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return databaseError("deleting repertoire entry", err)
	}

	return nil
}

// GetRepertoireEntry returns a user's entry for a public piece, returning datasources.ErrNotFound if the piece is
// not in their repertoire or no public piece has the slug.
func (r *Repository) GetRepertoireEntry(
	ctx context.Context, userID, pieceSlug string,
) (domain.RepertoireEntry, error) {
	var entry domain.RepertoireEntry
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		entry = c.repertoireEntry(ctx, userID, pieceSlug)
		return c.err
	})
	if err != nil {
		return domain.RepertoireEntry{}, fmt.Errorf("getting repertoire entry: %w", err)
	}

	return entry, nil
}

// repertoireEntry returns a user's entry for the public piece with slug, failing the reader with
// datasources.ErrNotFound if there is none.
func (c *catalogueReader) repertoireEntry(ctx context.Context, userID, slug string) domain.RepertoireEntry {
	if c.err != nil {
		return domain.RepertoireEntry{}
	}
	row, err := c.q.GetRepertoireEntry(ctx, queries.GetRepertoireEntryParams{UserID: userID, Slug: slug})
	if errors.Is(err, sql.ErrNoRows) {
		c.fail(datasources.ErrNotFound)
		return domain.RepertoireEntry{}
	}
	if err != nil {
		c.fail(fmt.Errorf("fetching repertoire entry: %w", queryError(err)))
		return domain.RepertoireEntry{}
	}

	return c.ownRepertoireEntries(ctx, []queries.ListRepertoireRow{queries.ListRepertoireRow(row)})[0]
}

// ListRepertoire lists one page of a user's own repertoire, entries of public pieces only, newest first, then by
// piece ID, with how many match in all.
func (r *Repository) ListRepertoire(
	ctx context.Context, query datasources.RepertoireQuery,
) (domain.RepertoireList, error) {
	var visibility queries.NullRepertoireVisibility
	if query.Visibility != nil {
		visibility = queries.NullRepertoireVisibility{
			RepertoireVisibility: queries.RepertoireVisibility(*query.Visibility), Valid: true,
		}
	}
	var list domain.RepertoireList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "repertoire", c.q.ListRepertoire, queries.ListRepertoireParams{
			UserID: query.UserID, Visibility: visibility, Limit: query.Page.Limit, Offset: query.Page.Offset,
		})
		list.Entries = c.ownRepertoireEntries(ctx, rows)
		list.Total = fetch(ctx, c, "repertoire count", c.q.CountRepertoire, queries.CountRepertoireParams{
			UserID: query.UserID, Visibility: visibility,
		})
		return c.err
	})
	if err != nil {
		return domain.RepertoireList{}, fmt.Errorf("listing repertoire: %w", err)
	}

	return list, nil
}

// ownRepertoireEntries converts the rows of a user's own repertoire to entries, with the pieces' summaries.
func (c *catalogueReader) ownRepertoireEntries(
	ctx context.Context, rows []queries.ListRepertoireRow,
) []domain.RepertoireEntry {
	slugs := make([]string, 0, len(rows))
	for _, row := range rows {
		slugs = append(slugs, row.Slug)
	}
	details := c.pieceDetails(ctx, slugs)
	entries := make([]domain.RepertoireEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, domain.RepertoireEntry{
			Piece:      c.pieceSummary(details, row.Slug, row.Title, row.Kind, row.ConservationStatus, row.IsStub),
			Notes:      stringPtr(row.Notes),
			Visibility: enumOf[domain.RepertoireVisibility](c, row.Visibility),
			AddedAt:    row.AddedAt,
			UpdatedAt:  row.UpdatedAt,
		})
	}

	return entries
}

// ListPublicRepertoire lists one page of the public entries, of public pieces, of the repertoire of the user with
// publicID, newest first, then by piece ID, with their owner and how many there are in all. An unknown public ID,
// a blocked user and a repertoire without such an entry are each datasources.ErrNotFound, so that a response
// never tells them apart. Notes are never read.
func (r *Repository) ListPublicRepertoire(
	ctx context.Context, publicID string, page domain.Page,
) (domain.PublicRepertoire, error) {
	var repertoire domain.PublicRepertoire
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		owner, err := c.q.GetRepertoireOwner(ctx, sql.NullString{String: publicID, Valid: true})
		if err != nil {
			return lookupError("repertoire owner", err)
		}
		repertoire.Total = fetch(ctx, c, "public repertoire count", c.q.CountPublicRepertoire, owner.ID)
		if c.err == nil && repertoire.Total == 0 {
			return datasources.ErrNotFound
		}
		rows := fetch(ctx, c, "public repertoire", c.q.ListPublicRepertoire, queries.ListPublicRepertoireParams{
			UserID: owner.ID, Limit: page.Limit, Offset: page.Offset,
		})
		role, err := domain.ParseRole(string(owner.Role))
		if err != nil {
			return fmt.Errorf("repertoire owner: %w", err)
		}
		repertoire.Owner = domain.RepertoireOwner{DisplayName: owner.DisplayName, Role: role}
		repertoire.Entries = c.publicRepertoireEntries(ctx, rows)
		return c.err
	})
	if err != nil {
		return domain.PublicRepertoire{}, fmt.Errorf("listing public repertoire: %w", err)
	}

	return repertoire, nil
}

// publicRepertoireEntries converts the rows of a public repertoire to entries, with the pieces' summaries.
func (c *catalogueReader) publicRepertoireEntries(
	ctx context.Context, rows []queries.ListPublicRepertoireRow,
) []domain.PublicRepertoireEntry {
	slugs := make([]string, 0, len(rows))
	for _, row := range rows {
		slugs = append(slugs, row.Slug)
	}
	details := c.pieceDetails(ctx, slugs)
	entries := make([]domain.PublicRepertoireEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, domain.PublicRepertoireEntry{
			Piece:   c.pieceSummary(details, row.Slug, row.Title, row.Kind, row.ConservationStatus, row.IsStub),
			AddedAt: row.AddedAt,
		})
	}

	return entries
}
