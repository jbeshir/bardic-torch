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

// The links of the merge fixtures.
const (
	linkA                 = "https://example.org/a"
	linkB                 = "https://example.org/b"
	linkC                 = "https://example.org/c"
	linkNew               = "https://example.org/new"
	linkNewToo            = "https://example.org/new-too"
	theirsTitle           = "Theirs"
	fieldTitle  FieldPath = "title"
)

// mergeBase is the base piece of the merge fixtures: validPiece with links A, B and C, and a preference.
func mergeBase() PieceDocument {
	d := validPiece()
	d.Links = []DocPieceLink{pieceLink(linkA, "A"), pieceLink(linkB, "B"), pieceLink(linkC, "C")}
	d.Preferences = []DocPreference{{Dimension: PreferenceDimensionRecording, Value: json.RawMessage(`"ask"`),
		SourceURL: preferenceSource}}

	return d
}

func pieceLink(url, title string) DocPieceLink {
	return DocPieceLink{DocLink: DocLink{URL: url, Title: ptr(title), Roles: []LinkRole{LinkRoleLyrics}}}
}

// The builders of the merge fixtures each return an edited copy of d, sharing no list with it.

func withLinks(d PieceDocument, links ...DocPieceLink) PieceDocument {
	d.Links = links

	return d
}

func addLink(d PieceDocument, link DocPieceLink) PieceDocument {
	return withLinks(d, append(slices.Clone(d.Links), link)...)
}

func removeLink(d PieceDocument, url string) PieceDocument {
	return withLinks(d, slices.DeleteFunc(slices.Clone(d.Links), func(l DocPieceLink) bool { return l.URL == url })...)
}

func retitleLink(d PieceDocument, url, title string) PieceDocument {
	links := slices.Clone(d.Links)
	for i := range links {
		if links[i].URL == url {
			links[i].Title = ptr(title)
		}
	}

	return withLinks(d, links...)
}

// orderLinks is d with its links in the order of urls.
func orderLinks(d PieceDocument, urls ...string) PieceDocument {
	var links []DocPieceLink
	for _, url := range urls {
		i := slices.IndexFunc(d.Links, func(l DocPieceLink) bool { return l.URL == url })
		links = append(links, d.Links[i])
	}

	return withLinks(d, links...)
}

func linkURLsOf(d PieceDocument) []string {
	return linkURLs(d)
}

func edit(d PieceDocument, mutate func(*PieceDocument)) PieceDocument {
	mutate(&d)

	return d
}

// mergeRow is a row of the stale-and-rebase table (plan §6.2) at the merge's level: ours (the revision) and theirs
// (the current document) as edits of mergeBase, the conflicts expected and a check of a clean merge.
type mergeRow struct {
	id        string
	ours      func(PieceDocument) PieceDocument
	theirs    func(PieceDocument) PieceDocument
	conflicts []FieldPath
	check     func(t *testing.T, merged PieceDocument)
}

