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
	prefsURL   = "https://example.org/prefs"
	// respelledHiddenURL normalizes to hiddenURL.
	respelledHiddenURL = "HTTPS://Example.org/hidden?utm_source=x"
	otherURL           = "https://example.org/other"
	// deletedURL is no link's URL now, and recasedURL differs from visibleURL in case only: neither is visible.
	deletedURL  = "https://example.org/deleted"
	recasedURL  = "https://example.org/Visible"
	revelName   = "Midwinter Revel"
	wrenName    = "Wren of the Vale"
	wrenSlug    = "wren-of-the-vale"
	stageName   = "Wren"
	hiddenAlias = "Jo Marker"
)

var (
	brassKettle = PieceSummary{Slug: "the-brass-kettle", Title: "The Brass Kettle", Kind: PieceKindSong,
		ConservationStatus: ConservationStatusLowRisk}
	hollis      = PersonSummary{Slug: "hollis-fenn", Name: "Hollis Fenn", Kind: PersonKindCommunityBard}
	quietPerson = PersonSummary{Slug: "quiet-one", Name: "Quiet One", Kind: PersonKindCommunityBard, IsStub: true}
	war         = TermSummary{Slug: "war", Facet: FacetTopic, DisplayName: "War"}
	songbook    = CollectionSummary{ID: publicCollectionID, Name: "The Northmarch Songbook",
		Kind: CollectionKindSongbookPDF}
	revel = EventSummary{Slug: "midwinter-revel", Name: revelName, Kind: EventKindEvent}
)

// testVisibility is the current state of the referenced entities: one public entity of each type, a no-page
// person, the subject person's current public alias, and two visible links: hiddenURL is a hidden link's.
func testVisibility() SnapshotVisibility {
	return SnapshotVisibility{
		Pieces:        map[int64]PieceSummary{publicPieceID: brassKettle},
		Persons:       map[int64]PersonSummary{publicPersonID: hollis},
		NoPagePersons: map[int64]PersonSummary{noPagePersonID: quietPerson},
		PublicAliases: map[int64]map[string]bool{subjectPersonID: {stageName: true}},
		Terms:         map[int64]TermSummary{publicTermID: war},
		Collections:   map[int64]CollectionSummary{publicCollectionID: songbook},
		Events:        map[int64]EventSummary{publicEventID: revel},
		VisibleLinks:  map[string]string{visibleURL: visibleURL, otherURL: otherURL},
		HiddenSources: map[string]bool{hiddenURL: true},
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
		Preferences: []DocPreference{
			preference(PreferenceDimensionPerformancePermission, hiddenURL, PreferenceChoiceGiven),
			preference(PreferenceDimensionRecording, "https://exa mple.org/prefs", PreferenceChoiceNoRecording),
			preference(PreferenceDimensionAttribution, prefsURL, PreferenceChoiceSCAName),
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
		PeriodStart:           new(int16(1580)),
		Aliases:               []PieceAlias{{Text: "Kettle Song", Kind: PieceAliasKindNickname}},
		Terms:                 []PieceTerm{{Term: war, Part: new(PiecePartWords)}},
		Credits: []PieceCredit{
			{Role: CreditRoleLyrics, Attribution: AttributionNamed, Certainty: CertaintyCertain, Person: &hollis},
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
						Event: &revel, Performers: []PersonSummary{hollis}},
					{DescriptionVerbatim: "Elsewhere"},
				},
			},
			{SnapshotLink: SnapshotLink{URL: otherURL}},
		},
		Preferences: []DocPreference{preference(PreferenceDimensionAttribution, prefsURL, PreferenceChoiceSCAName)},
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
		SourceURLs:    []string{hiddenURL, "https://exa mple.org/prefs", prefsURL, "https://example.org/assessment"},
	}, refs)
}

