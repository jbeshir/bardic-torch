//go:build integration

// Package catalogtest seeds catalogue fixtures for integration tests with plain SQL, as BE3's importer and BE4's
// revision engine will write them: an entity's editable content is its snapshot document (internal/domain), and
// an approved entity has an approved revision holding that document as its current revision.
//
// A test with its own data seeds a fresh database:
//
//	db := mysqltest.NewDatabase(t)
//	c := catalogtest.New(t, db)
//	war := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "War"}})
//	swords := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Swords",
//		ParentID: &war.ID}})
//	wren := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Wren of the Vale"}})
//	kettle := c.Piece(catalogtest.Piece{
//		Doc: domain.PieceDocument{
//			Title: "The Copper Kettle",
//			Terms: []domain.DocPieceTerm{{TermID: swords.ID, IsPrimary: true}},
//			Credits: []domain.DocCredit{{PersonID: &wren.ID, Role: domain.CreditRoleLyrics}},
//		},
//		SortTitle: "Copper Kettle",
//	})
//	draft := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Unsung"}, Unapproved: true})
//
// Every method writes in one transaction and commits before it returns, because FULLTEXT indexes see committed
// rows only. Methods fail the test on any error, so call them from the test's own goroutine. References between
// entities are by ID, so seed an entity before anything that refers to it.
//
// Tests that only read can share one seeded database per test binary instead; see Shared.
package catalogtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Ref identifies a seeded entity.
type Ref struct {
	ID int64
	// Slug is the entity's address; collections, which are addressed by ID, have none.
	Slug string
	// RevisionID is the approved revision that is the entity's current revision, or 0 if it is unapproved.
	RevisionID int64
}

// Catalog seeds catalogue rows into one database.
type Catalog struct {
	t         testing.TB
	db        *sql.DB
	revisions int
}

// New returns a Catalog that seeds db, a database migrated by mysqltest, failing t on any error.
func New(t testing.TB, db *sql.DB) *Catalog {
	return &Catalog{t: t, db: db}
}

// revisionBaseTime is the created_at of the first revision a Catalog writes without one; each later revision
// is one second newer, so creation order is history order.
var revisionBaseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Term is a term to seed. Doc.Facet defaults to topic, Doc.DisplayName to Doc.Name and Slug to
// domain.Slugify(Doc.Name). The term_ancestors closure rows of the term, including its own depth-0 row, are
// written with it.
type Term struct {
	Doc        domain.TermDocument
	Slug       string
	Unapproved bool
}

// Doc.LicensingStatus to unknown; Slug defaults to domain.Slugify(Doc.Name), SortName to Doc.Name (persons sort
// by their name as written) and SortLetter to domain.SortLetter(SortName), as the importer writes them.
// the upper-cased first letter of SortName, or 0 if it does not start with an ASCII letter.
type Person struct {
	Doc        domain.PersonDocument
	Slug       string
	SortName   string
	SortLetter string
	Unapproved bool
}

// Piece is a piece to seed. Doc.Kind defaults to song, Doc.Conservation.Status to unassessed and
// Doc.LicensingStatus to unknown; Slug defaults to domain.Slugify(Doc.Title), SortTitle to
// domain.SortTitle(Doc.Title) and SortLetter to domain.SortLetter(SortTitle). Credits default to certainty
// certain and attribution named.
type Piece struct {
	Doc                 domain.PieceDocument
	Slug                string
	SortTitle           string
	SortLetter          string
	ConservationHistory []ConservationEntry
	Unapproved          bool
}

// ConservationEntry is a conservation_history row. At defaults to the time of the first default revision.
type ConservationEntry struct {
	Status    domain.ConservationStatus
	Reason    string
	SourceURL *string
	At        time.Time
}

// Collection is a collection to seed. Doc.Kind defaults to other and Doc.Enumerable to unknown, SortName to
// domain.SortTitle(Doc.Name) and SortLetter to domain.SortLetter(SortName). Doc.IsBardicResource is written as
// given.
type Collection struct {
	Doc        domain.CollectionDocument
	SortName   string
	SortLetter string
	Unapproved bool
}

// Event is an event or venue to seed. Doc.Kind defaults to event and Slug to domain.Slugify(Doc.Name).
type Event struct {
	Doc        domain.EventDocument
	Slug       string
	Unapproved bool
}

