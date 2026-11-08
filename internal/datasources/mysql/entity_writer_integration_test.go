//go:build integration

package mysql_test

import (
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// moderatorSubmit saves doc as Rowan's draft of the entity key (nil for a new entity) and submits it, which approves
// it at once, returning the approved revision.
func (f *editingFixture) moderatorSubmit(t *testing.T, key *domain.EntityKey, doc domain.AnyDocument) int64 {
	t.Helper()
	result := f.submit(t, actorRowan, f.saveDraft(t, actorRowan, key, doc).Draft.ID)
	require.Equal(t, domain.RevisionStatusApproved, result.Status)

	return result.RevisionID
}

// snapshotOf returns the public view of the approved revision id.
func snapshotOf[R domain.Revision](t *testing.T, f *editingFixture, id int64) R {
	t.Helper()
	revision, err := f.repo.GetPublicRevision(t.Context(), id)
	require.NoError(t, err)

	return revision.(R)
}

// entitySlug is the slug of the entity of table with id.
func (f *editingFixture) entitySlug(t *testing.T, table string, id int64) string {
	t.Helper()
	var slug string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT e.slug FROM "+table+" e JOIN revisions r "+
		"ON r.entity_id = e.id WHERE r.id = ?", id).Scan(&slug))

	return slug
}

// fullNewPiece is a new piece holding every kind of row a piece's document writes.
func fullNewPiece(f *editingFixture) domain.PieceDocument {
	doc := newPieceDocument(kestrelTitle)
	doc.SecondaryKinds = []domain.PieceKind{domain.PieceKindPoem}
	doc.NotesMD, doc.VersionNotesMD, doc.HistoricalSourceMD = new("Notes."), new("Version."), new("A source.")
	doc.PeriodStart, doc.PeriodEnd = new(int16(1500)), new(int16(1550))
	doc.LicensingStatus, doc.Licence, doc.LicenceVersion = domain.LicensingStatusKnown, new(domain.LicenceCCBYSA),
		new(domain.LicenceVersion30)
	doc.Aliases = []domain.DocPieceAlias{{Text: "The Kestrel", Kind: domain.PieceAliasKindAltTitle},
		{Text: "Lament of the Kestrel", Kind: domain.PieceAliasKindAltTitle}}
	doc.Terms = []domain.DocPieceTerm{{TermID: f.ballads.ID, IsPrimary: true}}
	doc.Relations = []domain.DocPieceRelation{{ToPieceID: f.song.ID, Type: domain.PieceRelationTypeVariantOf}}
	doc.Credits = []domain.DocCredit{
		{PersonID: &f.morwen.ID, Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
			Certainty: domain.CertaintyCertain, Note: new("Words.")},
		{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional, Certainty: domain.CertaintyProbable},
	}
	doc.Links = []domain.DocPieceLink{
		{DocLink: domain.DocLink{URL: "https://example.org/kestrel/lyrics", Title: new("Lyrics"),
			CollectionID: &f.archive.ID, Locator: new("p. 4"),
			Roles: []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleText}},
			Performances: []domain.DocPerformance{{EventID: &f.fair.ID, Year: new(int16(2001)),
				Uploader: new("Ash"), DescriptionVerbatim: "At the fair", PerformerIDs: []int64{f.morwen.ID, f.ash.ID}}}},
		{DocLink: domain.DocLink{URL: "https://example.org/kestrel/tune",
			Roles: []domain.LinkRole{domain.LinkRoleOther, domain.LinkRoleLicensingSource}}},
	}
	doc.Preferences = []domain.DocPreference{domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL, domain.PreferenceChoiceUploadsAllowed)}

	return doc
}

