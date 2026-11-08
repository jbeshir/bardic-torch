package domain

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures of the withhold and restore tests.
const (
	hiddenLinkURL                   = "https://example.org/hidden-recording"
	songbookPageURL                 = "https://example.org/songbook-page"
	refHiddenColl             int64 = 32
	privateNotes                    = "Private profile notes."
	preferenceSource                = "https://example.org/preference-source"
	feastPerformance                = "Sung at the feast"
	secretPerformance               = "At the secret revel"
	pathFirstLinkPerformances       = "/links/0/performances"
)

// editorVisibility is what the fixtures' caller sees, as role: public entities, their own pending term and person,
// and the hidden link.
func editorVisibility(t *testing.T, role Role) EditorVisibility {
	t.Helper()
	hidden, err := NormalizeURL(hiddenLinkURL)
	require.NoError(t, err)

	return EditorVisibility{
		SnapshotVisibility: SnapshotVisibility{
			Pieces:        map[int64]PieceSummary{refPiece: {Slug: "piece"}, refOtherPiece: {Slug: "other-piece"}},
			Persons:       map[int64]PersonSummary{refPerson: {Slug: "ash", Name: "Ash Example"}},
			NoPagePersons: map[int64]PersonSummary{refNoPagePerson: {Slug: "rowan", Name: "Rowan Placeholder"}},
			Terms: map[int64]TermSummary{refMood: {Slug: "merry"}, refOrigin: {Slug: "period-origin"},
				refAffiliation: {Slug: "northshield"}, refTopic: {Slug: "sea"}},
			Collections: map[int64]CollectionSummary{refCollection: {ID: refCollection}, refChildColl: {ID: refChildColl}},
			Events:      map[int64]EventSummary{refEvent: {Slug: "revel"}},
		},
		OwnPending: OwnPendingRefs{
			Terms:   map[int64]TermSummary{refOwnTerm: {Slug: "pending-term-1"}},
			Persons: map[int64]PersonSummary{refOwnPerson: {Slug: "pending-person-1", Name: "Hazel Placeholder"}},
		},
		HiddenLinkURLs: map[string]bool{hidden: true},
		Role:           role,
	}
}

func testPreferences() []DocPreference {
	return []DocPreference{{Dimension: PreferenceDimensionRecording, Value: json.RawMessage(`"ask"`),
		SourceURL: preferenceSource}}
}

// fullPiece is a piece holding every kind of element a contributor may be withheld.
func fullPiece() PieceDocument {
	d := validPiece()
	d.Terms = append(slices.Clone(d.Terms), DocPieceTerm{TermID: refOthersTerm}, DocPieceTerm{TermID: refOwnTerm})
	d.Relations = append(slices.Clone(d.Relations),
		DocPieceRelation{ToPieceID: refPendingPiece, Type: PieceRelationTypeContrafactumOf})
	d.Credits = []DocCredit{
		{PersonID: ptr(refPerson), Role: CreditRoleLyrics, Attribution: AttributionNamed, Certainty: CertaintyCertain},
		{PersonID: ptr(refNoPagePerson), Role: CreditRoleTune, Attribution: AttributionNamed, Certainty: CertaintyCertain},
		{Role: CreditRoleTune, Attribution: AttributionTraditional, Certainty: CertaintyProbable},
		{PersonID: ptr(refOwnPerson), Role: CreditRoleMusic, Attribution: AttributionNamed, Certainty: CertaintyCertain},
	}
	wren := d.Links[0]
	wren.Performances = []DocPerformance{
		{EventID: ptr(refEvent), Year: ptr(int16(2001)), DescriptionVerbatim: "Sung at the revel",
			PerformerIDs: []int64{refPerson, refNoPagePerson}},
		{EventID: ptr(refPendingEvent), DescriptionVerbatim: feastPerformance, PerformerIDs: []int64{refPerson}},
	}
	d.Links = []DocPieceLink{wren,
		{DocLink: DocLink{URL: hiddenLinkURL, Title: ptr("A recording"), Roles: []LinkRole{LinkRolePerformance}}},
		{DocLink: DocLink{URL: songbookPageURL, CollectionID: ptr(refHiddenColl), Roles: []LinkRole{LinkRoleLyrics}}},
	}
	d.Preferences = testPreferences()

	return d
}

