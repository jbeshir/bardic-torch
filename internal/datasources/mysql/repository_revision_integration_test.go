//go:build integration

package mysql_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revisionFixture is a catalogue whose revisions reference everything a snapshot view must redact by the
// catalogue's current state: unapproved terms, pieces, persons, collections and events; a person who chose not
// to have a page after the revision; an alias and links hidden after it.
type revisionFixture struct {
	harvest, reaping, broken, pendingPiece                 catalogtest.Ref
	lark, quiet, choir, pendingPerson                      catalogtest.Ref
	ballads, seaBallads, underPending, pendingTopic, bards catalogtest.Ref
	archive, volume, pendingBook, fair, pendingFair        catalogtest.Ref
	edit, redacted, imported, foreignBase                  int64
	pending, rejected, withdrawn, pendingPieceApproved     int64
	quietEdit, futureVersion, wrongShape, brokenBase       int64
	// unknownEnum holds a kind this release does not know; addedField a field it does not know.
	unknownEnum, addedField int64
}

// Fixture strings the revision tests compare more than once.
const (
	harvestTitle     = "The Harvest Ballad"
	seaBalladsName   = "Sea Ballads"
	volumeName       = "Ballad Archive Volume Two"
	larkName         = "Lark Ashdown"
	quietName        = "Quiet Harper"
	choirName        = "Hearth Choir"
	balladsName      = "Ballads"
	bardsName        = "Bards Guild"
	archiveName      = "Ballad Archive"
	fairName         = "Harvest Fair"
	quillName        = "Morwen Quill"
	quillID          = "google-oauth2|quill"
	harvestLyricsURL = "https://example.org/harvest/lyrics"
	harvestTakenDown = "https://example.org/harvest/taken-down"
	larkSiteURL      = "https://example.org/lark"
	larkTakenDownURL = "https://example.org/lark/taken-down"
	harvestAliasText = "Reapers' Ballad"
	harvestSourceURL = "https://example.org/assessments/harvest"
	larkStageName    = "Lark"
	larkFormerName   = "Old Lark"
	larkHiddenName   = "Hidden Name"
	quietTuneNote    = "The tune is the harper's own"
	fairPerformance  = "Sung at the fair"
	pendingFairDesc  = "Sung at a fair still pending"
	larkFairTale     = "A tale at the fair"
	larkPendingPerf  = "Of the pending piece"
	editSummary      = "Add an alias"
)

