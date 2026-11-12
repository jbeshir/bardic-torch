package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The IDs of the entities the snapshot tests' documents reference. Each "hidden" one is in no map of
// testVisibility, so it is not public now.
const (
	publicPieceID int64 = iota + 1
	hiddenPieceID
	subjectPersonID
	publicPersonID
	noPagePersonID
	hiddenPersonID
	publicTermID
	hiddenTermID
	publicCollectionID
	hiddenCollectionID
	publicEventID
	hiddenEventID
)

const (
	visibleURL = "https://example.org/visible"
	hiddenURL  = "https://example.org/hidden"
	otherURL   = "https://example.org/other"
	// deletedURL is no link's URL now, and recasedURL differs from visibleURL in case only: a view shows both as
	// written, as they are not a hidden link's.
	deletedURL = "https://example.org/deleted"
	recasedURL = "https://example.org/Visible"
	revelName  = "Midwinter Revel"
	wrenName   = "Wren of the Vale"
	wrenSlug   = "wren-of-the-vale"
	stageName  = "Wren"
)

var (
	brassKettle = PieceSummary{Slug: "the-brass-kettle", Title: "The Brass Kettle", Kind: PieceKindSong,
		ConservationStatus: ConservationStatusLowRisk}
	hollis      = PersonSummary{Slug: "hollis-fenn", Name: "Hollis Fenn", Kind: PersonKindCommunityBard}
	quietPerson = PersonSummary{Slug: "quiet-one", Name: "Quiet One", Kind: PersonKindCommunityBard, IsStub: true,
		NoLink: true}
	war      = TermSummary{Slug: "war", Facet: FacetTopic, DisplayName: "War"}
	songbook = CollectionSummary{ID: publicCollectionID, Name: "The Northmarch Songbook",
		Kind: CollectionKindSongbookPDF}
	revel = EventSummary{Slug: "midwinter-revel", Name: revelName, Kind: EventKindEvent}
)

// testVisibility is the current state of the referenced entities: one public entity of each type, a person who
// prefers no page, and a hidden link: hiddenURL.
func testVisibility() SnapshotVisibility {
	return SnapshotVisibility{
		Pieces:      map[int64]PieceSummary{publicPieceID: brassKettle},
		Persons:     map[int64]PersonSummary{publicPersonID: hollis, noPagePersonID: quietPerson},
		Terms:       map[int64]TermSummary{publicTermID: war},
		Collections: map[int64]CollectionSummary{publicCollectionID: songbook},
		Events:      map[int64]EventSummary{publicEventID: revel},
		HiddenLinks: map[string]bool{hiddenURL: true},
	}
}

func id(v int64) *int64 { return &v }