// Revision is a revisions row to seed, for history and revision tests. Status defaults to approved, Source to
// import and CreatedAt to one second after the previous default; BaseRevisionID and AuthorUserID are NULL when
// zero, and an AuthorUserID must be an existing user. Snapshot is stored as its JSON encoding: a domain document,
// or a json.RawMessage for a malformed one. Seeding a revision does not change the entity's current revision.
type Revision struct {
	EntityType     domain.EntityType
	EntityID       int64
	Snapshot       any
	Status         domain.RevisionStatus
	Source         domain.RevisionSource
	BaseRevisionID int64
	AuthorUserID   string
	Summary        string
	CreatedAt      time.Time
	Contributors   []string
}

// LinkState is the system and moderator state of a link, which no document holds. Liveness defaults to unknown.
type LinkState struct {
	Liveness      domain.Liveness
	LastCheckedAt *time.Time
	ArchivedURL   *string
	IsHidden      bool
}

// Term seeds a term with its aliases and closure rows.
func (c *Catalog) Term(f Term) Ref {
	doc := f.Doc
	doc.SchemaVersion = domain.DocumentSchemaVersion
	doc.Facet = orDefault(doc.Facet, domain.FacetTopic)
	doc.DisplayName = orDefault(doc.DisplayName, doc.Name)
	ref := Ref{Slug: orDefault(f.Slug, domain.Slugify(doc.Name))}

	c.write(func(w writer) {
		ref.ID = w.exec("INSERT INTO terms (facet, name, display_name, slug, parent_id, related_affiliation_id, "+
			"description_md, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", doc.Facet, doc.Name, doc.DisplayName,
			ref.Slug, doc.ParentID, doc.RelatedAffiliationID, doc.DescriptionMD, doc.Position)
		for _, alias := range doc.Aliases {
			w.exec("INSERT INTO term_aliases (term_id, name) VALUES (?, ?)", ref.ID, alias)
		}
		// The closure: the term itself at depth 0 and each ancestor, found through parent_id, one level deeper.
		w.exec("INSERT INTO term_ancestors (ancestor_id, descendant_id, depth) "+
			"WITH RECURSIVE up (id, depth) AS ("+
			"SELECT id, 0 FROM terms WHERE id = ? "+
			"UNION ALL SELECT t.parent_id, up.depth + 1 FROM up JOIN terms t ON t.id = up.id "+
			"WHERE t.parent_id IS NOT NULL) "+
			"SELECT id, ?, depth FROM up", ref.ID, ref.ID)
		ref.RevisionID = w.approve(domain.EntityTypeTerm, ref.ID, doc, f.Unapproved)
	})

	return ref
}

// Person seeds a person with their aliases, roles, affiliations, outgoing relations, preferences and links.
func (c *Catalog) Person(f Person) Ref {
	doc := f.Doc
	doc.SchemaVersion = domain.DocumentSchemaVersion
	doc.Kind = orDefault(doc.Kind, domain.PersonKindCommunityBard)
	doc.Status = orDefault(doc.Status, domain.PersonStatusActive)
	doc.LicensingStatus = orDefault(doc.LicensingStatus, domain.LicensingStatusUnknown)
	ref := Ref{Slug: orDefault(f.Slug, domain.Slugify(doc.Name))}
	sortName := orDefault(f.SortName, doc.Name)
	sortLetter := orDefault(f.SortLetter, string(domain.SortLetter(sortName)))

	c.write(func(w writer) {
		ref.ID = w.exec("INSERT INTO persons (slug, name, sort_name, sort_letter, kind, status, status_date, "+
			"status_source_url, pronouns, notes_md, licensing_notes_md, licensing_status, is_stub) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", ref.Slug, doc.Name, sortName, sortLetter, doc.Kind,
			doc.Status, dateValue(doc.StatusDate), doc.StatusSourceURL, doc.Pronouns, doc.NotesMD, doc.LicensingNotesMD,
			doc.LicensingStatus, doc.IsStub)
		for _, alias := range doc.Aliases {
			w.exec("INSERT INTO person_aliases (person_id, name, kind, is_public) VALUES (?, ?, ?, ?)",
				ref.ID, alias.Name, alias.Kind, alias.IsPublic)
		}
		for _, role := range doc.Roles {
			w.exec("INSERT INTO person_roles (person_id, role) VALUES (?, ?)", ref.ID, role)
		}
		for _, termID := range doc.AffiliationIDs {
			w.exec("INSERT INTO person_affiliations (person_id, term_id) VALUES (?, ?)", ref.ID, termID)
		}
		for _, rel := range doc.Relations {
			w.exec("INSERT INTO person_relations (from_person_id, to_person_id, type) VALUES (?, ?, ?)",
				ref.ID, rel.ToPersonID, rel.Type)
		}
		for _, pref := range doc.Preferences {
			w.exec("INSERT INTO person_preferences (person_id, dimension, value_json, source_url) "+
				"VALUES (?, ?, ?, ?)", ref.ID, pref.Dimension, string(pref.Value), pref.SourceURL)
		}
		for position, link := range doc.Links {
			linkID := w.link(link.DocLink)
			for _, role := range link.Roles {
				w.exec("INSERT INTO link_attachments (link_id, person_id, role, position) VALUES (?, ?, ?, ?)",
					linkID, ref.ID, role, position)
			}
			for _, perf := range link.Performances {
				w.performance(linkID, perf.PieceID, perf.DocPerformance)
			}
		}
		ref.RevisionID = w.approve(domain.EntityTypePerson, ref.ID, doc, f.Unapproved)
	})

	return ref
}