// harvestDocument is the Harvest Ballad's document as first imported: it references, alongside public entities,
// an unapproved term and a term under one, an unapproved piece, an unapproved person, a person who later chose
// not to have a page, an unapproved collection and event, and a link that is later hidden.
func harvestDocument(fx revisionFixture) domain.PieceDocument {
	return domain.PieceDocument{
		SchemaVersion:  domain.DocumentSchemaVersion,
		Title:          harvestTitle,
		Kind:           domain.PieceKindSong,
		SecondaryKinds: []domain.PieceKind{domain.PieceKindPoem},
		Conservation: domain.DocConservation{Status: domain.ConservationStatusEndangered,
			SourceURL: new(harvestSourceURL)},
		LicensingStatus: domain.LicensingStatusUnknown,
		Terms: []domain.DocPieceTerm{
			{TermID: fx.ballads.ID, IsPrimary: true}, {TermID: fx.pendingTopic.ID}, {TermID: fx.underPending.ID},
		},
		Relations: []domain.DocPieceRelation{
			{ToPieceID: fx.pendingPiece.ID, Type: domain.PieceRelationTypeVariantOf},
			{ToPieceID: fx.reaping.ID, Type: domain.PieceRelationTypeSettingOf},
		},
		Credits: []domain.DocCredit{
			{PersonID: &fx.lark.ID, Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyCertain},
			{PersonID: &fx.quiet.ID, Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyProbable, Note: new(quietTuneNote)},
			{PersonID: &fx.pendingPerson.ID, Role: domain.CreditRoleMusic, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyCertain},
			{Role: domain.CreditRoleArrangement, Attribution: domain.AttributionTraditional,
				Certainty: domain.CertaintyPossible},
		},
		Links: []domain.DocPieceLink{
			{
				DocLink: domain.DocLink{URL: harvestLyricsURL, Title: new("Harvest lyrics"),
					CollectionID: &fx.pendingBook.ID, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{
					{EventID: &fx.fair.ID, Year: new(int16(2024)), DescriptionVerbatim: fairPerformance,
						PerformerIDs: []int64{fx.lark.ID, fx.quiet.ID, fx.pendingPerson.ID}},
					{EventID: &fx.pendingFair.ID, DescriptionVerbatim: pendingFairDesc},
				},
			},
			{DocLink: domain.DocLink{URL: harvestTakenDown, CollectionID: &fx.archive.ID,
				Roles: []domain.LinkRole{domain.LinkRolePerformance}}},
		},
	}
}

// larkDocument is Lark's document as first imported: an alias later hidden, a link later hidden, a relation to a
// person who later chose not to have a page, and references to unapproved entities.
func larkDocument(fx revisionFixture) domain.PersonDocument {
	return domain.PersonDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Name:            larkName,
		Kind:            domain.PersonKindCommunityBard,
		Status:          domain.PersonStatusActive,
		Pronouns:        new("they/them"),
		NotesMD:         new("A singer of harvest songs."),
		LicensingStatus: domain.LicensingStatusKnown,
		Aliases: []domain.DocPersonAlias{
			{Name: larkStageName, Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: larkFormerName, Kind: domain.PersonAliasKindFormer, IsPublic: true},
			{Name: larkHiddenName, Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRoleSongwriter},
		AffiliationIDs: []int64{fx.pendingTopic.ID, fx.bards.ID},
		Relations: []domain.DocPersonRelation{
			{ToPersonID: fx.quiet.ID, Type: domain.PersonRelationTypeHeirOf},
			{ToPersonID: fx.pendingPerson.ID, Type: domain.PersonRelationTypeSameAs},
			{ToPersonID: fx.choir.ID, Type: domain.PersonRelationTypeMemberOf},
		},
		Links: []domain.DocPersonLink{
			{
				DocLink: domain.DocLink{URL: larkSiteURL, CollectionID: &fx.archive.ID,
					Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
				Performances: []domain.DocPersonPerformance{
					{PieceID: &fx.pendingPiece.ID, DocPerformance: domain.DocPerformance{
						DescriptionVerbatim: larkPendingPerf}},
					{DocPerformance: domain.DocPerformance{EventID: &fx.fair.ID, DescriptionVerbatim: larkFairTale,
						PerformerIDs: []int64{fx.quiet.ID, fx.choir.ID}}},
				},
			},
			{DocLink: domain.DocLink{URL: larkTakenDownURL, Roles: []domain.LinkRole{domain.LinkRoleOther}}},
		},
	}
}

// quietDocument is the Quiet Harper's document, from before they chose not to have a page.
func quietDocument(fx revisionFixture) domain.PersonDocument {
	return domain.PersonDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Name:            quietName,
		Kind:            domain.PersonKindCommunityBard,
		Status:          domain.PersonStatusActive,
		NotesMD:         new("Plays the harp at every fair."),
		LicensingStatus: domain.LicensingStatusUnknown,
		Aliases:         []domain.DocPersonAlias{{Name: "Harper", Kind: domain.PersonAliasKindStage, IsPublic: true}},
		Roles:           []domain.PersonRole{domain.PersonRoleMusician},
		AffiliationIDs:  []int64{fx.bards.ID},
		Relations:       []domain.DocPersonRelation{{ToPersonID: fx.choir.ID, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: "https://example.org/harper",
			Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}},
	}
}

// importContributors are the wiki users who wrote the imported revision, in order.
func importContributors() []string { return []string{"Lorekeeper", "Archivist"} }

// revisionsAt is a day of 2026, when the fixture.s revisions were created.
func revisionsAt(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

var revisions = catalogtest.NewShared(func(c *catalogtest.Catalog) revisionFixture {
	var fx revisionFixture
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'contributor', ?)",
		quillID, quillName, revisionsAt(time.January, 1))

	fx.ballads = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: balladsName}})
	fx.pendingTopic = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Pending Topic"}, Unapproved: true})
	fx.underPending = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Under Pending", ParentID: &fx.pendingTopic.ID}})
	fx.bards = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: bardsName}})
	fx.seaBallads = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: seaBalladsName,
		ParentID: &fx.ballads.ID, RelatedAffiliationID: &fx.pendingTopic.ID, Position: 2,
		Aliases: []string{"Shanties"}}})

	fx.archive = c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: archiveName,
		Kind: domain.CollectionKindWiki}})
	fx.pendingBook = c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Pending Book"},
		Unapproved: true})
	fx.volume = c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: volumeName,
		URL: new("https://example.org/volume-two.pdf"), Kind: domain.CollectionKindSongbookPDF,
		RegionTermID: &fx.pendingTopic.ID, ParentID: &fx.archive.ID, IsBardicResource: true,
		Enumerable: domain.CollectionEnumerablePDF}})
	fx.fair = c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: fairName,
		DescriptionMD: new("An autumn fair.")}})
	fx.pendingFair = c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Pending Fair"}, Unapproved: true})

	fx.pendingPiece = c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Pending Piece"}, Unapproved: true})
	fx.reaping = c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Reaping Song"}})
	fx.choir = c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: choirName, Kind: domain.PersonKindGroup}})
	fx.pendingPerson = c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Pending Piper"},
		Unapproved: true})
	fx.quiet = c.Person(catalogtest.Person{Doc: quietDocument(fx)})
	fx.lark = c.Person(catalogtest.Person{Doc: larkDocument(fx)})
	fx.harvest = c.Piece(catalogtest.Piece{Doc: harvestDocument(fx)})
	fx.broken = c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Broken Song"}})

	// Hidden after the fact: an alias, two links, and a person who now prefers not to have a page.
	c.Exec("UPDATE person_aliases SET is_public = FALSE WHERE name = ?", larkFormerName)
	c.SetLinkState(harvestTakenDown, catalogtest.LinkState{IsHidden: true})
	c.SetLinkState(larkTakenDownURL, catalogtest.LinkState{IsHidden: true})
	c.Exec("UPDATE persons SET status = 'prefers_no_page' WHERE id = ?", fx.quiet.ID)

	harvest := func(r catalogtest.Revision) int64 {
		r.EntityType, r.EntityID = domain.EntityTypePiece, fx.harvest.ID
		return c.Revision(r)
	}
	edited := harvestDocument(fx)
	edited.Aliases = []domain.DocPieceAlias{{Text: harvestAliasText, Kind: domain.PieceAliasKindAltTitle}}
	edited.IsStub = true
	fx.edit = harvest(catalogtest.Revision{Snapshot: edited, Source: domain.RevisionSourceUser,
		BaseRevisionID: fx.harvest.RevisionID, AuthorUserID: quillID, Summary: editSummary,
		CreatedAt: revisionsAt(time.June, 1)})
	// A revision changing only what its view redacts: its view equals its base's.
	redacted := edited
	redacted.Credits = append(append([]domain.DocCredit{}, edited.Credits...), domain.DocCredit{
		PersonID: &fx.pendingPerson.ID, Role: domain.CreditRoleTranslation, Attribution: domain.AttributionNamed,
		Certainty: domain.CertaintyCertain})
	redacted.Relations = append(append([]domain.DocPieceRelation{}, edited.Relations...),
		domain.DocPieceRelation{ToPieceID: fx.pendingPiece.ID, Type: domain.PieceRelationTypeContrafactumOf})
	redacted.Links = append(append([]domain.DocPieceLink{}, edited.Links[:1]...), domain.DocPieceLink{
		DocLink: domain.DocLink{URL: larkTakenDownURL, Roles: []domain.LinkRole{domain.LinkRoleOther}}})
	fx.redacted = harvest(catalogtest.Revision{Snapshot: redacted, Source: domain.RevisionSourceUser,
		BaseRevisionID: fx.edit, AuthorUserID: quillID, Summary: "Credit a translator",
		CreatedAt: revisionsAt(time.July, 1)})
	fx.pending = harvest(catalogtest.Revision{Snapshot: edited, Status: domain.RevisionStatusPending,
		BaseRevisionID: fx.edit, AuthorUserID: quillID, CreatedAt: revisionsAt(time.August, 1)})
	fx.rejected = harvest(catalogtest.Revision{Snapshot: edited, Status: domain.RevisionStatusRejected,
		CreatedAt: revisionsAt(time.August, 2)})
	fx.withdrawn = harvest(catalogtest.Revision{Snapshot: edited, Status: domain.RevisionStatusWithdrawn,
		CreatedAt: revisionsAt(time.August, 4)})
	// Created at the same time as the redacted revision, after it: history breaks the tie by ID. Its base is
	// pending, so it is not shown.
	fx.imported = harvest(catalogtest.Revision{Snapshot: edited, BaseRevisionID: fx.pending,
		Contributors: importContributors(), CreatedAt: revisionsAt(time.July, 1)})
	// Its base is a revision of another entity, so it is not shown.
	fx.foreignBase = harvest(catalogtest.Revision{Snapshot: edited, BaseRevisionID: fx.lark.RevisionID,
		CreatedAt: revisionsAt(time.May, 1)})

	fx.quietEdit = c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: fx.quiet.ID,
		Snapshot: quietDocument(fx), BaseRevisionID: fx.quiet.RevisionID, CreatedAt: revisionsAt(time.June, 1)})
	fx.pendingPieceApproved = c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece,
		EntityID: fx.pendingPiece.ID, Snapshot: harvestDocument(fx)})

	broken := func(snapshot any, base int64) int64 {
		return c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: fx.broken.ID,
			Snapshot: snapshot, BaseRevisionID: base})
	}
	fx.futureVersion = broken(json.RawMessage(`{"schema_version": 2, "title": "From a newer release"}`), 0)
	fx.wrongShape = broken(json.RawMessage(`{"schema_version": 1, "title": 7}`), 0)
	fx.brokenBase = broken(domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: "Broken Song",
		Kind: domain.PieceKindSong, Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown}, fx.wrongShape)
	fx.unknownEnum = broken(json.RawMessage(`{"schema_version": 1, "title": "Broken Ballad", "kind": "ballad", `+
		`"conservation": {"status": "unassessed", "source_url": null}, "licensing_status": "unknown"}`), 0)
	fx.addedField = broken(json.RawMessage(`{"schema_version": 1, "title": "Later Song", "kind": "song", `+
		`"conservation": {"status": "unassessed", "source_url": null}, "licensing_status": "unknown", `+
		`"added_in_a_later_release": {"nested": true}}`), 0)

	return fx
})

