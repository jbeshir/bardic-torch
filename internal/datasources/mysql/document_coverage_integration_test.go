//go:build integration

package mysql_test

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The column classifications of TestDocumentCoverage, one column per line:
//
//	table.column = field    the column stores the document field (JSON names joined by "."; a list's elements
//	                        share the list's name)
//	table.column ! reason   the column is system-owned: no document holds it, for the reason given
//
// A document owns every table it classifies columns of.

// linkColumns classifies the link tables that the piece and person documents share; pieceLinkColumns and
// personLinkColumns classify the columns that differ.
const linkColumns = `
links.id                               ! the row's own ID
links.url                              = links.url
links.normalized_url                   ! derived from url by the writer
links.title                            = links.title
links.description                      = links.description
links.platform                         = links.platform
links.place_type                       = links.place_type
links.collection_id                    = links.collection_id
links.locator                          = links.locator
links.liveness                         ! link checker state
links.last_checked_at                  ! link checker state
links.archived_url                     ! link checker state
links.is_hidden                        ! moderator takedown state
link_attachments.id                    ! the row's own ID
link_attachments.link_id               ! the link of the links[] element
link_attachments.role                  = links.roles
link_attachments.position              ! the element's index in its document list
performances.id                        ! the row's own ID
performances.link_id                   ! the link of the links[] element
performances.event_id                  = links.performances.event_id
performances.year                      = links.performances.year
performances.uploader                  = links.performances.uploader
performances.description_verbatim      = links.performances.description_verbatim
performance_performers.performance_id  ! the performance of the links[].performances[] element
performance_performers.person_id       = links.performances.performer_ids
performance_performers.position        ! the element's index in its document list
`

const pieceLinkColumns = linkColumns + `
link_attachments.piece_id              ! the document's entity
link_attachments.person_id             ! NULL: an attachment belongs to one piece or one person
performances.piece_id                  ! the document's entity
`

const personLinkColumns = linkColumns + `
link_attachments.person_id             ! the document's entity
link_attachments.piece_id              ! NULL: an attachment belongs to one piece or one person
performances.piece_id                  = links.performances.piece_id
`

const pieceColumns = pieceLinkColumns + `
pieces.id                              ! the row's own ID
pieces.slug                            ! the entity's address, assigned once by the writer
pieces.title                           = title
pieces.sort_title                      ! derived from the title by the writer
pieces.sort_letter                     ! derived from the title by the writer
pieces.kind                            = kind
pieces.conservation_status             = conservation.status
pieces.is_stub                         = is_stub
pieces.notes_md                        = notes_md
pieces.version_notes_md                = version_notes_md
pieces.licensing_notes_md              = licensing_notes_md
pieces.licensing_status                = licensing_status
pieces.licence                         = licence
pieces.historical_source_md            = historical_source_md
pieces.period_start                    = period_start
pieces.period_end                      = period_end
pieces.legacy_had_inline_lyrics        ! imported from the legacy wiki; never edited or exposed
pieces.current_revision_id             ! the entity's approved revision, set by approval
pieces.legacy_wiki_pageid              ! imported from the legacy wiki; never edited or exposed
pieces.legacy_title                    ! imported from the legacy wiki; never edited or exposed
piece_secondary_kinds.piece_id         ! the document's entity
piece_secondary_kinds.kind             = secondary_kinds
piece_aliases.id                       ! the row's own ID
piece_aliases.piece_id                 ! the document's entity
piece_aliases.text                     = aliases.text
piece_aliases.kind                     = aliases.kind
piece_terms.piece_id                   ! the document's entity
piece_terms.term_id                    = terms.term_id
piece_terms.is_primary                 = terms.is_primary
piece_terms.part                       = terms.part
piece_terms.position                   ! the element's index in its document list
piece_relations.from_piece_id          ! the document's entity
piece_relations.to_piece_id            = relations.to_piece_id
piece_relations.type                   = relations.type
piece_credits.id                       ! the row's own ID
piece_credits.piece_id                 ! the document's entity
piece_credits.person_id                = credits.person_id
piece_credits.role                     = credits.role
piece_credits.attribution              = credits.attribution
piece_credits.certainty                = credits.certainty
piece_credits.part                     = credits.part
piece_credits.note                     = credits.note
piece_credits.position                 ! the element's index in its document list
piece_preferences.piece_id             ! the document's entity
piece_preferences.dimension            = preferences.dimension
piece_preferences.value_json           = preferences.value
piece_preferences.source_url           = preferences.source_url
`

