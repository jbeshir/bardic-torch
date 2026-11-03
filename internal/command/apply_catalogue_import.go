package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[domain.CatalogueImport, Empty] = (*ApplyCatalogueImport)(nil)

// ApplyCatalogueImport writes a catalogue import into the empty catalogue.
type ApplyCatalogueImport struct {
	Importer datasources.CatalogueImporter
}

// Execute validates imp and writes it in one transaction. An invalid import returns an error wrapping
// domain.ErrInvalidImport and writes nothing; a catalogue that is not empty, or an import already in progress,
// returns datasources.ErrCatalogueNotEmpty or datasources.ErrImportInProgress, wrapped. No error quotes a value
// of the import.
func (c *ApplyCatalogueImport) Execute(ctx context.Context, imp domain.CatalogueImport) (Empty, error) {
	if err := imp.Validate(); err != nil {
		return Empty{}, err
	}
	if err := c.Importer.ImportCatalogue(ctx, imp); err != nil {
		return Empty{}, fmt.Errorf("applying catalogue import: %w", err)
	}

	return Empty{}, nil
}
