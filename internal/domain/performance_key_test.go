package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures of the performance key tests.
const (
	revelPerformance = "Sung at the revel"
	fairPerformance  = "Sung at the fair"
	issuedKey        = "a1a1a1a1a1a1a1a1"
	unknownKey       = "b2b2b2b2b2b2b2b2"
	// firstFreshKey is the first key countingKeys issues.
	firstFreshKey = "0000000000000001"
)

// keyedPerformance is a performance with an issued key, a withheld event and a withheld performer.
func keyedPerformance() DocPerformance {
	return DocPerformance{Key: issuedKey, EventID: ptr(refPendingEvent), Year: ptr(int16(2001)),
		DescriptionVerbatim: revelPerformance, PerformerIDs: []int64{refPerson, refNoPagePerson}}
}

// importedPerformance is a performance stored without a key, with a withheld performer.
func importedPerformance() DocPerformance {
	return DocPerformance{DescriptionVerbatim: feastPerformance, PerformerIDs: []int64{refPerson, refNoPagePerson}}
}

// pieceWithPerformances is validPiece with its first link recording performances.
func pieceWithPerformances(performances ...DocPerformance) PieceDocument {
	d := validPiece()
	d.Links = slices.Clone(d.Links)
	d.Links[0].Performances = performances

	return d
}

// shown is a performance as a contributor is shown it: without its withheld event and performer, with key.
func shown(p DocPerformance, key string) DocPerformance {
	p.Key, p.EventID, p.PerformerIDs = key, nil, []int64{refPerson}

	return p
}