func revisionRepository(t *testing.T) (*mysql.Repository, revisionFixture) {
	t.Helper()

	db, fx := revisions.Get(t)
	return mysql.New(db), fx
}

var (
	balladsSummary = termSummary("ballads", domain.FacetTopic, balladsName)
	guildSummary   = termSummary("bards-guild", domain.FacetAffiliation, bardsName)
	larkSummary    = domain.PersonSummary{Slug: "lark-ashdown", Name: larkName, Kind: domain.PersonKindCommunityBard}
	choirSummary   = domain.PersonSummary{Slug: "hearth-choir", Name: choirName, Kind: domain.PersonKindGroup}
	reapingSummary = domain.PieceSummary{Slug: "reaping-song", Title: "Reaping Song", Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusUnassessed}
	harvestFairSummary = domain.EventSummary{Slug: "harvest-fair", Name: fairName, Kind: domain.EventKindEvent}
	quillAuthor        = &domain.RevisionAuthor{DisplayName: quillName}
)

// harvestView is the view of harvestDocument: of what it references, only the public term, the related public
// piece, the credits to the public person with a page and the one with no person, the visible link without its
// unapproved collection, and its performance at the public event with the public performer remain.
func harvestView() domain.PieceSnapshot {
	return domain.PieceSnapshot{
		Title:                 harvestTitle,
		Kind:                  domain.PieceKindSong,
		SecondaryKinds:        []domain.PieceKind{domain.PieceKindPoem},
		ConservationStatus:    domain.ConservationStatusEndangered,
		ConservationSourceURL: new(harvestSourceURL),
		LicensingStatus:       domain.LicensingStatusUnknown,
		Terms:                 []domain.PieceTerm{{Term: balladsSummary, IsPrimary: true}},
		Relations:             []domain.SnapshotPieceRelation{{Type: domain.PieceRelationTypeSettingOf, Piece: reapingSummary}},
		Credits: []domain.PieceCredit{
			{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
				Person: &larkSummary},
			{Role: domain.CreditRoleArrangement, Attribution: domain.AttributionTraditional,
				Certainty: domain.CertaintyPossible},
		},
		Links: []domain.SnapshotPieceLink{{
			SnapshotLink: domain.SnapshotLink{URL: harvestLyricsURL, Title: new("Harvest lyrics"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
			Performances: []domain.PiecePerformance{
				{Year: new(int16(2024)), DescriptionVerbatim: fairPerformance, Event: &harvestFairSummary,
					Performers: []domain.PersonSummary{larkSummary}},
				{DescriptionVerbatim: pendingFairDesc},
			},
		}},
	}
}

// TestRepository_ListPublicHistory checks a public entity's history: approved revisions only, newest first
// with ties by ID, authors by display name, imports with no author and their contributors in order, and pages.
func TestRepository_ListPublicHistory(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)
	ctx := t.Context()

	imported := domain.RevisionSummary{ID: fx.imported, Source: domain.RevisionSourceImport,
		Contributors: importContributors(), CreatedAt: revisionsAt(time.July, 1)}
	redacted := domain.RevisionSummary{ID: fx.redacted, Source: domain.RevisionSourceUser,
		Summary: "Credit a translator", Author: quillAuthor, CreatedAt: revisionsAt(time.July, 1)}
	edit := domain.RevisionSummary{ID: fx.edit, Source: domain.RevisionSourceUser, Summary: editSummary,
		Author: quillAuthor, CreatedAt: revisionsAt(time.June, 1)}
	foreignBase := domain.RevisionSummary{ID: fx.foreignBase, Source: domain.RevisionSourceImport,
		CreatedAt: revisionsAt(time.May, 1)}

	history, err := repo.ListPublicHistory(ctx, domain.SlugEntityTypePiece, fx.harvest.Slug,
		domain.Page{Limit: domain.DefaultPageLimit})
	require.NoError(t, err)
	require.Len(t, history.Revisions, 5)
	assert.Equal(t, domain.RevisionHistory{
		Revisions: []domain.RevisionSummary{imported, redacted, edit, foreignBase, {
			ID: fx.harvest.RevisionID, Source: domain.RevisionSourceImport,
			CreatedAt: history.Revisions[4].CreatedAt,
		}},
		Total: 5,
	}, history)

	page, err := repo.ListPublicHistory(ctx, domain.SlugEntityTypePiece, fx.harvest.Slug, domain.Page{Limit: 2, Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionHistory{Revisions: []domain.RevisionSummary{redacted, edit}, Total: 5}, page)

	past, err := repo.ListPublicHistory(ctx, domain.SlugEntityTypePiece, fx.harvest.Slug, domain.Page{Limit: 2, Offset: 5})
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionHistory{Revisions: []domain.RevisionSummary{}, Total: 5}, past)

	for _, entity := range []struct {
		entityType domain.SlugEntityType
		ref        catalogtest.Ref
		total      int64
	}{
		{domain.SlugEntityTypePerson, fx.lark, 1},
		{domain.SlugEntityTypePerson, fx.quiet, 2},
		{domain.SlugEntityTypeTerm, fx.seaBallads, 1},
		{domain.SlugEntityTypeEvent, fx.fair, 1},
	} {
		history, err := repo.ListPublicHistory(ctx, entity.entityType, entity.ref.Slug,
			domain.Page{Limit: domain.DefaultPageLimit})
		require.NoError(t, err, entity.ref.Slug)
		assert.Equal(t, entity.total, history.Total, entity.ref.Slug)
		assert.Equal(t, entity.ref.RevisionID, history.Revisions[len(history.Revisions)-1].ID, entity.ref.Slug)
	}

	collection, err := repo.ListPublicCollectionHistory(ctx, fx.volume.ID, domain.Page{Limit: domain.DefaultPageLimit})
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionHistory{
		Revisions: []domain.RevisionSummary{{ID: fx.volume.RevisionID, Source: domain.RevisionSourceImport,
			CreatedAt: collection.Revisions[0].CreatedAt}},
		Total: 1,
	}, collection)
}