// Piece seeds a piece with all its child rows, its conservation history and its links.
func (c *Catalog) Piece(f Piece) Ref {
	doc := f.Doc
	doc.SchemaVersion = domain.DocumentSchemaVersion
	doc.Kind = orDefault(doc.Kind, domain.PieceKindSong)
	doc.Conservation.Status = orDefault(doc.Conservation.Status, domain.ConservationStatusUnassessed)
	doc.LicensingStatus = orDefault(doc.LicensingStatus, domain.LicensingStatusUnknown)
	doc.Credits = append([]domain.DocCredit(nil), doc.Credits...)
	for i := range doc.Credits {
		doc.Credits[i].Attribution = orDefault(doc.Credits[i].Attribution, domain.AttributionNamed)
		doc.Credits[i].Certainty = orDefault(doc.Credits[i].Certainty, domain.CertaintyCertain)
	}
	ref := Ref{Slug: orDefault(f.Slug, domain.Slugify(doc.Title))}
	sortTitle := orDefault(f.SortTitle, domain.SortTitle(doc.Title))
	sortLetter := orDefault(f.SortLetter, string(domain.SortLetter(sortTitle)))

	c.write(func(w writer) {
		ref.ID = w.exec("INSERT INTO pieces (slug, title, sort_title, sort_letter, kind, conservation_status, "+
			"is_stub, notes_md, version_notes_md, licensing_notes_md, licensing_status, licence, "+
			"historical_source_md, period_start, period_end) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			ref.Slug, doc.Title, sortTitle, sortLetter, doc.Kind, doc.Conservation.Status, doc.IsStub, doc.NotesMD,
			doc.VersionNotesMD, doc.LicensingNotesMD, doc.LicensingStatus, doc.Licence, doc.HistoricalSourceMD,
			doc.PeriodStart, doc.PeriodEnd)
		for _, kind := range doc.SecondaryKinds {
			w.exec("INSERT INTO piece_secondary_kinds (piece_id, kind) VALUES (?, ?)", ref.ID, kind)
		}
		for _, alias := range doc.Aliases {
			w.exec("INSERT INTO piece_aliases (piece_id, text, kind) VALUES (?, ?, ?)", ref.ID, alias.Text, alias.Kind)
		}
		for position, term := range doc.Terms {
			w.exec("INSERT INTO piece_terms (piece_id, term_id, is_primary, part, position) VALUES (?, ?, ?, ?, ?)",
				ref.ID, term.TermID, term.IsPrimary, term.Part, position)
		}
		for _, rel := range doc.Relations {
			w.exec("INSERT INTO piece_relations (from_piece_id, to_piece_id, type) VALUES (?, ?, ?)",
				ref.ID, rel.ToPieceID, rel.Type)
		}
		for position, credit := range doc.Credits {
			w.exec("INSERT INTO piece_credits (piece_id, person_id, role, attribution, certainty, part, note, "+
				"position) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", ref.ID, credit.PersonID, credit.Role, credit.Attribution,
				credit.Certainty, credit.Part, credit.Note, position)
		}
		for _, pref := range doc.Preferences {
			w.exec("INSERT INTO piece_preferences (piece_id, dimension, value_json, source_url) VALUES (?, ?, ?, ?)",
				ref.ID, pref.Dimension, string(pref.Value), pref.SourceURL)
		}
		for _, entry := range f.ConservationHistory {
			w.exec("INSERT INTO conservation_history (piece_id, status, reason, source_url, at) "+
				"VALUES (?, ?, ?, ?, ?)", ref.ID, entry.Status, entry.Reason, entry.SourceURL,
				orDefault(entry.At, revisionBaseTime))
		}
		for position, link := range doc.Links {
			linkID := w.link(link.DocLink)
			for _, role := range link.Roles {
				w.exec("INSERT INTO link_attachments (link_id, piece_id, role, position) VALUES (?, ?, ?, ?)",
					linkID, ref.ID, role, position)
			}
			for _, perf := range link.Performances {
				w.performance(linkID, &ref.ID, perf)
			}
		}
		ref.RevisionID = w.approve(domain.EntityTypePiece, ref.ID, doc, f.Unapproved)
	})

	return ref
}

