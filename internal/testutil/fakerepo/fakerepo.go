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

	ModerationLink  *domain.ModerationLink
	ContactRequests domain.ContactRequestList
	// ContactRequest is the last contact request stored.
	ContactRequest *datasources.NewContactRequest

	// The revision reads: RevisionView answers both revision gets, RevisionList both lists, and IDs every
	// reference resolution.
	Editable     *datasources.Editable
	RevisionBase *datasources.RevisionBase
	IDs          domain.IDs
	Candidates   []domain.DuplicateCandidate
	RevisionView domain.RevisionView
	RevisionList domain.RevisionList
	// Submitted is the last revision submitted.
	Submitted *datasources.NewRevision

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

func (r *Repo) ListPublicEvents(context.Context, domain.Page) (domain.EventList, error) {
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
	context.Context, domain.ReportStatus, domain.Page,
) (domain.ContactRequestList, error) {
	return r.ContactRequests, r.Err
}

func (r *Repo) CloseContactRequest(context.Context, datasources.Closing) error { return r.Err }

func (r *Repo) SubmitRevision(_ context.Context, rev datasources.NewRevision) (int64, error) {
	r.Submitted = &rev
	return 1, r.Err
}

func (r *Repo) GetRevisionBase(context.Context, domain.EntityType, int64) (datasources.RevisionBase, error) {
	if r.Err != nil || r.RevisionBase == nil {
		return datasources.RevisionBase{}, notFoundOr(r.Err)
	}
	return *r.RevisionBase, nil
}

func (r *Repo) WithdrawRevision(context.Context, int64, string) error { return r.Err }

func (r *Repo) RejectRevision(context.Context, int64, string, string, time.Time) error { return r.Err }

func (r *Repo) ApproveRevision(context.Context, int64, string, bool, time.Time) error { return r.Err }

func (r *Repo) GetEditable(context.Context, domain.EntityType, string, string) (datasources.Editable, error) {
	if r.Err != nil || r.Editable == nil {
		return datasources.Editable{}, notFoundOr(r.Err)
	}
	return *r.Editable, nil
}

func (r *Repo) ResolveReferences(context.Context, domain.SlugReferences) (domain.IDs, error) {
	return r.IDs, r.Err
}

func (r *Repo) ValidationContext(context.Context, int64) (domain.ValidationContext, error) {
	return domain.ValidationContext{}, r.Err
}

func (r *Repo) DuplicateCandidates(
	context.Context, domain.EntityType, domain.Facet,
) ([]domain.DuplicateCandidate, error) {
	return r.Candidates, r.Err
}

func (r *Repo) ListMyRevisions(
	context.Context, string, *domain.RevisionStatus, domain.Page,
) (domain.RevisionList, error) {
	return r.RevisionList, r.Err
}

func (r *Repo) GetMyRevision(context.Context, string, int64) (domain.RevisionView, error) {
	return r.revisionView()
}

func (r *Repo) ListModerationQueue(context.Context, domain.Page) (domain.RevisionList, error) {
	return r.RevisionList, r.Err
}

func (r *Repo) GetModerationRevision(context.Context, int64) (domain.RevisionView, error) {
	return r.revisionView()
}

func (r *Repo) revisionView() (domain.RevisionView, error) {
	if r.Err != nil || r.RevisionView == nil {
		return nil, notFoundOr(r.Err)
	}
	return r.RevisionView, nil
}
