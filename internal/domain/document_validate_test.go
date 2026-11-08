package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The paths and names the validation tests use more than once.
const (
	pathTitle                 = "/title"
	pathName                  = "/name"
	pathKind                  = "/kind"
	pathNotes                 = "/notes_md"
	pathSummary               = "/summary"
	pathAliases               = "/aliases"
	pathSecondAlias           = "/aliases/1"
	pathNewTerm               = "/terms/2/term"
	pathFirstAttributedPerson = "/credits/0/person"
	pathLicence               = "/licence"
	pathLicensingStatus       = "/licensing_status"
	pathTermParent            = "/parent"
	pathRelatedAffiliation    = "/related_affiliation"
	pathCollectionParent      = "/parent_id"
	pathRegion                = "/region"
	pathLicensingLinkURL      = "/links/1/url"
	pathStatusSource          = "/status_source_url"
	pathConservationSource    = "/conservation/source_url"
	pathPreferenceSource      = "/preferences/0/source_url"
	moodName                  = "Merry"
	secondAliasName           = "Morwen of the Hill"
	legalName                 = "Wren Q. Placeholder"
	morwenTheBard             = "Morwen the Bard"
	otherLegalName            = "Wren R. Placeholder"
	testSummary               = "An edit."
	pathFirstPerformanceEvent = "/links/0/performances/0/event"
	pathThirdLink             = "/links/2"
)

// The entities the validation fixtures reference, by ID.
const (
	refPiece          int64 = 1
	refOtherPiece     int64 = 2
	refPendingPiece   int64 = 3
	refPerson         int64 = 10
	refNoPagePerson   int64 = 11
	refOwnPerson      int64 = 12
	refOthersPerson   int64 = 13
	refMood           int64 = 20
	refOrigin         int64 = 21
	refAffiliation    int64 = 22
	refOwnTerm        int64 = 23
	refOthersTerm     int64 = 24
	refTopic          int64 = 25
	refMoodChild      int64 = 26
	refMoodGrandchild int64 = 27
	refMoodLeaf       int64 = 28
	refCollection     int64 = 30
	refChildColl      int64 = 31
	refEvent          int64 = 40
	refPendingEvent   int64 = 41
	unknownID         int64 = 99
)

var (
	public     = ReferencedEntity{Public: true}
	ownPending = ReferencedEntity{OwnPending: true}
	validToday = Date{time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}
)

func testRefs() ReferenceIndex {
	return ReferenceIndex{
		Pieces: map[int64]ReferencedEntity{refPiece: public, refOtherPiece: public, refPendingPiece: {}},
		Persons: map[int64]ReferencedPerson{
			refPerson: {ReferencedEntity: public}, refNoPagePerson: {ReferencedEntity: public, PrefersNoPage: true},
			refOwnPerson: {ReferencedEntity: ownPending}, refOthersPerson: {},
		},
		Terms: map[int64]ReferencedTerm{
			refMood:           {ReferencedEntity: public, Facet: FacetMood, Name: moodName},
			refOrigin:         {ReferencedEntity: public, Facet: FacetOrigin, Name: "Period"},
			refAffiliation:    {ReferencedEntity: public, Facet: FacetAffiliation, Name: "Northshield"},
			refOwnTerm:        {ReferencedEntity: ownPending, Facet: FacetMood},
			refOthersTerm:     {Facet: FacetMood},
			refTopic:          {ReferencedEntity: public, Facet: FacetTopic, Name: "Sea"},
			refMoodChild:      {ReferencedEntity: public, Facet: FacetMood, Name: "Jolly", ParentID: ptr(refMood)},
			refMoodGrandchild: {ReferencedEntity: public, Facet: FacetMood, Name: "Jaunty", ParentID: ptr(refMoodChild)},
			refMoodLeaf:       {ReferencedEntity: public, Facet: FacetMood, Name: "Brisk", ParentID: ptr(refMoodGrandchild)},
		},
		Collections: map[int64]ReferencedCollection{
			refCollection: {ReferencedEntity: public}, refChildColl: {ReferencedEntity: public, ParentID: ptr(refCollection)},
		},
		Events: map[int64]ReferencedEntity{refEvent: public, refPendingEvent: {}},
	}
}

func validPiece() PieceDocument {
	return PieceDocument{
		SchemaVersion: DocumentSchemaVersion, Title: "The Wren's Song", Kind: PieceKindSong,
		SecondaryKinds: []PieceKind{PieceKindPoem}, Conservation: DocConservation{Status: ConservationStatusUnassessed},
		LicensingStatus: LicensingStatusUnknown, NotesMD: ptr("Sung *slowly*.\n\nTwice."),
		Aliases:   []DocPieceAlias{{Text: "Wren Song", Kind: PieceAliasKindAltTitle}},
		Terms:     []DocPieceTerm{{TermID: refMood, IsPrimary: true}, {TermID: refOrigin, Part: ptr(PiecePartTune)}},
		Relations: []DocPieceRelation{{ToPieceID: refOtherPiece, Type: PieceRelationTypeVariantOf}},
		Credits: []DocCredit{
			{PersonID: ptr(refPerson), Role: CreditRoleLyrics, Attribution: AttributionNamed, Certainty: CertaintyCertain},
			{Role: CreditRoleTune, Attribution: AttributionTraditional, Certainty: CertaintyProbable},
		},
		Links: []DocPieceLink{{
			DocLink: DocLink{URL: "https://example.org/wren", Title: ptr("The Wren's Song"),
				CollectionID: ptr(refCollection), Roles: []LinkRole{LinkRoleLyrics}},
			Performances: []DocPerformance{{EventID: ptr(refEvent), Year: ptr(int16(2001)),
				DescriptionVerbatim: revelPerformance, PerformerIDs: []int64{refPerson}}},
		}},
	}
}

