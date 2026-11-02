//go:build integration

package mysql_test

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

// Tables the tests name more than once.
const (
	tableRevisions    = "revisions"
	tableTerms        = "terms"
	tablePersons      = "persons"
	tablePieces       = "pieces"
	tableCollections  = "collections"
	tableEvents       = "events"
	tableLinks        = "links"
	tablePerformances = "performances"
	tablePerformers   = "performance_performers"
)

// seededTitle is the title and text of rows seeded before an import.
const seededTitle = "Seeded"

// imported is a database holding the scrubbed import, for the tests that only read it.
var imported = catalogtest.NewShared(func(c *catalogtest.Catalog) domain.CatalogueImport {
	imp, _ := importtest.Scrubbed(c.T())
	c.Import(imp)

	return imp
})

// importRowCounts are the rows imp writes, by table.
func importRowCounts(t *testing.T, imp domain.CatalogueImport) map[string]int {
	t.Helper()
	ancestors, err := imp.TermAncestors()
	require.NoError(t, err)
	n := map[string]int{
		tableTerms: len(imp.Terms), "term_ancestors": len(ancestors), tableCollections: len(imp.Collections),
		tableEvents: len(imp.Events), tablePersons: len(imp.Persons), tablePieces: len(imp.Pieces), tableLinks: len(imp.Links),
	}
	revision := func(r domain.ImportedRevision) {
		n[tableRevisions]++
		n["revision_contributors"] += len(r.Contributors)
	}
	attach := func(l domain.DocLink) { n["link_attachments"] += len(l.Roles) }
	perform := func(p domain.DocPerformance) {
		n[tablePerformances]++
		n[tablePerformers] += len(p.PerformerIDs)
	}
	for _, t := range imp.Terms {
		revision(t.Revision)
		n["term_aliases"] += len(t.Doc.Aliases)
	}
	for _, c := range imp.Collections {
		revision(c.Revision)
	}
	for _, e := range imp.Events {
		revision(e.Revision)
	}
	for _, p := range imp.Persons {
		revision(p.Revision)
		d := p.Doc
		n["person_aliases"] += len(d.Aliases)
		n["person_roles"] += len(d.Roles)
		n["person_affiliations"] += len(d.AffiliationIDs)
		n["person_relations"] += len(d.Relations)
		n["person_preferences"] += len(d.Preferences)
		for _, l := range d.Links {
			attach(l.DocLink)
			for _, perf := range l.Performances {
				perform(perf.DocPerformance)
			}
		}
	}
	for _, p := range imp.Pieces {
		revision(p.Revision)
		d := p.Doc
		n["piece_secondary_kinds"] += len(d.SecondaryKinds)
		n["piece_aliases"] += len(d.Aliases)
		n["piece_terms"] += len(d.Terms)
		n["piece_relations"] += len(d.Relations)
		n["piece_credits"] += len(d.Credits)
		n["piece_preferences"] += len(d.Preferences)
		if d.Conservation.Status != domain.ConservationStatusUnassessed {
			n["conservation_history"]++
		}
		for _, l := range d.Links {
			attach(l.DocLink)
			for _, perf := range l.Performances {
				perform(perf)
			}
		}
	}

	return n
}

// tableRowCounts counts the rows of every catalogue table.
func tableRowCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	n := map[string]int{}
	for _, table := range mysql.CatalogueTables {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))
		n[table] = count
	}

	return n
}

// emptyCounts is every catalogue table with no rows, but those given.
func emptyCounts(rows map[string]int) map[string]int {
	n := map[string]int{}
	for _, table := range mysql.CatalogueTables {
		n[table] = rows[table]
	}

	return n
}

func TestRepository_ImportCatalogue_CatalogueTablesAreEveryCatalogueTable(t *testing.T) {
	t.Parallel()

	var tables []string
	for table := range tableColumns(t, mysqltest.NewDatabase(t)) {
		if table != "users" && table != "api_tokens" {
			tables = append(tables, table)
		}
	}
	assert.ElementsMatch(t, tables, mysql.CatalogueTables)
}