func TestNewPieceSnapshot(t *testing.T) {
	t.Parallel()

	doc := PieceDocument{
		SchemaVersion:  DocumentSchemaVersion,
		Title:          "The Copper Kettle",
		Kind:           PieceKindSong,
		SecondaryKinds: []PieceKind{PieceKindDanceMusic},
		Conservation: DocConservation{Status: ConservationStatusEndangered,
			SourceURL: new("https://example.org/assessment")},
		IsStub:          true,
		NotesMD:         new("Notes"),
		LicensingStatus: LicensingStatusKnown,
		Licence:         new(LicenceCCBY),
		LicenceVersion:  new(LicenceVersion40),
		PeriodStart:     new(int16(1580)),
		Aliases:         []DocPieceAlias{{Text: "Kettle Song", Kind: PieceAliasKindNickname}},
		Terms: []DocPieceTerm{
			{TermID: hiddenTermID, IsPrimary: true},
			{TermID: publicTermID, Part: new(PiecePartWords)},
		},
		Relations: []DocPieceRelation{
			{ToPieceID: hiddenPieceID, Type: PieceRelationTypeVariantOf},
			{ToPieceID: publicPieceID, Type: PieceRelationTypeSettingOf},
		},
		Credits: []DocCredit{
			{PersonID: id(publicPersonID), Role: CreditRoleLyrics, Attribution: AttributionNamed,
				Certainty: CertaintyCertain},
			{PersonID: id(noPagePersonID), Role: CreditRoleTune, Attribution: AttributionNamed,
				Certainty: CertaintyCertain, Note: new("by the quiet one")},
			{PersonID: id(hiddenPersonID), Role: CreditRoleMusic, Attribution: AttributionNamed,
				Certainty: CertaintyProbable},
			{Role: CreditRoleArrangement, Attribution: AttributionNamed, Certainty: CertaintyCertain},
			// A credit that is not named never shows a person, even one it names.
			{PersonID: id(publicPersonID), Role: CreditRoleTune, Attribution: AttributionTraditional,
				Certainty: CertaintyDisputed, Part: new(PiecePartChorus)},
		},
		Links: []DocPieceLink{
			{DocLink: DocLink{URL: hiddenURL, Title: new("Hidden"), Roles: []LinkRole{LinkRoleLyrics}},
				Performances: []DocPerformance{{DescriptionVerbatim: "Hidden performance"}}},
			{
				DocLink: DocLink{URL: visibleURL, Title: new("Visible"), Platform: new("example.org"),
					PlaceType: new(LinkPlaceTypeCommunityArchive), CollectionID: id(publicCollectionID),
					Locator: new("p. 12"), Roles: []LinkRole{LinkRoleLyrics, LinkRoleSheetMusic}},
				Performances: []DocPerformance{
					{EventID: id(publicEventID), Year: new(int16(2025)), Uploader: new("Northmarch Bards"),
						DescriptionVerbatim: "At the revel",
						PerformerIDs:        []int64{publicPersonID, noPagePersonID, hiddenPersonID}},
					{EventID: id(hiddenEventID), DescriptionVerbatim: "Elsewhere"},
				},
			},
			{DocLink: DocLink{URL: otherURL, CollectionID: id(hiddenCollectionID)}},
			{DocLink: DocLink{URL: deletedURL, Title: new("Deleted")}},
			{DocLink: DocLink{URL: recasedURL, Title: new("Recased")}},
		},
	}

	assert.Equal(t, PieceSnapshot{
		Title:                 "The Copper Kettle",
		Kind:                  PieceKindSong,
		SecondaryKinds:        []PieceKind{PieceKindDanceMusic},
		ConservationStatus:    ConservationStatusEndangered,
		ConservationSourceURL: new("https://example.org/assessment"),
		IsStub:                true,
		NotesMD:               new("Notes"),
		LicensingStatus:       LicensingStatusKnown,
		Licence:               new(LicenceCCBY),
		LicenceVersion:        new(LicenceVersion40),
		PeriodStart:           new(int16(1580)),
		Aliases:               []PieceAlias{{Text: "Kettle Song", Kind: PieceAliasKindNickname}},
		Terms:                 []PieceTerm{{Term: war, Part: new(PiecePartWords)}},
		Credits: []PieceCredit{
			{Role: CreditRoleLyrics, Attribution: AttributionNamed, Certainty: CertaintyCertain, Person: &hollis},
			{Role: CreditRoleTune, Attribution: AttributionNamed, Certainty: CertaintyCertain,
				Note: new("by the quiet one"), Person: &quietPerson},
			{Role: CreditRoleTune, Attribution: AttributionTraditional, Certainty: CertaintyDisputed,
				Part: new(PiecePartChorus)},
		},
		Relations: []SnapshotPieceRelation{{Type: PieceRelationTypeSettingOf, Piece: brassKettle}},
		Links: []SnapshotPieceLink{
			{
				SnapshotLink: SnapshotLink{URL: visibleURL, Title: new("Visible"), Platform: new("example.org"),
					PlaceType:  new(LinkPlaceTypeCommunityArchive),
					Collection: &CollectionRef{ID: publicCollectionID, Name: songbook.Name},
					Locator:    new("p. 12"), Roles: []LinkRole{LinkRoleLyrics, LinkRoleSheetMusic}},
				Performances: []PiecePerformance{
					{Year: new(int16(2025)), Uploader: new("Northmarch Bards"), DescriptionVerbatim: "At the revel",
						Event: &revel, Performers: []PersonSummary{hollis, quietPerson}},
					{DescriptionVerbatim: "Elsewhere"},
				},
			},
			{SnapshotLink: SnapshotLink{URL: otherURL}},
			{SnapshotLink: SnapshotLink{URL: deletedURL, Title: new("Deleted")}},
			{SnapshotLink: SnapshotLink{URL: recasedURL, Title: new("Recased")}},
		},
	}, NewPieceSnapshot(doc, testVisibility()))

	var refs SnapshotReferences
	doc.AddReferences(&refs)
	assert.Equal(t, SnapshotReferences{
		PieceIDs: []int64{hiddenPieceID, publicPieceID},
		PersonIDs: []int64{publicPersonID, noPagePersonID, hiddenPersonID, publicPersonID, publicPersonID,
			noPagePersonID, hiddenPersonID},
		TermIDs:       []int64{hiddenTermID, publicTermID},
		CollectionIDs: []int64{publicCollectionID, hiddenCollectionID},
		EventIDs:      []int64{publicEventID, hiddenEventID},
		LinkURLs:      []string{hiddenURL, visibleURL, otherURL, deletedURL, recasedURL},
	}, refs)
}

