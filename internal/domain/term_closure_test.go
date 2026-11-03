package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTermClosure_SelfRowsAndAncestors(t *testing.T) {
	t.Parallel()

	rows, err := TermClosure(map[int64]*int64{1: nil, 2: id(1), 3: id(2), 4: id(3), 5: id(1), 6: nil})
	require.NoError(t, err)
	assert.Equal(t, []TermAncestor{
		{AncestorID: 1, DescendantID: 1, Depth: 0},
		{AncestorID: 2, DescendantID: 2, Depth: 0},
		{AncestorID: 1, DescendantID: 2, Depth: 1},
		{AncestorID: 3, DescendantID: 3, Depth: 0},
		{AncestorID: 2, DescendantID: 3, Depth: 1},
		{AncestorID: 1, DescendantID: 3, Depth: 2},
		{AncestorID: 4, DescendantID: 4, Depth: 0},
		{AncestorID: 3, DescendantID: 4, Depth: 1},
		{AncestorID: 2, DescendantID: 4, Depth: 2},
		{AncestorID: 1, DescendantID: 4, Depth: 3},
		{AncestorID: 5, DescendantID: 5, Depth: 0},
		{AncestorID: 1, DescendantID: 5, Depth: 1},
		{AncestorID: 6, DescendantID: 6, Depth: 0},
	}, rows)

	empty, err := TermClosure(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestTermClosure_RejectsCycle(t *testing.T) {
	t.Parallel()

	for name, parents := range map[string]map[int64]*int64{
		"self":  {1: id(1)},
		"pair":  {1: id(2), 2: id(1)},
		"long":  {1: id(2), 2: id(3), 3: id(4), 4: id(5), 5: id(1)},
		"above": {1: id(2), 2: id(3), 3: id(2)},
	} {
		_, err := TermClosure(parents)
		require.ErrorIs(t, err, ErrTermCycle, name)
		assert.EqualError(t, err, "term is its own ancestor: term 1", name)
	}
}

func TestTermClosure_RejectsDepth(t *testing.T) {
	t.Parallel()

	_, err := TermClosure(map[int64]*int64{1: nil, 2: id(1), 3: id(2), 4: id(3), 5: id(4)})
	require.ErrorIs(t, err, ErrTermTooDeep)
	assert.EqualError(t, err, "term is more than 3 levels below its root: term 5")
}

func TestTermClosure_RejectsUnknownParent(t *testing.T) {
	t.Parallel()

	_, err := TermClosure(map[int64]*int64{1: nil, 2: id(9)})
	require.ErrorIs(t, err, ErrTermUnknownParent)
}

func TestTermClosure_Deterministic(t *testing.T) {
	t.Parallel()

	parents := map[int64]*int64{}
	for i := int64(1); i <= 50; i++ {
		if i > 5 {
			parents[i] = id(i % 5)
		} else {
			parents[i] = nil
		}
	}
	parents[0] = nil
	first, err := TermClosure(parents)
	require.NoError(t, err)
	for range 20 {
		again, err := TermClosure(parents)
		require.NoError(t, err)
		assert.Equal(t, first, again)
	}
}
