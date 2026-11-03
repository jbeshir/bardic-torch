package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// editorRefFixture is a piece and a person document referencing every kind of entity, and the slugs and
// references a caller who may see all of them has.
type editorRefFixture struct {
	slugs SlugMap
	refs  RefMap
}

const (
	editorTermID, editorPieceID, editorPersonID, editorEventID, editorCollectionID int64 = 11, 12, 13, 14, 15

	editorTermSlug   = "linnet-ballads"
	editorPieceSlug  = "the-linnet-kettle"
	editorPersonSlug = "linnet-placeholder"
	editorEventSlug  = "linnet-revel"
)

func newEditorRefFixture() editorRefFixture {
	return editorRefFixture{
		slugs: NewSlugMap([]EditorReference{
			{EntityType: EntityTypeTerm, ID: editorTermID, Slug: editorTermSlug},
			{EntityType: EntityTypePiece, ID: editorPieceID, Slug: editorPieceSlug},
			{EntityType: EntityTypePerson, ID: editorPersonID, Slug: editorPersonSlug},
			{EntityType: EntityTypeEvent, ID: editorEventID, Slug: editorEventSlug},
			{EntityType: EntityTypeCollection, ID: editorCollectionID},
		}),
		refs: RefMap{
			Terms: map[string]int64{editorTermSlug: editorTermID}, Pieces: map[string]int64{editorPieceSlug: editorPieceID},
			Persons: map[string]int64{editorPersonSlug: editorPersonID},
			Events:  map[string]int64{editorEventSlug: editorEventID}, Collections: map[int64]bool{editorCollectionID: true},
		},
	}
}

func editorPieceDocument() PieceDocument {
	return PieceDocument{
		SchemaVersion: DocumentSchemaVersion, Title: "The Linnet's Song", Kind: PieceKindSong,
		SecondaryKinds: []PieceKind{PieceKindPoem}, LicensingStatus: LicensingStatusUnknown,
		Conservation: DocConservation{Status: ConservationStatusUnassessed},
		Aliases:      []DocPieceAlias{{Text: "Linnet Song", Kind: PieceAliasKindNickname}},
		Terms:        []DocPieceTerm{{TermID: editorTermID, IsPrimary: true}},
		Relations:    []DocPieceRelation{{ToPieceID: editorPieceID, Type: PieceRelationTypeVariantOf}},
		Credits: []DocCredit{
			{PersonID: new(editorPersonID), Role: CreditRoleLyrics, Attribution: AttributionNamed,
				Certainty: CertaintyCertain},
			{Role: CreditRoleTune, Attribution: AttributionTraditional, Certainty: CertaintyPossible},
		},
		Links: []DocPieceLink{{
			DocLink: DocLink{URL: "https://example.org/linnet", CollectionID: new(editorCollectionID),
				Roles: []LinkRole{LinkRoleLyrics}},
			Performances: []DocPerformance{{EventID: new(editorEventID), DescriptionVerbatim: "At the fair",
				PerformerIDs: []int64{editorPersonID}}},
		}},
	}
}

func editorPersonDocument() PersonDocument {
	return PersonDocument{
		SchemaVersion: DocumentSchemaVersion, Name: tamsinName, Kind: PersonKindCommunityBard,
		Status: PersonStatusActive, LicensingStatus: LicensingStatusUnknown,
		Aliases:        []DocPersonAlias{{Name: tamsinStage, Kind: PersonAliasKindStage, IsPublic: true}},
		Roles:          []PersonRole{PersonRoleSinger},
		AffiliationIDs: []int64{editorTermID},
		Relations:      []DocPersonRelation{{ToPersonID: editorPersonID, Type: PersonRelationTypeMemberOf}},
		Links: []DocPersonLink{{
			DocLink: DocLink{URL: "https://example.org/tamsin", Roles: []LinkRole{LinkRoleWebsite}},
			Performances: []DocPersonPerformance{{PieceID: new(editorPieceID),
				DocPerformance: DocPerformance{DescriptionVerbatim: "A song", PerformerIDs: []int64{editorPersonID}}}},
		}},
	}
}

