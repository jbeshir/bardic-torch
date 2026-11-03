package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testBardName is the bard of validImport.
const testBardName = "Wren Placeholder"

// secretValue is planted in the mutated field of every invalid import: no error message may show it.
const secretValue = "Secretvalue Placeholder"

// testBardSlug is the slug of the fixture bard.
const testBardSlug = "wren-placeholder"

func importRevision(id int64) ImportedRevision {
	return ImportedRevision{ID: id, Summary: ImportSummaryPage(id), Contributors: []string{"Wiki User 01"},
		CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
}

func ptr[T any](v T) *T {
	return &v
}

// validImport is a small import every constraint holds for: two affiliation terms (a root and its child), a topic
// related to the child, a collection, an event, two persons and a piece crediting one of them.
func validImport() CatalogueImport {
	link := DocLink{URL: "https://example.org/song?utm_source=x", Title: ptr("Song"), CollectionID: ptr(int64(1))}
	pieceLink := link
	pieceLink.Roles = []LinkRole{LinkRoleLyrics}
	personLink := link
	personLink.Roles = []LinkRole{LinkRolePerformance}

	return CatalogueImport{
		Terms: []ImportedTerm{
			{ID: 1, Slug: "sca", Revision: importRevision(1), Doc: TermDocument{SchemaVersion: DocumentSchemaVersion,
				Facet: FacetAffiliation, Name: "SCA", DisplayName: "SCA", Aliases: []string{"SCA Bards"}}},
			{ID: 2, Slug: "calontir", Revision: importRevision(2), Doc: TermDocument{
				SchemaVersion: DocumentSchemaVersion, Facet: FacetAffiliation, Name: "Calontir", DisplayName: "Calontir",
				ParentID: ptr(int64(1)), Aliases: []string{}}},
			{ID: 3, Slug: "patriotic-calontir", Revision: importRevision(3), Doc: TermDocument{
				SchemaVersion: DocumentSchemaVersion, Facet: FacetTopic, Name: "Patriotic (Calontir)",
				DisplayName: "Patriotic (Calontir)", RelatedAffiliationID: ptr(int64(2)), Aliases: []string{}},
				Legacy: &LegacyPage{PageID: ptr(uint32(30)), Title: "Category:Patriotic (Calontir)"}},
		},
		Collections: []ImportedCollection{{ID: 1, SortName: "Songbook", SortLetter: "S", Revision: importRevision(4),
			Doc: CollectionDocument{SchemaVersion: DocumentSchemaVersion, Name: "Songbook",
				URL: ptr("https://example.org/"), Kind: CollectionKindWebsite, RegionTermID: ptr(int64(2)),
				IsBardicResource: true, Enumerable: CollectionEnumerableUnknown}}},
		Events: []ImportedEvent{{ID: 1, Slug: "green-dragon", Revision: importRevision(5),
			Doc: EventDocument{SchemaVersion: DocumentSchemaVersion, Name: "Green Dragon", Kind: EventKindVenue}}},
		Persons: []ImportedPerson{
			{ID: 1, Slug: testBardSlug, SortName: testBardName, SortLetter: "W", Revision: importRevision(6),
				Legacy: &LegacyPage{PageID: ptr(uint32(10)), Title: testBardName},
				Doc: PersonDocument{SchemaVersion: DocumentSchemaVersion, Name: testBardName,
					Kind: PersonKindCommunityBard, Status: PersonStatusActive, LicensingStatus: LicensingStatusUnknown,
					Aliases:        []DocPersonAlias{{Name: "Legal Name 001", Kind: PersonAliasKindLegal}},
					Roles:          []PersonRole{PersonRoleSinger},
					AffiliationIDs: []int64{2}, Relations: []DocPersonRelation{{ToPersonID: 2, Type: PersonRelationTypeHeirOf}},
					Links: []DocPersonLink{{DocLink: personLink, Performances: []DocPersonPerformance{{
						PieceID: ptr(int64(1)), DocPerformance: DocPerformance{EventID: ptr(int64(1)),
							DescriptionVerbatim: "Video", PerformerIDs: []int64{1}}}}}}}},
			{ID: 2, Slug: "ash-placeholder", SortName: "Ash Placeholder", SortLetter: "A",
				Revision: importRevision(7), Doc: PersonDocument{SchemaVersion: DocumentSchemaVersion,
					Name: "Ash Placeholder", Kind: PersonKindCommunityBard, Status: PersonStatusActive,
					LicensingStatus: LicensingStatusUnknown, IsStub: true}},
		},
		Pieces: []ImportedPiece{{ID: 1, Slug: "a-song", SortTitle: "Song", SortLetter: "S", Revision: importRevision(8),
			Legacy: &LegacyPage{PageID: ptr(uint32(20)), Title: "A Song"},
			Doc: PieceDocument{SchemaVersion: DocumentSchemaVersion, Title: "A Song", Kind: PieceKindSong,
				Conservation:    DocConservation{Status: ConservationStatusUnassessed},
				LicensingStatus: LicensingStatusUnknown, PeriodStart: ptr(int16(1201)), PeriodEnd: ptr(int16(1300)),
				Aliases: []DocPieceAlias{{Text: "The Song", Kind: PieceAliasKindAltTitle}},
				Terms:   []DocPieceTerm{{TermID: 3, IsPrimary: true}},
				Credits: []DocCredit{{PersonID: ptr(int64(1)), Role: CreditRoleLyrics, Attribution: AttributionNamed,
					Certainty: CertaintyCertain}, {Role: CreditRoleTune, Attribution: AttributionTraditional,
					Certainty: CertaintyCertain}},
				Links: []DocPieceLink{{DocLink: pieceLink, Performances: []DocPerformance{}}}}}},
		Links: []ImportedLink{{ID: 1, Link: link}},
	}
}

func TestCatalogueImportValidate_ValidImport(t *testing.T) {
	require.NoError(t, validImport().Validate())
}

// requireInvalid checks that the mutated import fails with ErrInvalidImport, naming field but not the value.
func requireInvalid(t *testing.T, mutate func(*CatalogueImport), field string) {
	t.Helper()
	imp := validImport()
	mutate(&imp)
	err := imp.Validate()
	require.ErrorIs(t, err, ErrInvalidImport)
	assert.Contains(t, err.Error(), field)
	assert.NotContains(t, err.Error(), secretValue)
}

func TestCatalogueImportValidate_IDsAndSlugs(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) {
		extra := imp.Persons[1]
		extra.ID, extra.Slug, extra.Revision.ID = 0, "ash-placeholder-2", 9
		imp.Persons = append(imp.Persons, extra)
	}, "person 0: id")
	requireInvalid(t, func(imp *CatalogueImport) {
		dup := imp.Persons[1]
		dup.Slug, dup.Revision.ID = "ash-placeholder-2", 9
		imp.Persons = append(imp.Persons, dup)
	}, "person 2: id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[1].Slug = testBardSlug }, "person 2: slug")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Events[0].Slug = secretValue }, "event 1: slug")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[1].Revision.ID = 6 }, "person 2: revision.id")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[1].Legacy = &LegacyPage{PageID: ptr(uint32(10)), Title: secretValue}
	}, "person 2: legacy_wiki_pageid")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].SortLetter = "s" }, "piece 1: sort_letter")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].SortLetter = "T" }, "piece 1: sort_letter")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].SortLetter = LetterOther }, "person 1: sort_letter")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Collections[0].SortName = "" }, "collection 1: sort_name")
}