// pieceProjection is fullPiece as a contributor may edit it.
func pieceProjection() PieceDocument {
	d := fullPiece()
	d.Terms = []DocPieceTerm{d.Terms[0], d.Terms[1], d.Terms[3]}
	d.Relations = d.Relations[:1]
	d.Credits = []DocCredit{d.Credits[0], d.Credits[2], d.Credits[3]}
	wren := d.Links[0]
	wren.Performances = []DocPerformance{wren.Performances[0], wren.Performances[1]}
	wren.Performances[0].PerformerIDs = []int64{refPerson}
	wren.Performances[1].EventID = nil
	songbook := d.Links[2]
	songbook.CollectionID = nil
	d.Links = []DocPieceLink{wren, songbook}
	d.Preferences = nil

	return d
}

// fullPerson is a person holding every kind of element a contributor may be withheld.
func fullPerson() PersonDocument {
	d, _ := personWithHiddenAlias()
	d.AffiliationIDs = []int64{refAffiliation, refOthersTerm}
	d.Relations = []DocPersonRelation{{ToPersonID: refPerson, Type: PersonRelationTypeMemberOf},
		{ToPersonID: refNoPagePerson, Type: PersonRelationTypeHeirOf}}
	morwen := d.Links[0]
	morwen.Performances = append(slices.Clone(morwen.Performances), DocPersonPerformance{PieceID: ptr(refPendingPiece),
		DocPerformance: DocPerformance{DescriptionVerbatim: secretPerformance}})
	d.Links = []DocPersonLink{morwen,
		{DocLink: DocLink{URL: hiddenLinkURL, Roles: []LinkRole{LinkRolePerformance}}}}
	d.Preferences = testPreferences()

	return d
}

func personProjection() PersonDocument {
	d := fullPerson()
	d.Aliases = []DocPersonAlias{d.Aliases[0], d.Aliases[2]}
	d.AffiliationIDs = d.AffiliationIDs[:1]
	d.Relations = d.Relations[:1]
	morwen := d.Links[0]
	morwen.Performances = morwen.Performances[:1]
	d.Links = []DocPersonLink{morwen}
	d.Preferences = nil

	return d
}

// noPagePerson is a person who prefers no page, with a profile.
func noPagePerson() PersonDocument {
	d := validPerson()
	d.Status, d.StatusSourceURL = PersonStatusPrefersNoPage, ptr("https://example.org/opt-out")
	d.NotesMD, d.Pronouns = ptr(privateNotes), ptr("they/them")
	d.Aliases = append(slices.Clone(d.Aliases), hiddenLegalName)
	d.Preferences = testPreferences()

	return d
}

