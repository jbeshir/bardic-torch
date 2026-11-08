package domain_test

import (
	"fmt"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/stretchr/testify/assert"
)

// TestRestore_RoundTripScrubbedImport checks that restoring a caller's untouched projection of a document gives the
// document back, with no flags, for every document of the scrubbed import and for every caller: a contributor, a
// moderator, and a contributor who may see no event and no collection (as if each were pending), whose projections
// withhold every performance event and link collection too.
func TestRestore_RoundTripScrubbedImport(t *testing.T) {
	t.Parallel()

	imp, _ := importtest.Scrubbed(t)
	contributor := importVisibility(imp)
	moderator := contributor
	moderator.Role = domain.RoleModerator
	restricted := contributor
	restricted.Events, restricted.Collections = nil, nil
	for name, vis := range map[string]domain.EditorVisibility{
		"Contributor": contributor, "Moderator": moderator, "ContributorWithoutEventsOrCollections": restricted,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			withheld := 0
			for _, d := range imp.Pieces {
				withheld += importRoundTrip(t, d.ID, d.Doc, vis)
			}
			for _, d := range imp.Persons {
				withheld += importRoundTrip(t, d.ID, d.Doc, vis)
			}
			for _, d := range imp.Terms {
				withheld += importRoundTrip(t, d.ID, d.Doc, vis)
			}
			for _, d := range imp.Collections {
				withheld += importRoundTrip(t, d.ID, d.Doc, vis)
			}
			for _, d := range imp.Events {
				withheld += importRoundTrip(t, d.ID, d.Doc, vis)
			}
			if vis.Role == domain.RoleContributor {
				assert.Positive(t, withheld, "some projections withhold something")
			}
		})
	}
}

// importRoundTrip checks that restoring the untouched projection of doc gives doc, with no flags. It returns 1 if
// the projection withheld something, else 0.
func importRoundTrip[D domain.Document](t *testing.T, id int64, doc D, vis domain.EditorVisibility) int {
	t.Helper()
	editable, received := domain.EditableDocument(id, doc, vis)
	got, flags := domain.RestoreDocument(id, editable, &doc, received, vis)
	label := fmt.Sprintf("%T %d", doc, id)
	assert.Equal(t, doc, got, label)
	assert.Empty(t, flags, label)
	if domain.EqualValues(editable, doc) {
		return 0
	}

	return 1
}