const personColumns = personLinkColumns + `
persons.id                             ! the row's own ID
persons.slug                           ! the entity's address, assigned once by the writer
persons.name                           = name
persons.sort_name                      ! derived from the name by the writer
persons.sort_letter                    ! derived from the name by the writer
persons.kind                           = kind
persons.status                         = status
persons.status_date                    = status_date
persons.status_source_url              = status_source_url
persons.pronouns                       = pronouns
persons.notes_md                       = notes_md
persons.licensing_notes_md             = licensing_notes_md
persons.licensing_status               = licensing_status
persons.is_stub                        = is_stub
persons.claimed_by_user_id             ! set when a user claims the page, not by editing
persons.current_revision_id            ! the entity's approved revision, set by approval
persons.legacy_wiki_pageid             ! imported from the legacy wiki; never edited or exposed
persons.legacy_title                   ! imported from the legacy wiki; never edited or exposed
person_aliases.id                      ! the row's own ID
person_aliases.person_id               ! the document's entity
person_aliases.name                    = aliases.name
person_aliases.kind                    = aliases.kind
person_aliases.is_public               = aliases.is_public
person_roles.person_id                 ! the document's entity
person_roles.role                      = roles
person_affiliations.person_id          ! the document's entity
person_affiliations.term_id            = affiliation_ids
person_relations.from_person_id        ! the document's entity
person_relations.to_person_id          = relations.to_person_id
person_relations.type                  = relations.type
person_preferences.person_id           ! the document's entity
person_preferences.dimension           = preferences.dimension
person_preferences.value_json          = preferences.value
person_preferences.source_url          = preferences.source_url
`

const termColumns = `
terms.id                               ! the row's own ID
terms.facet                            = facet
terms.name                             = name
terms.display_name                     = display_name
terms.slug                             ! the entity's address, assigned once by the writer
terms.parent_id                        = parent_id
terms.related_affiliation_id           = related_affiliation_id
terms.description_md                   = description_md
terms.position                         = position
terms.current_revision_id              ! the entity's approved revision, set by approval
terms.legacy_wiki_pageid               ! imported from the legacy wiki; never edited or exposed
terms.legacy_title                     ! imported from the legacy wiki; never edited or exposed
term_aliases.id                        ! the row's own ID
term_aliases.term_id                   ! the document's entity
term_aliases.name                      = aliases
`

const collectionColumns = `
collections.id                         ! the row's own ID
collections.name                       = name
collections.sort_name                  ! derived from the name by the writer
collections.sort_letter                ! derived from the name by the writer
collections.url                        = url
collections.kind                       = kind
collections.description_md             = description_md
collections.region_term_id             = region_term_id
collections.parent_id                  = parent_id
collections.is_bardic_resource         = is_bardic_resource
collections.enumerable                 = enumerable
collections.current_revision_id        ! the entity's approved revision, set by approval
`

const eventColumns = `
events.id                              ! the row's own ID
events.slug                            ! the entity's address, assigned once by the writer
events.name                            = name
events.kind                            = kind
events.description_md                  = description_md
events.current_revision_id             ! the entity's approved revision, set by approval
events.legacy_wiki_pageid              ! imported from the legacy wiki; never edited or exposed
events.legacy_title                    ! imported from the legacy wiki; never edited or exposed
`

// nonDocumentTables lists the tables no document owns, one per line with the reason.
const nonDocumentTables = `
users                  ! accounts (BE1)
api_tokens             ! accounts (BE1)
revisions              ! the revision workflow that stores the documents
revision_contributors  ! the revision workflow that stores the documents
term_ancestors         ! derived from terms.parent_id when a term revision is approved
conservation_history   ! appended when a revision changing a piece's conservation status is approved
`

// schemaVersionField is the field every document has and no column stores: it describes the stored snapshot.
const schemaVersionField = "schema_version"

type documentCoverage struct {
	doc     any
	columns string
	// elsewhere maps a document field, other than schema_version, that no owned column stores to where it goes.
	elsewhere map[string]string
}

var documentCoverages = []documentCoverage{
	{doc: domain.PieceDocument{}, columns: pieceColumns, elsewhere: map[string]string{
		"conservation.source_url": "conservation_history.source_url, appended when a status change is approved",
	}},
	{doc: domain.PersonDocument{}, columns: personColumns},
	{doc: domain.TermDocument{}, columns: termColumns},
	{doc: domain.CollectionDocument{}, columns: collectionColumns},
	{doc: domain.EventDocument{}, columns: eventColumns},
}

// columnClass is a parsed classification line: the field a column stores, or why it is system-owned.
type columnClass struct {
	field  string
	reason string
}

// parseClasses parses classification lines into classes by table and column, failing on a malformed or
// repeated line.
func parseClasses(t *testing.T, spec string) map[string]map[string]columnClass {
	t.Helper()

	classes := map[string]map[string]columnClass{}
	for line := range strings.Lines(spec) {
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		require.GreaterOrEqual(t, len(words), 3, "malformed line %q", line)
		table, column, ok := strings.Cut(words[0], ".")
		require.True(t, ok, "malformed line %q", line)
		rest := strings.Join(words[2:], " ")
		var class columnClass
		switch words[1] {
		case "=":
			class.field = rest
		case "!":
			class.reason = rest
		default:
			require.Failf(t, "malformed line", "%q", line)
		}
		if classes[table] == nil {
			classes[table] = map[string]columnClass{}
		}
		_, repeated := classes[table][column]
		require.False(t, repeated, "%s.%s is classified twice", table, column)
		classes[table][column] = class
	}

	return classes
}