func validPerson() PersonDocument {
	return PersonDocument{
		SchemaVersion: DocumentSchemaVersion, Name: "Morwen Placeholder", Kind: PersonKindCommunityBard,
		Status: PersonStatusActive, LicensingStatus: LicensingStatusUnknown, NotesMD: ptr("Sings in the north."),
		Aliases:        []DocPersonAlias{{Name: secondAliasName, Kind: PersonAliasKindSCA, IsPublic: true}},
		Roles:          []PersonRole{PersonRoleSinger},
		AffiliationIDs: []int64{refAffiliation},
		Relations:      []DocPersonRelation{{ToPersonID: refPerson, Type: PersonRelationTypeMemberOf}},
		Links: []DocPersonLink{{
			DocLink: DocLink{URL: "https://example.org/morwen", Roles: []LinkRole{LinkRoleWebsite}},
			Performances: []DocPersonPerformance{{PieceID: ptr(refPiece),
				DocPerformance: DocPerformance{DescriptionVerbatim: "At the summer revel", PerformerIDs: []int64{refPerson}}}},
		}},
	}
}

func validTerm() TermDocument {
	return TermDocument{
		SchemaVersion: DocumentSchemaVersion, Facet: FacetMood, Name: "Wistful", DisplayName: "Wistful",
		ParentID: ptr(refMood), Position: 3, Aliases: []string{"Pensive"},
	}
}

func validCollection() CollectionDocument {
	return CollectionDocument{
		SchemaVersion: DocumentSchemaVersion, Name: "Wren Songbook", URL: ptr("https://example.org/songbook"),
		Kind: CollectionKindSongbookPDF, RegionTermID: ptr(refAffiliation), ParentID: ptr(refCollection),
		Enumerable: CollectionEnumerableUnknown,
	}
}

func validEvent() EventDocument {
	return EventDocument{SchemaVersion: DocumentSchemaVersion, Name: "Wren Revel", Kind: EventKindEvent}
}

// The IDs the fixture documents are validated as.
const (
	pieceID      int64 = 5
	personID     int64 = 15
	termID       int64 = 29
	collectionID int64 = 35
)

func validationContext[D Document](id int64, prev *D, author Role) ValidationContext[D] {
	return ValidationContext[D]{Author: author, ID: id, Previous: prev, Refs: testRefs(), Summary: testSummary,
		Today: validToday}
}

// problemRow is a mutation of a valid document and the problem it must cause. A row with no code must cause no
// problem at all.
type problemRow[D Document] struct {
	name   string
	mutate func(*D)
	author Role
	path   string
	code   ProblemCode
}

// checkRows validates each row's document with the valid document as the comparison document (an edit), and
// requires the row's problem, or none. Every problem is checked to hold no value of the document.
func checkRows[D Document](t *testing.T, id int64, valid func() D, rows []problemRow[D]) {
	t.Helper()
	base := valid()
	require.Empty(t, validateDocument(base, validationContext(id, &base, RoleContributor)), "the valid document")
	require.Empty(t, validateDocument(base, validationContext[D](id, nil, RoleContributor)), "the valid document, new")
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			doc := valid()
			row.mutate(&doc)
			author := row.author
			if author == "" {
				author = RoleContributor
			}
			problems := validateDocument(doc, validationContext(id, &base, author))
			assertHoldsNoValues(t, doc, problems)
			if row.code == "" {
				assert.Empty(t, problems)

				return
			}
			assert.Contains(t, problems, DocumentProblem{Path: row.path, Code: row.code})
		})
	}
}

// assertHoldsNoValues requires that no problem's path or code holds a string of doc that is not a vocabulary
// value.
func assertHoldsNoValues(t *testing.T, doc any, problems []DocumentProblem) {
	t.Helper()
	walkStrings(reflect.ValueOf(doc), "", documentPaths, func(_ string, v reflect.Value) {
		if _, enum := v.Interface().(validator); enum || len(v.String()) < 4 {
			return
		}
		for _, p := range problems {
			assert.NotContains(t, p.Path, v.String())
			assert.NotContains(t, string(p.Code), v.String())
		}
	})
}

