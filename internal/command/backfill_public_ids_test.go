package command

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// publicIDSet is a call to SetUserPublicIDIfMissing and what it returns.
type publicIDSet struct {
	userID, publicID string
	err              error
}

// TestBackfillPublicIDs gives every user without a public ID one, in the lister's order, retrying a public ID
// another user has with a fresh one, and logs each collision by user ID only, never the public ID.
func TestBackfillPublicIDs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		missing []string
		sets    []publicIDSet
		wantErr error
		// wantLogs are the user IDs each warning line names, in order.
		wantLogs []string
	}{
		{name: "none missing", missing: []string{}},
		{
			name: "two missing", missing: []string{backfillWrenID, backfillAshID},
			sets: []publicIDSet{
				{userID: backfillWrenID, publicID: firstPublicID}, {userID: backfillAshID, publicID: secondPublicID},
			},
		},
		{
			name: "collision retried", missing: []string{backfillWrenID, backfillAshID},
			sets: []publicIDSet{
				{userID: backfillWrenID, publicID: firstPublicID, err: datasources.ErrPublicIDTaken},
				{userID: backfillWrenID, publicID: secondPublicID},
				{userID: backfillAshID, publicID: thirdPublicID},
			},
			wantLogs: []string{backfillWrenID},
		},
		{
			name: "collision every time", missing: []string{backfillWrenID, backfillAshID},
			sets: []publicIDSet{
				{userID: backfillWrenID, publicID: firstPublicID, err: datasources.ErrPublicIDTaken},
				{userID: backfillWrenID, publicID: secondPublicID, err: datasources.ErrPublicIDTaken},
				{userID: backfillWrenID, publicID: thirdPublicID, err: datasources.ErrPublicIDTaken},
			},
			wantErr:  datasources.ErrPublicIDTaken,
			wantLogs: []string{backfillWrenID, backfillWrenID, backfillWrenID},
		},
		{
			name: "set fails", missing: []string{backfillWrenID, backfillAshID},
			sets:    []publicIDSet{{userID: backfillWrenID, publicID: firstPublicID, err: errDatasource}},
			wantErr: errDatasource,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewUsersWithoutPublicIDLister(t)
			lister.EXPECT().ListUsersWithoutPublicID(mock.Anything).Return(tc.missing, nil)
			setter := mocks.NewPublicIDSetter(t)
			var previous *mock.Call
			for _, set := range tc.sets {
				call := setter.EXPECT().SetUserPublicIDIfMissing(mock.Anything, set.userID, set.publicID).
					Return(set.err).Once()
				if previous != nil {
					call.NotBefore(previous)
				}
				previous = call
			}

			var logs bytes.Buffer
			ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
			_, err := (&BackfillPublicIDs{Lister: lister, Setter: setter, PublicIDs: publicIDSequence()}).Execute(
				ctx, Empty{})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}

			var logged []string
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
				if line == "" {
					continue
				}
				assert.Contains(t, line, "level=WARN")
				_, userID, ok := strings.Cut(line, "user_id=")
				require.True(t, ok, line)
				logged = append(logged, userID)
			}
			assert.Equal(t, tc.wantLogs, logged)
			for _, set := range tc.sets {
				assert.NotContains(t, logs.String(), set.publicID, "the log names the user, never the public ID")
			}
		})
	}
}

func TestBackfillPublicIDs_ListError(t *testing.T) {
	t.Parallel()

	lister := mocks.NewUsersWithoutPublicIDLister(t)
	lister.EXPECT().ListUsersWithoutPublicID(mock.Anything).Return(nil, errDatasource)
	_, err := (&BackfillPublicIDs{Lister: lister, Setter: mocks.NewPublicIDSetter(t), PublicIDs: unusedPublicIDs(t)}).
		Execute(t.Context(), Empty{})
	require.ErrorIs(t, err, errDatasource)
}

// The users the backfill tests give public IDs.
const (
	backfillWrenID = "auth0|wren"
	backfillAshID  = "auth0|ash"
)