func TestEditorDocuments_RoundTrip(t *testing.T) {
	t.Parallel()
	fx := newEditorRefFixture()

	piece := editorPieceDocument()
	editorPiece := ToEditorPiece(piece, fx.slugs)
	assert.Equal(t, editorTermSlug, editorPiece.Terms[0].Term)
	assert.Equal(t, editorPersonSlug, *editorPiece.Credits[0].Person)
	assert.Equal(t, []string{editorPersonSlug}, editorPiece.Links[0].Performances[0].Performers)
	back, problems := FromEditorPiece(editorPiece, fx.refs)
	assert.Empty(t, problems)
	assert.Equal(t, piece, back)

	person := editorPersonDocument()
	editorPerson := ToEditorPerson(person, fx.slugs)
	assert.Equal(t, []string{editorTermSlug}, editorPerson.Affiliations)
	backPerson, problems := FromEditorPerson(editorPerson, fx.refs)
	assert.Empty(t, problems)
	assert.Equal(t, person, backPerson)

	term := TermDocument{SchemaVersion: DocumentSchemaVersion, Facet: FacetTopic, Name: "Tidewater", DisplayName: "Tidewater",
		ParentID: new(editorTermID), RelatedAffiliationID: new(editorTermID), Aliases: []string{"Ocean"}}
	backTerm, problems := FromEditorTerm(ToEditorTerm(term, fx.slugs), fx.refs)
	assert.Empty(t, problems)
	assert.Equal(t, term, backTerm)

	collection := CollectionDocument{SchemaVersion: DocumentSchemaVersion, Name: "Linnet Archive", Kind: CollectionKindOther,
		RegionTermID: new(editorTermID), ParentID: new(editorCollectionID), Enumerable: CollectionEnumerableUnknown}
	backCollection, problems := FromEditorCollection(ToEditorCollection(collection, fx.slugs), fx.refs)
	assert.Empty(t, problems)
	assert.Equal(t, collection, backCollection)

	event := EventDocument{SchemaVersion: DocumentSchemaVersion, Name: linnetRevel, Kind: EventKindEvent}
	assert.Equal(t, event, FromEditorEvent(ToEditorEvent(event)))
}

func TestEditorDocuments_NoPreferences(t *testing.T) {
	t.Parallel()
	fx := newEditorRefFixture()
	piece := editorPieceDocument()
	piece.Preferences = []DocPreference{{Dimension: PreferenceDimensionRecording, SourceURL: "https://example.org/p"}}

	back, _ := FromEditorPiece(ToEditorPiece(piece, fx.slugs), fx.refs)

	assert.Nil(t, back.Preferences)
}

// TestToEditor_ReferencesWithoutSlugAreLeftOut checks that a reference the caller may not see, which a projection
// never holds, is left out of the editor format rather than shown by ID.
func TestToEditor_ReferencesWithoutSlugAreLeftOut(t *testing.T) {
	t.Parallel()
	piece := ToEditorPiece(editorPieceDocument(), SlugMap{})

	assert.Empty(t, piece.Terms)
	assert.Empty(t, piece.Relations)
	assert.Len(t, piece.Credits, 1, "the credit naming no one stays")
	assert.Nil(t, piece.Links[0].CollectionID)
	assert.Nil(t, piece.Links[0].Performances[0].Event)
	assert.Empty(t, piece.Links[0].Performances[0].Performers)

	person := ToEditorPerson(editorPersonDocument(), SlugMap{})
	assert.Empty(t, person.Affiliations)
	assert.Empty(t, person.Relations)
	assert.Empty(t, person.Links[0].Performances, "a performance of a piece the caller may not see")
}