func TestMerge(t *testing.T) {
	t.Parallel()

	same := func(d PieceDocument) PieceDocument { return d }
	normalizedA, err := NormalizeURL(linkA)
	require.NoError(t, err)
	normalizedNew, err := NormalizeURL(linkNew)
	require.NoError(t, err)
	rows := []mergeRow{
		{id: "NotStale", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.NotesMD = ptr("Ours.") })
		}, theirs: same, check: func(t *testing.T, m PieceDocument) { assert.Equal(t, "Ours.", *m.NotesMD) }},
		{id: "DisjointScalars", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.NotesMD = ptr("Ours.") })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = theirsTitle })
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, "Ours.", *m.NotesMD)
			assert.Equal(t, theirsTitle, m.Title)
		}},
		{id: "SameScalarDifferent", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = "Ours" })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = theirsTitle })
		}, conflicts: []FieldPath{fieldTitle}},
		{id: "SameScalarEqual", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = "Both" })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = "Both" })
		}, check: func(t *testing.T, m PieceDocument) { assert.Equal(t, "Both", m.Title) }},
		{id: "SourcedGroupNeverSplit", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.LicensingNotesMD = ptr("Ask first.") })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) {
				d.LicensingStatus, d.Licence = LicensingStatusKnown, ptr(LicenceCCBY)
			})
		}, conflicts: []FieldPath{"licensing"}},
		{id: "ConservationOneSide", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) {
				d.Conservation = DocConservation{Status: ConservationStatusEndangered, SourceURL: ptr(linkA)}
			})
		}, theirs: same, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, DocConservation{Status: ConservationStatusEndangered, SourceURL: ptr(linkA)}, m.Conservation)
		}},
		{id: "BothAddDifferentLinks", ours: func(d PieceDocument) PieceDocument {
			return withLinks(d, d.Links[0], pieceLink(linkNew, "New"), d.Links[1], d.Links[2])
		}, theirs: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNewToo, "New too"))
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkA, linkNew, linkB, linkC, linkNewToo}, linkURLsOf(m))
		}},
		{id: "SameURLDifferentTitles", ours: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNew, "Ours"))
		}, theirs: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNew, theirsTitle))
		}, conflicts: []FieldPath{FieldPath("links[" + normalizedNew + "]")}},
		{id: "SameURLRespelled", ours: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNew, "New"))
		}, theirs: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(strings.Replace(linkNew, "example", "EXAMPLE", 1), "New"))
		}, conflicts: []FieldPath{FieldPath("links[" + normalizedNew + "]")}},
		{id: "SameURLEqual", ours: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNew, "New"))
		}, theirs: func(d PieceDocument) PieceDocument {
			return addLink(d, pieceLink(linkNew, "New"))
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkA, linkB, linkC, linkNew}, linkURLsOf(m), "added once")
		}},
		{id: "EditVersusDelete", ours: func(d PieceDocument) PieceDocument {
			return retitleLink(d, linkA, "Retitled")
		}, theirs: func(d PieceDocument) PieceDocument {
			return removeLink(d, linkA)
		}, conflicts: []FieldPath{FieldPath("links[" + normalizedA + "]")}},
		{id: "DeleteUnchanged", ours: func(d PieceDocument) PieceDocument {
			return removeLink(d, linkA)
		}, theirs: same, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkB, linkC}, linkURLsOf(m))
		}},
		{id: "ReorderVersusAdd", ours: func(d PieceDocument) PieceDocument {
			return orderLinks(d, linkC, linkA, linkB)
		}, theirs: func(d PieceDocument) PieceDocument {
			return withLinks(d, d.Links[0], pieceLink(linkNew, "New"), d.Links[1], d.Links[2])
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkC, linkA, linkNew, linkB}, linkURLsOf(m))
		}},
		{id: "BothReorder", ours: func(d PieceDocument) PieceDocument {
			return orderLinks(d, linkC, linkA, linkB)
		}, theirs: func(d PieceDocument) PieceDocument {
			return orderLinks(d, linkB, linkA, linkC)
		}, conflicts: []FieldPath{"links#order"}},
		{id: "BothReorderEqually", ours: func(d PieceDocument) PieceDocument {
			return orderLinks(d, linkC, linkA, linkB)
		}, theirs: func(d PieceDocument) PieceDocument {
			return orderLinks(d, linkC, linkA, linkB)
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkC, linkA, linkB}, linkURLsOf(m))
		}},
		{id: "CreditsUnitList", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Credits = d.Credits[:1] })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Credits = d.Credits[1:] })
		}, conflicts: []FieldPath{"credits"}},
		{id: "SetAddVersusRemove", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.SecondaryKinds = []PieceKind{PieceKindPoem, PieceKindStory} })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.SecondaryKinds = nil })
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []PieceKind{PieceKindStory}, m.SecondaryKinds)
		}},
		{id: "AliasesSameKey", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) {
				d.Aliases = append(slices.Clone(d.Aliases), DocPieceAlias{Text: "Wren the Bold", Kind: PieceAliasKindNickname})
			})
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) {
				d.Aliases = append(slices.Clone(d.Aliases), DocPieceAlias{Text: "wren the bold", Kind: PieceAliasKindNickname})
			})
		}, conflicts: []FieldPath{"aliases[wren the bold]"}},
		{id: "WithheldLinkUnchanged", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.NotesMD = ptr("Ours.") })
		}, theirs: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.Title = theirsTitle })
		}, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, []string{linkA, linkB, linkC}, linkURLsOf(m), "a link neither side changed stays")
		}},
		{id: "TermReferenceUnchanged", ours: func(d PieceDocument) PieceDocument {
			return edit(d, func(d *PieceDocument) { d.NotesMD = ptr("Ours.") })
		}, theirs: same, check: func(t *testing.T, m PieceDocument) {
			assert.Equal(t, mergeBase().Terms, m.Terms)
		}},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			base := mergeBase()
			ours, theirs := row.ours(base), row.theirs(base)
			merged, conflicts := MergeDocuments(base, ours, theirs)
			assert.Equal(t, row.conflicts, conflictPaths(conflicts))
			if row.check != nil {
				row.check(t, merged)
			}
			assert.Equal(t, base, mergeBase(), "the inputs are not changed")
		})
	}
}

