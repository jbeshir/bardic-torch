//go:build integration

package mysql_test

import (
	"database/sql"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MySQL error numbers for violated constraints.
const (
	// errDuplicateEntry is ER_DUP_ENTRY: a unique index already has the value.
	errDuplicateEntry = 1062
	// errNoReferencedRow is ER_NO_REFERENCED_ROW_2: a foreign key names a row that does not exist.
	errNoReferencedRow = 1452
)

// requireMySQLError requires err to be a MySQL error with the given number.
func requireMySQLError(t *testing.T, err error, number uint16) {
	t.Helper()

	var mysqlErr *mysqldriver.MySQLError
	require.ErrorAs(t, err, &mysqlErr)
	assert.Equal(t, number, mysqlErr.Number, mysqlErr.Message)
}

// enumColumnType returns the information_schema.columns.column_type MySQL reports for an ENUM of values.
func enumColumnType[T ~string](values ...T) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "'" + string(v) + "'"
	}

	return "enum(" + strings.Join(quoted, ",") + ")"
}

// TestMigrations_EnumColumns requires every ENUM column to hold exactly the values of its domain type, in the
// same order, and every ENUM column to be listed, so that a value added on one side only fails here.
func TestMigrations_EnumColumns(t *testing.T) {
	t.Parallel()

	entityTypes := enumColumnType(domain.EntityTypePiece, domain.EntityTypePerson, domain.EntityTypeTerm,
		domain.EntityTypeCollection, domain.EntityTypeEvent)
	revisionSources := enumColumnType(domain.RevisionSourceUser, domain.RevisionSourceImport)
	revisionStatuses := enumColumnType(domain.RevisionStatusPending, domain.RevisionStatusApproved,
		domain.RevisionStatusRejected, domain.RevisionStatusWithdrawn)
	facets := enumColumnType(domain.FacetMood, domain.FacetTopic, domain.FacetCulture, domain.FacetLanguage,
		domain.FacetLegendaria, domain.FacetForm, domain.FacetOrigin, domain.FacetOther, domain.FacetAffiliation)
	collectionKinds := enumColumnType(domain.CollectionKindIndexOrLinkList, domain.CollectionKindSongbookPDF,
		domain.CollectionKindSongbookWebsite, domain.CollectionKindWebsite, domain.CollectionKindWikiCategory,
		domain.CollectionKindWiki, domain.CollectionKindYouTubeChannel, domain.CollectionKindYouTubePlaylist,
		domain.CollectionKindGoogleDocSongbook, domain.CollectionKindBlog, domain.CollectionKindArchivedSongbook,
		domain.CollectionKindHostedDocument, domain.CollectionKindOther)
	enumerable := enumColumnType(domain.CollectionEnumerableUnknown, domain.CollectionEnumerableYes,
		domain.CollectionEnumerablePDF, domain.CollectionEnumerableBrowser, domain.CollectionEnumerableAPI,
		domain.CollectionEnumerableNo)
	eventKinds := enumColumnType(domain.EventKindVenue, domain.EventKindEvent)
	personKinds := enumColumnType(domain.PersonKindCommunityBard, domain.PersonKindHistorical,
		domain.PersonKindGroup)
	personStatuses := enumColumnType(domain.PersonStatusActive, domain.PersonStatusDeceased,
		domain.PersonStatusPrefersNoPage)
	licensingStatuses := enumColumnType(domain.LicensingStatusKnown, domain.LicensingStatusUnknown)
	personAliasKinds := enumColumnType(domain.PersonAliasKindLegal, domain.PersonAliasKindSCA,
		domain.PersonAliasKindStage, domain.PersonAliasKindNickname, domain.PersonAliasKindFormer,
		domain.PersonAliasKindRedirect)
	personRoles := enumColumnType(domain.PersonRoleSinger, domain.PersonRoleSongwriter, domain.PersonRoleMusician,
		domain.PersonRoleInstrumentalist, domain.PersonRoleStoryteller, domain.PersonRolePoet,
		domain.PersonRoleJester, domain.PersonRoleDancer)
	personRelationTypes := enumColumnType(domain.PersonRelationTypeHeirOf, domain.PersonRelationTypeMemberOf,
		domain.PersonRelationTypeSameAs)
	pieceKinds := enumColumnType(domain.PieceKindSong, domain.PieceKindPoem, domain.PieceKindStory,
		domain.PieceKindDanceMusic, domain.PieceKindInstrumental, domain.PieceKindComedy)
	conservationStatuses := enumColumnType(domain.ConservationStatusUnassessed, domain.ConservationStatusLowRisk,
		domain.ConservationStatusEndangered, domain.ConservationStatusCriticallyEndangered,
		domain.ConservationStatusRestricted)
	licences := enumColumnType(domain.LicencePD, domain.LicenceCCBY, domain.LicenceCCBYSA, domain.LicenceCCBYNC,
		domain.LicenceCCBYNCSA, domain.LicenceCCBYND, domain.LicenceCCBYNCND, domain.LicenceARR,
		domain.LicenceOther)
	pieceAliasKinds := enumColumnType(domain.PieceAliasKindAltTitle, domain.PieceAliasKindNickname,
		domain.PieceAliasKindSubtitle, domain.PieceAliasKindRedirect)
	parts := enumColumnType(domain.PiecePartWords, domain.PiecePartTune, domain.PiecePartVerses,
		domain.PiecePartChorus)
	pieceRelationTypes := enumColumnType(domain.PieceRelationTypeVariantOf,
		domain.PieceRelationTypeContrafactumOf, domain.PieceRelationTypeSettingOf,
		domain.PieceRelationTypeSameTextOtherForm)
	creditRoles := enumColumnType(domain.CreditRoleLyrics, domain.CreditRoleTune, domain.CreditRoleAuthor,
		domain.CreditRoleChoreography, domain.CreditRoleMusic, domain.CreditRoleArrangement,
		domain.CreditRoleTranslation)
	attributions := enumColumnType(domain.AttributionNamed, domain.AttributionTraditional,
		domain.AttributionUnknown, domain.AttributionAnonymous, domain.AttributionVarious)
	certainties := enumColumnType(domain.CertaintyCertain, domain.CertaintyProbable, domain.CertaintyPossible,
		domain.CertaintyDisputed)
	placeTypes := enumColumnType(domain.LinkPlaceTypeRecordingPlatform, domain.LinkPlaceTypeStreaming,
		domain.LinkPlaceTypeAuthorSite, domain.LinkPlaceTypeCommunityArchive, domain.LinkPlaceTypePhysicalArchive,
		domain.LinkPlaceTypeBook, domain.LinkPlaceTypeVendor, domain.LinkPlaceTypeAuthorContact,
		domain.LinkPlaceTypeOralTradition, domain.LinkPlaceTypeOther)
	liveness := enumColumnType(domain.LivenessUnknown, domain.LivenessOK, domain.LivenessDead,
		domain.LivenessBlocked)
	linkRoles := enumColumnType(domain.LinkRoleOfficial, domain.LinkRoleLyrics, domain.LinkRoleText,
		domain.LinkRoleSheetMusic, domain.LinkRoleDanceInstructions, domain.LinkRoleTextSource,
		domain.LinkRoleTranslation, domain.LinkRolePerformance, domain.LinkRoleLicensingSource,
		domain.LinkRoleWebsite, domain.LinkRoleExternalProfile, domain.LinkRoleOther)

	want := map[string]string{
		"users.role":                 enumColumnType(domain.RoleContributor, domain.RoleModerator),
		"revisions.entity_type":      entityTypes,
		"revisions.source":           revisionSources,
		"revisions.status":           revisionStatuses,
		"terms.facet":                facets,
		"collections.kind":           collectionKinds,
		"collections.enumerable":     enumerable,
		"events.kind":                eventKinds,
		"persons.kind":               personKinds,
		"persons.status":             personStatuses,
		"persons.licensing_status":   licensingStatuses,
		"person_aliases.kind":        personAliasKinds,
		"person_roles.role":          personRoles,
		"person_relations.type":      personRelationTypes,
		"pieces.kind":                pieceKinds,
		"pieces.conservation_status": conservationStatuses,
		"pieces.licensing_status":    licensingStatuses,
		"pieces.licence":             licences,
		"pieces.licence_version": enumColumnType("1.0", "2.0", "2.5",
			"3.0", "4.0"),
		"link_reports.reason":         enumColumnType("broken", "wrong_link", "privacy", "copyright", "lyrics", "other"),
		"link_reports.status":         enumColumnType("open", "resolved", "dismissed"),
		"contact_requests.kind":       enumColumnType("takedown", "opt_out", "correction", "general"),
		"contact_requests.status":     enumColumnType("open", "resolved", "dismissed"),
		"piece_secondary_kinds.kind":  pieceKinds,
		"piece_aliases.kind":          pieceAliasKinds,
		"piece_terms.part":            parts,
		"piece_relations.type":        pieceRelationTypes,
		"piece_credits.role":          creditRoles,
		"piece_credits.attribution":   attributions,
		"piece_credits.certainty":     certainties,
		"piece_credits.part":          parts,
		"conservation_history.status": conservationStatuses,
		"links.place_type":            placeTypes,
		"links.liveness":              liveness,
		"link_attachments.role":       linkRoles,
	}

	db := mysqltest.NewDatabase(t)
	rows, err := db.QueryContext(t.Context(), "SELECT CONCAT(c.table_name, '.', c.column_name), c.column_type "+
		"FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema = c.table_schema "+
		"AND t.table_name = c.table_name WHERE c.table_schema = DATABASE() AND t.table_type = 'BASE TABLE' "+
		"AND c.data_type = 'enum'")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	got := map[string]string{}
	for rows.Next() {
		var column, columnType string
		require.NoError(t, rows.Scan(&column, &columnType))
		got[column] = columnType
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, want, got)
}