func TestPieceDocument_Validate(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 256)
	checkRows(t, pieceID, validPiece, []problemRow[PieceDocument]{
		{"blank title", func(d *PieceDocument) { d.Title = " \t" }, "", pathTitle, ProblemBlank},
		{"withheld placeholder", func(d *PieceDocument) { d.Title = "Song of " + WithheldName }, "", pathTitle,
			ProblemWithheldPlaceholder},
		{"long title", func(d *PieceDocument) { d.Title = long }, "", pathTitle, ProblemTooLong},
		{"bidi control", func(d *PieceDocument) { d.Title = "Wren \u202e Song" }, "", pathTitle,
			ProblemInvalidCharacters},
		{"format character in a new name", func(d *PieceDocument) { d.Title = "Wren\u200bSong" }, "", pathTitle,
			ProblemInvalidCharacters},
		{"not NFC", func(d *PieceDocument) { d.Title = "Cafe\u0301" }, "", pathTitle, ProblemNotNFC},
		{"control in notes", func(d *PieceDocument) { d.NotesMD = ptr("a\x1b[31m") }, "", pathNotes,
			ProblemInvalidCharacters},
		{"tabs and newlines in notes", func(d *PieceDocument) { d.NotesMD = ptr("a\tb\nc") }, "", "", ""},
		{"long notes", func(d *PieceDocument) { d.NotesMD = ptr(strings.Repeat("x", 65536)) }, "", pathNotes,
			ProblemTooLong},
		{"schema version", func(d *PieceDocument) { d.SchemaVersion = 2 }, "", "/schema_version", ProblemOutOfRange},
		{"unknown piece kind", func(d *PieceDocument) { d.Kind = "ballad" }, "", pathKind, ProblemOutOfRange},
		{"secondary kind is the kind", func(d *PieceDocument) { d.SecondaryKinds = []PieceKind{PieceKindSong} }, "",
			"/secondary_kinds/0", ProblemDuplicate},
		{"secondary kinds repeat", func(d *PieceDocument) {
			d.SecondaryKinds = []PieceKind{PieceKindPoem, PieceKindPoem}
		}, "", "/secondary_kinds/1", ProblemDuplicate},
		{"alias repeats by collation", func(d *PieceDocument) {
			d.Aliases = append(d.Aliases, DocPieceAlias{Text: "WREN SONG", Kind: PieceAliasKindNickname})
		}, "", pathSecondAlias, ProblemDuplicate},
		{"blank alias", func(d *PieceDocument) { d.Aliases[0].Text = "" }, "", "/aliases/0/text", ProblemBlank},
		{"too many aliases", func(d *PieceDocument) {
			d.Aliases = nil
			for k := range 101 {
				d.Aliases = append(d.Aliases, DocPieceAlias{Text: "Wren " + strings.Repeat("a", k+1),
					Kind: PieceAliasKindAltTitle})
			}
		}, "", pathAliases, ProblemTooMany},
		{"term repeats", func(d *PieceDocument) { d.Terms = append(d.Terms, DocPieceTerm{TermID: refMood}) }, "",
			"/terms/2", ProblemDuplicate},
		{"second primary term", func(d *PieceDocument) {
			d.Terms = append(d.Terms, DocPieceTerm{TermID: refMoodChild, IsPrimary: true})
		}, "", "/terms/2/is_primary", ProblemSecondPrimaryTerm},
		{"part on a mood term", func(d *PieceDocument) { d.Terms[0].Part = ptr(PiecePartWords) }, "", "/terms/0/part",
			ProblemPartNotAllowed},
		{"affiliation term added", func(d *PieceDocument) {
			d.Terms = append(d.Terms, DocPieceTerm{TermID: refAffiliation})
		}, "", pathNewTerm, ProblemWrongFacet},
		{"unknown term", func(d *PieceDocument) { d.Terms = append(d.Terms, DocPieceTerm{TermID: unknownID}) }, "",
			pathNewTerm, ProblemUnknownReference},
		{"another user's pending term", func(d *PieceDocument) {
			d.Terms = append(d.Terms, DocPieceTerm{TermID: refOthersTerm})
		}, "", pathNewTerm, ProblemUnknownReference},
		{"own pending term", func(d *PieceDocument) { d.Terms = append(d.Terms, DocPieceTerm{TermID: refOwnTerm}) },
			"", "", ""},
		{"relation to itself", func(d *PieceDocument) { d.Relations[0].ToPieceID = pieceID }, "", "/relations/0/piece",
			ProblemSelfReference},
		{"relation to a piece that is not public", func(d *PieceDocument) { d.Relations[0].ToPieceID = refPendingPiece },
			"", "/relations/0/piece", ProblemUnknownReference},
		{"relation repeats", func(d *PieceDocument) { d.Relations = append(d.Relations, d.Relations[0]) }, "",
			"/relations/1", ProblemDuplicate},
		{"named credit without a person", func(d *PieceDocument) { d.Credits[0].PersonID = nil }, "",
			pathFirstAttributedPerson, ProblemNamedNeedsPerson},
		{"person on a traditional credit", func(d *PieceDocument) { d.Credits[1].PersonID = ptr(refPerson) }, "",
			"/credits/1/attribution", ProblemPersonNeedsNamed},
		{"long credit note", func(d *PieceDocument) { d.Credits[0].Note = ptr(strings.Repeat("x", 501)) }, "",
			"/credits/0/note", ProblemTooLong},
		{"credit added to a no-page person", func(d *PieceDocument) { d.Credits[0].PersonID = ptr(refNoPagePerson) }, "",
			pathFirstAttributedPerson, ProblemCreditToNoPagePerson},
		{"credit to own pending person", func(d *PieceDocument) { d.Credits[0].PersonID = ptr(refOwnPerson) }, "", "",
			""},
		{"credit to another user's pending person", func(d *PieceDocument) {
			d.Credits[0].PersonID = ptr(refOthersPerson)
		}, "", pathFirstAttributedPerson, ProblemUnknownReference},
		{"too many credits", func(d *PieceDocument) {
			for range 100 {
				d.Credits = append(d.Credits, d.Credits[1])
			}
		}, "", "/credits", ProblemTooMany},
		{"period reversed", func(d *PieceDocument) { d.PeriodStart, d.PeriodEnd = ptr(int16(1400)), ptr(int16(1300)) },
			"", "/period_end", ProblemOutOfRange},
		{"period too early", func(d *PieceDocument) { d.PeriodStart = ptr(int16(-3001)) }, "", "/period_start",
			ProblemOutOfRange},
		{"period too late", func(d *PieceDocument) { d.PeriodEnd = ptr(int16(2101)) }, "", "/period_end",
			ProblemOutOfRange},
		{"licence without known status", func(d *PieceDocument) { d.Licence = ptr(LicenceCCBY) }, "", pathLicence,
			ProblemLicenceNeedsKnown},
		{"link URL invalid", func(d *PieceDocument) { d.Links[0].URL = "ftp://example.org/wren" }, "", "/links/0/url",
			ProblemInvalidURL},
		{"link URL too long", func(d *PieceDocument) {
			d.Links[0].URL = "https://example.org/" + strings.Repeat("x", 2048)
		}, "", "/links/0/url", ProblemInvalidURL},
		{"link repeats by normalized URL", func(d *PieceDocument) {
			d.Links = append(d.Links, DocPieceLink{DocLink: DocLink{URL: "https://www.example.org/wren",
				Roles: []LinkRole{LinkRoleText}}})
		}, "", "/links/1", ProblemDuplicate},
		{"link without roles", func(d *PieceDocument) { d.Links[0].Roles = nil }, "", "/links/0/roles", ProblemRequired},
		{"link role repeats", func(d *PieceDocument) { d.Links[0].Roles = []LinkRole{LinkRoleText, LinkRoleText} }, "",
			"/links/0/roles/1", ProblemDuplicate},
		{"link with too many roles", func(d *PieceDocument) {
			d.Links[0].Roles = append(append([]LinkRole{}, linkRoleValues...), LinkRoleOther)
		}, "", "/links/0/roles", ProblemTooMany},
		{"link to an unknown collection", func(d *PieceDocument) { d.Links[0].CollectionID = ptr(unknownID) }, "",
			"/links/0/collection_id", ProblemUnknownReference},
		{"unknown place type", func(d *PieceDocument) { d.Links[0].PlaceType = ptr(LinkPlaceType("shop")) }, "",
			"/links/0/place_type", ProblemOutOfRange},
		{"long link title", func(d *PieceDocument) { d.Links[0].Title = ptr(strings.Repeat("x", 501)) }, "",
			"/links/0/title", ProblemTooLong},
		{"long platform", func(d *PieceDocument) { d.Links[0].Platform = ptr(strings.Repeat("x", 65)) }, "",
			"/links/0/platform", ProblemTooLong},
		{"too many links", func(d *PieceDocument) {
			for k := range 200 {
				d.Links = append(d.Links, DocPieceLink{DocLink: DocLink{
					URL: "https://example.org/" + strings.Repeat("l", k+1), Roles: []LinkRole{LinkRoleText}}})
			}
		}, "", "/links", ProblemTooMany},
		{"performance without description", func(d *PieceDocument) {
			d.Links[0].Performances[0].DescriptionVerbatim = ""
		}, "", "/links/0/performances/0/description_verbatim", ProblemRequired},
		{"performance year", func(d *PieceDocument) { d.Links[0].Performances[0].Year = ptr(int16(2101)) }, "",
			"/links/0/performances/0/year", ProblemOutOfRange},
		{"performance key issued", func(d *PieceDocument) { d.Links[0].Performances[0].Key = "0123456789abcdef" }, "",
			"", ""},
		{"performance key not hex", func(d *PieceDocument) { d.Links[0].Performances[0].Key = "0123456789ABCDEF" }, "",
			"/links/0/performances/0/key", ProblemOutOfRange},
		{"performance key too short", func(d *PieceDocument) { d.Links[0].Performances[0].Key = "c0ffee" }, "",
			"/links/0/performances/0/key", ProblemOutOfRange},
		{"performance at an event that is not public", func(d *PieceDocument) {
			d.Links[0].Performances[0].EventID = ptr(refPendingEvent)
		}, "", pathFirstPerformanceEvent, ProblemUnknownReference},
		{"performer repeats", func(d *PieceDocument) {
			d.Links[0].Performances[0].PerformerIDs = []int64{refPerson, refPerson}
		}, "", "/links/0/performances/0/performers/1", ProblemDuplicate},
		{"performer unknown", func(d *PieceDocument) {
			d.Links[0].Performances[0].PerformerIDs = []int64{unknownID}
		}, "", "/links/0/performances/0/performers/0", ProblemUnknownReference},
		{"too many performers", func(d *PieceDocument) {
			for k := range 51 {
				d.Links[0].Performances[0].PerformerIDs = append(d.Links[0].Performances[0].PerformerIDs, 1000+int64(k))
			}
		}, "", "/links/0/performances/0/performers", ProblemTooMany},
		{"blank uploader", func(d *PieceDocument) { d.Links[0].Performances[0].Uploader = ptr(" ") }, "",
			"/links/0/performances/0/uploader", ProblemBlank},
	})
}

