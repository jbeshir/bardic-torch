//go:build integration

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
)

// importedCatalogue is a database holding the import of the committed scrubbed crawl, as --apply writes it.
var importedCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) domain.CatalogueImport {
	imp, _ := importtest.Scrubbed(c.T())
	c.Import(imp)

	return imp
})

// The marks of private data in the scrubbed copy (internal/scrub) that no public response may show: the
// placeholder lines standing in for lyrics, a legal name's placeholder in any form ("Legal Name 001",
// "legal-name-001", "Legal_Name_001", "LegalName001"), and a withheld name or email followed by the number of the
// placeholder it replaced ("[name withheld] 001").
var (
	lyricPlaceholder        = regexp.MustCompile(`scrubbedlyric`)
	legalNamePlaceholder    = regexp.MustCompile(`(?i)legal[ _-]?name[ _-]?[0-9]{3}`)
	numberedWithheldMarkers = regexp.MustCompile(`\[[a-z]+ withheld\][ _-]?[0-9]{2,3}\b`)
)

// hiddenAliases returns the aliases imp hides.
func hiddenAliases(imp domain.CatalogueImport) []string {
	var out []string
	for _, p := range imp.Persons {
		for _, a := range p.Doc.Aliases {
			if !a.IsPublic {
				out = append(out, a.Name)
			}
		}
	}

	return out
}

// importedLeaks returns what of the scrubbed copy's private data body shows: each kind of placeholder, and each
// hidden alias (as a whole word or words).
func importedLeaks(body string, hidden []*regexp.Regexp) []string {
	var leaks []string
	for name, marker := range map[string]*regexp.Regexp{"lyric placeholder": lyricPlaceholder,
		"legal name placeholder": legalNamePlaceholder, "numbered withheld marker": numberedWithheldMarkers} {
		if found := marker.FindString(body); found != "" {
			leaks = append(leaks, name+": "+found)
		}
	}
	for _, alias := range hidden {
		if found := alias.FindString(body); found != "" {
			leaks = append(leaks, "hidden alias: "+found)
		}
	}

	return leaks
}

// aliasPatterns matches each alias as whole words, in its JSON-escaped form as a response holds it.
func aliasPatterns(t *testing.T, aliases []string) []*regexp.Regexp {
	t.Helper()
	patterns := make([]*regexp.Regexp, 0, len(aliases))
	for _, a := range aliases {
		escaped, err := json.Marshal(a)
		require.NoError(t, err)
		patterns = append(patterns, regexp.MustCompile(`\b`+regexp.QuoteMeta(string(escaped[1:len(escaped)-1]))+`\b`))
	}

	return patterns
}

// importedReader sends GETs to the handler over the imported catalogue, validating each against the spec, and
// checks every body for leaks.
type importedReader struct {
	t        *testing.T
	contract *openapitest.Validator
	handler  http.Handler
	hidden   []*regexp.Regexp
}

// get sends a GET of path, requires 200 and no leak, and returns the body.
func (r importedReader) get(path string) []byte {
	r.t.Helper()
	rec := r.contract.Serve(r.t, r.handler, httptest.NewRequestWithContext(r.t.Context(), http.MethodGet, path, nil))
	require.Equal(r.t, http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())
	assert.Empty(r.t, importedLeaks(rec.Body.String(), r.hidden), "%s shows private data", path)

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
// Every response is valid by the spec, and none shows a lyric placeholder, a legal name's placeholder in any
// form, a withheld marker with a placeholder's number, or a hidden alias.
func TestHTTP_ImportedCatalogue(t *testing.T) {
	t.Parallel()

	db, imp := importedCatalogue.Get(t)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil))
	contract := openapitest.New(t)
	hidden := hiddenAliases(imp)
	require.NotEmpty(t, hidden, "the scrubbed import hides aliases, so this test checks them")
	patterns := aliasPatterns(t, hidden)
	reader := func(t *testing.T) importedReader {
		return importedReader{t: t, contract: contract, handler: handler, hidden: patterns}
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

// TestHTTP_ImportedCatalogue_PlantedLeakFails shows that the scan can fail: with one hidden alias of a person
// with a page made public, that person's page shows it, and the scan reports it, as it reports every placeholder
// kind planted in a body.
func TestHTTP_ImportedCatalogue_PlantedLeakFails(t *testing.T) {
	t.Parallel()

	imp, _ := importtest.Scrubbed(t)
	hidden := hiddenAliases(imp)
	var slug, alias string
	for k, p := range imp.Persons {
		for a, al := range p.Doc.Aliases {
			if !al.IsPublic && slug == "" && p.Doc.Status != domain.PersonStatusPrefersNoPage {
				imp.Persons[k].Doc.Aliases[a].IsPublic = true
				slug, alias = p.Slug, al.Name
			}
		}
	}
	require.NotEmpty(t, slug, "the scrubbed import has a person with a page and a hidden alias")
	db := mysqltest.NewDatabase(t)
	catalogtest.New(t, db).Import(imp)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/persons/"+slug, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	escaped, err := json.Marshal(alias)
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), string(escaped), "the alias made public is shown")

	assert.NotEmpty(t, importedLeaks(rec.Body.String(), aliasPatterns(t, hidden)))
	for _, planted := range []string{"scrubbedlyric placeholder line", "by Legal Name 007", "/wiki/Legal_Name_007",
		"legal-name-007.example.org", "by [name withheld] 007"} {
		assert.NotEmpty(t, importedLeaks(`{"notes_md":"`+planted+`"}`, nil), planted)
	}
	assert.Empty(t, importedLeaks(`{"notes_md":"[name withheld], 2003; Wiki User 01"}`, nil))
}