// personDocument is a person document referencing a public and a hidden entity of every type it can, with a
// couple of aliases.
func personDocument() PersonDocument {
	return PersonDocument{
		SchemaVersion:    DocumentSchemaVersion,
		Name:             wrenName,
		Kind:             PersonKindCommunityBard,
		Status:           PersonStatusActive,
		StatusDate:       &Date{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		StatusSourceURL:  new("https://example.org/status"),
		Pronouns:         new("she/her"),
		NotesMD:          new("A bard."),
		LicensingStatus:  LicensingStatusKnown,
		LicensingNotesMD: new("Sing freely."),
		Aliases: []DocPersonAlias{
			{Name: stageName, Kind: PersonAliasKindStage},
			{Name: "Wren the Elder", Kind: PersonAliasKindFormer},
		},
		Roles:          []PersonRole{PersonRoleSinger, PersonRolePoet},
		AffiliationIDs: []int64{hiddenTermID, publicTermID},
		Relations: []DocPersonRelation{
			{ToPersonID: noPagePersonID, Type: PersonRelationTypeHeirOf},
			{ToPersonID: hiddenPersonID, Type: PersonRelationTypeSameAs},
			{ToPersonID: publicPersonID, Type: PersonRelationTypeMemberOf},
		},
		Links: []DocPersonLink{
			{DocLink: DocLink{URL: hiddenURL, Roles: []LinkRole{LinkRoleWebsite}}},
			{
				DocLink: DocLink{URL: visibleURL, CollectionID: id(hiddenCollectionID),
					Roles: []LinkRole{LinkRoleWebsite}},
				Performances: []DocPersonPerformance{
					{PieceID: id(hiddenPieceID), DocPerformance: DocPerformance{DescriptionVerbatim: "Of a draft"}},
					{PieceID: id(publicPieceID), DocPerformance: DocPerformance{DescriptionVerbatim: "Of the Kettle",
						EventID: id(publicEventID), PerformerIDs: []int64{subjectPersonID, publicPersonID}}},
					{DocPerformance: DocPerformance{DescriptionVerbatim: "A tale", EventID: id(hiddenEventID)}},
				},
			},
		},
	}
}

func TestNewPersonSnapshot(t *testing.T) {
	t.Parallel()

	vis := testVisibility()
	vis.Persons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName,
		Kind: PersonKindCommunityBard}
	statusDate := Date{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}

	snapshot, err := NewPersonSnapshot(subjectPersonID, personDocument(), vis)
	require.NoError(t, err)
	assert.Equal(t, PersonSnapshot{
		Name:            wrenName,
		Kind:            PersonKindCommunityBard,
		Status:          PersonStatusActive,
		StatusDate:      &statusDate,
		StatusSourceURL: new("https://example.org/status"),
		Profile: &PersonSnapshotProfile{
			Pronouns:         new("she/her"),
			NotesMD:          new("A bard."),
			LicensingStatus:  LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases: []PersonAlias{{Name: stageName, Kind: PersonAliasKindStage},
				{Name: "Wren the Elder", Kind: PersonAliasKindFormer}},
			Roles:        []PersonRole{PersonRoleSinger, PersonRolePoet},
			Affiliations: []TermSummary{war},
			Relations: []SnapshotPersonRelation{{Type: PersonRelationTypeHeirOf, Person: quietPerson},
				{Type: PersonRelationTypeMemberOf, Person: hollis}},
			Links: []SnapshotPersonLink{{
				SnapshotLink: SnapshotLink{URL: visibleURL, Roles: []LinkRole{LinkRoleWebsite}},
				Performances: []SnapshotPersonPerformance{
					{Piece: &brassKettle, PiecePerformance: PiecePerformance{DescriptionVerbatim: "Of the Kettle",
						Event: &revel, Performers: []PersonSummary{vis.Persons[subjectPersonID], hollis}}},
					{PiecePerformance: PiecePerformance{DescriptionVerbatim: "A tale"}},
				},
			}},
		},
	}, snapshot)

	var refs SnapshotReferences
	personDocument().AddReferences(subjectPersonID, &refs)
	assert.Equal(t, SnapshotReferences{
		PieceIDs: []int64{hiddenPieceID, publicPieceID},
		PersonIDs: []int64{subjectPersonID, noPagePersonID, hiddenPersonID, publicPersonID, subjectPersonID,
			publicPersonID},
		TermIDs:       []int64{hiddenTermID, publicTermID},
		CollectionIDs: []int64{hiddenCollectionID},
		EventIDs:      []int64{publicEventID, hiddenEventID},
		LinkURLs:      []string{hiddenURL, visibleURL},
	}, refs)
}