func TestPieceDocument_Validate_Summary(t *testing.T) {
	t.Parallel()

	d := validPiece()
	v := validationContext(pieceID, &d, RoleContributor)
	v.Summary = strings.Repeat("x", 501)
	assert.Contains(t, d.Validate(v), DocumentProblem{Path: pathSummary, Code: ProblemTooLong})
	v.Summary = "a\x00b"
	assert.Contains(t, d.Validate(v), DocumentProblem{Path: pathSummary, Code: ProblemInvalidCharacters})
}

func TestPersonDocument_Validate(t *testing.T) {
	t.Parallel()

	checkRows(t, personID, validPerson, []problemRow[PersonDocument]{
		{"blank person name", func(d *PersonDocument) { d.Name = "" }, "", pathName, ProblemBlank},
		{"alias is the name", func(d *PersonDocument) { d.Aliases[0].Name = "MORWEN placeholder" }, "",
			"/aliases/0/name", ProblemDuplicate},
		{"alias repeats", func(d *PersonDocument) {
			d.Aliases = append(d.Aliases, DocPersonAlias{Name: secondAliasName, Kind: PersonAliasKindStage,
				IsPublic: true})
		}, "", pathSecondAlias, ProblemDuplicate},
		{"unknown alias kind", func(d *PersonDocument) { d.Aliases[0].Kind = "title" }, "", "/aliases/0/kind",
			ProblemOutOfRange},
		{"contributor adds a public legal name", func(d *PersonDocument) {
			d.Aliases = append(d.Aliases, DocPersonAlias{Name: legalName, Kind: PersonAliasKindLegal,
				IsPublic: true})
		}, "", "/aliases/1/is_public", ProblemAliasVisibility},
		{"contributor adds a hidden legal name", func(d *PersonDocument) {
			d.Aliases = append(d.Aliases, DocPersonAlias{Name: legalName, Kind: PersonAliasKindLegal})
		}, "", "", ""},
		{"contributor adds a hidden stage name", func(d *PersonDocument) {
			d.Aliases = append(d.Aliases, DocPersonAlias{Name: "The Wren", Kind: PersonAliasKindStage})
		}, "", "/aliases/1/is_public", ProblemAliasVisibility},
		{"contributor makes an alias hidden", func(d *PersonDocument) { d.Aliases[0].IsPublic = false }, "",
			"/aliases/0/is_public", ProblemAliasVisibility},
		{"moderator adds a public legal name", func(d *PersonDocument) {
			d.Aliases = append(d.Aliases, DocPersonAlias{Name: legalName, Kind: PersonAliasKindLegal,
				IsPublic: true})
		}, RoleModerator, "", ""},
		{"too many aliases", func(d *PersonDocument) {
			for k := range 100 {
				d.Aliases = append(d.Aliases, DocPersonAlias{Name: "Morwen " + strings.Repeat("a", k+1),
					Kind: PersonAliasKindNickname, IsPublic: true})
			}
		}, "", pathAliases, ProblemTooMany},
		{"role repeats", func(d *PersonDocument) { d.Roles = append(d.Roles, PersonRoleSinger) }, "", "/roles/1",
			ProblemDuplicate},
		{"affiliation of another facet", func(d *PersonDocument) {
			d.AffiliationIDs = append(d.AffiliationIDs, refMood)
		}, "", "/affiliations/1", ProblemWrongFacet},
		{"affiliation repeats", func(d *PersonDocument) {
			d.AffiliationIDs = append(d.AffiliationIDs, refAffiliation)
		}, "", "/affiliations/1", ProblemDuplicate},
		{"relation to themself", func(d *PersonDocument) { d.Relations[0].ToPersonID = personID }, "",
			"/relations/0/person", ProblemSelfReference},
		{"relation added to a no-page person", func(d *PersonDocument) {
			d.Relations = append(d.Relations, DocPersonRelation{ToPersonID: refNoPagePerson,
				Type: PersonRelationTypeHeirOf})
		}, "", "/relations/1/person", ProblemRelationToNoPagePerson},
		{"relation repeats", func(d *PersonDocument) { d.Relations = append(d.Relations, d.Relations[0]) }, "",
			"/relations/1", ProblemDuplicate},
		{"performance of a piece that is not public", func(d *PersonDocument) {
			d.Links[0].Performances[0].PieceID = ptr(refPendingPiece)
		}, "", "/links/0/performances/0/piece", ProblemUnknownReference},
		{"status date in the future", func(d *PersonDocument) {
			d.Status, d.StatusSourceURL = PersonStatusDeceased, ptr("https://example.org/obituary")
			d.StatusDate = &Date{validToday.AddDate(0, 0, 1)}
		}, "", "/status_date", ProblemOutOfRange},
		{"status date today", func(d *PersonDocument) {
			d.Status, d.StatusSourceURL = PersonStatusDeceased, ptr("https://example.org/obituary")
			d.StatusDate = &validToday
		}, "", "", ""},
		{"long pronouns", func(d *PersonDocument) { d.Pronouns = ptr(strings.Repeat("x", 65)) }, "", "/pronouns",
			ProblemTooLong},
	})
}

