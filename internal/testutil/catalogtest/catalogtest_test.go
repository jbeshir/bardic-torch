//go:build integration

package catalogtest_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// Invented fixture names used by several tests.
const (
	kettleTitle  = "The Copper Kettle"
	revelName    = "Harvest Revel"
	wrenName     = "Wren of the Vale"
	firesideName = "Fireside Songbook"
	swordsName   = "Swords"
)

func ptr[T any](v T) *T { return &v }

// queryPairs returns the rows of a query selecting two integer columns.
func queryPairs(t *testing.T, db *sql.DB, query string, args ...any) [][2]int64 {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), query, args...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	var pairs [][2]int64
	for rows.Next() {
		var p [2]int64
		require.NoError(t, rows.Scan(&p[0], &p[1]))
		pairs = append(pairs, p)
	}
	require.NoError(t, rows.Err())

	return pairs
}

func queryInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()

	var n int64
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&n))

	return n
}

// TestCatalog_TermClosure checks that every term has its own depth-0 row and one row per ancestor, to depth 3.
func TestCatalog_TermClosure(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	root := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "War"}})
	child := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Battles", ParentID: &root.ID}})
	grandchild := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Sieges", ParentID: &child.ID}})
	greatGrandchild := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Long Sieges",
		ParentID: &grandchild.ID}, Unapproved: true})
	sibling := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: swordsName, ParentID: &root.ID}})
	other := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Warlike"}})

	for _, tc := range []struct {
		term      catalogtest.Ref
		ancestors [][2]int64 // (ancestor ID, depth)
	}{
		{root, [][2]int64{{root.ID, 0}}},
		{child, [][2]int64{{child.ID, 0}, {root.ID, 1}}},
		{grandchild, [][2]int64{{grandchild.ID, 0}, {child.ID, 1}, {root.ID, 2}}},
		{greatGrandchild, [][2]int64{{greatGrandchild.ID, 0}, {grandchild.ID, 1}, {child.ID, 2}, {root.ID, 3}}},
		{sibling, [][2]int64{{sibling.ID, 0}, {root.ID, 1}}},
		{other, [][2]int64{{other.ID, 0}}},
	} {
		got := queryPairs(t, db, "SELECT ancestor_id, depth FROM term_ancestors WHERE descendant_id = ?", tc.term.ID)
		assert.ElementsMatch(t, tc.ancestors, got, tc.term.Slug)
	}
	assert.Equal(t, int64(4+3+2+1+2+1), queryInt(t, db, "SELECT COUNT(*) FROM term_ancestors"))
	assert.Equal(t, "long-sieges", greatGrandchild.Slug)
}

// requireCurrentRevision checks that the entity in table has revisionID as its current revision, an approved
// import revision of it whose snapshot decodes to want.
func requireCurrentRevision[D domain.Document](t *testing.T, db *sql.DB, table string, entityType domain.EntityType,
	ref catalogtest.Ref, want D) {
	t.Helper()

	require.NotZero(t, ref.RevisionID)
	var current int64
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT current_revision_id FROM "+table+" WHERE id = ?", ref.ID).Scan(&current))
	assert.Equal(t, ref.RevisionID, current)

	var gotType, status, source string
	var entityID int64
	var snapshot []byte
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT entity_type, entity_id, status, source, snapshot_json "+
		"FROM revisions WHERE id = ?", ref.RevisionID).Scan(&gotType, &entityID, &status, &source, &snapshot))
	assert.Equal(t, string(entityType), gotType)
	assert.Equal(t, ref.ID, entityID)
	assert.Equal(t, string(domain.RevisionStatusApproved), status)
	assert.Equal(t, string(domain.RevisionSourceImport), source)

	got, err := domain.DecodeDocument[D](snapshot)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// requireUnapproved checks that the entity in table has no current revision and no revisions at all.
func requireUnapproved(t *testing.T, db *sql.DB, table string, entityType domain.EntityType, ref catalogtest.Ref) {
	t.Helper()

	assert.Zero(t, ref.RevisionID)
	var current sql.NullInt64
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT current_revision_id FROM "+table+" WHERE id = ?", ref.ID).Scan(&current))
	assert.False(t, current.Valid)
	assert.Zero(t, queryInt(t, db, "SELECT COUNT(*) FROM revisions WHERE entity_type = ? AND entity_id = ?",
		entityType, ref.ID))
}

