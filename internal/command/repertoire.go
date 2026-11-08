package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ Command[PutRepertoireEntryRequest, PutRepertoireEntryResult] = (*PutRepertoireEntry)(nil)
	_ Command[RepertoireEntryRequest, Empty]                       = (*RemoveRepertoireEntry)(nil)
	_ Command[RepertoireEntryRequest, domain.RepertoireEntry]      = (*GetRepertoireEntry)(nil)
	_ Command[RepertoireListRequest, domain.RepertoireList]        = (*ListRepertoire)(nil)
)

// A user's repertoire is their own: every command acts on the principal's repertoire, whichever credential they
// use, and never reads or writes another user's.

// PutRepertoireEntryRequest is Principal's entry for the public piece with slug PieceSlug: their notes, normalised
// as moderation free text, and its visibility.
type PutRepertoireEntryRequest struct {
	Principal  domain.Principal
	PieceSlug  string
	Notes      *string
	Visibility domain.RepertoireVisibility
}

// PutRepertoireEntryResult is the entry as stored; Created is set when it is new.
type PutRepertoireEntryResult struct {
	Entry   domain.RepertoireEntry
	Created bool
}

// PutRepertoireEntry adds a public piece to the principal's repertoire, or replaces its entry's notes and
// visibility. A repertoire holds at most domain.MaxRepertoireEntries entries.
type PutRepertoireEntry struct {
	Putter datasources.RepertoireEntryPutter
	Now    func() time.Time
}

// Execute writes the entry. It fails with the errors of datasources.RepertoireEntryPutter.
func (c *PutRepertoireEntry) Execute(
	ctx context.Context, req PutRepertoireEntryRequest,
) (PutRepertoireEntryResult, error) {
	entry, created, err := c.Putter.PutRepertoireEntry(ctx, datasources.RepertoireEntryWrite{
		UserID: req.Principal.User.ID, PieceSlug: req.PieceSlug, Notes: req.Notes, Visibility: req.Visibility,
		MaxEntries: domain.MaxRepertoireEntries, Now: c.Now(),
	})
	if err != nil {
		return PutRepertoireEntryResult{}, fmt.Errorf("putting repertoire entry: %w", err)
	}

	return PutRepertoireEntryResult{Entry: entry, Created: created}, nil
}

// RepertoireEntryRequest is Principal's request about their entry for the public piece with slug PieceSlug.
type RepertoireEntryRequest struct {
	Principal domain.Principal
	PieceSlug string
}

// RemoveRepertoireEntry removes a public piece from the principal's repertoire.
type RemoveRepertoireEntry struct {
	Deleter datasources.RepertoireEntryDeleter
}

// Execute removes the entry. It fails with the errors of datasources.RepertoireEntryDeleter.
func (c *RemoveRepertoireEntry) Execute(ctx context.Context, req RepertoireEntryRequest) (Empty, error) {
	if err := c.Deleter.DeleteRepertoireEntry(ctx, req.Principal.User.ID, req.PieceSlug); err != nil {
		return Empty{}, fmt.Errorf("removing repertoire entry: %w", err)
	}

	return Empty{}, nil
}

// GetRepertoireEntry returns the principal's entry for a public piece, notes included.
type GetRepertoireEntry struct {
	Getter datasources.RepertoireEntryGetter
}

// Execute returns the entry, or datasources.ErrNotFound if the piece is not in the principal's repertoire.
func (c *GetRepertoireEntry) Execute(ctx context.Context, req RepertoireEntryRequest) (domain.RepertoireEntry, error) {
	entry, err := c.Getter.GetRepertoireEntry(ctx, req.Principal.User.ID, req.PieceSlug)
	if err != nil {
		return domain.RepertoireEntry{}, fmt.Errorf("getting repertoire entry: %w", err)
	}

	return entry, nil
}

// RepertoireListRequest is Principal's request for one page of their repertoire, optionally of one visibility.
type RepertoireListRequest struct {
	Principal  domain.Principal
	Visibility *domain.RepertoireVisibility
	Page       domain.Page
}

// ListRepertoire lists one page of the principal's repertoire, newest entry first.
type ListRepertoire struct {
	Lister datasources.RepertoireLister
}

// Execute returns the page.
func (c *ListRepertoire) Execute(ctx context.Context, req RepertoireListRequest) (domain.RepertoireList, error) {
	list, err := c.Lister.ListRepertoire(ctx, datasources.RepertoireQuery{
		UserID: req.Principal.User.ID, Visibility: req.Visibility, Page: req.Page,
	})
	if err != nil {
		return domain.RepertoireList{}, fmt.Errorf("listing repertoire: %w", err)
	}

	return list, nil
}