// TestRepository_ListPublicHistory_NotFound checks that the history of an entity that is not public is not found,
// like an unknown one's, even if it has an approved revision.
func TestRepository_ListPublicHistory_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)
	page := domain.Page{Limit: domain.DefaultPageLimit}

	for _, entity := range []struct {
		entityType domain.SlugEntityType
		slug       string
	}{
		{domain.SlugEntityTypePiece, fx.pendingPiece.Slug},
		{domain.SlugEntityTypePiece, "no-such-piece"},
		{domain.SlugEntityTypePerson, fx.pendingPerson.Slug},
		{domain.SlugEntityTypeTerm, fx.pendingTopic.Slug},
		{domain.SlugEntityTypeTerm, fx.underPending.Slug},
		{domain.SlugEntityTypeEvent, fx.pendingFair.Slug},
		// Slugs are per entity type.
		{domain.SlugEntityTypeEvent, fx.harvest.Slug},
	} {
		_, err := repo.ListPublicHistory(t.Context(), entity.entityType, entity.slug, page)
		require.ErrorIs(t, err, datasources.ErrNotFound, entity.slug)
	}

	for _, id := range []int64{fx.pendingBook.ID, 0, -1, fx.volume.ID + 1000} {
		_, err := repo.ListPublicCollectionHistory(t.Context(), id, page)
		require.ErrorIs(t, err, datasources.ErrNotFound, id)
	}

	_, err := repo.ListPublicHistory(t.Context(), domain.SlugEntityType{}, fx.archive.Slug, page)
	require.Error(t, err)
	assert.NotErrorIs(t, err, datasources.ErrNotFound)
}

