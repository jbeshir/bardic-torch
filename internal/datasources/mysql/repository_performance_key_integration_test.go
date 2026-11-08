//go:build integration

package mysql_test

import (
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasFlag is whether flags hold one of code.
func hasFlag(flags []domain.Flag, code domain.FlagCode) bool {
	return slices.ContainsFunc(flags, func(f domain.Flag) bool { return f.Code == code })
}

// TestRepository_SubmitDraft_EditedPerformanceKeepsHiddenPerformerByKey checks that a contributor who edits the
// description and year of a performance with a performer they may not see, sending back the key their editable
// document gave it, keeps that performer: the submission matches the performance by its key, puts the performer
// back with no flag, and approving it stores the edit with both performers. A further edit without the key is
// matched by content, so it is flagged hidden_data_conflict.
func TestRepository_SubmitDraft_EditedPerformanceKeepsHiddenPerformerByKey(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	const edited = "Sung again at the fair"
	editedDocument := func(description string, withKey bool) domain.PieceDocument {
		doc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
		doc.Links = slices.Clone(doc.Links)
		doc.Links[0].Performances = slices.Clone(doc.Links[0].Performances)
		perf := &doc.Links[0].Performances[0]
		require.NotEmpty(t, perf.Key, "the editable document gives the stored performance a key")
		require.Equal(t, []int64{f.morwen.ID}, perf.PerformerIDs, "the no-page performer is withheld")
		perf.Year, perf.DescriptionVerbatim = new(int16(2003)), description
		if !withKey {
			perf.Key = ""
		}

		return doc
	}

	result := f.submit(t, actorWren, f.saveDraft(t, actorWren, &key, editedDocument(edited, true)).Draft.ID)
	require.Equal(t, domain.RevisionStatusPending, result.Status)
	assert.False(t, hasFlag(f.storedFlags(t, result.RevisionID), domain.FlagHiddenDataConflict),
		"matched by its key, nothing withheld is lost")
	f.mustApprove(t, result.RevisionID)

	current := f.editable(t, actorRowan, key).Document.(domain.PieceDocument)
	got := current.Links[0].Performances[0]
	assert.Equal(t, edited, got.DescriptionVerbatim)
	assert.Equal(t, new(int16(2003)), got.Year)
	assert.Equal(t, []int64{f.morwen.ID, f.quiet.ID}, got.PerformerIDs, "the hidden performer is kept")
	assert.Equal(t, &f.fair.ID, got.EventID)
	assert.Equal(t, 2, f.count(t, "SELECT COUNT(*) FROM performance_performers pp "+
		"JOIN performances p ON p.id = pp.performance_id WHERE p.description_verbatim = ?", edited),
		"both performers are written to the tables")

	draft := f.saveDraft(t, actorWren, &key, editedDocument("Sung a third time at the fair", false))
	withoutKey := f.submit(t, actorWren, draft.Draft.ID)
	assert.True(t, hasFlag(f.storedFlags(t, withoutKey.RevisionID), domain.FlagHiddenDataConflict),
		"without a key the edited performance matches nothing")
}

// withoutKeys is doc with its performances' keys cleared, failing the test unless each had one: an editable
// document gives every performance a key, derived for one stored without.
func withoutKeys(t *testing.T, doc domain.AnyDocument) domain.AnyDocument {
	t.Helper()
	drop := func(perf *domain.DocPerformance) {
		assert.Len(t, perf.Key, 16, "the performance has a key")
		perf.Key = ""
	}
	switch d := doc.(type) {
	case domain.PieceDocument:
		d.Links = slices.Clone(d.Links)
		for i := range d.Links {
			d.Links[i].Performances = slices.Clone(d.Links[i].Performances)
			for j := range d.Links[i].Performances {
				drop(&d.Links[i].Performances[j])
			}
		}

		return d
	case domain.PersonDocument:
		d.Links = slices.Clone(d.Links)
		for i := range d.Links {
			d.Links[i].Performances = slices.Clone(d.Links[i].Performances)
			for j := range d.Links[i].Performances {
				drop(&d.Links[i].Performances[j].DocPerformance)
			}
		}

		return d
	}

	return doc
}