// personDocument is a person document referencing a public and a hidden entity of every type it can, with a
// public alias that is public now, one that is no longer public, and a hidden one that is public now.
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
			{Name: stageName, Kind: PersonAliasKindStage, IsPublic: true},
			{Name: "Wren the Elder", Kind: PersonAliasKindFormer, IsPublic: true},
			{Name: hiddenAlias, Kind: PersonAliasKindLegal},
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
		Preferences: []DocPreference{
			preference(PreferenceDimensionPerformancePermission, respelledHiddenURL, PreferenceChoiceGiven),
			preference(PreferenceDimensionRecording, prefsURL, PreferenceChoiceNoRecording),
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
			// "Wren the Elder" was public in the document but is not now; the legal alias is hidden in it.
			Aliases:      []PersonAlias{{Name: stageName, Kind: PersonAliasKindStage}},
			Roles:        []PersonRole{PersonRoleSinger, PersonRolePoet},
			Affiliations: []TermSummary{war},
			Relations:    []SnapshotPersonRelation{{Type: PersonRelationTypeMemberOf, Person: hollis}},
			Links: []SnapshotPersonLink{{
				SnapshotLink: SnapshotLink{URL: visibleURL, Roles: []LinkRole{LinkRoleWebsite}},
				Performances: []SnapshotPersonPerformance{
					{Piece: &brassKettle, PiecePerformance: PiecePerformance{DescriptionVerbatim: "Of the Kettle",
						Event: &revel, Performers: []PersonSummary{vis.Persons[subjectPersonID], hollis}}},
					{PiecePerformance: PiecePerformance{DescriptionVerbatim: "A tale"}},
				},
			}},
			Preferences: []DocPreference{preference(PreferenceDimensionRecording, prefsURL, PreferenceChoiceNoRecording)},
		},
	}, snapshot)

	var refs SnapshotReferences
	personDocument().AddReferences(subjectPersonID, &refs)
	assert.Equal(t, SnapshotReferences{
		PieceIDs: []int64{hiddenPieceID, publicPieceID},
		PersonIDs: []int64{subjectPersonID, noPagePersonID, hiddenPersonID, publicPersonID, subjectPersonID,
			publicPersonID},
		TermIDs:        []int64{hiddenTermID, publicTermID},
		CollectionIDs:  []int64{hiddenCollectionID},
		EventIDs:       []int64{publicEventID, hiddenEventID},
		LinkURLs:       []string{hiddenURL, visibleURL},
		SourceURLs:     []string{respelledHiddenURL, prefsURL, "https://example.org/status"},
		AliasPersonIDs: []int64{subjectPersonID},
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
		vis.NoPagePersons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName,
			Kind: PersonKindCommunityBard, IsStub: true}
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

// TestNewPersonSnapshot_Name checks that a snapshot shows the document's name only if it is the person's name
// now, or one of their public alias names now, compared exactly, and their current name otherwise: a former name
// that is now a hidden alias must not show, with or without a page.
func TestNewPersonSnapshot_Name(t *testing.T) {
	t.Parallel()

	const currentName = "Wren Ashdown"
	current := PersonSummary{Slug: "wren-ashdown", Name: currentName, Kind: PersonKindCommunityBard}
	cases := []struct {
		name    string
		docName string
		noPage  bool
		want    string
	}{
		{name: "current_name", docName: currentName, want: currentName},
		{name: "public_alias", docName: stageName, want: stageName},
		{name: "hidden_alias", docName: hiddenAlias, want: currentName},
		{name: "unknown_name", docName: "Wren the Elder", want: currentName},
		{name: "current_name_recased", docName: "wren ashdown", want: currentName},
		{name: "public_alias_recased", docName: "WREN", want: currentName},
		{name: "no_page_hidden_alias", docName: hiddenAlias, noPage: true, want: currentName},
		{name: "no_page_public_alias", docName: stageName, noPage: true, want: stageName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			vis := testVisibility()
			if tc.noPage {
				vis.NoPagePersons[subjectPersonID] = current
			} else {
				vis.Persons[subjectPersonID] = current
			}
			doc := personDocument()
			doc.Name = tc.docName
			snapshot, err := NewPersonSnapshot(subjectPersonID, doc, vis)
			require.NoError(t, err)
			assert.Equal(t, tc.want, snapshot.Name)
			assert.Equal(t, tc.noPage, snapshot.Profile == nil)
		})
	}
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

// TestNewPieceSnapshot_LinkMatchedByNormalizedURL checks that a document's link is shown, under the visible link's
// URL, when its normalized form is a visible link's, even though the URL as entered is no link's.
func TestNewPieceSnapshot_LinkMatchedByNormalizedURL(t *testing.T) {
	t.Parallel()

	doc := PieceDocument{Links: []DocPieceLink{
		{DocLink: DocLink{URL: "https://Example.org:443/visible?utm_source=share"}},
		{DocLink: DocLink{URL: "https://example.org/Visible?utm_source=share"}},
	}}

	snapshot := NewPieceSnapshot(doc, testVisibility())

	assert.Equal(t, []SnapshotPieceLink{{SnapshotLink: SnapshotLink{URL: visibleURL}}}, snapshot.Links)
}

// TestNewPieceSnapshot_LinkShownUnderVisibleLinkURL checks that a document's link is shown under the visible
// link's URL, not the document's spelling, when the two differ only in a tracking parameter normalizing drops: a
// moderator can take down only the visible link, so the view shows nothing it does not hold.
func TestNewPieceSnapshot_LinkShownUnderVisibleLinkURL(t *testing.T) {
	t.Parallel()

	const linkURL = visibleURL + "?utm_source=share"
	vis := testVisibility()
	vis.VisibleLinks = map[string]string{visibleURL: linkURL}
	doc := PieceDocument{Links: []DocPieceLink{{DocLink: DocLink{URL: linkURL + "&utm_content=wren"}}}}

	snapshot := NewPieceSnapshot(doc, vis)

	assert.Equal(t, []SnapshotPieceLink{{SnapshotLink: SnapshotLink{URL: linkURL}}}, snapshot.Links)
}

// TestNewPieceSnapshot_UnnormalizableLinkDropped checks that a document's link whose URL does not normalize is
// never shown, even if the visible set holds it as entered.
func TestNewPieceSnapshot_UnnormalizableLinkDropped(t *testing.T) {
	t.Parallel()

	const unnormalizable = "mailto:wren@example.invalid"
	vis := testVisibility()
	vis.VisibleLinks = map[string]string{unnormalizable: unnormalizable, visibleURL: visibleURL}
	doc := PieceDocument{Links: []DocPieceLink{{DocLink: DocLink{URL: unnormalizable}}, {DocLink: DocLink{URL: visibleURL}}}}

	snapshot := NewPieceSnapshot(doc, vis)

	assert.Equal(t, []SnapshotPieceLink{{SnapshotLink: SnapshotLink{URL: visibleURL}}}, snapshot.Links)
}

// personLinks returns the links of the view of a person document that holds only links, with the subject person
// public.
func personLinks(t *testing.T, links []DocPersonLink, vis SnapshotVisibility) []SnapshotPersonLink {
	t.Helper()

	vis.Persons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName, Kind: PersonKindCommunityBard}
	doc := PersonDocument{SchemaVersion: DocumentSchemaVersion, Name: wrenName, Kind: PersonKindCommunityBard,
		Status: PersonStatusActive, Links: links}
	snapshot, err := NewPersonSnapshot(subjectPersonID, doc, vis)
	require.NoError(t, err)
	require.NotNil(t, snapshot.Profile)

	return snapshot.Profile.Links
}