func TestFromEditor_UnknownReferences(t *testing.T) {
	t.Parallel()
	fx := newEditorRefFixture()
	unknown := "not-a-slug"

	piece := ToEditorPiece(editorPieceDocument(), fx.slugs)
	piece.Terms[0].Term = unknown
	piece.Relations = append(piece.Relations, EditorPieceRelation{Piece: unknown, Type: PieceRelationTypeVariantOf})
	piece.Credits[0].Person = &unknown
	piece.Links = append(piece.Links, EditorPieceLink{DocLink: DocLink{URL: "https://example.org/linnet/2",
		CollectionID: new(int64(999)), Roles: []LinkRole{LinkRoleLyrics}}, Performances: []EditorPerformance{}})
	piece.Links[0].Performances[0].Event = &unknown
	piece.Links[0].Performances[0].Performers = []string{editorPersonSlug, unknown}
	doc, problems := FromEditorPiece(piece, fx.refs)
	assert.ElementsMatch(t, []DocumentProblem{
		{Path: "/terms/0/term", Code: ProblemUnknownReference},
		{Path: "/relations/1/piece", Code: ProblemUnknownReference},
		{Path: "/credits/0/person", Code: ProblemUnknownReference},
		{Path: "/links/1/collection_id", Code: ProblemUnknownReference},
		{Path: "/links/0/performances/0/event", Code: ProblemUnknownReference},
		{Path: "/links/0/performances/0/performers/1", Code: ProblemUnknownReference},
	}, problems)
	assert.Empty(t, doc.Terms)
	assert.Len(t, doc.Credits, 1)
	assert.Equal(t, []int64{editorPersonID}, doc.Links[0].Performances[0].PerformerIDs)

	person := ToEditorPerson(editorPersonDocument(), fx.slugs)
	person.Affiliations = []string{unknown}
	person.Relations[0].Person = unknown
	person.Links[0].Performances[0].Piece = &unknown
	_, problems = FromEditorPerson(person, fx.refs)
	assert.ElementsMatch(t, []DocumentProblem{
		{Path: "/affiliations/0", Code: ProblemUnknownReference},
		{Path: "/relations/0/person", Code: ProblemUnknownReference},
		{Path: "/links/0/performances/0/piece", Code: ProblemUnknownReference},
	}, problems)

	_, problems = FromEditorTerm(EditorTermDocument{Parent: &unknown, RelatedAffiliation: &unknown}, fx.refs)
	assert.ElementsMatch(t, []DocumentProblem{
		{Path: "/parent", Code: ProblemUnknownReference}, {Path: "/related_affiliation", Code: ProblemUnknownReference},
	}, problems)

	_, problems = FromEditorCollection(EditorCollectionDocument{Region: &unknown, ParentID: new(int64(999))}, fx.refs)
	assert.ElementsMatch(t, []DocumentProblem{
		{Path: "/region", Code: ProblemUnknownReference}, {Path: "/parent_id", Code: ProblemUnknownReference},
	}, problems)
}

func TestEditorDocuments_References(t *testing.T) {
	t.Parallel()
	fx := newEditorRefFixture()

	refs := ToEditorPiece(editorPieceDocument(), fx.slugs).References()
	assert.Equal(t, []string{editorTermSlug}, refs.TermSlugs)
	assert.Equal(t, []string{editorPieceSlug}, refs.PieceSlugs)
	assert.ElementsMatch(t, []string{editorPersonSlug, editorPersonSlug}, refs.PersonSlugs)
	assert.Equal(t, []string{editorEventSlug}, refs.EventSlugs)
	assert.Equal(t, []int64{editorCollectionID}, refs.CollectionIDs)

	personRefs := ToEditorPerson(editorPersonDocument(), fx.slugs).References()
	assert.Equal(t, []string{editorTermSlug}, personRefs.TermSlugs)
	assert.Equal(t, []string{editorPieceSlug}, personRefs.PieceSlugs)

	termRefs := ToEditorTerm(TermDocument{ParentID: new(editorTermID)}, fx.slugs).References()
	assert.Equal(t, []string{editorTermSlug}, termRefs.TermSlugs)

	collectionRefs := ToEditorCollection(CollectionDocument{RegionTermID: new(editorTermID),
		ParentID: new(editorCollectionID)}, fx.slugs).References()
	assert.Equal(t, []string{editorTermSlug}, collectionRefs.TermSlugs)
	assert.Equal(t, []int64{editorCollectionID}, collectionRefs.CollectionIDs)
}

func TestEditorDocuments_ListsArePresent(t *testing.T) {
	t.Parallel()
	piece := ToEditorPiece(PieceDocument{}, SlugMap{})
	require.NotNil(t, piece.SecondaryKinds)
	assert.NotNil(t, piece.Aliases)
	assert.NotNil(t, piece.Terms)
	assert.NotNil(t, piece.Links)
	person := ToEditorPerson(PersonDocument{}, SlugMap{})
	assert.NotNil(t, person.Affiliations)
	assert.NotNil(t, person.Roles)
}

func TestDocumentEntityTypes(t *testing.T) {
	t.Parallel()
	docs := []AnyDocument{PieceDocument{}, PersonDocument{}, TermDocument{}, CollectionDocument{}, EventDocument{}}
	for i, want := range entityTypeValues {
		assert.Equal(t, want, docs[i].EntityType())
	}
}