func TestRestore_PerformanceKey(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	url := validPiece().Links[0].URL
	imported := importedPerformance()
	derived := derivedPerformanceKey(url, imported, nil)
	conflict := []Flag{{Code: FlagHiddenDataConflict, Path: pathFirstLinkPerformances}}
	withYear := func(p DocPerformance, year int16) DocPerformance {
		p.Year = &year

		return p
	}
	withDescription := func(p DocPerformance, description string) DocPerformance {
		p.DescriptionVerbatim = description

		return p
	}
	tests := []struct {
		name string
		base []DocPerformance
		// edit gives the submitted performances from those the contributor was shown.
		edit      func(shown []DocPerformance) []DocPerformance
		want      []DocPerformance
		wantFlags []Flag
	}{
		{name: "key_and_year_edited_restored",
			base: []DocPerformance{keyedPerformance()},
			edit: func(s []DocPerformance) []DocPerformance { return []DocPerformance{withYear(s[0], 2002)} },
			want: []DocPerformance{withYear(keyedPerformance(), 2002)}},
		{name: "key_and_description_edited_restored",
			base: []DocPerformance{keyedPerformance()},
			edit: func(s []DocPerformance) []DocPerformance {
				return []DocPerformance{withDescription(s[0], "Sung at the winter revel")}
			},
			want: []DocPerformance{withDescription(keyedPerformance(), "Sung at the winter revel")}},
		{name: "key_deleted_is_conflict",
			base: []DocPerformance{keyedPerformance(), imported},
			edit: func(s []DocPerformance) []DocPerformance { return s[1:] },
			want: []DocPerformance{imported}, wantFlags: conflict},
		{name: "unknown_key_is_new",
			base: []DocPerformance{keyedPerformance()},
			edit: func(s []DocPerformance) []DocPerformance {
				p := s[0]
				p.Key = unknownKey

				return []DocPerformance{p}
			},
			want:      []DocPerformance{shown(keyedPerformance(), firstFreshKey)},
			wantFlags: conflict},
		{name: "duplicate_key_first_kept",
			base: []DocPerformance{keyedPerformance()},
			edit: func(s []DocPerformance) []DocPerformance {
				return []DocPerformance{withYear(s[0], 2002), withDescription(s[0], "Sung again")}
			},
			want: []DocPerformance{withYear(keyedPerformance(), 2002),
				shown(withDescription(keyedPerformance(), "Sung again"), firstFreshKey)}},
		{name: "derived_key_matches_imported",
			base: []DocPerformance{imported},
			edit: func(s []DocPerformance) []DocPerformance { return []DocPerformance{withYear(s[0], 1999)} },
			want: []DocPerformance{withYear(imported, 1999)}},
		{name: "derived_key_collision_matches_by_content",
			base: []DocPerformance{imported, imported},
			edit: func(s []DocPerformance) []DocPerformance { return s },
			want: []DocPerformance{imported, imported}},
		{name: "absent_key_matches_by_content",
			base: []DocPerformance{keyedPerformance(), imported},
			edit: func(s []DocPerformance) []DocPerformance {
				return []DocPerformance{shown(keyedPerformance(), ""), shown(imported, "")}
			},
			want: []DocPerformance{keyedPerformance(), imported}},
		{name: "absent_key_edited_is_conflict",
			base: []DocPerformance{keyedPerformance()},
			edit: func([]DocPerformance) []DocPerformance {
				return []DocPerformance{shown(withYear(keyedPerformance(), 2002), "")}
			},
			want:      []DocPerformance{shown(withYear(keyedPerformance(), 2002), firstFreshKey)},
			wantFlags: conflict},
		{name: "new_performance_gets_fresh_key",
			base: []DocPerformance{imported},
			edit: func(s []DocPerformance) []DocPerformance {
				return append(slices.Clone(s), DocPerformance{DescriptionVerbatim: fairPerformance})
			},
			want: []DocPerformance{imported, {Key: firstFreshKey, DescriptionVerbatim: fairPerformance}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := pieceWithPerformances(tt.base...)
			projection, received := EditableDocument(pieceID, base, vis)
			sub := projection
			sub.Links = slices.Clone(sub.Links)
			sub.Links[0].Performances = tt.edit(slices.Clone(sub.Links[0].Performances))
			got, flags := RestoreDocument(pieceID, sub, &base, received, vis, countingKeys())
			assert.Equal(t, tt.want, got.Links[0].Performances)
			assert.Equal(t, tt.wantFlags, flags)
		})
	}
	t.Run("shown_keys", func(t *testing.T) {
		t.Parallel()
		projection, _ := EditableDocument(pieceID, pieceWithPerformances(keyedPerformance(), imported), vis)
		assert.Equal(t, []DocPerformance{shown(keyedPerformance(), issuedKey), shown(imported, derived)},
			projection.Links[0].Performances, "an issued key as stored, a derived one for a performance without")
	})
	t.Run("new_link_performances_get_fresh_keys", func(t *testing.T) {
		t.Parallel()
		base := validPiece()
		sub, received := EditableDocument(pieceID, base, vis)
		sub.Links = append(slices.Clone(sub.Links), DocPieceLink{
			DocLink:      DocLink{URL: "https://example.org/new-recording", Roles: []LinkRole{LinkRolePerformance}},
			Performances: []DocPerformance{{Key: issuedKey, DescriptionVerbatim: fairPerformance}},
		})
		got, _ := RestoreDocument(pieceID, sub, &base, received, vis, countingKeys())
		assert.Equal(t, firstFreshKey, got.Links[1].Performances[0].Key, "a key the base does not have is not kept")
	})
	t.Run("person_link_performance", func(t *testing.T) {
		t.Parallel()
		base := validPerson()
		base.Links = slices.Clone(base.Links)
		perf := keyedPerformance()
		base.Links[0].Performances = []DocPersonPerformance{{PieceID: ptr(refPiece), DocPerformance: perf}}
		projection, received := EditableDocument(personID, base, vis)
		require.Equal(t, issuedKey, projection.Links[0].Performances[0].Key)
		sub := projection
		sub.Links = slices.Clone(sub.Links)
		sub.Links[0].Performances = slices.Clone(sub.Links[0].Performances)
		sub.Links[0].Performances[0].Year = ptr(int16(2002))
		got, flags := RestoreDocument(personID, sub, &base, received, vis, countingKeys())
		assert.Equal(t, []DocPersonPerformance{{PieceID: ptr(refPiece), DocPerformance: withYear(perf, 2002)}},
			got.Links[0].Performances, "the withheld event and performer are restored")
		assert.Empty(t, flags)
	})
}