func TestCatalogueImportValidate_Summaries(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Revision.Summary = "Imported " + secretValue },
		"piece 1: revision.summary")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Revision.Contributors = []string{strings.Repeat("x", 256)}
	}, "revision.contributors[0]")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Revision.CreatedAt = time.Time{} },
		"revision.created_at")
}

func TestCatalogueImportValidate_Documents(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.SchemaVersion = 2 }, "doc.schema_version")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Kind = secretValue }, "piece 1: doc")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.Aliases[0].Kind = "mundane" }, "person 1: doc")
}

func TestCatalogueImportValidate_ColumnWidths(t *testing.T) {
	long := secretValue + strings.Repeat("x", 300)
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Title = long }, "doc.title")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Aliases[0].Text = long }, "doc.aliases[0].text")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.Aliases[0].Name = long }, "doc.aliases[0].name")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Legacy.Title = long }, "legacy_title")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Credits[1].Note = ptr(long + strings.Repeat("x", 300))
	}, "doc.credits[1].note")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links[0].Link.Title = ptr(long + strings.Repeat("x", 300)) },
		"link 1: title")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links[0].Link.Platform = ptr("plätform") }, "link 1: platform")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Links[0].Performances[0].Uploader = ptr(long)
	}, "performances[0].uploader")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.NotesMD = ptr(secretValue + strings.Repeat("x", 70000))
	}, "doc.notes_md")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.Pronouns = ptr(long) }, "doc.pronouns")
}

