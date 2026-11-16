package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
)

// oneTermImport is a valid import of one term.
func oneTermImport() domain.CatalogueImport {
	return domain.CatalogueImport{
		Terms: []domain.ImportedTerm{{ID: 1, Slug: "sca", Revision: domain.ImportedRevision{ID: 1,
			Summary: domain.ImportSummaryCategory, Contributors: []string{}, CreatedAt: fixedNow},
			Doc: domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: domain.FacetAffiliation,
				Name: "SCA", DisplayName: "SCA"}}},
	}
}

func TestApplyCatalogueImport_ValidatesBeforeWriting(t *testing.T) {
	t.Parallel()

	imp := oneTermImport()
	imp.Terms[0].Slug = ""
	repo := &fakerepo.Repo{}

	_, err := (&ApplyCatalogueImport{Importer: repo}).Execute(t.Context(), imp)

	require.ErrorIs(t, err, domain.ErrInvalidImport)
	assert.Nil(t, repo.Imported)
}