// documentFieldPaths returns the leaf field paths of a document type: JSON names joined by ".", descending into
// structs and lists of structs, and flattening embedded structs as encoding/json does. A struct with its own JSON
// encoding, such as domain.Date, is a leaf.
func documentFieldPaths(typ reflect.Type, prefix string) []string {
	var paths []string
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Anonymous && name == "" {
			paths = append(paths, documentFieldPaths(field.Type, prefix)...)

			continue
		}
		path := prefix + name
		elem := field.Type
		for elem.Kind() == reflect.Pointer || (elem.Kind() == reflect.Slice && elem != reflect.TypeFor[json.RawMessage]()) {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct && !reflect.PointerTo(elem).Implements(reflect.TypeFor[json.Unmarshaler]()) {
			paths = append(paths, documentFieldPaths(elem, path+".")...)
		} else {
			paths = append(paths, path)
		}
	}

	return paths
}

// tableColumns returns the columns of every base table in the current database, by table.
func tableColumns(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT c.table_name, c.column_name FROM information_schema.columns c "+
		"JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name "+
		"WHERE c.table_schema = DATABASE() AND t.table_type = 'BASE TABLE' AND c.table_name <> 'schema_migrations'")
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	columns := map[string][]string{}
	for rows.Next() {
		var table, column string
		require.NoError(t, rows.Scan(&table, &column))
		columns[table] = append(columns[table], column)
	}
	require.NoError(t, rows.Err())

	return columns
}

func TestDocumentFieldPaths(t *testing.T) {
	t.Parallel()

	paths := documentFieldPaths(reflect.TypeFor[domain.PersonDocument](), "")
	assert.Contains(t, paths, "links.performances.performer_ids", "descends into lists of structs")
	assert.Contains(t, paths, "links.performances.piece_id", "flattens embedded structs")
	assert.Contains(t, paths, "links.url", "flattens embedded structs")
	assert.Contains(t, paths, "preferences.value", "keeps raw JSON as one field")
	assert.Contains(t, paths, "roles", "keeps lists of scalars as one field")
	assert.NotContains(t, paths, "links.performances", "lists of structs are not leaves")
}

// checkColumns checks that every column of the tables coverage classifies is classified, and that every
// classified column exists. It returns how many columns store each document field, and the owned tables.
func checkColumns(t *testing.T, coverage documentCoverage, paths []string,
	columns map[string][]string) (map[string]int, []string) {
	t.Helper()

	stored := map[string]int{}
	var owned []string
	for table, classes := range parseClasses(t, coverage.columns) {
		owned = append(owned, table)
		tableCols, ok := columns[table]
		if !assert.True(t, ok, "table %s does not exist", table) {
			continue
		}
		for _, column := range tableCols {
			class, ok := classes[column]
			if !assert.True(t, ok, "%s.%s is neither a document field nor system-owned", table, column) {
				continue
			}
			if class.field != "" {
				assert.Contains(t, paths, class.field, "%s.%s maps to a field the document lacks", table, column)
				stored[class.field]++
			}
		}
		for column := range classes {
			assert.Contains(t, tableCols, column, "classified column %s.%s does not exist", table, column)
		}
	}

	return stored, owned
}

// checkFieldsStored checks that every document field is stored by exactly one column, or listed as stored
// elsewhere and stored by none.
func checkFieldsStored(t *testing.T, coverage documentCoverage, paths []string, stored map[string]int) {
	t.Helper()

	for _, path := range paths {
		_, isElsewhere := coverage.elsewhere[path]
		if path == schemaVersionField || isElsewhere {
			assert.Zero(t, stored[path], "document field %s is listed as stored elsewhere", path)
		} else {
			assert.Equal(t, 1, stored[path], "document field %s must be stored by exactly one column", path)
		}
	}
	for path := range coverage.elsewhere {
		assert.Contains(t, paths, path, "elsewhere lists %s, which the document lacks", path)
	}
}

// TestDocumentCoverage requires every column of every table a snapshot document owns to be either a
// document field or classified as system-owned with a reason, and every document field to be stored by exactly
// one column of its tables or listed as stored elsewhere, so that a column or field added later fails here
// until it is classified. Every other table must be listed as owned by no document.
func TestDocumentCoverage(t *testing.T) {
	t.Parallel()

	columns := tableColumns(t, mysqltest.NewDatabase(t))

	owned := map[string]bool{}
	for _, coverage := range documentCoverages {
		typ := reflect.TypeOf(coverage.doc)
		t.Run(typ.Name(), func(t *testing.T) {
			paths := documentFieldPaths(typ, "")
			stored, tables := checkColumns(t, coverage, paths, columns)
			checkFieldsStored(t, coverage, paths, stored)
			for _, table := range tables {
				owned[table] = true
			}
		})
	}

	notOwned := map[string]bool{}
	for line := range strings.Lines(nonDocumentTables) {
		if words := strings.Fields(line); len(words) > 0 {
			notOwned[words[0]] = true
		}
	}
	for table := range columns {
		assert.NotEqual(t, owned[table], notOwned[table],
			"table %s must be owned by a document or listed in nonDocumentTables, not both or neither", table)
	}
	for table := range notOwned {
		assert.Contains(t, columns, table, "nonDocumentTables lists %s, which does not exist", table)
	}
}