func TestCatalogueImportValidate_CollationUniques(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Aliases = append(imp.Pieces[0].Doc.Aliases,
			DocPieceAlias{Text: "THE SÖNG", Kind: PieceAliasKindNickname})
	}, "doc.aliases[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Aliases = append(imp.Persons[0].Doc.Aliases,
			DocPersonAlias{Name: "legal name 001", Kind: PersonAliasKindRedirect})
	}, "person 1: doc.aliases[1]")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Terms[0].Doc.Aliases = []string{"SCA Bards", "sca bards"} },
		"term 1: doc.aliases[1]")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Terms[1].Doc.Name = "sca" }, "term 2: doc.name")
}

func TestCatalogueImportValidate_ChildPrimaryKeys(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Terms = append(imp.Pieces[0].Doc.Terms, DocPieceTerm{TermID: 3})
	}, "doc.terms[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Terms = []DocPieceTerm{{TermID: 1, IsPrimary: true}, {TermID: 2, IsPrimary: true}}
	}, "doc.terms[1].is_primary")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Links[0].Performances[0].PerformerIDs = []int64{1, 1}
	}, "performer_ids[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Roles = []PersonRole{PersonRoleSinger, PersonRoleSinger}
	}, "doc.roles[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		l := imp.Pieces[0].Doc.Links[0]
		l.URL = "https://EXAMPLE.org/song"
		imp.Pieces[0].Doc.Links = append(imp.Pieces[0].Doc.Links, l)
	}, "doc.links[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Links[0].Roles = []LinkRole{LinkRoleLyrics, LinkRoleLyrics}
	}, "doc.links[0].roles[1]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.SecondaryKinds = []PieceKind{PieceKindComedy, PieceKindComedy}
	}, "doc.secondary_kinds[1]")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.AffiliationIDs = []int64{2, 2} },
		"doc.affiliation_ids[1]")
}

func TestCatalogueImportValidate_Checks(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Credits[0].PersonID = nil },
		"doc.credits[0].person_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Credits[1].PersonID = ptr(int64(1)) },
		"doc.credits[1].person_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.PeriodEnd = ptr(int16(1100)) }, "doc.period_end")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Licence = ptr(LicencePD) }, "doc.licence")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.Relations[0].ToPersonID = 1 },
		"doc.relations[0].to_person_id")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Pieces[0].Doc.Relations = []DocPieceRelation{{ToPieceID: 1, Type: PieceRelationTypeVariantOf}}
	}, "doc.relations[0].to_piece_id")
}

func TestCatalogueImportValidate_References(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Terms[0].TermID = 9 }, "doc.terms[0].term_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Credits[0].PersonID = ptr(int64(9)) },
		"doc.credits[0].person_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Doc.AffiliationIDs = []int64{3} },
		"doc.affiliation_ids[0]")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Links[0].Performances[0].EventID = ptr(int64(9))
	}, "event_id")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Persons[0].Doc.Links[0].Performances[0].PieceID = ptr(int64(9))
	}, "piece_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Collections[0].Doc.RegionTermID = ptr(int64(9)) },
		"doc.region_term_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links[0].Link.CollectionID = ptr(int64(9)) },
		"link 1: collection_id")
}

func TestCatalogueImportValidate_InsertOrder(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Terms[0], imp.Terms[1] = imp.Terms[1], imp.Terms[0]
	}, "term 2: doc.parent_id")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Terms[2].Doc.RelatedAffiliationID = ptr(int64(3))
	}, "term 3: doc.related_affiliation_id")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Terms = []ImportedTerm{imp.Terms[2], imp.Terms[0], imp.Terms[1]}
	}, "term 3: doc.related_affiliation_id")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Collections[0].Doc.ParentID = ptr(int64(1)) },
		"collection 1: doc.parent_id")
}

func TestCatalogueImportValidate_Links(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links = nil }, "doc.links[0].url")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links[0].Link.Title = ptr(secretValue) }, "doc.links[0]")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Links[0].Link.Roles = []LinkRole{LinkRoleLyrics} },
		"link 1: roles")
	requireInvalid(t, func(imp *CatalogueImport) {
		imp.Links = append(imp.Links, ImportedLink{ID: 2, Link: DocLink{URL: "https://example.org/song"}})
	}, "link 2: url")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Links[0].Roles = nil }, "doc.links[0].roles")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Links[0].URL = "mailto:" + secretValue },
		"doc.links[0].url")
}

func TestCatalogueImport_TermAncestors(t *testing.T) {
	ancestors, err := validImport().TermAncestors()

	require.NoError(t, err)
	assert.Equal(t, []TermAncestor{{1, 1, 0}, {2, 2, 0}, {1, 2, 1}, {3, 3, 0}}, ancestors)
}

