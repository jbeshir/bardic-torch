package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.UserGetter                 = (*Repository)(nil)
	_ datasources.UserEnsurer                = (*Repository)(nil)
	_ datasources.ModeratorPromoter          = (*Repository)(nil)
	_ datasources.DisplayNameUpdater         = (*Repository)(nil)
	_ datasources.UserBlocker                = (*Repository)(nil)
	_ datasources.UserUnblocker              = (*Repository)(nil)
	_ datasources.DisplayNameKeyLister       = (*Repository)(nil)
	_ datasources.DisplayNameKeySetter       = (*Repository)(nil)
	_ datasources.PublicIDSetter             = (*Repository)(nil)
	_ datasources.UsersWithoutPublicIDLister = (*Repository)(nil)
)

// GetUser retrieves a user by ID, returning datasources.ErrNotFound if there is none.
func (r *Repository) GetUser(ctx context.Context, userID string) (domain.User, error) {
	row, err := r.queries.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("user [%s]: %w", userID, datasources.ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("fetching user [%s]: %w", userID, unavailableIfDisconnected(err))
	}

	return convertUser(row)
}

// EnsureUser creates a contributor from user unless a user with its ID already exists, and returns the
// stored user. Concurrent calls for the same ID create one user. A new user's display name key is set after the
// insert, and left NULL if another user has it. A user without a public ID is then given user.PublicID, or
// datasources.ErrPublicIDTaken is returned if another user has it.
func (r *Repository) EnsureUser(ctx context.Context, user datasources.NewUser) (domain.User, error) {
	if !domain.IsPublicID(user.PublicID) {
		return domain.User{}, fmt.Errorf("ensuring user [%s]: %w", user.ID, errInvalidPublicID)
	}
	err := r.queries.InsertUserIfMissing(ctx, queries.InsertUserIfMissingParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt,
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("inserting user [%s]: %w", user.ID, unavailableIfDisconnected(err))
	}
	if key := displayNameKey(user.DisplayName); key.Valid {
		err = r.queries.SetMissingDisplayNameKey(ctx, queries.SetMissingDisplayNameKeyParams{
			DisplayNameKey: key, ID: user.ID, DisplayName: user.DisplayName,
		})
		if err != nil && mysqlErrorNumber(err) != erDuplicateEntry {
			return domain.User{}, fmt.Errorf("setting display name key of user [%s]: %w", user.ID,
				unavailableIfDisconnected(err))
		}
	}
	if err := r.SetUserPublicIDIfMissing(ctx, user.ID, user.PublicID); err != nil {
		return domain.User{}, err
	}

	return r.GetUser(ctx, user.ID)
}

// PromoteToModerator makes a user a moderator and unblocks it, creating it from user if it does not exist yet.
func (r *Repository) PromoteToModerator(ctx context.Context, user datasources.NewUser) error {
	err := r.queries.PromoteToModerator(ctx, queries.PromoteToModeratorParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("promoting user [%s] to moderator: %w", user.ID, unavailableIfDisconnected(err))
	}

	return nil
}

// UpdateDisplayName sets a user's display name and its key, returning datasources.ErrNotFound if there is no such
// user and datasources.ErrDisplayNameTaken if another user has the key.
func (r *Repository) UpdateDisplayName(ctx context.Context, userID, displayName string) error {
	err := requireMatched(r.queries.UpdateDisplayName(ctx, queries.UpdateDisplayNameParams{
		DisplayName:    displayName,
		DisplayNameKey: displayNameKey(displayName),
		ID:             userID,
	}))
	if err != nil {
		return fmt.Errorf("updating display name of user [%s]: %w", userID,
			unavailableIfDisconnected(displayNameKeyError(err)))
	}

	return nil
}

// ListDisplayNameKeys lists every user's display name and stored key, oldest user first, then by ID.
func (r *Repository) ListDisplayNameKeys(ctx context.Context) ([]datasources.UserDisplayNameKey, error) {
	rows, err := r.queries.ListDisplayNameKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing display name keys: %w", unavailableIfDisconnected(err))
	}
	keys := make([]datasources.UserDisplayNameKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, datasources.UserDisplayNameKey{
			UserID: row.ID, DisplayName: row.DisplayName, Key: stringPtr(row.DisplayNameKey),
		})
	}

	return keys, nil
}

// SetDisplayNameKey stores a user's display name key, nil for none, returning datasources.ErrNotFound if there is
// no such user and datasources.ErrDisplayNameTaken if another user has the key.
func (r *Repository) SetDisplayNameKey(ctx context.Context, userID string, key *string) error {
	err := requireMatched(r.queries.SetDisplayNameKey(ctx, queries.SetDisplayNameKeyParams{
		DisplayNameKey: nullString(key), ID: userID,
	}))
	if err != nil {
		return fmt.Errorf("setting display name key of user [%s]: %w", userID,
			unavailableIfDisconnected(displayNameKeyError(err)))
	}

	return nil
}