// MySQL error numbers for violated constraints, besides errDuplicateEntry and errNoReferencedRow.
const (
	// errRowIsReferenced is ER_ROW_IS_REFERENCED_2: a RESTRICT foreign key still references the row.
	errRowIsReferenced = 1451
	// errCheckConstraintViolated is ER_CHECK_CONSTRAINT_VIOLATED.
	errCheckConstraintViolated = 3819
)

// catalogueRows holds the IDs of the rows seedCatalogue inserts.
type catalogueRows struct {
	term, affiliation, otherTerm         int64
	person, otherPerson                  int64
	piece, otherPiece                    int64
	collection, link, event, performance int64
}

// seedCatalogue inserts a minimal valid catalogue: every constraint test violates one rule against it. Inserting
// it at all checks that valid rows satisfy every CHECK.
func seedCatalogue(t *testing.T, db *sql.DB) catalogueRows {
	t.Helper()

	var r catalogueRows
	r.term = insertRow(t, db, "INSERT INTO terms (facet, name, display_name, slug) VALUES ('topic', 'War', 'War', 'war')")
	r.affiliation = insertRow(t, db, "INSERT INTO terms (facet, name, display_name, slug) "+
		"VALUES ('affiliation', 'Northmarch', 'Northmarch', 'northmarch')")
	r.otherTerm = insertRow(t, db, "INSERT INTO terms (facet, name, display_name, slug, parent_id) "+
		"VALUES ('topic', 'Swords', 'Swords', 'swords', ?)", r.term)
	r.person = insertRow(t, db, "INSERT INTO persons (slug, name, sort_name, sort_letter, kind) "+
		"VALUES ('wren-of-the-vale', 'Wren of the Vale', 'Wren of the Vale', 'W', 'community_bard')")
	r.otherPerson = insertRow(t, db, "INSERT INTO persons (slug, name, sort_name, sort_letter, kind) "+
		"VALUES ('the-lantern-singers', 'The Lantern Singers', 'Lantern Singers', 'L', 'group')")
	r.piece = insertRow(t, db, "INSERT INTO pieces (slug, title, sort_title, sort_letter, kind, licensing_status, "+
		"licence, period_start, period_end) VALUES ('the-copper-kettle', 'The Copper Kettle', 'Copper Kettle', 'C', "+
		"'song', 'known', 'cc_by', 1500, 1500)")
	r.otherPiece = insertRow(t, db, "INSERT INTO pieces (slug, title, sort_title, sort_letter, kind) "+
		"VALUES ('ballad-of-grey-hill', 'Ballad of Grey Hill', 'Ballad of Grey Hill', 'B', 'poem')")
	r.collection = insertRow(t, db, "INSERT INTO collections (name, sort_name, sort_letter, kind) "+
		"VALUES ('Fireside Songbook', 'Fireside Songbook', 'F', 'songbook_pdf')")
	r.link = insertRow(t, db, "INSERT INTO links (url, normalized_url, collection_id) "+
		"VALUES ('https://example.org/Kettle', 'https://example.org/Kettle', ?)", r.collection)
	r.event = insertRow(t, db, "INSERT INTO events (slug, name, kind) VALUES ('harvest-revel', 'Harvest Revel', 'event')")
	r.performance = insertRow(t, db, "INSERT INTO performances (link_id, piece_id, event_id, description_verbatim) "+
		"VALUES (?, ?, ?, 'Sung at the revel')", r.link, r.piece, r.event)

	insertRow(t, db, "INSERT INTO piece_terms (piece_id, term_id, position) VALUES (?, ?, 0)", r.piece, r.otherTerm)
	insertRow(t, db, "INSERT INTO person_affiliations (person_id, term_id) VALUES (?, ?)", r.person, r.affiliation)
	insertRow(t, db, "INSERT INTO piece_credits (piece_id, person_id, role, attribution, position) "+
		"VALUES (?, ?, 'lyrics', 'named', 0)", r.piece, r.person)
	insertRow(t, db, "INSERT INTO piece_credits (piece_id, role, attribution, position) "+
		"VALUES (?, 'tune', 'traditional', 1)", r.piece)
	insertRow(t, db, "INSERT INTO link_attachments (link_id, piece_id, role, position) VALUES (?, ?, 'lyrics', 0)",
		r.link, r.piece)
	insertRow(t, db, "INSERT INTO link_attachments (link_id, person_id, role, position) "+
		"VALUES (?, ?, 'performance', 0)", r.link, r.otherPerson)
	insertRow(t, db, "INSERT INTO performance_performers (performance_id, person_id, position) VALUES (?, ?, 0)",
		r.performance, r.otherPerson)
	insertRow(t, db, "INSERT INTO person_relations (from_person_id, to_person_id, type) VALUES (?, ?, 'member_of')",
		r.person, r.otherPerson)
	insertRow(t, db, "INSERT INTO piece_relations (from_piece_id, to_piece_id, type) VALUES (?, ?, 'variant_of')",
		r.otherPiece, r.piece)

	return r
}

