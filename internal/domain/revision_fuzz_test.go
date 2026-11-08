package domain_test

import (
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/stretchr/testify/require"
)

// The revision engine's fuzz targets start from the documents of the scrubbed import (importtest.Scrubbed), so
// the fuzzer explores the shapes the catalogue really holds: imported credits, links with performances, hidden
// aliases, hidden links and persons who prefer no page. Plain test runs run every seed; `go test -fuzz
// FuzzMergePiece ./internal/domain/` (or FuzzRestorePerson) explores further.

// mergeSeedOps are pairs of edits (ours, theirs) for domain.MutatePiece.
var mergeSeedOps = [][2][]byte{
	{{0}, {1, 0x31}},
	{{4}, {0x13, 4}},
	{{2, 0x12}, {3, 0x12}},
	{{5, 6, 7}, {6, 7, 0x21}},
}

// FuzzMergePiece checks the three-way merge of two edits of a piece: it never panics, is deterministic, never
// invents a link, and merging its own clean result again changes nothing.
func FuzzMergePiece(f *testing.F) {
	imp, _ := importtest.Scrubbed(f)
	bases := []domain.PieceDocument{domain.MergeBase()}
	for _, p := range imp.Pieces {
		bases = append(bases, p.Doc)
	}
	for i := range bases {
		ops := mergeSeedOps[i%len(mergeSeedOps)]
		f.Add(uint16(i), ops[0], ops[1])
	}

	f.Fuzz(func(t *testing.T, index uint16, oursOps, theirsOps []byte) {
		base := bases[int(index)%len(bases)]
		ours, theirs := domain.MutatePiece(base, oursOps), domain.MutatePiece(base, theirsOps)
		merged, conflicts := domain.MergeDocuments(base, ours, theirs)
		again, againConflicts := domain.MergeDocuments(base, ours, theirs)
		require.Equal(t, merged, again, "deterministic")
		require.Equal(t, conflicts, againConflicts)
		inputs := append(slices.Clone(ours.Links), theirs.Links...)
		for _, l := range merged.Links {
			require.True(t, slices.ContainsFunc(inputs, func(e domain.DocPieceLink) bool {
				return domain.EqualValues(e, l)
			}), "never invents an element")
		}
		if len(conflicts) == 0 {
			remerged, remergeConflicts := domain.MergeDocuments(base, merged, theirs)
			require.Empty(t, remergeConflicts)
			require.True(t, domain.EqualValues(merged, remerged), "idempotent")
		}
	})
}

// restoreSeedOps are edits for mutateProjection.
var restoreSeedOps = [][]byte{{}, {0}, {1}, {2, 3}, {4, 5}, {3, 4, 0, 1}}

// FuzzRestorePerson checks restore on submit for a contributor's edit of every imported person: whatever the
// contributor does to their projection, restore never panics and is deterministic, an untouched projection
// restores the base, every hidden alias and hidden link of the base survives (by key), the preferences are the
// base's, and the contributor's next projection of the result still withholds every hidden alias and link.
func FuzzRestorePerson(f *testing.F) {
	imp, _ := importtest.Scrubbed(f)
	vis := importVisibility(imp)
	for i := range imp.Persons {
		f.Add(uint16(i), restoreSeedOps[i%len(restoreSeedOps)])
	}

	f.Fuzz(func(t *testing.T, index uint16, ops []byte) {
		person := imp.Persons[int(index)%len(imp.Persons)]
		base := person.Doc
		projection, received := domain.EditableDocument(person.ID, base, vis)
		submitted := mutateProjection(projection, base, vis, ops)
		restored, flags := domain.RestoreDocument(person.ID, submitted, &base, received, vis, domaintest.Keys())
		again, againFlags := domain.RestoreDocument(person.ID, submitted, &base, received, vis, domaintest.Keys())
		require.Equal(t, restored, again, "deterministic")
		require.Equal(t, flags, againFlags)
		if len(ops) == 0 {
			require.True(t, domain.EqualValues(base, restored), "an untouched projection restores the base")
			require.Empty(t, flags)
		}

		aliases := map[string]bool{}
		for _, a := range restored.Aliases {
			aliases[domain.CollationKey(a.Name)] = true
		}
		links := map[string]bool{}
		for _, l := range restored.Links {
			links[normalized(l.URL)] = true
		}
		for _, a := range base.Aliases {
			if !a.IsPublic {
				require.True(t, aliases[domain.CollationKey(a.Name)], "a hidden alias survives")
			}
		}
		for _, l := range base.Links {
			if vis.HiddenLinkURLs[normalized(l.URL)] {
				require.True(t, links[normalized(l.URL)], "a hidden link survives")
			}
		}
		require.True(t, domain.EqualValues(base.Preferences, restored.Preferences), "preferences are the base's")

		next, _ := domain.EditableDocument(person.ID, restored, vis)
		for _, a := range next.Aliases {
			require.True(t, a.IsPublic, "the next projection withholds every hidden alias")
		}
		for _, l := range next.Links {
			require.False(t, vis.HiddenLinkURLs[normalized(l.URL)], "the next projection withholds every hidden link")
		}
	})
}