func TestEditable(t *testing.T) {
	t.Parallel()

	contributor := editorVisibility(t, RoleContributor)
	moderator := editorVisibility(t, RoleModerator)
	withoutPreferences := func(d PersonDocument) PersonDocument {
		d.Preferences = nil

		return d
	}
	tests := []struct {
		id   string
		got  any
		want any
	}{
		{id: "W01_HiddenAliasAbsent", got: first(EditableDocument(personID, fullPerson(), contributor)).Aliases,
			want: personProjection().Aliases},
		{id: "W02_ModeratorSeesHiddenAlias", got: first(EditableDocument(personID, fullPerson(), moderator)),
			want: withoutPreferences(fullPerson())},
		{id: "W08_HiddenLinkAbsent", got: linkURLs(first(EditableDocument(pieceID, fullPiece(), contributor))),
			want: []string{validPiece().Links[0].URL, songbookPageURL}},
		{id: "W11_NoPageCreditAbsent", got: first(EditableDocument(pieceID, fullPiece(), contributor)).Credits,
			want: pieceProjection().Credits},
		{id: "W12_NoPagePerformerAbsent",
			got:  first(EditableDocument(pieceID, fullPiece(), contributor)).Links[0].Performances[0].PerformerIDs,
			want: []int64{refPerson}},
		{id: "W13_NoPagePersonIdentityOnly", got: first(EditableDocument(refNoPagePerson, noPagePerson(), contributor)),
			want: identityOnly(noPagePerson())},
		{id: "W13b_NoPageNowIdentityOnly", got: first(EditableDocument(refNoPagePerson, validPerson(), contributor)),
			want: identityOnly(validPerson())},
		{id: "W16_PreferencesAbsentForEveryone",
			got: first(EditableDocument(pieceID, fullPiece(), moderator)).Preferences, want: []DocPreference(nil)},
		{id: "W17_OthersPendingTermAbsent", got: first(EditableDocument(pieceID, fullPiece(), contributor)).Terms,
			want: pieceProjection().Terms},
		{id: "W18_OwnPendingTermPresent",
			got: slices.Contains(first(EditableDocument(pieceID, fullPiece(), contributor)).Terms,
				DocPieceTerm{TermID: refOwnTerm}), want: true},
		{id: "W19_HiddenCollectionNulled",
			got: first(EditableDocument(pieceID, fullPiece(), contributor)).Links[1].CollectionID, want: (*int64)(nil)},
		{id: "W20_CurrentName", got: first(EditableDocument(personID, fullPerson(), contributor)).Name,
			want: fullPerson().Name},
		{id: "W36_OwnPendingPersonCredited",
			got: slices.ContainsFunc(first(EditableDocument(pieceID, fullPiece(), contributor)).Credits,
				func(c DocCredit) bool { return c.PersonID != nil && *c.PersonID == refOwnPerson }), want: true},
		{id: "W40_APITokenModeratorIsContributor",
			got:  first(EditableDocument(pieceID, fullPiece(), editorVisibility(t, RoleContributor))),
			want: pieceProjection()},
		{id: "Piece", got: first(EditableDocument(pieceID, fullPiece(), contributor)), want: pieceProjection()},
		{id: "Person", got: first(EditableDocument(personID, fullPerson(), contributor)), want: personProjection()},
		{id: "TermReferences", got: first(EditableDocument(termID, termReferencing(refOthersTerm, refOwnTerm), contributor)),
			want: termReferencing(0, refOwnTerm)},
		{id: "CollectionReferences",
			got:  first(EditableDocument(collectionID, collectionReferencing(refOthersTerm, refHiddenColl), contributor)),
			want: collectionReferencing(0, 0)},
		{id: "Event", got: first(EditableDocument(0, validEvent(), contributor)), want: validEvent()},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func first[A, B any](a A, _ B) A { return a }

// termReferencing is validTerm with the parent and related affiliation given, 0 for none.
func termReferencing(parent, related int64) TermDocument {
	d := validTerm()
	d.ParentID, d.RelatedAffiliationID = optionalID(parent), optionalID(related)

	return d
}

func collectionReferencing(region, parent int64) CollectionDocument {
	d := validCollection()
	d.RegionTermID, d.ParentID = optionalID(region), optionalID(parent)

	return d
}

func optionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}

	return &id
}

func TestEditable_ReceivedKeys(t *testing.T) {
	t.Parallel()

	_, keys := EditableDocument(personID, fullPerson(), editorVisibility(t, RoleContributor))
	assert.True(t, keys[aliasElementKey(secondAliasName)])
	assert.False(t, keys[aliasElementKey(legalName)], "a withheld element is not received")
	assert.False(t, keys[linkElementKey(hiddenLinkURL)])
	assert.True(t, keys[profileKey])
	_, keys = EditableDocument(refNoPagePerson, noPagePerson(), editorVisibility(t, RoleContributor))
	assert.False(t, keys[profileKey], "a no-page profile is withheld whole")
	assert.False(t, keys[aliasElementKey(secondAliasName)])
}

func TestEditable_NeverHoldsWithheld(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	withheld := []string{legalName, hiddenLinkURL, privateNotes, "they/them", preferenceSource, secretPerformance}
	check := func(t *testing.T, doc any) {
		t.Helper()
		walkStrings(reflect.ValueOf(doc), "", documentPaths, func(path string, v reflect.Value) {
			for _, w := range withheld {
				assert.NotContains(t, v.String(), w, path)
			}
		})
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		for _, id := range []string{`"term_id":24`, `"to_piece_id":3,`, `"person_id":11`, `"collection_id":32`,
			`"event_id":41`, `11]`, `24]`} {
			assert.NotContains(t, string(data), id)
		}
	}
	check(t, first(EditableDocument(pieceID, fullPiece(), vis)))
	check(t, first(EditableDocument(personID, fullPerson(), vis)))
	check(t, first(EditableDocument(refNoPagePerson, noPagePerson(), vis)))
}