func TestPersonDocument_Validate_NoPageProfileIsIdentityOnly(t *testing.T) {
	t.Parallel()

	base := validPerson()
	base.Status, base.StatusSourceURL = PersonStatusPrefersNoPage, ptr("https://example.org/request")
	edited := base
	edited.NotesMD = ptr("New notes.")
	edited.Name = "Morwen Renamed"

	assert.Equal(t, []DocumentProblem{{Path: pathNotes, Code: ProblemWithheldFieldChanged}},
		edited.Validate(validationContext(personID, &base, RoleContributor)))
	assert.Empty(t, edited.Validate(validationContext(personID, &base, RoleModerator)), "moderators see the profile")

	identity := base
	identity.Name, identity.Status = "Morwen Renamed", PersonStatusActive
	assert.Empty(t, identity.Validate(validationContext(personID, &base, RoleContributor)), "identity changes are allowed")

	// A person who has asked for no page since the base was written.
	active := validPerson()
	changed := active
	changed.Roles = []PersonRole{PersonRolePoet}
	v := validationContext(personID, &active, RoleContributor)
	v.Refs.Persons[personID] = ReferencedPerson{ReferencedEntity: public, PrefersNoPage: true}
	assert.Equal(t, []DocumentProblem{{Path: "/roles", Code: ProblemWithheldFieldChanged}}, changed.Validate(v))
}