// TestRepository_GetPublicRevision_Piece checks a piece revision's views: references to entities
// that are not public now and links hidden now are left out of the snapshot and its base, with no system fields.
func TestRepository_GetPublicRevision_Piece(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	revision, err := repo.GetPublicRevision(t.Context(), fx.edit)
	require.NoError(t, err)

	edited := harvestView()
	edited.Aliases = []domain.PieceAlias{{Text: harvestAliasText, Kind: domain.PieceAliasKindAltTitle}}
	edited.IsStub = true
	base := harvestView()
	harvestID := fx.harvest.RevisionID
	assert.Equal(t, domain.PieceRevision{
		RevisionSummary: domain.RevisionSummary{ID: fx.edit, Source: domain.RevisionSourceUser, Summary: editSummary,
			Author: quillAuthor, CreatedAt: revisionsAt(time.June, 1)},
		Entity: domain.PieceSummary{Slug: fx.harvest.Slug, Title: harvestTitle, Kind: domain.PieceKindSong,
			SecondaryKinds: []domain.PieceKind{domain.PieceKindPoem}, ConservationStatus: domain.ConservationStatusEndangered},
		Snapshot: edited,
		Base:     &domain.RevisionBase[domain.PieceSnapshot]{ID: harvestID, Snapshot: base},
	}, revision)
}