// TestNewPersonSnapshot_NoPage checks that the snapshot of a person who prefers no page now, or did in the
// document, shows the identity only.
func TestNewPersonSnapshot_NoPage(t *testing.T) {
	t.Parallel()

	identity := PersonSnapshot{
		Name:            wrenName,
		Kind:            PersonKindCommunityBard,
		Status:          PersonStatusActive,
		StatusDate:      &Date{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		StatusSourceURL: new("https://example.org/status"),
		IsStub:          true,
	}

	t.Run("now", func(t *testing.T) {
		t.Parallel()

		vis := testVisibility()
		vis.Persons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName,
			Kind: PersonKindCommunityBard, IsStub: true, NoLink: true}
		snapshot, err := NewPersonSnapshot(subjectPersonID, personDocument(), vis)
		require.NoError(t, err)
		assert.Equal(t, identity, snapshot)
	})

	t.Run("in_the_document", func(t *testing.T) {
		t.Parallel()

		doc := personDocument()
		doc.Status = PersonStatusPrefersNoPage
		vis := testVisibility()
		vis.Persons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName,
			Kind: PersonKindCommunityBard}
		snapshot, err := NewPersonSnapshot(subjectPersonID, doc, vis)
		require.NoError(t, err)
		want := identity
		want.Status = PersonStatusPrefersNoPage
		assert.Equal(t, want, snapshot)
	})
}

// TestNewPersonSnapshot_NotPublic checks that the view of a person the visibility does not hold fails, as their
// current name, which a redacted name is replaced with, is unknown.
func TestNewPersonSnapshot_NotPublic(t *testing.T) {
	t.Parallel()

	_, err := NewPersonSnapshot(subjectPersonID, personDocument(), testVisibility())
	require.ErrorIs(t, err, ErrPersonNotPublic)
}

func TestNewTermSnapshot(t *testing.T) {
	t.Parallel()

	doc := TermDocument{
		SchemaVersion: DocumentSchemaVersion, Facet: FacetTopic, Name: "Swords (topic)", DisplayName: "Swords",
		ParentID: id(publicTermID), RelatedAffiliationID: id(hiddenTermID), DescriptionMD: new("Blades."),
		Position: 3, Aliases: []string{"Blades"},
	}
	assert.Equal(t, TermSnapshot{
		Facet: FacetTopic, Name: "Swords (topic)", DisplayName: "Swords", Parent: &war,
		DescriptionMD: new("Blades."), Position: 3, Aliases: []string{"Blades"},
	}, NewTermSnapshot(doc, testVisibility()))

	var refs SnapshotReferences
	doc.AddReferences(&refs)
	assert.Equal(t, SnapshotReferences{TermIDs: []int64{publicTermID, hiddenTermID}}, refs)
}

func TestNewCollectionSnapshot(t *testing.T) {
	t.Parallel()

	doc := CollectionDocument{
		SchemaVersion: DocumentSchemaVersion, Name: "Volume Two", URL: new("https://example.org/v2.pdf"),
		Kind: CollectionKindSongbookPDF, DescriptionMD: new("The second volume."), RegionTermID: id(hiddenTermID),
		ParentID: id(publicCollectionID), IsBardicResource: true, Enumerable: CollectionEnumerablePDF,
	}
	assert.Equal(t, CollectionSnapshot{
		Name: "Volume Two", URL: new("https://example.org/v2.pdf"), Kind: CollectionKindSongbookPDF,
		DescriptionMD: new("The second volume."), Parent: &songbook, IsBardicResource: true,
		Enumerable: CollectionEnumerablePDF,
	}, NewCollectionSnapshot(doc, testVisibility()))

	var refs SnapshotReferences
	doc.AddReferences(&refs)
	assert.Equal(t, SnapshotReferences{TermIDs: []int64{hiddenTermID}, CollectionIDs: []int64{publicCollectionID}},
		refs)
}

func TestNewEventSnapshot(t *testing.T) {
	t.Parallel()

	assert.Equal(t, EventSnapshot{Name: revelName, Kind: EventKindVenue, DescriptionMD: new("A hall.")},
		NewEventSnapshot(EventDocument{SchemaVersion: DocumentSchemaVersion, Name: revelName,
			Kind: EventKindVenue, DescriptionMD: new("A hall.")}))
}

// TestVisibleLicenceVersion checks that a licence version is shown only beside a Creative Commons licence.
func TestVisibleLicenceVersion(t *testing.T) {
	t.Parallel()

	version := new(LicenceVersion40)
	assert.Equal(t, version, VisibleLicenceVersion(new(LicenceCCBYSA), version))
	assert.Nil(t, VisibleLicenceVersion(new(LicenceARR), version))
	assert.Nil(t, VisibleLicenceVersion(nil, version))
}