func TestTermDocument_Validate(t *testing.T) {
	t.Parallel()

	checkRows(t, termID, validTerm, []problemRow[TermDocument]{
		{"facet changed", func(d *TermDocument) { d.Facet = FacetTopic; d.ParentID = nil }, "", "/facet",
			ProblemFacetImmutable},
		{"blank term name", func(d *TermDocument) { d.Name = " " }, "", pathName, ProblemBlank},
		{"blank display name", func(d *TermDocument) { d.DisplayName = "" }, "", "/display_name", ProblemBlank},
		{"public name of the facet", func(d *TermDocument) { d.Name = "JOLLY" }, "", pathName, ProblemDuplicate},
		{"public name of another facet", func(d *TermDocument) { d.Name = "Sea" }, "", "", ""},
		{"position", func(d *TermDocument) { d.Position = -1 }, "", "/position", ProblemOutOfRange},
		{"position too large", func(d *TermDocument) { d.Position = 32768 }, "", "/position", ProblemOutOfRange},
		{"alias is the name", func(d *TermDocument) { d.Aliases = []string{"wistful"} }, "", "/aliases/0",
			ProblemDuplicate},
		{"alias repeats", func(d *TermDocument) { d.Aliases = []string{"Pensive", "PENSIVE"} }, "", pathSecondAlias,
			ProblemDuplicate},
		{"parent of another facet", func(d *TermDocument) { d.ParentID = ptr(refTopic) }, "", pathTermParent,
			ProblemWrongFacet},
		{"parent is itself", func(d *TermDocument) { d.ParentID = ptr(termID) }, "", pathTermParent, ProblemSelfReference},
		{"parent unknown", func(d *TermDocument) { d.ParentID = ptr(unknownID) }, "", pathTermParent,
			ProblemUnknownReference},
		{"parent another user's pending term", func(d *TermDocument) { d.ParentID = ptr(refOthersTerm) }, "", pathTermParent,
			ProblemUnknownReference},
		{"parent too deep", func(d *TermDocument) { d.ParentID = ptr(refMoodLeaf) }, "", pathTermParent, ProblemTooDeep},
		{"parent at the deepest level allowed", func(d *TermDocument) { d.ParentID = ptr(refMoodGrandchild) }, "", "",
			""},
		{"related affiliation of another facet", func(d *TermDocument) {
			d.Facet, d.ParentID, d.RelatedAffiliationID = FacetTopic, nil, ptr(refMood)
		}, RoleModerator, pathRelatedAffiliation, ProblemWrongFacet},
		{"related affiliation on a mood term", func(d *TermDocument) { d.RelatedAffiliationID = ptr(refAffiliation) },
			"", pathRelatedAffiliation, ProblemWrongFacet},
	})
}

func TestTermDocument_Validate_Cycle(t *testing.T) {
	t.Parallel()

	// The mood root moved under its own grandchild.
	root := TermDocument{SchemaVersion: DocumentSchemaVersion, Facet: FacetMood, Name: moodName, DisplayName: moodName}
	moved := root
	moved.ParentID = ptr(refMoodGrandchild)
	assert.Contains(t, moved.Validate(validationContext(refMood, &root, RoleModerator)),
		DocumentProblem{Path: pathTermParent, Code: ProblemCycle})
}

func TestTermDocument_Validate_NewTermAtPublicName(t *testing.T) {
	t.Parallel()

	d := validTerm()
	d.Name = moodName
	assert.Equal(t, []DocumentProblem{{Path: pathName, Code: ProblemDuplicate}},
		d.Validate(validationContext[TermDocument](0, nil, RoleContributor)))
	own := d
	own.ParentID = nil
	assert.Empty(t, own.Validate(validationContext(refMood, &own, RoleContributor)), "its own name")
}