// TestRepository_GetPublicRevision_RedactedChange checks that a revision that changed only what its view
// redacts has a view equal to its base's, so no change can show.
func TestRepository_GetPublicRevision_RedactedChange(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	revision, err := repo.GetPublicRevision(t.Context(), fx.redacted)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok, "%T", revision)
	require.NotNil(t, pieceRevision.Base)
	assert.Equal(t, pieceRevision.Base.Snapshot, pieceRevision.Snapshot)
	assert.Equal(t, fx.edit, pieceRevision.Base.ID)
}

// TestRepository_GetPublicRevision_Base checks that a base is shown only if it is an approved revision of the
// same entity.
func TestRepository_GetPublicRevision_Base(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	for name, id := range map[string]int64{
		"no_base":      fx.harvest.RevisionID,
		"pending_base": fx.imported,
		"foreign_base": fx.foreignBase,
	} {
		revision, err := repo.GetPublicRevision(t.Context(), id)
		require.NoError(t, err, name)
		pieceRevision, ok := revision.(domain.PieceRevision)
		require.True(t, ok, "%s: %T", name, revision)
		assert.Nil(t, pieceRevision.Base, name)
	}

	imported, err := repo.GetPublicRevision(t.Context(), fx.imported)
	require.NoError(t, err)
	assert.Equal(t, importContributors(), imported.(domain.PieceRevision).Contributors)
}

