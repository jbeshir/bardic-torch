package command

import (
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	submitter  = domain.User{ID: "google-oauth2|123", Role: domain.RoleContributor}
	revelMatch = domain.DuplicateCandidate{EntityType: domain.EntityTypeEvent, Slug: new("midwinter-revel"),
		Name: "Midwinter Revel", Keys: []string{domain.DuplicateKey("Midwinter Revel")}}
	revelEdit = domain.EditorEventDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: "Midwinter Revel"}
)

func submit(t *testing.T, repo *fakerepo.Repo, req SubmitRevisionRequest) error {
	t.Helper()
	repo.RevisionView = domain.EventRevisionView{}
	c := &SubmitRevision{Revisions: repo, Now: func() time.Time { return time.Unix(0, 0) }}
	_, err := c.Execute(t.Context(), req)
	return err
}

// TestSubmitRevision_Duplicates checks that a new entity matching a public one needs the match acknowledged.
func TestSubmitRevision_Duplicates(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{Candidates: []domain.DuplicateCandidate{revelMatch}}
	err := submit(t, repo, SubmitRevisionRequest{User: submitter, Document: revelEdit})
	var duplicate *DuplicateError
	require.ErrorAs(t, err, &duplicate)
	assert.Equal(t, []domain.DuplicateCandidate{revelMatch}, duplicate.Matches)
	assert.Nil(t, repo.Submitted)

	require.NoError(t, submit(t, repo, SubmitRevisionRequest{User: submitter, Document: revelEdit,
		AcknowledgeDuplicates: true}))
	require.NotNil(t, repo.Submitted)
	assert.Equal(t, domain.EventDocument(revelEdit), repo.Submitted.Document)
	assert.False(t, repo.Submitted.ByModerator)
}

// TestSubmitRevision_NewPersonMatchingNoPagePerson checks that a new person matching a person who prefers no
// page is refused, acknowledged or not.
func TestSubmitRevision_NewPersonMatchingNoPagePerson(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{Candidates: []domain.DuplicateCandidate{{EntityType: domain.EntityTypePerson,
		Keys: []string{domain.DuplicateKey("Wren")}, PrefersNoPage: true}}}
	err := submit(t, repo, SubmitRevisionRequest{User: submitter, AcknowledgeDuplicates: true,
		Document: domain.EditorPersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: "Wren",
			Kind: domain.PersonKindCommunityBard, Status: domain.PersonStatusActive,
			LicensingStatus: domain.LicensingStatusUnknown}})

	var invalid *datasources.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, []domain.Problem{{Path: "name", Code: domain.ProblemPrefersNoPage}}, invalid.Problems)
	assert.Nil(t, repo.Submitted)
}