func TestRestore(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	piece, pieceKeys := EditableDocument(pieceID, fullPiece(), vis)
	person, personKeys := EditableDocument(personID, fullPerson(), vis)
	restorePiece := func(edit func(*PieceDocument)) (PieceDocument, []Flag) {
		sub := piece
		edit(&sub)
		base := fullPiece()

		return RestoreDocument(pieceID, sub, &base, pieceKeys, vis)
	}
	restorePerson := func(edit func(*PersonDocument)) (PersonDocument, []Flag) {
		sub := person
		edit(&sub)
		base := fullPerson()

		return RestoreDocument(personID, sub, &base, personKeys, vis)
	}
	aliasNames := func(d PersonDocument) []string {
		var names []string
		for _, a := range d.Aliases {
			names = append(names, a.Name)
		}

		return names
	}

	t.Run("W03_Untouched", func(t *testing.T) {
		t.Parallel()
		got, flags := restorePerson(func(*PersonDocument) {})
		assert.Equal(t, fullPerson(), got)
		assert.Empty(t, flags)
	})
	t.Run("W04_AfterNearestSurvivingPredecessor", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePerson(func(d *PersonDocument) { d.Aliases = d.Aliases[:1] })
		assert.Equal(t, []string{secondAliasName, legalName}, aliasNames(got))
		got, _ = restorePerson(func(d *PersonDocument) { d.Aliases = d.Aliases[1:] })
		assert.Equal(t, []string{legalName, morwenTheBard}, aliasNames(got), "first, with no predecessor left")
	})
	t.Run("W05_AliasCollision", func(t *testing.T) {
		t.Parallel()
		typed := DocPersonAlias{Name: strings.ToLower(legalName), Kind: PersonAliasKindNickname, IsPublic: true}
		got, flags := restorePerson(func(d *PersonDocument) { d.Aliases = append(slices.Clone(d.Aliases), typed) })
		assert.Equal(t, []string{secondAliasName, morwenTheBard, typed.Name}, aliasNames(got),
			"the contributor's alias is kept, the hidden one not added")
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasCollision, Path: "/aliases/2"}}, flags)
	})
	t.Run("W09_HiddenLinkRestored", func(t *testing.T) {
		t.Parallel()
		got, flags := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, fullPiece(), got)
		assert.Empty(t, flags)
	})
	t.Run("W10_LinkCollision", func(t *testing.T) {
		t.Parallel()
		respelled := strings.Replace(hiddenLinkURL, "example", "EXAMPLE", 1)
		got, flags := restorePiece(func(d *PieceDocument) {
			d.Links = append(slices.Clone(d.Links), DocPieceLink{DocLink: DocLink{URL: respelled, Roles: []LinkRole{LinkRoleLyrics}}})
		})
		assert.Equal(t, []string{validPiece().Links[0].URL, songbookPageURL, respelled}, linkURLs(got))
		assert.Equal(t, []Flag{{Code: FlagHiddenLinkCollision, Path: pathThirdLink}}, flags)
	})
	t.Run("W11_NoPageCreditRestored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(d *PieceDocument) { d.Credits = slices.Clone(d.Credits[:2]) })
		assert.Equal(t, fullPiece().Credits[:3], got.Credits, "restored after its predecessor; the deleted credit stays deleted")
	})
	t.Run("W12_PerformerRestored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, []int64{refPerson, refNoPagePerson}, got.Links[0].Performances[0].PerformerIDs)
		assert.Equal(t, ptr(refPendingEvent), got.Links[0].Performances[1].EventID, "the nulled event is restored")
	})
	t.Run("PerformanceKeyChanged", func(t *testing.T) {
		t.Parallel()
		editPerformance := func(edit func(*DocPerformance)) func(*PieceDocument) {
			return func(d *PieceDocument) {
				d.Links = slices.Clone(d.Links)
				d.Links[0].Performances = slices.Clone(d.Links[0].Performances)
				edit(&d.Links[0].Performances[0])
			}
		}
		conflict := []Flag{{Code: FlagHiddenDataConflict, Path: pathFirstLinkPerformances}}
		for name, edit := range map[string]func(*PieceDocument){
			"Year":        editPerformance(func(p *DocPerformance) { p.Year = ptr(int16(2002)) }),
			"Description": editPerformance(func(p *DocPerformance) { p.DescriptionVerbatim = "Sung at another revel" }),
		} {
			got, flags := restorePiece(edit)
			assert.Equal(t, []int64{refPerson}, got.Links[0].Performances[0].PerformerIDs,
				"%s: the submitted performance is kept as sent", name)
			assert.Equal(t, conflict, flags, "%s: the withheld performer is never silently removed", name)
		}
		got, flags := restorePiece(func(d *PieceDocument) {
			d.Links = slices.Clone(d.Links)
			d.Links[0].Performances = d.Links[0].Performances[:1]
		})
		assert.Len(t, got.Links[0].Performances, 1, "the deleted performance stays deleted")
		assert.Equal(t, conflict, flags, "deleting a performance with a withheld event is flagged")
	})
	t.Run("PerformanceWithoutWithheldParts", func(t *testing.T) {
		t.Parallel()
		base := fullPiece()
		base.Links = slices.Clone(base.Links)
		base.Links[0].Performances = slices.Clone(base.Links[0].Performances)
		base.Links[0].Performances[1].EventID = ptr(refEvent)
		sub, keys := EditableDocument(pieceID, base, vis)
		sub.Links = slices.Clone(sub.Links)
		sub.Links[0].Performances = slices.Clone(sub.Links[0].Performances)
		sub.Links[0].Performances[1].Year = ptr(int16(1999))
		got, flags := RestoreDocument(pieceID, sub, &base, keys, vis)
		assert.Empty(t, flags, "an edited performance with nothing withheld is no conflict")
		assert.Equal(t, ptr(int16(1999)), got.Links[0].Performances[1].Year)
		sub.Links[0].Performances = sub.Links[0].Performances[:1]
		_, flags = RestoreDocument(pieceID, sub, &base, keys, vis)
		assert.Empty(t, flags, "nor is a deleted one")
	})
	t.Run("PersonPerformanceKeyChanged", func(t *testing.T) {
		t.Parallel()
		base := fullPerson()
		base.Links = slices.Clone(base.Links)
		base.Links[0].Performances = slices.Clone(base.Links[0].Performances)
		base.Links[0].Performances[0].EventID = ptr(refPendingEvent)
		base.Links[0].Performances[0].PerformerIDs = []int64{refPerson, refNoPagePerson}
		projection, keys := EditableDocument(personID, base, vis)
		restoreEdited := func(edit func(*DocPersonPerformance)) (PersonDocument, []Flag) {
			sub := projection
			sub.Links = slices.Clone(sub.Links)
			sub.Links[0].Performances = slices.Clone(sub.Links[0].Performances)
			edit(&sub.Links[0].Performances[0])

			return RestoreDocument(personID, sub, &base, keys, vis)
		}
		got, flags := restoreEdited(func(*DocPersonPerformance) {})
		assert.Equal(t, base, got, "unchanged: the withheld event and performer are restored")
		assert.Empty(t, flags)
		for name, edit := range map[string]func(*DocPersonPerformance){
			"Year":        func(p *DocPersonPerformance) { p.Year = ptr(int16(2002)) },
			"Description": func(p *DocPersonPerformance) { p.DescriptionVerbatim = "At the winter revel" },
		} {
			got, flags := restoreEdited(edit)
			edited := got.Links[0].Performances[1]
			assert.Equal(t, []int64{refPerson}, edited.PerformerIDs, "%s: kept as sent", name)
			assert.Nil(t, edited.EventID, name)
			assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: pathFirstLinkPerformances}}, flags, name)
		}
	})
	t.Run("W14_NoPageProfileFromBase", func(t *testing.T) {
		t.Parallel()
		base := noPagePerson()
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		sub.NotesMD = ptr("Typed notes.")
		got, flags := RestoreDocument(refNoPagePerson, sub, &base, keys, vis)
		assert.Equal(t, base, got, "the profile is the base's, the notes ignored")
		assert.Empty(t, flags)
	})
	t.Run("W15_NoPageStatusChange", func(t *testing.T) {
		t.Parallel()
		base := noPagePerson()
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		sub.Status, sub.StatusSourceURL = PersonStatusActive, ptr("https://example.org/opt-in")
		got, _ := RestoreDocument(refNoPagePerson, sub, &base, keys, vis)
		want := base
		want.Status, want.StatusSourceURL = sub.Status, sub.StatusSourceURL
		assert.Equal(t, want, got, "the identity is the contributor's")
	})
	t.Run("W16_PreferencesRestored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(d *PieceDocument) {
			d.Preferences = []DocPreference{{Dimension: PreferenceDimensionLicensing, SourceURL: linkA}}
		})
		assert.Equal(t, testPreferences(), got.Preferences, "copied whole from the base")
	})
	t.Run("W17_OthersPendingTermRestored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, fullPiece().Terms, got.Terms)
	})
	t.Run("W19_CollectionRestoredWhileNull", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, ptr(refHiddenColl), got.Links[2].CollectionID)
		got, _ = restorePiece(func(d *PieceDocument) {
			d.Links = slices.Clone(d.Links)
			d.Links[1].CollectionID = ptr(refCollection)
		})
		assert.Equal(t, ptr(refCollection), got.Links[2].CollectionID, "the contributor's value wins")
	})
	t.Run("W36_OwnPendingCreditKept", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, fullPiece().Credits[3], got.Credits[3])
	})
	t.Run("ReceivedAndDeleted", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(d *PieceDocument) { d.Links = d.Links[:1] })
		assert.Equal(t, []string{validPiece().Links[0].URL, hiddenLinkURL}, linkURLs(got),
			"a received link the author deleted stays deleted; the hidden one is restored")
		person, _ := restorePerson(func(d *PersonDocument) {
			d.Links = slices.Clone(d.Links)
			d.Links[0].Performances = nil
		})
		assert.Equal(t, fullPerson().Links[0].Performances[1:], person.Links[0].Performances,
			"the withheld performance of a piece that is not public is restored")
	})
	t.Run("R32_UnhiddenBeforeSubmit", func(t *testing.T) {
		t.Parallel()
		now := vis
		now.HiddenLinkURLs = nil
		base := fullPiece()
		got, _ := RestoreDocument(pieceID, piece, &base, pieceKeys, now)
		assert.Contains(t, linkURLs(got), hiddenLinkURL, "not received, so restored rather than deleted")
	})
	t.Run("NewEntity", func(t *testing.T) {
		t.Parallel()
		got, flags := RestoreDocument(0, validPiece(), nil, ElementKeySet{}, vis)
		assert.Equal(t, validPiece(), got)
		assert.Empty(t, flags)
	})
	t.Run("References", func(t *testing.T) {
		t.Parallel()
		term := termReferencing(refOthersTerm, refOwnTerm)
		sub, keys := EditableDocument(termID, term, vis)
		got, _ := RestoreDocument(termID, sub, &term, keys, vis)
		assert.Equal(t, term, got)
		collection := collectionReferencing(refOthersTerm, refHiddenColl)
		subColl, keys := EditableDocument(collectionID, collection, vis)
		gotColl, _ := RestoreDocument(collectionID, subColl, &collection, keys, vis)
		assert.Equal(t, collection, gotColl)
	})
}