// Collection seeds a collection.
func (c *Catalog) Collection(f Collection) Ref {
	doc := f.Doc
	doc.SchemaVersion = domain.DocumentSchemaVersion
	doc.Kind = orDefault(doc.Kind, domain.CollectionKindOther)
	doc.Enumerable = orDefault(doc.Enumerable, domain.CollectionEnumerableUnknown)
	sortName := orDefault(f.SortName, domain.SortTitle(doc.Name))
	sortLetter := orDefault(f.SortLetter, string(domain.SortLetter(sortName)))

	var ref Ref
	c.write(func(w writer) {
		ref.ID = w.exec("INSERT INTO collections (name, sort_name, sort_letter, url, kind, description_md, "+
			"region_term_id, parent_id, is_bardic_resource, enumerable) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			doc.Name, sortName, sortLetter, doc.URL, doc.Kind, doc.DescriptionMD, doc.RegionTermID, doc.ParentID,
			doc.IsBardicResource, doc.Enumerable)
		ref.RevisionID = w.approve(domain.EntityTypeCollection, ref.ID, doc, f.Unapproved)
	})

	return ref
}

// Event seeds an event or venue.
func (c *Catalog) Event(f Event) Ref {
	doc := f.Doc
	doc.SchemaVersion = domain.DocumentSchemaVersion
	doc.Kind = orDefault(doc.Kind, domain.EventKindEvent)
	ref := Ref{Slug: orDefault(f.Slug, domain.Slugify(doc.Name))}

	c.write(func(w writer) {
		ref.ID = w.exec("INSERT INTO events (slug, name, kind, description_md) VALUES (?, ?, ?, ?)",
			ref.Slug, doc.Name, doc.Kind, doc.DescriptionMD)
		ref.RevisionID = w.approve(domain.EntityTypeEvent, ref.ID, doc, f.Unapproved)
	})

	return ref
}

// Revision seeds a revision with its contributors, in order, and returns its ID.
func (c *Catalog) Revision(r Revision) int64 {
	var id int64
	c.write(func(w writer) { id = w.revision(r) })

	return id
}

// SetLinkState sets the system state of the link whose normalized URL is url's, which a piece or person seeded
// before.
func (c *Catalog) SetLinkState(url string, s LinkState) {
	c.t.Helper()
	normalized := c.normalizeURL(url)
	c.write(func(w writer) {
		result, err := w.tx.ExecContext(w.ctx(), "UPDATE links SET liveness = ?, last_checked_at = ?, "+
			"archived_url = ?, is_hidden = ? WHERE normalized_url = ?",
			orDefault(s.Liveness, domain.LivenessUnknown), s.LastCheckedAt, s.ArchivedURL, s.IsHidden, normalized)
		if err != nil {
			c.t.Fatalf("catalogtest: setting link state of %s: %v", url, err)
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			c.t.Fatalf("catalogtest: setting link state of %s: no such link (%d rows, %v)", url, n, err)
		}
	})
}

// normalizeURL returns domain.NormalizeURL(url), failing the test if url does not normalize.
func (c *Catalog) normalizeURL(url string) string {
	c.t.Helper()
	normalized, err := domain.NormalizeURL(url)
	if err != nil {
		c.t.Fatalf("catalogtest: link URL %s: %v", url, err)
	}

	return normalized
}

// Exec runs one statement and commits it, for the system-owned columns no document holds, such as legacy_title
// or claimed_by_user_id, and for rows no method seeds, such as users.
func (c *Catalog) Exec(query string, args ...any) {
	c.t.Helper()
	c.write(func(w writer) { w.exec(query, args...) })
}

