package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// oneTermImport is a valid import of one term.
func oneTermImport() domain.CatalogueImport {
	return domain.CatalogueImport{
		Terms: []domain.ImportedTerm{{ID: 1, Slug: "sca", Revision: domain.ImportedRevision{ID: 1,
			Summary: domain.ImportSummaryCategory, Contributors: []string{}, CreatedAt: fixedNow},
			Doc: domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: domain.FacetAffiliation,
				Name: "SCA", DisplayName: "SCA", Aliases: []string{}}}},
		TermAncestors: []domain.TermAncestor{{AncestorID: 1, DescendantID: 1, Depth: 0}},
	}
}

func TestApplyCatalogueImport_Writes(t *testing.T) {
	t.Parallel()

	imp := oneTermImport()
	importer := mocks.NewCatalogueImporter(t)
	importer.EXPECT().ImportCatalogue(mock.Anything, imp).Return(nil).Once()

	_, err := (&ApplyCatalogueImport{Importer: importer}).Execute(t.Context(), imp)

	require.NoError(t, err)
}

func TestApplyCatalogueImport_ValidatesBeforeWriting(t *testing.T) {
	t.Parallel()

	imp := oneTermImport()
	imp.TermAncestors = nil
	// The mock fails the test if it is called.
	importer := mocks.NewCatalogueImporter(t)

	_, err := (&ApplyCatalogueImport{Importer: importer}).Execute(t.Context(), imp)

	require.ErrorIs(t, err, domain.ErrInvalidImport)
}

func TestApplyCatalogueImport_PassesErrorsThrough(t *testing.T) {
	t.Parallel()

	for _, want := range []error{datasources.ErrCatalogueNotEmpty, datasources.ErrImportInProgress} {
		t.Run(want.Error(), func(t *testing.T) {
			t.Parallel()

			importer := mocks.NewCatalogueImporter(t)
			importer.EXPECT().ImportCatalogue(mock.Anything, mock.Anything).Return(want).Once()

			_, err := (&ApplyCatalogueImport{Importer: importer}).Execute(t.Context(), oneTermImport())

			assert.ErrorIs(t, err, want)
		})
	}
}