// displayNameKey is the key column's value for a display name: its domain.DisplayNameKey, NULL for a name with no
// letter or digit, which no other name is then kept from.
func displayNameKey(displayName string) sql.NullString {
	return optionalText(domain.DisplayNameKey(displayName))
}

// displayNameKeyError is datasources.ErrDisplayNameTaken for a duplicate key, the only UNIQUE index a users
// update can break, and err otherwise.
func displayNameKeyError(err error) error {
	if mysqlErrorNumber(err) == erDuplicateEntry {
		return datasources.ErrDisplayNameTaken
	}

	return err
}

// BlockUser blocks a contributor for the moderator moderatorUserID, keeping the original time if it is already
// blocked (setUserBlocked). It returns datasources.ErrNotFound if there is no contributor with the ID.
func (r *Repository) BlockUser(ctx context.Context, moderatorUserID, userID string, blockedAt time.Time) error {
	err := r.setUserBlocked(ctx, moderatorUserID, func(ctx context.Context, q *queries.Queries) (int64, error) {
		return q.BlockUser(ctx, queries.BlockUserParams{BlockedAt: nullTime(blockedAt), ID: userID})
	})
	if err != nil {
		return fmt.Errorf("blocking contributor [%s]: %w", userID, err)
	}

	return nil
}

// UnblockUser unblocks a contributor for the moderator moderatorUserID (setUserBlocked), returning
// datasources.ErrNotFound if there is no contributor with the ID.
func (r *Repository) UnblockUser(ctx context.Context, moderatorUserID, userID string) error {
	err := r.setUserBlocked(ctx, moderatorUserID, func(ctx context.Context, q *queries.Queries) (int64, error) {
		return q.UnblockUser(ctx, userID)
	})
	if err != nil {
		return fmt.Errorf("unblocking contributor [%s]: %w", userID, err)
	}

	return nil
}

// setUserBlocked runs update, a single-row update of a contributor's users row reporting the rows it matched, as a
// moderator-only write: it first locks the users row of moderatorUserID (lockModerator), refusing a moderator
// demoted or blocked since their request was authenticated, and one with no row with domain.ErrModeratorRequired.
// The contributor's row is the second users row it locks, after the moderator's; that cannot deadlock with a
// contributor's own writes, which lock no users row but theirs (docs/PLAN.md §5.1 "Approval"). An update matching no
// row is datasources.ErrNotFound.
func (r *Repository) setUserBlocked(
	ctx context.Context, moderatorUserID string, update func(ctx context.Context, q *queries.Queries) (int64, error),
) error {
	return r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		err := c.lockModerator(ctx, moderatorUserID)
		if errors.Is(err, datasources.ErrNotFound) {
			return domain.ErrModeratorRequired
		}
		if err != nil {
			return err
		}

		return requireMatched(update(ctx, c.q))
	})
}

func convertUser(row queries.GetUserRow) (domain.User, error) {
	role, err := domain.ParseRole(string(row.Role))
	if err != nil {
		return domain.User{}, fmt.Errorf("user [%s]: %w", row.ID, err)
	}

	return domain.User{
		ID:          row.ID,
		PublicID:    row.PublicID.String,
		DisplayName: row.DisplayName,
		Role:        role,
		CreatedAt:   row.CreatedAt,
		BlockedAt:   timePtr(row.BlockedAt),
	}, nil
}

// SetUserPublicIDIfMissing gives a user without a public ID publicID, returning datasources.ErrPublicIDTaken if
// another user has it. A user who has one, or no user with the ID, is left as is.
func (r *Repository) SetUserPublicIDIfMissing(ctx context.Context, userID, publicID string) error {
	err := r.queries.SetUserPublicIDIfMissing(ctx, queries.SetUserPublicIDIfMissingParams{
		PublicID: sql.NullString{String: publicID, Valid: true}, ID: userID,
	})
	if mysqlErrorNumber(err) == erDuplicateEntry {
		return fmt.Errorf("setting public ID of user [%s]: %w", userID, datasources.ErrPublicIDTaken)
	}
	if err != nil {
		return fmt.Errorf("setting public ID of user [%s]: %w", userID, unavailableIfDisconnected(err))
	}

	return nil
}

// ListUsersWithoutPublicID lists the IDs of the users without a public ID, oldest user first, then by ID.
func (r *Repository) ListUsersWithoutPublicID(ctx context.Context) ([]string, error) {
	ids, err := r.queries.ListUsersWithoutPublicID(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users without a public ID: %w", unavailableIfDisconnected(err))
	}

	return ids, nil
}

// errInvalidPublicID is a user to be given a public ID that does not have the form of one (domain.IsPublicID).
var errInvalidPublicID = errors.New("invalid public ID")