// TestRepository_GetPublicRevision_Person checks a person revision's view: an alias public in the snapshot is
// shown only if it is public now, and a link hidden since, relations to a person who chose since not to have a
// page, and references to unapproved entities are left out.
func TestRepository_GetPublicRevision_Person(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	revision, err := repo.GetPublicRevision(t.Context(), fx.lark.RevisionID)
	require.NoError(t, err)
	personRevision, ok := revision.(domain.PersonRevision)
	require.True(t, ok, "%T", revision)
	assert.Equal(t, larkSummary, personRevision.Entity)
	assert.Nil(t, personRevision.Base)
	assert.Equal(t, domain.PersonSnapshot{
		Name:   larkName,
		Kind:   domain.PersonKindCommunityBard,
		Status: domain.PersonStatusActive,
		Profile: &domain.PersonSnapshotProfile{
			Pronouns:        new("they/them"),
			NotesMD:         new("A singer of harvest songs."),
			LicensingStatus: domain.LicensingStatusKnown,
			Aliases:         []domain.PersonAlias{{Name: larkStageName, Kind: domain.PersonAliasKindStage}},
			Roles:           []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRoleSongwriter},
			Affiliations:    []domain.TermSummary{guildSummary},
			Relations:       []domain.SnapshotPersonRelation{{Type: domain.PersonRelationTypeMemberOf, Person: choirSummary}},
			Links: []domain.SnapshotPersonLink{{
				SnapshotLink: domain.SnapshotLink{URL: larkSiteURL,
					Collection: &domain.CollectionRef{ID: fx.archive.ID, Name: archiveName},
					Roles:      []domain.LinkRole{domain.LinkRoleWebsite}},
				Performances: []domain.SnapshotPersonPerformance{{PiecePerformance: domain.PiecePerformance{
					DescriptionVerbatim: larkFairTale, Event: &harvestFairSummary,
					Performers: []domain.PersonSummary{choirSummary},
				}}},
			}},
		},
	}, personRevision.Snapshot)
}

// TestRepository_GetPublicRevision_NoPage checks that the snapshots of a person who chose since not to have a
// page, and their base, show the identity only.
func TestRepository_GetPublicRevision_NoPage(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	revision, err := repo.GetPublicRevision(t.Context(), fx.quietEdit)
	require.NoError(t, err)
	identity := domain.PersonSnapshot{Name: quietName, Kind: domain.PersonKindCommunityBard,
		Status: domain.PersonStatusActive, IsStub: true}
	baseID := fx.quiet.RevisionID
	assert.Equal(t, domain.PersonRevision{
		RevisionSummary: domain.RevisionSummary{ID: fx.quietEdit, Source: domain.RevisionSourceImport,
			CreatedAt: revisionsAt(time.June, 1)},
		Entity: domain.PersonSummary{Slug: fx.quiet.Slug, Name: quietName, Kind: domain.PersonKindCommunityBard,
			IsStub: true},
		Snapshot: identity,
		Base:     &domain.RevisionBase[domain.PersonSnapshot]{ID: baseID, Snapshot: identity},
	}, revision)
}