func TestRestore_RoundTrip(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleContributor, RoleModerator} {
		vis := editorVisibility(t, role)
		roundTrip(t, pieceID, fullPiece(), vis)
		roundTrip(t, pieceID, validPiece(), vis)
		roundTrip(t, personID, fullPerson(), vis)
		roundTrip(t, refNoPagePerson, noPagePerson(), vis)
		roundTrip(t, termID, termReferencing(refOthersTerm, refOwnTerm), vis)
		roundTrip(t, collectionID, collectionReferencing(refOthersTerm, refHiddenColl), vis)
		roundTrip(t, 0, validEvent(), vis)
	}
}

// roundTrip checks restore(editable(d), d, keys(editable(d))) = d (W21).
func roundTrip[D Document](t *testing.T, id int64, doc D, vis EditorVisibility) {
	t.Helper()
	editable, keys := EditableDocument(id, doc, vis)
	got, flags := RestoreDocument(id, editable, &doc, keys, vis)
	assert.Equal(t, doc, got, "W21 %T as %s", doc, vis.Role)
	assert.Empty(t, flags)
}

func TestRestore_PreferencesEveryActor(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleModerator)
	base := fullPiece()
	editable, keys := EditableDocument(pieceID, base, vis)
	editable.NotesMD = ptr("A moderator's edit.")
	got, _ := RestoreDocument(pieceID, editable, &base, keys, vis)
	assert.Equal(t, testPreferences(), got.Preferences, "W37: preferences restored for a moderator too")
	assert.Equal(t, base.Links, got.Links, "a moderator is withheld nothing else")
}