func TestRepository_ImportCatalogue_WritesEverything(t *testing.T) {
	t.Parallel()

	db, imp := imported.Get(t)

	counts := tableRowCounts(t, db)
	assert.Equal(t, emptyCounts(importRowCounts(t, imp)), counts)
	for _, table := range mysql.CatalogueTables {
		// The wiki states no preferences.
		if !strings.HasSuffix(table, "_preferences") {
			assert.Positive(t, counts[table], "the scrubbed import writes %s rows", table)
		}
	}

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM term_ancestors "+
		"WHERE ancestor_id = descendant_id AND depth = 0").Scan(&n))
	assert.Equal(t, len(imp.Terms), n, "every term has its closure self row")
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM revisions WHERE source <> 'import' "+
		"OR status <> 'approved' OR author_user_id IS NOT NULL OR base_revision_id IS NOT NULL").Scan(&n))
	assert.Zero(t, n, "every revision is an approved import revision with no author")
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM links WHERE liveness <> 'unknown'").
		Scan(&n))
	assert.Zero(t, n, "liveness is the column default")
	for _, table := range []string{tableTerms, tableCollections, tableEvents, tablePersons, tablePieces} {
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" e "+
			"LEFT JOIN revisions r ON r.id = e.current_revision_id AND r.entity_id = e.id "+
			"AND r.entity_type = '"+table[:len(table)-1]+"' WHERE r.id IS NULL").Scan(&n))
		assert.Zero(t, n, "every %s row has its own revision as its current revision", table)
	}

	for _, p := range imp.Persons {
		assert.Equal(t, p.Revision.Contributors, revisionContributors(t, db, p.Revision.ID),
			"the contributors of person %d, in order", p.ID)
	}
}

func revisionContributors(t *testing.T, db *sql.DB, revisionID int64) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT name FROM revision_contributors WHERE revision_id = ? "+
		"ORDER BY position", revisionID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })
	names := []string{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())

	return names
}

func TestRepository_ImportCatalogue_SystemFields(t *testing.T) {
	t.Parallel()

	db, imp := imported.Get(t)

	var hidden, legacyPieces, inlineLyrics int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM links WHERE is_hidden").Scan(&hidden))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM pieces WHERE legacy_wiki_pageid "+
		"IS NOT NULL").Scan(&legacyPieces))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM pieces WHERE legacy_had_inline_lyrics").
		Scan(&inlineLyrics))

	var wantHidden, wantLegacy, wantLyrics int
	for _, l := range imp.Links {
		if l.IsHidden {
			wantHidden++
		}
	}
	for _, p := range imp.Pieces {
		if p.Legacy != nil && p.Legacy.PageID != nil {
			wantLegacy++
		}
		if p.LegacyHadInlineLyrics {
			wantLyrics++
		}
	}
	assert.Positive(t, wantHidden, "the scrubbed import hides links whose URL holds a name")
	assert.Equal(t, wantHidden, hidden)
	assert.Equal(t, wantLegacy, legacyPieces)
	assert.Equal(t, 11, inlineLyrics)
	assert.Equal(t, wantLyrics, inlineLyrics)
}

func TestRepository_ImportCatalogue_NormalizedURLInvariant(t *testing.T) {
	t.Parallel()

	db, _ := imported.Get(t)

	rows, err := db.QueryContext(t.Context(), "SELECT id, url, normalized_url FROM links")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })
	for rows.Next() {
		var id int64
		var url, normalized string
		require.NoError(t, rows.Scan(&id, &url, &normalized))
		want, err := domain.NormalizeURL(url)
		require.NoError(t, err)
		assert.Equal(t, want, normalized, "link %d", id)
	}
	require.NoError(t, rows.Err())
}

func TestRepository_ImportCatalogue_SnapshotsDecode(t *testing.T) {
	t.Parallel()

	db, _ := imported.Get(t)

	rows, err := db.QueryContext(t.Context(), "SELECT id, entity_type, snapshot_json FROM revisions")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })
	decode := map[domain.EntityType]func([]byte) error{
		domain.EntityTypePiece:      decodeAs[domain.PieceDocument],
		domain.EntityTypePerson:     decodeAs[domain.PersonDocument],
		domain.EntityTypeTerm:       decodeAs[domain.TermDocument],
		domain.EntityTypeCollection: decodeAs[domain.CollectionDocument],
		domain.EntityTypeEvent:      decodeAs[domain.EventDocument],
	}
	n := 0
	for rows.Next() {
		var id int64
		var entityType domain.EntityType
		var snapshot []byte
		require.NoError(t, rows.Scan(&id, &entityType, &snapshot))
		require.Contains(t, decode, entityType)
		require.NoError(t, decode[entityType](snapshot), "revision %d", id)
		n++
	}
	require.NoError(t, rows.Err())
	assert.Positive(t, n)
}

func decodeAs[D domain.Document](data []byte) error {
	_, err := domain.DecodeDocument[D](data)

	return err
}

func TestRepository_ImportCatalogue_ReadAPI(t *testing.T) {
	t.Parallel()

	db, imp := imported.Get(t)
	repo := mysql.New(db)

	for _, slug := range []string{"three-words", "band-of-brothers", "black-alman", "the-burning-of-tara"} {
		piece, err := repo.GetPublicPiece(t.Context(), slug)
		require.NoError(t, err, slug)
		assert.Equal(t, slug, piece.Slug)
	}

	checked := 0
	for _, p := range imp.Persons {
		var hidden []string
		for _, a := range p.Doc.Aliases {
			if !a.IsPublic {
				hidden = append(hidden, a.Name)
			}
		}
		if len(hidden) == 0 || p.Doc.Status == domain.PersonStatusPrefersNoPage {
			continue
		}
		person, err := repo.GetPublicPerson(t.Context(), p.Slug)
		require.NoError(t, err, p.Slug)
		require.NotNil(t, person.Profile)
		for _, a := range person.Profile.Aliases {
			assert.NotContains(t, hidden, a.Name, "person %d shows a hidden alias", p.ID)
		}
		checked++
	}
	assert.Positive(t, checked, "the scrubbed import has persons with hidden aliases")
}