func TestCollectionDocument_Validate(t *testing.T) {
	t.Parallel()

	checkRows(t, collectionID, validCollection, []problemRow[CollectionDocument]{
		{"blank collection name", func(d *CollectionDocument) { d.Name = "" }, "", pathName, ProblemBlank},
		{"invalid URL", func(d *CollectionDocument) { d.URL = ptr("javascript:alert(1)") }, "", "/url",
			ProblemInvalidURL},
		{"no URL", func(d *CollectionDocument) { d.URL = nil }, "", "", ""},
		{"unknown collection kind", func(d *CollectionDocument) { d.Kind = "shelf" }, "", pathKind, ProblemOutOfRange},
		{"unknown enumerable", func(d *CollectionDocument) { d.Enumerable = "maybe" }, "", "/enumerable",
			ProblemOutOfRange},
		{"parent is itself", func(d *CollectionDocument) { d.ParentID = ptr(collectionID) }, "", pathCollectionParent,
			ProblemSelfReference},
		{"parent unknown", func(d *CollectionDocument) { d.ParentID = ptr(unknownID) }, "", pathCollectionParent,
			ProblemUnknownReference},
		{"region of another facet", func(d *CollectionDocument) { d.RegionTermID = ptr(refMood) }, "", pathRegion,
			ProblemWrongFacet},
		{"region unknown", func(d *CollectionDocument) { d.RegionTermID = ptr(unknownID) }, "", pathRegion,
			ProblemUnknownReference},
	})
}

func TestCollectionDocument_Validate_Cycle(t *testing.T) {
	t.Parallel()

	parent := CollectionDocument{SchemaVersion: DocumentSchemaVersion, Name: "Wren Archive",
		Kind: CollectionKindWebsite, Enumerable: CollectionEnumerableUnknown}
	moved := parent
	moved.ParentID = ptr(refChildColl)
	assert.Contains(t, moved.Validate(validationContext(refCollection, &parent, RoleModerator)),
		DocumentProblem{Path: pathCollectionParent, Code: ProblemCycle})

	deep := map[int64]ReferencedCollection{}
	for id := int64(100); id < 112; id++ {
		deep[id] = ReferencedCollection{ReferencedEntity: public, ParentID: ptr(id + 1)}
	}
	deep[112] = ReferencedCollection{ReferencedEntity: public}
	v := validationContext[CollectionDocument](0, nil, RoleModerator)
	v.Refs.Collections = deep
	moved.ParentID = ptr(int64(100))
	assert.Contains(t, moved.Validate(v), DocumentProblem{Path: pathCollectionParent, Code: ProblemTooDeep})
	moved.ParentID = ptr(int64(103))
	assert.Empty(t, moved.Validate(v), "ten ancestors")
}

func TestEventDocument_Validate(t *testing.T) {
	t.Parallel()

	checkRows(t, 45, validEvent, []problemRow[EventDocument]{
		{"blank event name", func(d *EventDocument) { d.Name = "" }, "", pathName, ProblemBlank},
		{"unknown event kind", func(d *EventDocument) { d.Kind = "feast" }, "", pathKind, ProblemOutOfRange},
		{"long description", func(d *EventDocument) { d.DescriptionMD = ptr(strings.Repeat("x", 65536)) }, "",
			"/description_md", ProblemTooLong},
	})
}

func TestValidate_ProblemsNeverHoldValues(t *testing.T) {
	t.Parallel()

	secret := secretValue
	piece := validPiece()
	piece.Title = secret + strings.Repeat("x", 300)
	piece.Aliases = []DocPieceAlias{{Text: secret, Kind: PieceAliasKindAltTitle}, {Text: secret,
		Kind: PieceAliasKindAltTitle}}
	piece.NotesMD = ptr(secret + "\x00")
	piece.Links[0].URL = "javascript:" + secret
	piece.Links[0].Title = ptr(secret + strings.Repeat("x", 600))
	piece.Links[0].Performances[0].DescriptionVerbatim = secret + strings.Repeat("x", 70000)
	piece.Links[0].Performances[0].Uploader = ptr(secret + "\u202e")
	piece.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: ptr("ftp://" + secret)}
	problems := piece.Validate(validationContext(pieceID, ptr(validPiece()), RoleContributor))
	require.NotEmpty(t, problems)
	assertHoldsNoValues(t, piece, problems)
	assert.NotContains(t, fmt.Sprint(problems), "Secretvalue")

	person := validPerson()
	person.Name = secret
	person.Aliases = []DocPersonAlias{{Name: secret, Kind: PersonAliasKindLegal, IsPublic: true}}
	person.StatusSourceURL = ptr("not a url " + secret)
	person.Pronouns = ptr(secret + strings.Repeat("x", 100))
	problems = person.Validate(validationContext(personID, ptr(validPerson()), RoleContributor))
	require.NotEmpty(t, problems)
	assertHoldsNoValues(t, person, problems)
}

func TestValidate_UnchangedNoPageCredit(t *testing.T) {
	t.Parallel()

	base := validPiece()
	base.Credits[0].PersonID = ptr(refNoPagePerson) // the person has since chosen to have no page
	edited := base
	edited.NotesMD = ptr("Edited notes.")
	for _, author := range []Role{RoleContributor, RoleModerator} {
		assert.Empty(t, edited.Validate(validationContext(pieceID, &base, author)), author)
	}
}

func TestValidate_UnchangedNoPageRelation(t *testing.T) {
	t.Parallel()

	base := validPerson()
	base.Relations[0].ToPersonID = refNoPagePerson
	edited := base
	edited.NotesMD = ptr("Edited notes.")
	for _, author := range []Role{RoleContributor, RoleModerator} {
		assert.Empty(t, edited.Validate(validationContext(personID, &base, author)), author)
	}
}

