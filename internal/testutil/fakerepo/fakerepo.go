// Package fakerepo is the one hand-written fake of the datasources for tests above the database: it holds users
// in a map and returns canned catalogue values. Tests of the real queries use MySQL instead.
package fakerepo

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.Repository        = (*Repo)(nil)
	_ datasources.CatalogueImporter = (*Repo)(nil)
)

// Repo implements datasources.Repository and datasources.CatalogueImporter. A catalogue read returns its canned
// value, or datasources.ErrNotFound for a get whose value is nil. When Err is set every method returns it
// instead. A Repo is not safe for concurrent use: tests make one each.
type Repo struct {
	// Users are the stored users by ID.
	Users map[string]domain.User

	Piece       *domain.Piece
	Pieces      domain.PieceList
	Person      *domain.Person
	Persons     domain.PersonList
	Terms       []domain.TermListItem
	Term        *domain.Term
	Collection  *domain.Collection
	Collections domain.CollectionList
	Event       *domain.Event
	Events      domain.EventList
	History     domain.RevisionHistory
	Revision    domain.Revision
	NeedsWork   domain.NeedsWorkList

	LinkReports     domain.LinkReportList
	ModerationLink  *domain.ModerationLink
	ContactRequests domain.ContactRequestList
	// ContactRequest is the last contact request stored.
	ContactRequest *datasources.NewContactRequest

	// Err, if set, is returned by every method, and PingErr by Ping only.
	Err     error
	PingErr error

	// Imported is the last import written.
	Imported *domain.CatalogueImport
}

func (r *Repo) Ping(context.Context) error {
	if r.Err != nil {
		return r.Err
	}
	return r.PingErr
}

func (r *Repo) GetUser(_ context.Context, userID string) (domain.User, error) {
	user, ok := r.Users[userID]
	if r.Err != nil || !ok {
		return domain.User{}, notFoundOr(r.Err)
	}
	return user, nil
}

func (r *Repo) EnsureUser(_ context.Context, user datasources.NewUser) (domain.User, error) {
	if r.Err != nil {
		return domain.User{}, r.Err
	}
	if stored, ok := r.Users[user.ID]; ok {
		return stored, nil
	}
	stored := domain.User{ID: user.ID, DisplayName: user.DisplayName, Role: domain.RoleContributor,
		CreatedAt: user.CreatedAt}
	r.put(stored)
	return stored, nil
}

func (r *Repo) PromoteToModerator(_ context.Context, user datasources.NewUser) error {
	if r.Err != nil {
		return r.Err
	}
	stored, ok := r.Users[user.ID]
	if !ok {
		stored = domain.User{ID: user.ID, DisplayName: user.DisplayName, CreatedAt: user.CreatedAt}
	}
	stored.Role, stored.BlockedAt = domain.RoleModerator, nil
	r.put(stored)
	return nil
}

func (r *Repo) UpdateDisplayName(_ context.Context, userID, displayName string) error {
	stored, ok := r.Users[userID]
	if r.Err != nil || !ok {
		return notFoundOr(r.Err)
	}
	stored.DisplayName = displayName
	r.put(stored)
	return nil
}

func (r *Repo) BlockUser(_ context.Context, userID string, blockedAt time.Time) error {
	return r.setBlocked(userID, &blockedAt)
}

func (r *Repo) UnblockUser(_ context.Context, userID string) error {
	return r.setBlocked(userID, nil)
}

// setBlocked blocks or unblocks a contributor, as the real update matches only contributors.
func (r *Repo) setBlocked(userID string, blockedAt *time.Time) error {
	stored, ok := r.Users[userID]
	if r.Err != nil || !ok || stored.Role != domain.RoleContributor {
		return notFoundOr(r.Err)
	}
	if blockedAt == nil || stored.BlockedAt == nil {
		stored.BlockedAt = blockedAt
	}
	r.put(stored)
	return nil
}

func (r *Repo) put(user domain.User) {
	if r.Users == nil {
		r.Users = map[string]domain.User{}
	}
	r.Users[user.ID] = user
}

func (r *Repo) ImportCatalogue(_ context.Context, imp domain.CatalogueImport) error {
	if r.Err != nil {
		return r.Err
	}
	r.Imported = &imp
	return nil
}

