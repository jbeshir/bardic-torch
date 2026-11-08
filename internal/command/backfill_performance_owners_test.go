package command

import (
	"errors"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillPerformanceOwners(t *testing.T) {
	t.Parallel()

	backfiller := mocks.NewPerformanceOwnerBackfiller(t)
	backfiller.EXPECT().BackfillPerformanceOwners(mock.Anything).Return(3, nil).Once()
	cmd := &BackfillPerformanceOwners{Backfiller: backfiller}

	_, err := cmd.Execute(t.Context(), Empty{})
	require.NoError(t, err)
}

func TestBackfillPerformanceOwners_Failure(t *testing.T) {
	t.Parallel()

	failure := errors.New("unavailable")
	backfiller := mocks.NewPerformanceOwnerBackfiller(t)
	backfiller.EXPECT().BackfillPerformanceOwners(mock.Anything).Return(0, failure).Once()
	cmd := &BackfillPerformanceOwners{Backfiller: backfiller}

	_, err := cmd.Execute(t.Context(), Empty{})
	assert.ErrorIs(t, err, failure)
}