// importVisibility is a contributor's visibility of the import: every entity is public, persons who prefer no
// page are public without a page, and the import's hidden links are hidden.
func importVisibility(imp domain.CatalogueImport) domain.EditorVisibility {
	vis := domain.EditorVisibility{
		SnapshotVisibility: domain.SnapshotVisibility{
			Pieces: map[int64]domain.PieceSummary{}, Persons: map[int64]domain.PersonSummary{},
			NoPagePersons: map[int64]domain.PersonSummary{}, Terms: map[int64]domain.TermSummary{},
			Collections: map[int64]domain.CollectionSummary{}, Events: map[int64]domain.EventSummary{},
		},
		HiddenLinkURLs: map[string]bool{},
		Role:           domain.RoleContributor,
	}
	for _, p := range imp.Pieces {
		vis.Pieces[p.ID] = domain.PieceSummary{}
	}
	for _, p := range imp.Persons {
		if p.Doc.Status == domain.PersonStatusPrefersNoPage {
			vis.NoPagePersons[p.ID] = domain.PersonSummary{}
		} else {
			vis.Persons[p.ID] = domain.PersonSummary{}
		}
	}
	for _, term := range imp.Terms {
		vis.Terms[term.ID] = domain.TermSummary{}
	}
	for _, c := range imp.Collections {
		vis.Collections[c.ID] = domain.CollectionSummary{}
	}
	for _, e := range imp.Events {
		vis.Events[e.ID] = domain.EventSummary{}
	}
	for _, l := range imp.Links {
		if l.IsHidden {
			vis.HiddenLinkURLs[normalized(l.Link.URL)] = true
		}
	}

	return vis
}

// normalized is url's normalized form, or url itself if it does not normalize.
func normalized(url string) string {
	n, err := domain.NormalizeURL(url)
	if err != nil {
		return url
	}

	return n
}

// mutateProjection applies a contributor's edit to their projection d of base, chosen by each byte of ops. Some
// edits collide with what the projection withholds: a public alias named as a hidden alias of base, and a link to
// a hidden link's URL.
func mutateProjection(d, base domain.PersonDocument, vis domain.EditorVisibility, ops []byte) domain.PersonDocument {
	hiddenNames, hiddenURLs := withheldOf(base, vis)
	for _, op := range ops {
		d = applyProjectionEdit(d, op, hiddenNames, hiddenURLs)
	}

	return d
}

// withheldOf returns the names of base's hidden aliases and the URLs of its hidden links.
func withheldOf(base domain.PersonDocument, vis domain.EditorVisibility) (names, urls []string) {
	for _, a := range base.Aliases {
		if !a.IsPublic {
			names = append(names, a.Name)
		}
	}
	for _, l := range base.Links {
		if vis.HiddenLinkURLs[normalized(l.URL)] {
			urls = append(urls, l.URL)
		}
	}

	return names, urls
}

// applyProjectionEdit applies the edit op chooses to d.
func applyProjectionEdit(d domain.PersonDocument, op byte, hiddenNames, hiddenURLs []string) domain.PersonDocument {
	n := int(op >> 3)
	switch op % 6 {
	case 0:
		if len(d.Aliases) > 0 {
			d.Aliases = slices.Delete(slices.Clone(d.Aliases), n%len(d.Aliases), n%len(d.Aliases)+1)
		}
	case 1:
		if len(d.Links) > 0 {
			d.Links = slices.Delete(slices.Clone(d.Links), n%len(d.Links), n%len(d.Links)+1)
		}
	case 2:
		d.Aliases = slices.Clone(d.Aliases)
		slices.Reverse(d.Aliases)
		d.Links = slices.Clone(d.Links)
		slices.Reverse(d.Links)
	case 3:
		name := "Fuzz Alias"
		if len(hiddenNames) > 0 {
			name = hiddenNames[n%len(hiddenNames)]
		}
		d.Aliases = append(slices.Clone(d.Aliases), domain.DocPersonAlias{Name: name,
			Kind: domain.PersonAliasKindSCA, IsPublic: true})
	case 4:
		url := "https://example.org/fuzz-link"
		if len(hiddenURLs) > 0 {
			url = hiddenURLs[n%len(hiddenURLs)]
		}
		d.Links = append(slices.Clone(d.Links), domain.DocPersonLink{DocLink: domain.DocLink{URL: url,
			Roles: []domain.LinkRole{domain.LinkRoleWebsite}}})
	case 5:
		d.Name += " Fuzz"
	}

	return d
}