// TestRepository_GetPublicRevision_OtherTypes checks term, collection and event revisions' views: references to
// entities that are not public are null.
func TestRepository_GetPublicRevision_OtherTypes(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	term, err := repo.GetPublicRevision(t.Context(), fx.seaBallads.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, domain.TermRevision{
		RevisionSummary: domain.RevisionSummary{ID: fx.seaBallads.RevisionID, Source: domain.RevisionSourceImport,
			CreatedAt: term.(domain.TermRevision).CreatedAt},
		Entity: termSummary("sea-ballads", domain.FacetTopic, seaBalladsName),
		Snapshot: domain.TermSnapshot{Facet: domain.FacetTopic, Name: seaBalladsName, DisplayName: seaBalladsName,
			Parent: &balladsSummary, Position: 2, Aliases: []string{"Shanties"}},
	}, term)

	collection, err := repo.GetPublicRevision(t.Context(), fx.volume.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, domain.CollectionRevision{
		RevisionSummary: domain.RevisionSummary{ID: fx.volume.RevisionID, Source: domain.RevisionSourceImport,
			CreatedAt: collection.(domain.CollectionRevision).CreatedAt},
		Entity: domain.CollectionRef{ID: fx.volume.ID, Name: volumeName},
		Snapshot: domain.CollectionSnapshot{Name: volumeName,
			URL: new("https://example.org/volume-two.pdf"), Kind: domain.CollectionKindSongbookPDF,
			Parent:           &domain.CollectionSummary{ID: fx.archive.ID, Name: archiveName, Kind: domain.CollectionKindWiki},
			IsBardicResource: true, Enumerable: domain.CollectionEnumerablePDF},
	}, collection)

	event, err := repo.GetPublicRevision(t.Context(), fx.fair.RevisionID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventRevision{
		RevisionSummary: domain.RevisionSummary{ID: fx.fair.RevisionID, Source: domain.RevisionSourceImport,
			CreatedAt: event.(domain.EventRevision).CreatedAt},
		Entity:   harvestFairSummary,
		Snapshot: domain.EventSnapshot{Name: fairName, Kind: domain.EventKindEvent, DescriptionMD: new("An autumn fair.")},
	}, event)
}

// TestRepository_GetPublicRevision_NotFound checks that a revision that is not approved, or whose entity is not
// public, is not found like an unknown one.
func TestRepository_GetPublicRevision_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	for name, id := range map[string]int64{
		"pending":               fx.pending,
		"rejected":              fx.rejected,
		"withdrawn":             fx.withdrawn,
		"entity_not_public":     fx.pendingPieceApproved,
		"term_under_unapproved": fx.underPending.RevisionID,
		"zero":                  0,
		"negative":              -1,
		"unknown":               fx.brokenBase + 1000,
	} {
		_, err := repo.GetPublicRevision(t.Context(), id)
		require.ErrorIs(t, err, datasources.ErrNotFound, name)
	}
}

// TestRepository_GetPublicRevision_Undecodable checks that a snapshot or base snapshot that cannot be decoded,
// such as one of a newer schema version, fails the read with an error other than ErrNotFound.
func TestRepository_GetPublicRevision_Undecodable(t *testing.T) {
	t.Parallel()

	repo, fx := revisionRepository(t)

	_, err := repo.GetPublicRevision(t.Context(), fx.futureVersion)
	require.ErrorIs(t, err, domain.ErrUnsupportedSchemaVersion)

	for name, id := range map[string]int64{"wrong_shape": fx.wrongShape, "base": fx.brokenBase} {
		_, err := repo.GetPublicRevision(t.Context(), id)
		require.Error(t, err, name)
		require.NotErrorIs(t, err, datasources.ErrNotFound, name)
		require.NotErrorIs(t, err, domain.ErrUnsupportedSchemaVersion, name)
	}

	// A vocabulary value this release does not know, as after rolling back past the release that added it.
	_, err = repo.GetPublicRevision(t.Context(), fx.unknownEnum)
	require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
	require.NotErrorIs(t, err, datasources.ErrNotFound)

	// A field this release does not know, as after rolling back past the release that added it, is ignored.
	revision, err := repo.GetPublicRevision(t.Context(), fx.addedField)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok, "%T", revision)
	assert.Equal(t, "Later Song", pieceRevision.Snapshot.Title)
}