// TestCatalog_ApprovedSwitch checks that an approved entity has an approved revision holding its document, with
// defaults applied, as its current revision; an unapproved one has neither.
func TestCatalog_ApprovedSwitch(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)

	term := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Swords (Topic)", DisplayName: swordsName},
		Slug: "swords-topic"})
	requireCurrentRevision(t, db, "terms", domain.EntityTypeTerm, term, domain.TermDocument{SchemaVersion: 1,
		Facet: domain.FacetTopic, Name: "Swords (Topic)", DisplayName: swordsName})
	assert.Equal(t, "swords-topic", term.Slug)
	requireUnapproved(t, db, "terms", domain.EntityTypeTerm,
		c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Pending"}, Unapproved: true}))

	person := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: wrenName}})
	requireCurrentRevision(t, db, "persons", domain.EntityTypePerson, person, domain.PersonDocument{
		SchemaVersion: 1, Name: wrenName, Kind: domain.PersonKindCommunityBard,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown})
	requireUnapproved(t, db, "persons", domain.EntityTypePerson,
		c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Pending Bard"}, Unapproved: true}))

	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: kettleTitle,
		Credits: []domain.DocCredit{{PersonID: &person.ID, Role: domain.CreditRoleLyrics}}}})
	requireCurrentRevision(t, db, "pieces", domain.EntityTypePiece, piece, domain.PieceDocument{
		SchemaVersion: 1, Title: kettleTitle, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
		Credits: []domain.DocCredit{{PersonID: &person.ID, Role: domain.CreditRoleLyrics,
			Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}})
	requireUnapproved(t, db, "pieces", domain.EntityTypePiece,
		c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Unsung"}, Unapproved: true}))

	collection := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: firesideName}})
	requireCurrentRevision(t, db, "collections", domain.EntityTypeCollection, collection,
		domain.CollectionDocument{SchemaVersion: 1, Name: firesideName, Kind: domain.CollectionKindOther,
			Enumerable: domain.CollectionEnumerableUnknown})
	assert.Empty(t, collection.Slug)
	requireUnapproved(t, db, "collections", domain.EntityTypeCollection,
		c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Draft Book"}, Unapproved: true}))

	event := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: revelName}})
	requireCurrentRevision(t, db, "events", domain.EntityTypeEvent, event, domain.EventDocument{SchemaVersion: 1,
		Name: revelName, Kind: domain.EventKindEvent})
	requireUnapproved(t, db, "events", domain.EntityTypeEvent,
		c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Pending Revel"}, Unapproved: true}))

	// Default revisions are one second apart, in creation order.
	var first, last time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT MIN(created_at), MAX(created_at) FROM revisions").
		Scan(&first, &last))
	assert.Equal(t, 4*time.Second, last.Sub(first))
}