func (r *Repo) GetPublicPiece(context.Context, string) (domain.Piece, error) {
	if r.Err != nil || r.Piece == nil {
		return domain.Piece{}, notFoundOr(r.Err)
	}
	return *r.Piece, nil
}

func (r *Repo) ListPublicPieces(context.Context, datasources.PieceQuery) (domain.PieceList, error) {
	return r.Pieces, r.Err
}

func (r *Repo) GetPublicPerson(context.Context, string) (domain.Person, error) {
	if r.Err != nil || r.Person == nil {
		return domain.Person{}, notFoundOr(r.Err)
	}
	return *r.Person, nil
}

func (r *Repo) ListPublicPersons(context.Context, datasources.PersonQuery) (domain.PersonList, error) {
	return r.Persons, r.Err
}

func (r *Repo) ListPublicTerms(context.Context, *domain.Facet) ([]domain.TermListItem, error) {
	return r.Terms, r.Err
}

func (r *Repo) GetPublicTerm(context.Context, string, domain.Page) (domain.Term, error) {
	if r.Err != nil || r.Term == nil {
		return domain.Term{}, notFoundOr(r.Err)
	}
	return *r.Term, nil
}

func (r *Repo) ListPublicCollections(context.Context, datasources.CollectionQuery) (domain.CollectionList, error) {
	return r.Collections, r.Err
}

func (r *Repo) GetPublicCollection(context.Context, int64) (domain.Collection, error) {
	if r.Err != nil || r.Collection == nil {
		return domain.Collection{}, notFoundOr(r.Err)
	}
	return *r.Collection, nil
}

func (r *Repo) ListPublicEvents(context.Context, *domain.EventKind, domain.Page) (domain.EventList, error) {
	return r.Events, r.Err
}

func (r *Repo) GetPublicEvent(context.Context, string) (domain.Event, error) {
	if r.Err != nil || r.Event == nil {
		return domain.Event{}, notFoundOr(r.Err)
	}
	return *r.Event, nil
}

func (r *Repo) ListPublicHistory(
	context.Context, domain.SlugEntityType, string, domain.Page,
) (domain.RevisionHistory, error) {
	return r.History, r.Err
}

func (r *Repo) ListPublicCollectionHistory(context.Context, int64, domain.Page) (domain.RevisionHistory, error) {
	return r.History, r.Err
}

func (r *Repo) GetPublicRevision(context.Context, int64) (domain.Revision, error) {
	if r.Err != nil || r.Revision == nil {
		return nil, notFoundOr(r.Err)
	}
	return r.Revision, nil
}

func (r *Repo) ListPublicNeedsWork(
	context.Context, *domain.NeedsWorkReason, domain.Page,
) (domain.NeedsWorkList, error) {
	return r.NeedsWork, r.Err
}

// notFoundOr is err, or datasources.ErrNotFound if err is nil.
func notFoundOr(err error) error {
	if err != nil {
		return err
	}
	return datasources.ErrNotFound
}

func (r *Repo) InsertLinkReport(context.Context, datasources.NewLinkReport) (int64, error) {
	return 1, r.Err
}

func (r *Repo) ListLinkReports(context.Context, domain.ReportStatus, domain.Page) (domain.LinkReportList, error) {
	return r.LinkReports, r.Err
}

func (r *Repo) CloseLinkReport(context.Context, datasources.Closing, bool) error { return r.Err }

func (r *Repo) GetModerationLink(context.Context, string) (domain.ModerationLink, error) {
	if r.Err != nil || r.ModerationLink == nil {
		return domain.ModerationLink{}, notFoundOr(r.Err)
	}
	return *r.ModerationLink, nil
}

func (r *Repo) SetLinkHidden(context.Context, int64, bool) error { return r.Err }

func (r *Repo) InsertContactRequest(_ context.Context, request datasources.NewContactRequest) (int64, error) {
	r.ContactRequest = &request
	return 1, r.Err
}

func (r *Repo) ListContactRequests(
	context.Context, domain.ReportStatus, domain.Page, time.Time,
) (domain.ContactRequestList, error) {
	return r.ContactRequests, r.Err
}

func (r *Repo) CloseContactRequest(context.Context, datasources.Closing) error { return r.Err }