func TestDerivedPerformanceKey_HidesWithheldPiece(t *testing.T) {
	t.Parallel()

	const url = "https://example.org/morwen"
	perf := DocPerformance{Year: ptr(int16(2001)), DescriptionVerbatim: secretPerformance}
	contributor := &projector{vis: editorVisibility(t, RoleContributor)}
	moderator := &projector{vis: editorVisibility(t, RoleModerator)}
	withPieceZero := derivedPerformanceKey(url, perf, ptr(int64(0)))
	tests := []struct {
		name    string
		p       *projector
		perf    DocPerformance
		pieceID *int64
		want    string
	}{
		{name: "visible_piece_hashed", p: contributor, perf: perf, pieceID: ptr(refPiece),
			want: derivedPerformanceKey(url, perf, ptr(refPiece))},
		{name: "withheld_piece_is_zero", p: contributor, perf: perf, pieceID: ptr(refPendingPiece), want: withPieceZero},
		{name: "moderator_sees_piece", p: moderator, perf: perf, pieceID: ptr(refPendingPiece),
			want: derivedPerformanceKey(url, perf, ptr(refPendingPiece))},
		{name: "link_of_piece", p: contributor, perf: perf, want: derivedPerformanceKey(url, perf, nil)},
		{name: "stored_key_kept", p: contributor, perf: DocPerformance{Key: issuedKey, DescriptionVerbatim: "x"},
			pieceID: ptr(refPendingPiece), want: issuedKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.p.effectiveKey(url, tt.perf, tt.pieceID)
			assert.Equal(t, tt.want, got)
			assert.True(t, validPerformanceKey(got), "a derived key has the form of an issued one")
		})
	}
	t.Run("withheld_piece_differs_from_its_key", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, derivedPerformanceKey(url, perf, ptr(refPendingPiece)), withPieceZero)
	})
	t.Run("colliding_performances", func(t *testing.T) {
		t.Parallel()
		perf := importedPerformance()
		other := perf
		other.PerformerIDs = []int64{refPerson}
		assert.Equal(t, contributor.effectiveKey(url, perf, nil), contributor.effectiveKey(url, other, nil),
			"performances differing only in what the key does not hash share it, so restore matches them by content")
	})
}

func TestSnapshot_DropsPerformanceKey(t *testing.T) {
	t.Parallel()

	vis := testVisibility()
	piece := NewPieceSnapshot(pieceWithPerformances(keyedPerformance()), vis)
	assert.NotContains(t, fmt.Sprintf("%#v", piece), issuedKey)
	vis.Persons[subjectPersonID] = PersonSummary{Slug: wrenSlug, Name: wrenName, Kind: PersonKindCommunityBard}
	person := personDocument()
	person.Links = []DocPersonLink{{DocLink: DocLink{URL: "https://example.org/wren-recording",
		Roles: []LinkRole{LinkRolePerformance}}, Performances: []DocPersonPerformance{{DocPerformance: keyedPerformance()}}}}
	view, err := NewPersonSnapshot(subjectPersonID, person, vis)
	require.NoError(t, err)
	assert.NotContains(t, fmt.Sprintf("%#v", view), issuedKey)
}

func TestEditorPerformance_Key(t *testing.T) {
	t.Parallel()

	slugs := SlugMap{}
	editor := ToEditorPiece(pieceWithPerformances(keyedPerformance(), importedPerformance()), slugs)
	assert.Equal(t, ptr(issuedKey), editor.Links[0].Performances[0].Key)
	assert.Nil(t, editor.Links[0].Performances[1].Key, "a performance stored without a key is sent null")
	data, err := json.Marshal(editor.Links[0].Performances[1])
	require.NoError(t, err)
	assert.Contains(t, string(data), `"key":null`)
	doc, _ := FromEditorPiece(editor, RefMap{})
	assert.Equal(t, issuedKey, doc.Links[0].Performances[0].Key)
	assert.Empty(t, doc.Links[0].Performances[1].Key, "null is no key")
}