func TestMerge_ConflictValues(t *testing.T) {
	t.Parallel()

	base := mergeBase()
	_, conflicts := MergeDocuments(base, retitleLink(base, linkA, "Retitled"), removeLink(base, linkA))
	require.Len(t, conflicts, 1)
	assert.JSONEq(t, `{"url":"`+linkA+`","title":"A","description":null,"platform":null,"place_type":null,
		"collection_id":null,"locator":null,"roles":["lyrics"],"performances":null}`, string(conflicts[0].Base))
	assert.Contains(t, string(conflicts[0].Revision), "Retitled")
	assert.Equal(t, "null", string(conflicts[0].Current), "a missing element is null")

	_, conflicts = MergeDocuments(base, edit(base, func(d *PieceDocument) { d.LicensingNotesMD = ptr("Ask.") }),
		edit(base, func(d *PieceDocument) { d.LicensingStatus = LicensingStatusKnown }))
	require.Len(t, conflicts, 1)
	assert.JSONEq(t, `{"licensing_status":"unknown","licence":null,"licensing_notes_md":null}`,
		string(conflicts[0].Base), "a group is an object of its fields")
}

func TestMerge_Person(t *testing.T) {
	t.Parallel()

	base, _ := personWithHiddenAlias()
	ours := base
	ours.NotesMD = ptr("Ours.")
	theirs := base
	theirs.Aliases = slices.Delete(slices.Clone(base.Aliases), 1, 2)

	t.Run("WithheldAliasDeletedByCurrent", func(t *testing.T) {
		t.Parallel()
		merged, conflicts := MergeDocuments(base, ours, theirs)
		assert.Empty(t, conflicts)
		assert.Equal(t, theirs.Aliases, merged.Aliases, "the moderator's deletion wins")
		assert.Equal(t, "Ours.", *merged.NotesMD)
	})
	t.Run("SetsUnion", func(t *testing.T) {
		t.Parallel()
		o, th := base, base
		o.Roles = []PersonRole{PersonRoleSinger, PersonRolePoet}
		th.AffiliationIDs = nil
		merged, conflicts := MergeDocuments(base, o, th)
		assert.Empty(t, conflicts)
		assert.Equal(t, []PersonRole{PersonRoleSinger, PersonRolePoet}, merged.Roles)
		assert.Empty(t, merged.AffiliationIDs)
	})
	t.Run("StatusGroup", func(t *testing.T) {
		t.Parallel()
		o, th := base, base
		o.Status, o.StatusSourceURL = PersonStatusDeceased, ptr(linkA)
		th.StatusSourceURL = ptr(linkB)
		_, conflicts := MergeDocuments(base, o, th)
		require.Len(t, conflicts, 1)
		assert.Equal(t, FieldPath("status"), conflicts[0].Path)
	})
}

func TestMerge_OtherTypes(t *testing.T) {
	t.Parallel()

	term := validTerm()
	ours, theirs := term, term
	ours.Aliases = []string{validTerm().Aliases[0], "Dreamy"}
	theirs.Name = "Wistfulness"
	merged, conflicts := MergeDocuments(term, ours, theirs)
	assert.Empty(t, conflicts)
	assert.Equal(t, []string{validTerm().Aliases[0], "Dreamy"}, merged.Aliases)
	assert.Equal(t, "Wistfulness", merged.Name)

	// Sets never conflict: an alias removed on either side is removed, even if the other side re-spelled it under
	// the same collation key.
	respelled, removed := term, term
	respelled.Aliases = []string{strings.ToUpper(term.Aliases[0])}
	removed.Aliases = nil
	require.Equal(t, CollationKey(term.Aliases[0]), CollationKey(respelled.Aliases[0]))
	for name, sides := range map[string][2]TermDocument{"ours_respells": {respelled, removed},
		"theirs_respells": {removed, respelled}} {
		merged, conflicts = MergeDocuments(term, sides[0], sides[1])
		assert.Empty(t, conflicts, name)
		assert.Empty(t, merged.Aliases, name+": the removal wins")
	}

	collection := validCollection()
	co, ct := collection, collection
	co.ParentID, ct.ParentID = nil, ptr(refChildColl)
	_, conflicts = MergeDocuments(collection, co, ct)
	assert.Equal(t, []FieldPath{"parent_id"}, conflictPaths(conflicts))

	event := validEvent()
	eo := event
	eo.DescriptionMD = ptr("A revel.")
	mergedEvent, conflicts := MergeDocuments(event, eo, event)
	assert.Empty(t, conflicts)
	assert.Equal(t, eo, mergedEvent)
}

func conflictPaths(conflicts []RebaseConflict) []FieldPath {
	var paths []FieldPath
	for _, c := range conflicts {
		paths = append(paths, c.Path)
	}

	return paths
}