// TestCatalog_PieceAndPersonRows checks that every part of a piece and a person document reaches its rows, that a
// link both attach to is one row, and that FULLTEXT sees the rows as soon as the method returns.
func TestCatalog_PieceAndPersonRows(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	war := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "War"}})
	gaelic := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetCulture, Name: "Gaelic"}})
	northmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: "Northmarch"}})
	singers := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "The Lantern Singers",
		Kind: domain.PersonKindGroup}, SortName: "Lantern Singers"})
	collection := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: firesideName}})
	event := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: revelName}})

	shared := domain.DocLink{URL: "https://example.org/revel-video", Title: ptr("Revel video"),
		CollectionID: &collection.ID, Roles: []domain.LinkRole{domain.LinkRolePerformance}}
	wren := c.Person(catalogtest.Person{
		Doc: domain.PersonDocument{
			Name: wrenName,
			Aliases: []domain.DocPersonAlias{
				{Name: "Wren", Kind: domain.PersonAliasKindNickname, IsPublic: true},
				{Name: "Jo Marker", Kind: domain.PersonAliasKindLegal},
			},
			Roles:          []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
			AffiliationIDs: []int64{northmarch.ID},
			Relations:      []domain.DocPersonRelation{{ToPersonID: singers.ID, Type: domain.PersonRelationTypeMemberOf}},
			Links:          []domain.DocPersonLink{{DocLink: shared}},
			Preferences: []domain.DocPreference{{Dimension: domain.PreferenceDimensionRecording,
				Value: json.RawMessage(`{"allowed": false}`), SourceURL: "https://example.org/wren-prefs"}},
		},
		SortLetter: "W",
	})
	other := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Ballad of Grey Hill",
		Kind: domain.PieceKindPoem}})
	kettle := c.Piece(catalogtest.Piece{
		Doc: domain.PieceDocument{
			Title:          kettleTitle,
			SecondaryKinds: []domain.PieceKind{domain.PieceKindComedy},
			NotesMD:        ptr("A marmalade-coloured kettle."),
			Aliases:        []domain.DocPieceAlias{{Text: "Kettle Song", Kind: domain.PieceAliasKindNickname}},
			Terms: []domain.DocPieceTerm{
				{TermID: war.ID, IsPrimary: true},
				{TermID: gaelic.ID, Part: ptr(domain.PiecePartTune)},
			},
			Relations: []domain.DocPieceRelation{{ToPieceID: other.ID, Type: domain.PieceRelationTypeVariantOf}},
			Credits: []domain.DocCredit{
				{PersonID: &wren.ID, Role: domain.CreditRoleLyrics, Certainty: domain.CertaintyProbable},
				{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional},
			},
			Links: []domain.DocPieceLink{
				{DocLink: domain.DocLink{URL: "https://example.org/kettle-lyrics",
					Roles: []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleText}}},
				{DocLink: shared, Performances: []domain.DocPerformance{{EventID: &event.ID, Year: ptr[int16](2019),
					DescriptionVerbatim: "Sung at the revel", PerformerIDs: []int64{wren.ID, singers.ID}}}},
			},
		},
		SortTitle:           "Copper Kettle",
		ConservationHistory: []catalogtest.ConservationEntry{{Status: domain.ConservationStatusUnassessed, Reason: "Imported"}},
	})
	assert.Equal(t, "the-copper-kettle", kettle.Slug)

	var sortTitle, sortLetter string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT sort_title, sort_letter FROM pieces WHERE id = ?",
		kettle.ID).Scan(&sortTitle, &sortLetter))
	assert.Equal(t, []string{"Copper Kettle", "C"}, []string{sortTitle, sortLetter})

	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM piece_secondary_kinds WHERE piece_id = ?":                                               1,
		"SELECT COUNT(*) FROM piece_aliases WHERE piece_id = ?":                                                       1,
		"SELECT COUNT(*) FROM piece_relations WHERE from_piece_id = ?":                                                1,
		"SELECT COUNT(*) FROM conservation_history WHERE piece_id = ?":                                                1,
		"SELECT COUNT(*) FROM link_attachments WHERE piece_id = ?":                                                    3,
		"SELECT COUNT(*) FROM performances WHERE piece_id = ?":                                                        1,
		"SELECT COUNT(*) FROM piece_terms WHERE piece_id = ? AND is_primary AND part IS NULL":                         1,
		"SELECT COUNT(*) FROM piece_credits WHERE piece_id = ? AND person_id IS NULL AND attribution = 'traditional'": 1,
	} {
		assert.Equal(t, want, queryInt(t, db, query, kettle.ID), query)
	}
	assert.Equal(t, [][2]int64{{war.ID, 0}, {gaelic.ID, 1}},
		queryPairs(t, db, "SELECT term_id, position FROM piece_terms WHERE piece_id = ? ORDER BY position", kettle.ID))
	assert.Equal(t, [][2]int64{{wren.ID, 0}, {singers.ID, 1}}, queryPairs(t, db,
		"SELECT pp.person_id, pp.position FROM performance_performers pp "+
			"JOIN performances p ON p.id = pp.performance_id WHERE p.piece_id = ? ORDER BY pp.position", kettle.ID))
	assert.Equal(t, [][2]int64{{0, 1}, {1, 1}}, queryPairs(t, db, "SELECT position, COUNT(*) FROM link_attachments "+
		"WHERE piece_id = ? AND role <> 'text' GROUP BY position ORDER BY position", kettle.ID))

	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM person_aliases WHERE person_id = ? AND is_public":     1,
		"SELECT COUNT(*) FROM person_aliases WHERE person_id = ? AND NOT is_public": 1,
		"SELECT COUNT(*) FROM person_roles WHERE person_id = ?":                     2,
		"SELECT COUNT(*) FROM person_affiliations WHERE person_id = ?":              1,
		"SELECT COUNT(*) FROM person_relations WHERE from_person_id = ?":            1,
		"SELECT COUNT(*) FROM person_preferences WHERE person_id = ?":               1,
		"SELECT COUNT(*) FROM link_attachments WHERE person_id = ?":                 1,
	} {
		assert.Equal(t, want, queryInt(t, db, query, wren.ID), query)
	}

	// The shared link is one row, attached to both, with the person's editor-owned fields.
	assert.Equal(t, int64(2), queryInt(t, db, "SELECT COUNT(*) FROM links"))
	assert.Equal(t, int64(2), queryInt(t, db, "SELECT COUNT(*) FROM link_attachments a JOIN links l "+
		"ON l.id = a.link_id WHERE l.url = ? AND l.collection_id = ?", shared.URL, collection.ID))

	// Committed, so FULLTEXT finds the piece by its notes and alias right away.
	assert.Equal(t, kettle.ID, queryInt(t, db,
		"SELECT id FROM pieces WHERE MATCH(title, notes_md) AGAINST ('+marmalade*' IN BOOLEAN MODE)"))
	assert.Equal(t, kettle.ID, queryInt(t, db,
		"SELECT piece_id FROM piece_aliases WHERE MATCH(text) AGAINST ('+kettle*' IN BOOLEAN MODE)"))
	assert.Equal(t, wren.ID, queryInt(t, db,
		"SELECT person_id FROM person_aliases WHERE MATCH(name) AGAINST ('+marker*' IN BOOLEAN MODE)"))
}