// TestNewPersonSnapshot_LinkMatchedByNormalizedURL checks that a person document's link is shown, under the
// visible link's URL, when its normalized form is a visible link's, even though the URL as entered is no link's.
func TestNewPersonSnapshot_LinkMatchedByNormalizedURL(t *testing.T) {
	t.Parallel()

	const linkURL = visibleURL + "?utm_source=share"
	vis := testVisibility()
	vis.VisibleLinks = map[string]string{visibleURL: linkURL}
	links := []DocPersonLink{
		{DocLink: DocLink{URL: "https://Example.org:443/visible?utm_source=share&utm_content=wren"}},
		{DocLink: DocLink{URL: "https://example.org/Visible?utm_source=share"}},
	}

	assert.Equal(t, []SnapshotPersonLink{{SnapshotLink: SnapshotLink{URL: linkURL}}}, personLinks(t, links, vis))
}

// TestNewPersonSnapshot_UnnormalizableLinkDropped checks that a person document's link whose URL does not
// normalize is never shown, even if the visible set holds it as entered.
func TestNewPersonSnapshot_UnnormalizableLinkDropped(t *testing.T) {
	t.Parallel()

	const unnormalizable = "mailto:wren@example.invalid"
	vis := testVisibility()
	vis.VisibleLinks = map[string]string{unnormalizable: unnormalizable, visibleURL: visibleURL}
	links := []DocPersonLink{{DocLink: DocLink{URL: unnormalizable}}, {DocLink: DocLink{URL: visibleURL}}}

	assert.Equal(t, []SnapshotPersonLink{{SnapshotLink: SnapshotLink{URL: visibleURL}}}, personLinks(t, links, vis))
}
