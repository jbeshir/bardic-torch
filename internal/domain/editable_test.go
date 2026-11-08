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

// testPreferences are a preference whose source is the hidden link, which a contributor is withheld, and one
// whose source is not a link.
func testPreferences() []DocPreference {
	return []DocPreference{
		preference(PreferenceDimensionPerformancePermission, hiddenLinkURL, PreferenceChoiceGiven),
		preference(PreferenceDimensionRecording, preferenceSource, PreferenceChoiceAskBeforeRecording),
	}
}

// shownPreferences are the testPreferences a contributor is shown.
func shownPreferences() []DocPreference { return testPreferences()[1:] }

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
	d.Preferences = shownPreferences()

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
	d.Preferences = shownPreferences()

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
	tests := []struct {
		id   string
		got  any
		want any
	}{
		{id: "hidden_alias_absent", got: first(EditableDocument(personID, fullPerson(), contributor)).Aliases,
			want: personProjection().Aliases},
		{id: "moderator_sees_hidden_alias", got: first(EditableDocument(personID, fullPerson(), moderator)),
			want: fullPerson()},
		{id: "hidden_link_absent", got: linkURLs(first(EditableDocument(pieceID, fullPiece(), contributor))),
			want: []string{validPiece().Links[0].URL, songbookPageURL}},
		{id: "no_page_credit_absent", got: first(EditableDocument(pieceID, fullPiece(), contributor)).Credits,
			want: pieceProjection().Credits},
		{id: "no_page_performer_absent",
			got:  first(EditableDocument(pieceID, fullPiece(), contributor)).Links[0].Performances[0].PerformerIDs,
			want: []int64{refPerson}},
		{id: "no_page_person_identity_only", got: first(EditableDocument(refNoPagePerson, noPagePerson(), contributor)),
			want: identityOnly(noPagePerson())},
		{id: "no_page_now_identity_only", got: first(EditableDocument(refNoPagePerson, validPerson(), contributor)),
			want: identityOnly(validPerson())},
		{id: "moderator_sees_hidden_source_preference",
			got: first(EditableDocument(pieceID, fullPiece(), moderator)).Preferences, want: testPreferences()},
		{id: "others_pending_term_absent", got: first(EditableDocument(pieceID, fullPiece(), contributor)).Terms,
			want: pieceProjection().Terms},
		{id: "own_pending_term_present",
			got: slices.Contains(first(EditableDocument(pieceID, fullPiece(), contributor)).Terms,
				DocPieceTerm{TermID: refOwnTerm}), want: true},
		{id: "hidden_collection_nulled",
			got: first(EditableDocument(pieceID, fullPiece(), contributor)).Links[1].CollectionID, want: (*int64)(nil)},
		{id: "renamed_person_shows_current_name", got: first(EditableDocument(personID, fullPerson(), contributor)).Name,
			want: fullPerson().Name},
		{id: "own_pending_person_credited",
			got: slices.ContainsFunc(first(EditableDocument(pieceID, fullPiece(), contributor)).Credits,
				func(c DocCredit) bool { return c.PersonID != nil && *c.PersonID == refOwnPerson }), want: true},
		{id: "api_token_moderator_is_contributor",
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
	withheld := []string{legalName, hiddenLinkURL, privateNotes, "they/them", secretPerformance}
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

	t.Run("untouched", func(t *testing.T) {
		t.Parallel()
		got, flags := restorePerson(func(*PersonDocument) {})
		assert.Equal(t, fullPerson(), got)
		assert.Empty(t, flags)
	})
	t.Run("after_nearest_surviving_predecessor", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePerson(func(d *PersonDocument) { d.Aliases = d.Aliases[:1] })
		assert.Equal(t, []string{secondAliasName, legalName}, aliasNames(got))
		got, _ = restorePerson(func(d *PersonDocument) { d.Aliases = d.Aliases[1:] })
		assert.Equal(t, []string{legalName, morwenTheBard}, aliasNames(got), "first, with no predecessor left")
	})
	t.Run("alias_collision", func(t *testing.T) {
		t.Parallel()
		typed := DocPersonAlias{Name: strings.ToLower(legalName), Kind: PersonAliasKindNickname, IsPublic: true}
		got, flags := restorePerson(func(d *PersonDocument) { d.Aliases = append(slices.Clone(d.Aliases), typed) })
		assert.Equal(t, []string{secondAliasName, morwenTheBard, typed.Name}, aliasNames(got),
			"the contributor's alias is kept, the hidden one not added")
		assert.Equal(t, []Flag{{Code: FlagHiddenAliasCollision, Path: "/aliases/2"}}, flags)
	})
	t.Run("hidden_link_restored", func(t *testing.T) {
		t.Parallel()
		got, flags := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, fullPiece(), got)
		assert.Empty(t, flags)
	})
	t.Run("link_collision", func(t *testing.T) {
		t.Parallel()
		respelled := strings.Replace(hiddenLinkURL, "example", "EXAMPLE", 1)
		got, flags := restorePiece(func(d *PieceDocument) {
			d.Links = append(slices.Clone(d.Links), DocPieceLink{DocLink: DocLink{URL: respelled, Roles: []LinkRole{LinkRoleLyrics}}})
		})
		assert.Equal(t, []string{validPiece().Links[0].URL, songbookPageURL, respelled}, linkURLs(got))
		assert.Equal(t, []Flag{{Code: FlagHiddenLinkCollision, Path: pathThirdLink}}, flags)
	})
	t.Run("no_page_credit_restored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(d *PieceDocument) { d.Credits = slices.Clone(d.Credits[:2]) })
		assert.Equal(t, fullPiece().Credits[:3], got.Credits, "restored after its predecessor; the deleted credit stays deleted")
	})
	t.Run("performer_restored", func(t *testing.T) {
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
	t.Run("no_page_profile_from_base", func(t *testing.T) {
		t.Parallel()
		base := noPagePerson()
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		sub.NotesMD = ptr("Typed notes.")
		got, flags := RestoreDocument(refNoPagePerson, sub, &base, keys, vis)
		assert.Equal(t, base, got, "the profile is the base's, the notes ignored")
		assert.Empty(t, flags)
	})
	t.Run("no_page_status_change", func(t *testing.T) {
		t.Parallel()
		base := noPagePerson()
		sub, keys := EditableDocument(refNoPagePerson, base, vis)
		sub.Status, sub.StatusSourceURL = PersonStatusActive, ptr("https://example.org/opt-in")
		got, _ := RestoreDocument(refNoPagePerson, sub, &base, keys, vis)
		want := base
		want.Status, want.StatusSourceURL = sub.Status, sub.StatusSourceURL
		assert.Equal(t, want, got, "the identity is the contributor's")
	})
	t.Run("others_pending_term_restored", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, fullPiece().Terms, got.Terms)
	})
	t.Run("collection_restored_while_null", func(t *testing.T) {
		t.Parallel()
		got, _ := restorePiece(func(*PieceDocument) {})
		assert.Equal(t, ptr(refHiddenColl), got.Links[2].CollectionID)
		got, _ = restorePiece(func(d *PieceDocument) {
			d.Links = slices.Clone(d.Links)
			d.Links[1].CollectionID = ptr(refCollection)
		})
		assert.Equal(t, ptr(refCollection), got.Links[2].CollectionID, "the contributor's value wins")
	})
	t.Run("own_pending_credit_kept", func(t *testing.T) {
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
	t.Run("unhidden_before_submit", func(t *testing.T) {
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

// roundTrip checks restore(editable(d), d, keys(editable(d))) = d.
func roundTrip[D Document](t *testing.T, id int64, doc D, vis EditorVisibility) {
	t.Helper()
	editable, keys := EditableDocument(id, doc, vis)
	got, flags := RestoreDocument(id, editable, &doc, keys, vis)
	assert.Equal(t, doc, got, "%T as %s", doc, vis.Role)
	assert.Empty(t, flags)
}

// TestDocReferences_KeysMatchRestoreKeys checks that the key of each reference of a full piece and a full person is
// one of the keys restore records for what a moderator is shown, so that a reference key can be looked up in a
// draft's received keys.
func TestDocReferences_KeysMatchRestoreKeys(t *testing.T) {
	t.Parallel()

	moderator := editorVisibility(t, RoleModerator)
	_, pieceKeys := EditableDocument(pieceID, fullPiece(), moderator)
	for _, r := range documentReferences(fullPiece()) {
		assert.True(t, pieceKeys[r.Key], "piece reference %s has key %s, which restore does not record", r.Path, r.Key)
	}
	_, personKeys := EditableDocument(personID, fullPerson(), moderator)
	for _, r := range documentReferences(fullPerson()) {
		assert.True(t, personKeys[r.Key], "person reference %s has key %s, which restore does not record", r.Path,
			r.Key)
	}
	assert.NotEmpty(t, documentReferences(fullPiece()))
	assert.NotEmpty(t, documentReferences(fullPerson()))
}