// assertPieceIsView compares the public piece with the public view of its current revision's document, field by
// field: what the writer wrote is what the document says.
func assertPieceIsView(t *testing.T, piece domain.Piece, view domain.PieceSnapshot) {
	t.Helper()
	assert.Equal(t, view.Title, piece.Title)
	assert.Equal(t, view.Kind, piece.Kind)
	equalList(t, view.SecondaryKinds, piece.SecondaryKinds)
	assert.Equal(t, view.ConservationStatus, piece.ConservationStatus)
	assert.Equal(t, view.IsStub, piece.IsStub)
	assert.Equal(t, []*string{view.NotesMD, view.VersionNotesMD, view.LicensingNotesMD, view.HistoricalSourceMD},
		[]*string{piece.NotesMD, piece.VersionNotesMD, piece.LicensingNotesMD, piece.HistoricalSourceMD})
	assert.Equal(t, view.LicensingStatus, piece.LicensingStatus)
	assert.Equal(t, view.Licence, piece.Licence)
	assert.Equal(t, view.LicenceVersion, piece.LicenceVersion)
	assert.Equal(t, []*int16{view.PeriodStart, view.PeriodEnd}, []*int16{piece.PeriodStart, piece.PeriodEnd})
	equalList(t, view.Aliases, piece.Aliases, "aliases in the document's order")
	assert.ElementsMatch(t, view.Terms, piece.Terms)
	equalList(t, view.Credits, piece.Credits)
	var outgoing []domain.SnapshotPieceRelation
	for _, r := range piece.Relations {
		if r.Direction == domain.RelationDirectionOutgoing {
			outgoing = append(outgoing, domain.SnapshotPieceRelation{Type: r.Type, Piece: r.Piece})
		}
	}
	equalList(t, view.Relations, outgoing)
	require.Len(t, piece.Links, len(view.Links))
	for i, l := range piece.Links {
		assert.Equal(t, view.Links[i].SnapshotLink, snapshotLink(l.Link), "link %d", i)
		equalList(t, view.Links[i].Performances, l.Performances, "link performances")
	}
}

func snapshotLink(l domain.Link) domain.SnapshotLink {
	return domain.SnapshotLink{URL: l.URL, Title: l.Title, Description: l.Description, Platform: l.Platform,
		PlaceType: l.PlaceType, Collection: l.Collection, Locator: l.Locator, Roles: l.Roles}
}

// assertPersonIsView is assertPieceIsView for a person.
func assertPersonIsView(t *testing.T, person domain.Person, view domain.PersonSnapshot) {
	t.Helper()
	assert.Equal(t, []any{view.Name, view.Kind, view.Status, view.StatusSourceURL, view.IsStub},
		[]any{person.Name, person.Kind, person.Status, person.StatusSourceURL, person.IsStub})
	require.NotNil(t, person.Profile)
	require.NotNil(t, view.Profile)
	p, v := person.Profile, view.Profile
	assert.Equal(t, []*string{v.Pronouns, v.NotesMD, v.LicensingNotesMD}, []*string{p.Pronouns, p.NotesMD,
		p.LicensingNotesMD})
	assert.Equal(t, v.LicensingStatus, p.LicensingStatus)
	equalList(t, v.Aliases, p.Aliases, "the public aliases, in the document's order")
	assert.ElementsMatch(t, v.Roles, p.Roles)
	assert.ElementsMatch(t, v.Affiliations, p.Affiliations)
	var outgoing []domain.SnapshotPersonRelation
	for _, r := range p.Relations {
		if r.Direction == domain.RelationDirectionOutgoing {
			outgoing = append(outgoing, domain.SnapshotPersonRelation{Type: r.Type, Person: r.Person})
		}
	}
	equalList(t, v.Relations, outgoing)
	require.Len(t, p.Links, len(v.Links))
	for i, l := range p.Links {
		assert.Equal(t, v.Links[i].SnapshotLink, snapshotLink(l), "link %d", i)
	}
}

