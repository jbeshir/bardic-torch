package datasources

import (
	"context"
	"errors"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// CatalogueImporter writes an import into an empty catalogue in one transaction. It returns ErrImportInProgress if
// another import of the same database is running, and ErrCatalogueNotEmpty if any catalogue or revision row
// exists; either way, and on any other error, it writes nothing.
type CatalogueImporter interface {
	ImportCatalogue(ctx context.Context, imp domain.CatalogueImport) error
}

// ErrCatalogueNotEmpty is returned, possibly wrapped, by an import into a catalogue that already holds rows.
var ErrCatalogueNotEmpty = errors.New("catalogue is not empty")

// ErrImportInProgress is returned, possibly wrapped, by an import started while another import of the same
// database is running.
var ErrImportInProgress = errors.New("another catalogue import is in progress")
