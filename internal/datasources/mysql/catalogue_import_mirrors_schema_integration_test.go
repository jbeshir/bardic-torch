//go:build integration

package mysql_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
)

// The MySQL error numbers of the constraint classes Validate mirrors.
const (
	erDuplicateEntry          = 1062
	erDataTooLong             = 1406
	erCheckConstraintViolated = 3819
)

// TestCatalogueImportValidate_MirrorsSchema plants in the scrubbed import one violation of each class of constraint
// that CatalogueImport.Validate mirrors, and checks both sides of the mirror: Validate rejects the import, and
// MySQL, given it without validation, refuses the same row with that class's error.
func TestCatalogueImportValidate_MirrorsSchema(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		plant  func(t *testing.T, imp *domain.CatalogueImport)
		number uint16
	}{
		// A UNIQUE index compares by the collation: an alias equal to another but for case and accents.
		"unique_index_folded": {plant: func(t *testing.T, imp *domain.CatalogueImport) {
			p := pieceWith(t, imp, func(p domain.ImportedPiece) bool { return len(p.Doc.Aliases) > 0 })
			again := p.Doc.Aliases[0]
			again.Text = strings.ToUpper(again.Text)
			p.Doc.Aliases = append(p.Doc.Aliases, again)
		}, number: erDuplicateEntry},
		// A child table's primary key: the same term twice on a piece.
		"primary_key": {plant: func(t *testing.T, imp *domain.CatalogueImport) {
			p := pieceWith(t, imp, func(p domain.ImportedPiece) bool { return len(p.Doc.Terms) > 0 })
			again := p.Doc.Terms[0]
			again.IsPrimary = false
			p.Doc.Terms = append(p.Doc.Terms, again)
		}, number: erDuplicateEntry},
		// A VARCHAR(255) column: a title one character too long.
		"varchar_width": {plant: func(t *testing.T, imp *domain.CatalogueImport) {
			p := pieceWith(t, imp, func(domain.ImportedPiece) bool { return true })
			p.Doc.Title = strings.Repeat("a", 256)
		}, number: erDataTooLong},
		// A CHECK: a named credit with no person.
		"check": {plant: func(t *testing.T, imp *domain.CatalogueImport) {
			p := pieceWith(t, imp, func(p domain.ImportedPiece) bool {
				return len(p.Doc.Credits) > 0 && p.Doc.Credits[0].Attribution == domain.AttributionNamed
			})
			p.Doc.Credits[0].PersonID = nil
		}, number: erCheckConstraintViolated},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			imp, _ := importtest.Scrubbed(t)
			tc.plant(t, &imp)

			require.ErrorIs(t, imp.Validate(), domain.ErrInvalidImport, "Validate rejects it")
			err := mysql.New(catalogtest.Writable(t)).ImportCatalogue(t.Context(), imp)
			var dbErr *mysql.DatabaseError
			require.ErrorAs(t, err, &dbErr, "MySQL rejects it")
			assert.Equal(t, tc.number, dbErr.Number)
		})
	}
}

// pieceWith is the first piece of imp that ok holds for, to change in place.
func pieceWith(t *testing.T, imp *domain.CatalogueImport, ok func(domain.ImportedPiece) bool) *domain.ImportedPiece {
	t.Helper()
	for k := range imp.Pieces {
		if ok(imp.Pieces[k]) {
			return &imp.Pieces[k]
		}
	}
	require.FailNow(t, "the scrubbed import has no such piece")

	return nil
}