func TestDiffDocuments(t *testing.T) {
	t.Parallel()

	base := mergeBase()
	normalizedA, err := NormalizeURL(linkA)
	require.NoError(t, err)
	normalizedNew, err := NormalizeURL(linkNew)
	require.NoError(t, err)
	other := edit(orderLinks(retitleLink(addLink(base, pieceLink(linkNew, "New")), linkA, "Retitled"),
		linkB, linkA, linkC, linkNew), func(d *PieceDocument) {
		d.Title, d.PeriodEnd = "Other", ptr(int16(1400))
	})
	assert.Equal(t, []FieldPath{fieldTitle, "period", FieldPath("links[" + normalizedA + "]"),
		FieldPath("links[" + normalizedNew + "]"), "links#order"}, DiffDocuments(base, other))
	assert.Empty(t, DiffDocuments(base, base))

	empty := base
	empty.Aliases = []DocPieceAlias{}
	none := base
	none.Aliases = nil
	assert.Empty(t, DiffDocuments(empty, none), "an empty list is no list")
}

func TestMergeFields_CoverEveryDocumentField(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, typ reflect.Type, fields []docField) {
		t.Helper()
		var covered []string
		for _, f := range fields {
			for _, name := range f.names {
				require.NotNil(t, jsonFieldIndex(typ, name), "%s has no field %s", typ, name)
				covered = append(covered, name)
			}
		}
		var all []string
		for i := range typ.NumField() {
			if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ","); name != "schema_version" {
				all = append(all, name)
			}
		}
		assert.ElementsMatch(t, all, covered, "%s: every field but schema_version, once", typ)
	}
	check(t, reflect.TypeFor[PieceDocument](), pieceFields)
	check(t, reflect.TypeFor[PersonDocument](), personFields)
	check(t, reflect.TypeFor[TermDocument](), termFields)
	check(t, reflect.TypeFor[CollectionDocument](), collectionFields)
	check(t, reflect.TypeFor[EventDocument](), eventFields)
}

// mutatePiece applies an edit to d chosen by each byte of ops: the fuzz test's way of building documents.
func mutatePiece(d PieceDocument, ops []byte) PieceDocument {
	urls := []string{linkA, linkB, linkC, linkNew, linkNewToo}
	for _, op := range ops {
		url := urls[int(op>>4)%len(urls)]
		has := slices.ContainsFunc(d.Links, func(l DocPieceLink) bool { return l.URL == url })
		switch op % 8 {
		case 0:
			d.Title += string(rune('a' + op%26))
		case 1:
			if !has {
				d = addLink(d, pieceLink(url, "Added"))
			}
		case 2:
			d = removeLink(d, url)
		case 3:
			d = retitleLink(d, url, string(rune('A'+op%26)))
		case 4:
			links := slices.Clone(d.Links)
			slices.Reverse(links)
			d = withLinks(d, links...)
		case 5:
			d.SecondaryKinds = append(slices.Clone(d.SecondaryKinds), []PieceKind{PieceKindStory, PieceKindPoem}[op%2])
		case 6:
			d.LicensingNotesMD = ptr(string(rune('a' + op%26)))
		case 7:
			d.Aliases = append(slices.Clone(d.Aliases), DocPieceAlias{Text: string(rune('a' + op%26)),
				Kind: PieceAliasKindNickname})
		}
	}

	return d
}

func TestMerge_Properties(t *testing.T) {
	t.Parallel()

	base := mergeBase()
	for _, ops := range [][]byte{{0}, {1, 0x31}, {2, 0x12}, {3, 4}, {5, 6, 7}, {0x41, 0x22, 4, 0x13}} {
		x := mutatePiece(base, ops)
		y := mutatePiece(base, append([]byte{0x47}, ops...))
		merged, conflicts := MergeDocuments(base, base, x)
		assert.Empty(t, conflicts)
		assert.True(t, equalValues(reflect.ValueOf(x), reflect.ValueOf(merged)), "merge(b, b, t) = t")
		merged, conflicts = MergeDocuments(base, x, base)
		assert.Empty(t, conflicts)
		assert.True(t, equalValues(reflect.ValueOf(x), reflect.ValueOf(merged)), "merge(b, o, b) = o")
		merged, conflicts = MergeDocuments(base, x, x)
		assert.Empty(t, conflicts)
		assert.True(t, equalValues(reflect.ValueOf(x), reflect.ValueOf(merged)), "merge(b, x, x) = x")
		merged, _ = MergeDocuments(base, x, y)
		assert.Subset(t, unionKeys(linkURLs(x), linkURLs(y), linkURLs(base)), linkURLs(merged),
			"the merged keys are of the inputs")
	}
}