func TestValidate_AddedNoPageCredit(t *testing.T) {
	t.Parallel()

	base := validPiece()
	edited := validPiece()
	edited.Credits = append(edited.Credits, DocCredit{PersonID: ptr(refNoPagePerson), Role: CreditRoleTune,
		Attribution: AttributionNamed, Certainty: CertaintyCertain})
	for _, author := range []Role{RoleContributor, RoleModerator} {
		assert.Equal(t, []DocumentProblem{{Path: "/credits/2/person", Code: ProblemCreditToNoPagePerson}},
			edited.Validate(validationContext(pieceID, &base, author)), author)
	}

	// The same person, role and part again is a second credit, which is added.
	again := base
	again.Credits = append(append([]DocCredit{}, base.Credits...), base.Credits[0])
	again.Credits[0].PersonID, again.Credits[2].PersonID = ptr(refNoPagePerson), ptr(refNoPagePerson)
	prev := base
	prev.Credits = append([]DocCredit{}, again.Credits[:2]...)
	assert.Equal(t, []DocumentProblem{{Path: "/credits/2/person", Code: ProblemCreditToNoPagePerson}},
		again.Validate(validationContext(pieceID, &prev, RoleContributor)))
}

func TestValidate_UnchangedReferenceMustExist(t *testing.T) {
	t.Parallel()

	base := validPiece()
	base.Terms = append(base.Terms, DocPieceTerm{TermID: unknownID}) // the term has been deleted since
	edited := base
	edited.NotesMD = ptr("Edited notes.")
	assert.Contains(t, edited.Validate(validationContext(pieceID, &base, RoleModerator)),
		DocumentProblem{Path: pathNewTerm, Code: ProblemUnknownReference})
}

func TestValidate_RestoredHiddenStageAlias(t *testing.T) {
	t.Parallel()

	base := validPerson()
	base.Aliases = append(base.Aliases, DocPersonAlias{Name: "The Hidden Wren", Kind: PersonAliasKindStage})
	restored := base
	restored.NotesMD = ptr("Edited notes.")
	assert.Empty(t, restored.Validate(validationContext(personID, &base, RoleContributor)))
}

func TestChangedReferences(t *testing.T) {
	t.Parallel()

	prev := validPiece()
	next := validPiece()
	next.Credits[0].Part = ptr(PiecePartWords)                 // changed: another credit key
	next.Terms[1].IsPrimary = true                             // the same term
	next.Links[0].Performances[0].Year = ptr(int16(2002))      // another performance key
	next.Links[0].CollectionID = ptr(refChildColl)             // another collection
	next.Relations = append(next.Relations, prev.Relations[0]) // a second occurrence of a relation is the same key
	keys := map[string]string{}
	for _, r := range ChangedReferences(&prev, next) {
		keys[r.Path] = r.Key
		assert.NotEmpty(t, r.Key)
	}
	assert.ElementsMatch(t, []string{pathFirstAttributedPerson, "/links/0/collection_id",
		pathFirstPerformanceEvent, "/links/0/performances/0/performers/0"}, keysOf(keys))
	assert.Empty(t, ChangedReferences(&prev, prev))
	assert.Len(t, ChangedReferences(nil, prev), len(documentReferences(prev)))

	for _, r := range ChangedReferences(&prev, next) {
		if r.Path == pathFirstAttributedPerson {
			assert.Equal(t, DocReference{Path: r.Path, Key: r.Key, Type: EntityTypePerson, ID: refPerson,
				use: useCredit}, r)
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

func TestDocumentPaths_UseEditorNames(t *testing.T) {
	t.Parallel()

	var paths []string
	doc := validPiece()
	doc.Links[0].Performances[0].Uploader = ptr("Uploader")
	walkStrings(reflect.ValueOf(doc), "", documentPaths, func(path string, _ reflect.Value) {
		paths = append(paths, path)
	})
	assert.Contains(t, paths, "/links/0/url", "an embedded struct's fields are the link's")
	assert.Contains(t, paths, "/links/0/performances/0/uploader")
	assert.Contains(t, paths, "/credits/0/role")
	for _, f := range []struct {
		doc  any
		name string
		want string
	}{
		{DocCredit{}, "PersonID", "/person"}, {DocPieceTerm{}, "TermID", "/term"},
		{DocPieceRelation{}, "ToPieceID", "/piece"}, {DocPersonRelation{}, "ToPersonID", "/person"},
		{DocPerformance{}, "EventID", "/event"}, {DocPerformance{}, "PerformerIDs", "/performers"},
		{DocPersonPerformance{}, "PieceID", "/piece"}, {PersonDocument{}, "AffiliationIDs", "/affiliations"},
		{TermDocument{}, "ParentID", pathTermParent}, {TermDocument{}, "RelatedAffiliationID", pathRelatedAffiliation},
		{CollectionDocument{}, "RegionTermID", pathRegion}, {CollectionDocument{}, "ParentID", pathCollectionParent},
	} {
		field, ok := reflect.TypeOf(f.doc).FieldByName(f.name)
		require.True(t, ok, f.name)
		assert.Equal(t, f.want, documentPaths.field("", field), f.name)
	}
}