func TestRepository_ImportCatalogue_RefusesNonEmpty(t *testing.T) {
	t.Parallel()

	imp, _ := importtest.Scrubbed(t)
	for name, seed := range map[string]struct {
		seed func(c *catalogtest.Catalog)
		rows map[string]int
	}{
		"revision": {func(c *catalogtest.Catalog) {
			c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: 1,
				Snapshot: domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: seededTitle}})
		}, map[string]int{tableRevisions: 1}},
		// Rows of later tables only, and no revision.
		"performance": {func(c *catalogtest.Catalog) {
			c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: seededTitle, Links: []domain.DocPieceLink{{
				DocLink:      domain.DocLink{URL: "https://example.org/seeded"},
				Performances: []domain.DocPerformance{{DescriptionVerbatim: seededTitle}},
			}}}})
			c.Exec("UPDATE pieces SET current_revision_id = NULL")
			c.Exec("DELETE FROM revisions")
		}, map[string]int{tablePieces: 1, tableLinks: 1, tablePerformances: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := mysqltest.NewDatabase(t)
			seed.seed(catalogtest.New(t, db))

			err := mysql.New(db).ImportCatalogue(t.Context(), imp)

			require.ErrorIs(t, err, datasources.ErrCatalogueNotEmpty)
			assert.Equal(t, emptyCounts(seed.rows), tableRowCounts(t, db), "no rows were written")
		})
	}
}

// TestRepository_ImportCatalogue_RollsBackOnError plants the failure in the last table the import writes, so
// that every row of every earlier table is shown to be rolled back, and checks that the error does not quote the
// row the server refused.
func TestRepository_ImportCatalogue_RollsBackOnError(t *testing.T) {
	t.Parallel()

	imp, _ := importtest.Scrubbed(t)
	planted := false
	for k := len(imp.Pieces) - 1; k >= 0 && !planted; k-- {
		for l, link := range imp.Pieces[k].Doc.Links {
			for p, perf := range link.Performances {
				if len(perf.PerformerIDs) > 0 && !planted {
					// The performer twice: a duplicate primary key of performance_performers.
					ids := []int64{perf.PerformerIDs[0], perf.PerformerIDs[0]}
					imp.Pieces[k].Doc.Links[l].Performances[p].PerformerIDs = ids
					planted = true
				}
			}
		}
	}
	require.True(t, planted, "the scrubbed import has a performance with a performer")
	require.Equal(t, tablePerformers, mysql.CatalogueTables[len(mysql.CatalogueTables)-1])
	db := mysqltest.NewDatabase(t)

	err := mysql.New(db).ImportCatalogue(t.Context(), imp)

	var writeErr *mysql.ImportDatabaseError
	require.ErrorAs(t, err, &writeErr)
	assert.Equal(t, mysql.ImportDatabaseError{Step: "writing " + tablePerformers, Number: 1062}, *writeErr)
	assert.NotContains(t, err.Error(), "Duplicate", "the server's message, which quotes values, is not kept")
	assert.Equal(t, emptyCounts(nil), tableRowCounts(t, db), "every table is rolled back")
}

// TestRepository_ImportCatalogue_ConcurrentImports runs two imports into one empty database at once: exactly one
// succeeds, and the other fails with a typed error, having written nothing of its own.
func TestRepository_ImportCatalogue_ConcurrentImports(t *testing.T) {
	t.Parallel()

	imp, _ := importtest.Scrubbed(t)
	db := mysqltest.NewDatabase(t)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for k := range errs {
		wg.Go(func() { errs[k] = mysql.New(db).ImportCatalogue(t.Context(), imp) })
	}
	wg.Wait()

	succeeded := slices.IndexFunc(errs, func(err error) bool { return err == nil })
	require.NotEqual(t, -1, succeeded, "one import succeeds: %v", errs)
	failed := errs[1-succeeded]
	assert.True(t, errors.Is(failed, datasources.ErrImportInProgress) ||
		errors.Is(failed, datasources.ErrCatalogueNotEmpty), "the other fails with a typed error: %v", failed)
	assert.Equal(t, emptyCounts(importRowCounts(t, imp)), tableRowCounts(t, db), "the catalogue holds one import")

	// The lock was released: a third import gets past it to the emptiness check.
	require.ErrorIs(t, mysql.New(db).ImportCatalogue(t.Context(), imp), datasources.ErrCatalogueNotEmpty)
}
