package command

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// repertoireOwner is the principal whose repertoire the repertoire commands act on, with an API token, which acts
// on its user's repertoire like any credential.
var repertoireOwner = domain.Principal{
	User: domain.User{ID: resolvedUserID, Role: domain.RoleContributor}, Method: domain.AuthMethodAPIToken,
}

const repertoirePieceSlug = "the-copper-kettle"

var repertoireEntry = domain.RepertoireEntry{
	Piece: domain.PieceSummary{Slug: repertoirePieceSlug}, Notes: new("Capo 2."),
	Visibility: domain.RepertoireVisibilityPublic, AddedAt: fixedNow, UpdatedAt: fixedNow,
}

func TestPutRepertoireEntry_WritesThePrincipalsEntryUnderTheCap(t *testing.T) {
	t.Parallel()

	putter := mocks.NewRepertoireEntryPutter(t)
	putter.EXPECT().PutRepertoireEntry(mock.Anything, datasources.RepertoireEntryWrite{
		UserID: resolvedUserID, PieceSlug: repertoirePieceSlug, Notes: new("Capo 2."),
		Visibility: domain.RepertoireVisibilityPublic, MaxEntries: domain.MaxRepertoireEntries, Now: fixedNow,
	}).Return(repertoireEntry, true, nil).Once()
	putter.EXPECT().PutRepertoireEntry(mock.Anything, mock.Anything).
		Return(domain.RepertoireEntry{}, false, datasources.ErrRepertoireLimitReached).Once()
	cmd := &PutRepertoireEntry{Putter: putter, Now: fixedClock}
	req := PutRepertoireEntryRequest{Principal: repertoireOwner, PieceSlug: repertoirePieceSlug,
		Notes: new("Capo 2."), Visibility: domain.RepertoireVisibilityPublic}

	result, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, PutRepertoireEntryResult{Entry: repertoireEntry, Created: true}, result)

	_, err = cmd.Execute(t.Context(), req)
	require.ErrorIs(t, err, datasources.ErrRepertoireLimitReached)
}

func TestRepertoireReads_AreThePrincipals(t *testing.T) {
	t.Parallel()

	getter := mocks.NewRepertoireEntryGetter(t)
	getter.EXPECT().GetRepertoireEntry(mock.Anything, resolvedUserID, repertoirePieceSlug).Return(repertoireEntry, nil)
	entry, err := (&GetRepertoireEntry{Getter: getter}).Execute(t.Context(),
		RepertoireEntryRequest{Principal: repertoireOwner, PieceSlug: repertoirePieceSlug})
	require.NoError(t, err)
	assert.Equal(t, repertoireEntry, entry)

	private := domain.RepertoireVisibilityPrivate
	page := domain.Page{Limit: 2, Offset: 4}
	list := domain.RepertoireList{Entries: []domain.RepertoireEntry{repertoireEntry}, Total: 5}
	lister := mocks.NewRepertoireLister(t)
	lister.EXPECT().ListRepertoire(mock.Anything, datasources.RepertoireQuery{
		UserID: resolvedUserID, Visibility: &private, Page: page,
	}).Return(list, nil)
	got, err := (&ListRepertoire{Lister: lister}).Execute(t.Context(),
		RepertoireListRequest{Principal: repertoireOwner, Visibility: &private, Page: page})
	require.NoError(t, err)
	assert.Equal(t, list, got)
}

func TestRemoveRepertoireEntry_RemovesThePrincipalsEntry(t *testing.T) {
	t.Parallel()

	deleter := mocks.NewRepertoireEntryDeleter(t)
	deleter.EXPECT().DeleteRepertoireEntry(mock.Anything, resolvedUserID, repertoirePieceSlug).Return(nil).Once()
	deleter.EXPECT().DeleteRepertoireEntry(mock.Anything, resolvedUserID, repertoirePieceSlug).
		Return(datasources.ErrNotFound).Once()
	cmd := &RemoveRepertoireEntry{Deleter: deleter}
	req := RepertoireEntryRequest{Principal: repertoireOwner, PieceSlug: repertoirePieceSlug}

	_, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	_, err = cmd.Execute(t.Context(), req)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}