// insertRow runs an INSERT and returns the inserted row's auto-increment ID (0 for tables without one).
func insertRow(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()

	result, err := db.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)

	return id
}

// Statements deleting an entity by ID, which a RESTRICT foreign key may refuse.
const (
	deleteTerm   = "DELETE FROM terms WHERE id = ?"
	deletePerson = "DELETE FROM persons WHERE id = ?"
)

func TestMigrations_Constraints(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	r := seedCatalogue(t, db)

	cases := []struct {
		name  string
		query string
		args  []any
		want  uint16
	}{
		{
			name: "named credit without a person",
			query: "INSERT INTO piece_credits (piece_id, role, attribution, position) " +
				"VALUES (?, 'music', 'named', 2)",
			args: []any{r.piece},
			want: errCheckConstraintViolated,
		},
		{
			name: "traditional credit with a person",
			query: "INSERT INTO piece_credits (piece_id, person_id, role, attribution, position) " +
				"VALUES (?, ?, 'music', 'traditional', 2)",
			args: []any{r.piece, r.person},
			want: errCheckConstraintViolated,
		},
		{
			name:  "attachment to nothing",
			query: "INSERT INTO link_attachments (link_id, role, position) VALUES (?, 'official', 1)",
			args:  []any{r.link},
			want:  errCheckConstraintViolated,
		},
		{
			name: "attachment to a piece and a person",
			query: "INSERT INTO link_attachments (link_id, piece_id, person_id, role, position) " +
				"VALUES (?, ?, ?, 'official', 1)",
			args: []any{r.link, r.piece, r.person},
			want: errCheckConstraintViolated,
		},
		{
			name:  "person related to themself",
			query: "INSERT INTO person_relations (from_person_id, to_person_id, type) VALUES (?, ?, 'same_as')",
			args:  []any{r.person, r.person},
			want:  errCheckConstraintViolated,
		},
		{
			name:  "piece related to itself",
			query: "INSERT INTO piece_relations (from_piece_id, to_piece_id, type) VALUES (?, ?, 'setting_of')",
			args:  []any{r.piece, r.piece},
			want:  errCheckConstraintViolated,
		},
		{
			name:  "period ending before it starts",
			query: "UPDATE pieces SET period_start = 1600, period_end = 1599 WHERE id = ?",
			args:  []any{r.piece},
			want:  errCheckConstraintViolated,
		},
		{
			name:  "licence with unknown licensing",
			query: "UPDATE pieces SET licence = 'pd' WHERE id = ?",
			args:  []any{r.otherPiece},
			want:  errCheckConstraintViolated,
		},
		{
			name:  "licensing made unknown under a licence",
			query: "UPDATE pieces SET licensing_status = 'unknown' WHERE id = ?",
			args:  []any{r.piece},
			want:  errCheckConstraintViolated,
		},
		{
			name:  "duplicate normalized URL",
			query: "INSERT INTO links (url, normalized_url) VALUES ('https://example.org/kettle', 'https://example.org/Kettle')",
			want:  errDuplicateEntry,
		},
		{
			name: "duplicate piece slug",
			query: "INSERT INTO pieces (slug, title, sort_title, sort_letter, kind) " +
				"VALUES ('the-copper-kettle', 'Copper Kettle', 'Copper Kettle', 'C', 'song')",
			want: errDuplicateEntry,
		},
		{
			name: "duplicate person slug",
			query: "INSERT INTO persons (slug, name, sort_name, sort_letter, kind) " +
				"VALUES ('wren-of-the-vale', 'Wren', 'Wren', 'W', 'historical')",
			want: errDuplicateEntry,
		},
		{
			name: "duplicate term slug",
			query: "INSERT INTO terms (facet, name, display_name, slug) " +
				"VALUES ('mood', 'War (Mood)', 'War', 'war')",
			want: errDuplicateEntry,
		},
		{
			name:  "duplicate event slug",
			query: "INSERT INTO events (slug, name, kind) VALUES ('harvest-revel', 'Harvest Revel Hall', 'venue')",
			want:  errDuplicateEntry,
		},
		{
			name:  "duplicate term name in a facet",
			query: "INSERT INTO terms (facet, name, display_name, slug) VALUES ('topic', 'War', 'War', 'war-2')",
			want:  errDuplicateEntry,
		},
		{
			name:  "term in use by a piece",
			query: deleteTerm,
			args:  []any{r.otherTerm},
			want:  errRowIsReferenced,
		},
		{
			name:  "term in use as an affiliation",
			query: deleteTerm,
			args:  []any{r.affiliation},
			want:  errRowIsReferenced,
		},
		{
			name:  "term with a child",
			query: deleteTerm,
			args:  []any{r.term},
			want:  errRowIsReferenced,
		},
		{
			name:  "credited person",
			query: deletePerson,
			args:  []any{r.person},
			want:  errRowIsReferenced,
		},
		{
			name:  "performer",
			query: deletePerson,
			args:  []any{r.otherPerson},
			want:  errRowIsReferenced,
		},
		{
			name:  "collection with links",
			query: "DELETE FROM collections WHERE id = ?",
			args:  []any{r.collection},
			want:  errRowIsReferenced,
		},
		{
			name:  "piece tagged with a missing term",
			query: "INSERT INTO piece_terms (piece_id, term_id, position) VALUES (?, ?, 1)",
			args:  []any{r.piece, r.otherTerm + 1000},
			want:  errNoReferencedRow,
		},
		{
			name: "credit to a missing person",
			query: "INSERT INTO piece_credits (piece_id, person_id, role, attribution, position) " +
				"VALUES (?, ?, 'music', 'named', 2)",
			args: []any{r.piece, r.otherPerson + 1000},
			want: errNoReferencedRow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(t.Context(), tc.query, tc.args...)
			requireMySQLError(t, err, tc.want)
		})
	}
}

// TestMigrations_ConstraintsAllowCascades requires deleting an entity to cascade to the rows that depend on it,
// past the CHECKs on the cascading columns, and a deleted event to leave its performances without an event.
func TestMigrations_ConstraintsAllowCascades(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	r := seedCatalogue(t, db)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, "DELETE FROM events WHERE id = ?", r.event)
	require.NoError(t, err)
	var eventID sql.NullInt64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT event_id FROM performances WHERE id = ?", r.performance).
		Scan(&eventID))
	assert.False(t, eventID.Valid)

	_, err = db.ExecContext(ctx, "DELETE FROM pieces WHERE id = ?", r.piece)
	require.NoError(t, err)
	for _, table := range []string{"piece_terms", "piece_credits", "piece_relations", "performances"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count))
		assert.Zero(t, count, "table [%s]", table)
	}

	_, err = db.ExecContext(ctx, "DELETE FROM links WHERE id = ?", r.link)
	require.NoError(t, err)
	var attachments int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM link_attachments").Scan(&attachments))
	assert.Zero(t, attachments)
}
