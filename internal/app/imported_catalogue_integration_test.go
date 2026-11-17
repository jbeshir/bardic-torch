//go:build integration

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// importedCatalogue is a database holding the import of the committed scrubbed crawl, as --apply writes it.
var importedCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) domain.CatalogueImport {
	imp := importtest.Scrubbed(c.T())
	c.Import(imp)

	return imp
})

// importedReader sends GETs to the handler over the imported catalogue, validating each against the spec.
type importedReader struct {
	t        *testing.T
	contract *openapitest.Validator
	handler  http.Handler
}

// get sends a GET of path, requires 200, and returns the body.
func (r importedReader) get(path string) []byte {
	r.t.Helper()
	rec := r.contract.Serve(r.t, r.handler, httptest.NewRequestWithContext(r.t.Context(), http.MethodGet, path, nil))
	require.Equal(r.t, http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())

	return rec.Body.Bytes()
}

// getPages sends a GET of every page of the list at path, whose total is at
// totalField, and returns how many items it holds.
func (r importedReader) getPages(path, totalField string) int {
	r.t.Helper()
	const limit = domain.MaxPageLimit
	total := 0
	for offset := 0; offset == 0 || offset < total; offset += limit {
		var page map[string]json.RawMessage
		require.NoError(r.t, json.Unmarshal(r.get(fmt.Sprintf("%s?limit=%d&offset=%d", path, limit, offset)), &page))
		require.NoError(r.t, json.Unmarshal(page[totalField], &total), path)
	}

	return total
}

// TestHTTP_ImportedCatalogue applies the scrubbed import and reads all of it through the production handler:
// every page of every list, every piece, person, term, collection and event, every history and every revision.
// Every response is valid by the spec.
func TestHTTP_ImportedCatalogue(t *testing.T) {
	t.Parallel()

	db, imp := importedCatalogue.Get(t)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil))
	contract := openapitest.New(t)
	reader := func(t *testing.T) importedReader {
		return importedReader{t: t, contract: contract, handler: handler}
	}

	t.Run("lists", func(t *testing.T) {
		t.Parallel()
		r := reader(t)
		assert.Equal(t, len(imp.Pieces), r.getPages("/v1/pieces", "total"))
		assert.Equal(t, len(imp.Persons), r.getPages("/v1/persons", "total"))
		assert.Equal(t, len(imp.Collections), r.getPages("/v1/collections", "total"))
		assert.Equal(t, len(imp.Events), r.getPages("/v1/events", "total"))
		assert.Positive(t, r.getPages("/v1/needs-work", "total"))
		r.get("/v1/terms")
	})
	t.Run("pieces", func(t *testing.T) {
		t.Parallel()
		r := reader(t)
		for _, p := range imp.Pieces {
			r.get("/v1/pieces/" + p.Slug)
			r.get(historyPath("pieces", p.Slug))
			r.get(revisionPath(p.Revision))
		}
	})
	t.Run("persons", func(t *testing.T) {
		t.Parallel()
		r := reader(t)
		for _, p := range imp.Persons {
			r.get("/v1/persons/" + p.Slug)
			r.get(historyPath("persons", p.Slug))
			r.get(revisionPath(p.Revision))
		}
	})
	t.Run("terms", func(t *testing.T) {
		t.Parallel()
		r := reader(t)
		for _, term := range imp.Terms {
			r.getPages("/v1/terms/"+term.Slug, "piece_count")
			r.getPages("/v1/terms/"+term.Slug, "person_count")
			r.get(historyPath("terms", term.Slug))
			r.get(revisionPath(term.Revision))
		}
	})
	t.Run("collections_and_events", func(t *testing.T) {
		t.Parallel()
		r := reader(t)
		for _, c := range imp.Collections {
			id := strconv.FormatInt(c.ID, 10)
			r.get("/v1/collections/" + id)
			r.get(historyPath("collections", id))
			r.get(revisionPath(c.Revision))
		}
		for _, e := range imp.Events {
			r.get("/v1/events/" + e.Slug)
			r.get(historyPath("events", e.Slug))
			r.get(revisionPath(e.Revision))
		}
	})
}

func revisionPath(r domain.ImportedRevision) string {
	return "/v1/revisions/" + strconv.FormatInt(r.ID, 10)
}