// TestEntityWriter_RoundTrip writes every fixture document of each type through the approval writer, new and
// edited, and reads the public entity back: it equals the public view of the document, field by field (links,
// credits, aliases' order, performances with their owners).
func TestEntityWriter_RoundTrip(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)

	t.Run("piece", func(t *testing.T) {
		newID := f.moderatorSubmit(t, nil, fullNewPiece(f))
		slug := f.entitySlug(t, "pieces", newID)
		assertPieceIsView(t, f.publicPiece(t, slug), snapshotOf[domain.PieceRevision](t, f, newID).Snapshot)
		assert.Equal(t, new(domain.LicenceVersion30), f.publicPiece(t, slug).LicenceVersion)
		var pieceID int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT entity_id FROM revisions WHERE id = ?", newID).
			Scan(&pieceID))
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances WHERE owner_piece_id = ? AND piece_id = ?",
			pieceID, pieceID), "the piece owns its performances")

		doc := f.editable(t, actorRowan, *pieceKey(slug)).Document.(domain.PieceDocument)
		doc.Aliases = slices.Concat(doc.Aliases[1:], doc.Aliases[:1])
		doc.Credits = doc.Credits[1:]
		doc.Links = slices.Concat(doc.Links[1:], doc.Links[:1])
		edited := f.moderatorSubmit(t, pieceKey(slug), doc)
		assertPieceIsView(t, f.publicPiece(t, slug), snapshotOf[domain.PieceRevision](t, f, edited).Snapshot)

		song := f.moderatorSubmit(t, pieceKey(f.song.Slug), songDocument(f))
		assertPieceIsView(t, f.publicPiece(t, f.song.Slug), snapshotOf[domain.PieceRevision](t, f, song).Snapshot)
	})
	t.Run("person", func(t *testing.T) {
		doc := newPersonDocument("Linnet Placeholder")
		doc.Pronouns, doc.NotesMD = new("she/her"), new("A piper.")
		doc.Aliases = []domain.DocPersonAlias{{Name: "Linnet", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Lin", Kind: domain.PersonAliasKindNickname, IsPublic: true}}
		doc.Roles = []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet}
		doc.AffiliationIDs = []int64{f.guild.ID}
		doc.Relations = []domain.DocPersonRelation{{ToPersonID: f.ash.ID, Type: domain.PersonRelationTypeHeirOf}}
		doc.Links = []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/linnet", Title: new("Linnet's site"),
				Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{PieceID: &f.song.ID, DocPerformance: domain.DocPerformance{
				DescriptionVerbatim: "Piped at the fair", PerformerIDs: []int64{f.ash.ID}}}},
		}}
		id := f.moderatorSubmit(t, nil, doc)
		slug := f.entitySlug(t, "persons", id)
		person, err := f.repo.GetPublicPerson(t.Context(), slug)
		require.NoError(t, err)
		assertPersonIsView(t, person, snapshotOf[domain.PersonRevision](t, f, id).Snapshot)
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances p JOIN persons ps "+
			"ON ps.id = p.owner_person_id WHERE ps.slug = ? AND p.piece_id = ?", slug, f.song.ID))

		morwen := f.moderatorSubmit(t, personKey(f.morwen.Slug), morwenDocument(f))
		person, err = f.repo.GetPublicPerson(t.Context(), f.morwen.Slug)
		require.NoError(t, err)
		assertPersonIsView(t, person, snapshotOf[domain.PersonRevision](t, f, morwen).Snapshot)
		assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM person_aliases WHERE person_id = ? AND name = ? "+
			"AND NOT is_public", f.morwen.ID, morwenLegal), "the hidden alias is written hidden")
	})
	t.Run("term", func(t *testing.T) {
		doc := newTermDocument("Sea Songs")
		doc.ParentID, doc.DescriptionMD, doc.Position = &f.ballads.ID, new("Of the sea."), 3
		doc.Aliases = []string{shantiesAlias}
		id := f.moderatorSubmit(t, nil, doc)
		view := snapshotOf[domain.TermRevision](t, f, id).Snapshot
		term, err := f.repo.GetPublicTerm(t.Context(), f.entitySlug(t, "terms", id), domain.Page{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, []any{view.Facet, view.Name, view.DisplayName, view.DescriptionMD, view.Aliases},
			[]any{term.Facet, term.Name, term.DisplayName, term.DescriptionMD, term.Aliases})
		require.NotNil(t, view.Parent)
		assert.Equal(t, []domain.TermSummary{*view.Parent}, term.Ancestors)
	})
	t.Run("collection", func(t *testing.T) {
		doc := domain.CollectionDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: "The Wren Papers",
			URL: new("https://example.org/papers"), Kind: domain.CollectionKindWebsite,
			DescriptionMD: new("Papers."), ParentID: &f.archive.ID, IsBardicResource: true,
			Enumerable: domain.CollectionEnumerableUnknown}
		id := f.moderatorSubmit(t, nil, doc)
		view := snapshotOf[domain.CollectionRevision](t, f, id).Snapshot
		var collectionID int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT entity_id FROM revisions WHERE id = ?", id).
			Scan(&collectionID))
		collection, err := f.repo.GetPublicCollection(t.Context(), collectionID)
		require.NoError(t, err)
		assert.Equal(t, []any{view.Name, view.URL, view.Kind, view.DescriptionMD, view.IsBardicResource,
			view.Enumerable, view.Parent}, []any{collection.Name, collection.URL, collection.Kind,
			collection.DescriptionMD, collection.IsBardicResource, collection.Enumerable, collection.Parent})
	})
	t.Run("event", func(t *testing.T) {
		doc := newEventDocument("Winter Revel")
		doc.DescriptionMD = new("A revel.")
		id := f.moderatorSubmit(t, nil, doc)
		view := snapshotOf[domain.EventRevision](t, f, id).Snapshot
		event, err := f.repo.GetPublicEvent(t.Context(), f.entitySlug(t, "events", id))
		require.NoError(t, err)
		assert.Equal(t, []any{view.Name, view.Kind, view.DescriptionMD},
			[]any{event.Name, event.Kind, event.DescriptionMD})
	})
}

// equalList asserts that two lists are equal, an empty list being equal to none.
func equalList[T any](t *testing.T, want, got []T, msgAndArgs ...any) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	assert.Equal(t, want, got, msgAndArgs...)
}