// TestCatalogueImportValidate_RejectsWithheldPlaceholders: no name, title, alias, sort key, slug or uploader may
// hold a withheld-text placeholder; only free text may.
func TestCatalogueImportValidate_RejectsWithheldPlaceholders(t *testing.T) {
	for _, withheld := range []string{WithheldName, WithheldEmail} {
		in := "Song for " + withheld
		for field, mutate := range map[string]func(*CatalogueImport){
			"term 1: doc.name":              func(imp *CatalogueImport) { imp.Terms[0].Doc.Name = in },
			"term 1: doc.display_name":      func(imp *CatalogueImport) { imp.Terms[0].Doc.DisplayName = in },
			"term 1: doc.aliases[0]":        func(imp *CatalogueImport) { imp.Terms[0].Doc.Aliases[0] = in },
			"collection 1: doc.name":        func(imp *CatalogueImport) { imp.Collections[0].Doc.Name = in },
			"collection 1: sort_name":       func(imp *CatalogueImport) { imp.Collections[0].SortName = in },
			"event 1: doc.name":             func(imp *CatalogueImport) { imp.Events[0].Doc.Name = in },
			"person 2: doc.name":            func(imp *CatalogueImport) { imp.Persons[1].Doc.Name = in },
			"person 1: doc.aliases[0].name": func(imp *CatalogueImport) { imp.Persons[0].Doc.Aliases[0].Name = in },
			"piece 1: doc.title":            func(imp *CatalogueImport) { imp.Pieces[0].Doc.Title = in },
			"piece 1: doc.aliases[0].text":  func(imp *CatalogueImport) { imp.Pieces[0].Doc.Aliases[0].Text = in },
			"performances[0].uploader": func(imp *CatalogueImport) {
				imp.Persons[0].Doc.Links[0].Performances[0].Uploader = ptr(in)
			},
			"event 1: slug": func(imp *CatalogueImport) { imp.Events[0].Slug = "green-" + Slugify(withheld) },
		} {
			requireInvalid(t, mutate, field)
		}
	}
	notes := validImport()
	notes.Pieces[0].Doc.NotesMD = ptr("Written for " + WithheldName + ".")
	require.NoError(t, notes.Validate(), "free text may hold a placeholder")
}

func TestCatalogueImportValidate_StringsAreNFC(t *testing.T) {
	requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Title = "Café " + secretValue },
		"piece 1: .Doc.Title")
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Revision.Contributors = []string{"\xff"} },
		"person 1: .Revision.Contributors[0]")
}

func TestCatalogueImportValidate_StringsHoldNoControlCharacters(t *testing.T) {
	for _, c := range []string{"\x00", "\x1b[31m", "\u202e", "\u2066", "\u0085"} {
		requireInvalid(t, func(imp *CatalogueImport) { imp.Pieces[0].Doc.Title = "A" + c + secretValue },
			"piece 1: .Doc.Title")
	}
	requireInvalid(t, func(imp *CatalogueImport) { imp.Persons[0].Revision.Contributors = []string{"Wiki\x1bUser"} },
		"person 1: .Revision.Contributors[0]")
}

func TestIsControlCharacter(t *testing.T) {
	for _, r := range []rune{0, 0x1b, '\r', 0x7f, 0x85, 0x9f, 0x202a, 0x202e, 0x2066, 0x2069} {
		assert.True(t, IsControlCharacter(r), "%U", r)
	}
	for _, r := range []rune{'\t', '\n', ' ', 'a', 0xa0, 0x200d, 0x2029, 0x2070} {
		assert.False(t, IsControlCharacter(r), "%U", r)
	}
	assert.Equal(t, "Ash\tand\nRowan", WithoutControlCharacters("A\x00sh\tand\u202e\nRow\x1ban"))
}

func TestIsImportSummary(t *testing.T) {
	for _, s := range []string{ImportSummaryPage(42), ImportSummaryStub, ImportSummaryCollections,
		ImportSummaryCategory, ImportSummaryPerformances} {
		assert.True(t, IsImportSummary(s), s)
	}
	for _, s := range []string{"", "Imported from the wiki (page revision 0).", "Imported by " + secretValue} {
		assert.False(t, IsImportSummary(s), s)
	}
}

func TestCollationKey(t *testing.T) {
	assert.Equal(t, CollationKey("Astrið Ælfric"), CollationKey("astrid aelfric"))
	assert.NotEqual(t, CollationKey("O'Brien"), CollationKey("OBrien"), "apostrophes count, as in the collation")
}