// write runs f in a transaction and commits it.
func (c *Catalog) write(f func(w writer)) {
	c.t.Helper()

	tx, err := c.db.BeginTx(c.t.Context(), nil)
	if err != nil {
		c.t.Fatalf("catalogtest: beginning transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	f(writer{c: c, tx: tx})
	if err := tx.Commit(); err != nil {
		c.t.Fatalf("catalogtest: committing: %v", err)
	}
}

// writer writes rows in one transaction.
type writer struct {
	c  *Catalog
	tx *sql.Tx
}

func (w writer) ctx() context.Context { return w.c.t.Context() }

// exec runs a statement and returns the auto-increment ID of the row it inserted, if any.
func (w writer) exec(query string, args ...any) int64 {
	w.c.t.Helper()

	result, err := w.tx.ExecContext(w.ctx(), query, args...)
	if err != nil {
		w.c.t.Fatalf("catalogtest: %s: %v", query, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		w.c.t.Fatalf("catalogtest: %s: %v", query, err)
	}

	return id
}

// link inserts the link with url, or finds the one with the same normalized URL seeded before, whose URL as
// entered and editor-owned fields win, and returns its ID. Its normalized URL is domain.NormalizeURL(url), as the
// importer writes it (the invariant in docs/DEPLOY.md); a URL that does not normalize fails the test.
func (w writer) link(l domain.DocLink) int64 {
	return w.exec("INSERT INTO links (url, normalized_url, title, description, platform, place_type, "+
		"collection_id, locator) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)",
		l.URL, w.c.normalizeURL(l.URL), l.Title, l.Description, l.Platform, l.PlaceType, l.CollectionID, l.Locator)
}

// performance inserts a performance recorded by the link linkID, of the piece pieceID if not nil, with its
// performers in order.
func (w writer) performance(linkID int64, pieceID *int64, p domain.DocPerformance) {
	id := w.exec("INSERT INTO performances (link_id, piece_id, event_id, year, uploader, description_verbatim) "+
		"VALUES (?, ?, ?, ?, ?, ?)", linkID, pieceID, p.EventID, p.Year, p.Uploader, p.DescriptionVerbatim)
	for position, personID := range p.PerformerIDs {
		w.exec("INSERT INTO performance_performers (performance_id, person_id, position) VALUES (?, ?, ?)",
			id, personID, position)
	}
}

// setCurrentRevision has, per entity type, the statement making a revision its entity's current revision.
var setCurrentRevision = map[domain.EntityType]string{
	domain.EntityTypePiece:      "UPDATE pieces SET current_revision_id = ? WHERE id = ?",
	domain.EntityTypePerson:     "UPDATE persons SET current_revision_id = ? WHERE id = ?",
	domain.EntityTypeTerm:       "UPDATE terms SET current_revision_id = ? WHERE id = ?",
	domain.EntityTypeCollection: "UPDATE collections SET current_revision_id = ? WHERE id = ?",
	domain.EntityTypeEvent:      "UPDATE events SET current_revision_id = ? WHERE id = ?",
}

// approve, unless unapproved, seeds an approved import revision of the entity holding doc and makes it the
// entity's current revision, returning its ID; otherwise it returns 0.
func (w writer) approve(entityType domain.EntityType, entityID int64, doc any, unapproved bool) int64 {
	if unapproved {
		return 0
	}
	id := w.revision(Revision{EntityType: entityType, EntityID: entityID, Snapshot: doc})
	w.exec(setCurrentRevision[entityType], id, entityID)

	return id
}

func (w writer) revision(r Revision) int64 {
	w.c.t.Helper()

	if r.Snapshot == nil {
		w.c.t.Fatal("catalogtest: a revision needs a snapshot")
	}
	snapshot, err := json.Marshal(r.Snapshot)
	if err != nil {
		w.c.t.Fatalf("catalogtest: encoding snapshot: %v", err)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = revisionBaseTime.Add(time.Duration(w.c.revisions) * time.Second)
		w.c.revisions++
	}

	id := w.exec("INSERT INTO revisions (entity_type, entity_id, base_revision_id, author_user_id, source, "+
		"status, snapshot_json, summary, created_at) VALUES (?, ?, NULLIF(?, 0), NULLIF(?, ''), ?, ?, ?, ?, ?)",
		r.EntityType, r.EntityID, r.BaseRevisionID, r.AuthorUserID, orDefault(r.Source, domain.RevisionSourceImport),
		orDefault(r.Status, domain.RevisionStatusApproved), string(snapshot), r.Summary, r.CreatedAt)
	for position, name := range r.Contributors {
		w.exec("INSERT INTO revision_contributors (revision_id, position, name) VALUES (?, ?, ?)",
			id, position, name)
	}

	return id
}

// orDefault returns v, or def if v is the zero value.
func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}

	return v
}

// dateValue returns d as a DATE column's value: its time, or NULL for nil.
func dateValue(d *domain.Date) any {
	if d == nil {
		return nil
	}

	return d.Time
}