func TestCatalog_RevisionAndLinkState(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	event := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: revelName}, Slug: "revel"})

	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	pending := c.Revision(catalogtest.Revision{
		EntityType: domain.EntityTypeEvent, EntityID: event.ID, BaseRevisionID: event.RevisionID,
		Snapshot: json.RawMessage(`{"schema_version": 9}`), Status: domain.RevisionStatusPending,
		Source: domain.RevisionSourceUser, Summary: "Rename", CreatedAt: at,
		Contributors: []string{"Wiki Editor B", "Wiki Editor A"},
	})

	var base int64
	var status, source, summary string
	var createdAt time.Time
	var snapshot []byte
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT base_revision_id, status, source, summary, "+
		"created_at, snapshot_json FROM revisions WHERE id = ?", pending).
		Scan(&base, &status, &source, &summary, &createdAt, &snapshot))
	assert.Equal(t, event.RevisionID, base)
	assert.Equal(t, []string{"pending", "user", "Rename"}, []string{status, source, summary})
	assert.Equal(t, at, createdAt)
	_, err := domain.DecodeDocument[domain.EventDocument](snapshot)
	require.ErrorIs(t, err, domain.ErrUnsupportedSchemaVersion)

	rows, err := db.QueryContext(t.Context(),
		"SELECT name FROM revision_contributors WHERE revision_id = ? ORDER BY position", pending)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Wiki Editor B", "Wiki Editor A"}, names)
	assert.Equal(t, event.RevisionID, queryInt(t, db, "SELECT current_revision_id FROM events WHERE id = ?",
		event.ID), "seeding a revision leaves the current revision alone")

	const url = "https://example.org/kettle"
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: kettleTitle,
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: url, Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}}})
	c.SetLinkState(url, catalogtest.LinkState{Liveness: domain.LivenessDead, LastCheckedAt: &at, IsHidden: true})
	var liveness string
	var hidden bool
	var checked time.Time
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT liveness, is_hidden, last_checked_at FROM links WHERE url = ?", url).Scan(&liveness, &hidden, &checked))
	assert.Equal(t, "dead", liveness)
	assert.True(t, hidden)
	assert.Equal(t, at, checked)
}

type sharedFixture struct {
	piece catalogtest.Ref
}

var shared = catalogtest.NewShared(func(c *catalogtest.Catalog) sharedFixture {
	return sharedFixture{piece: c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: kettleTitle}})}
})

// TestShared_SeedsOnceReadOnly checks that parallel tests get the same seeded database through a pool that
// refuses writes.
func TestShared_SeedsOnceReadOnly(t *testing.T) {
	t.Parallel()

	names := make(chan string, 3)
	t.Run("readers", func(t *testing.T) {
		for range 3 {
			t.Run("reader", func(t *testing.T) {
				t.Parallel()

				db, fixture := shared.Get(t)
				assert.Equal(t, int64(1), queryInt(t, db, "SELECT COUNT(*) FROM pieces WHERE id = ?", fixture.piece.ID))
				var name string
				require.NoError(t, db.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&name))
				names <- name

				_, err := db.ExecContext(t.Context(), "UPDATE pieces SET is_stub = TRUE")
				var mysqlErr *mysqldriver.MySQLError
				require.ErrorAs(t, err, &mysqlErr)
				assert.Equal(t, uint16(1792), mysqlErr.Number)
			})
		}
	})
	close(names)

	distinct := map[string]bool{}
	for name := range names {
		distinct[name] = true
	}
	assert.Len(t, distinct, 1)
}
