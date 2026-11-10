package importer

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// reviewIntro opens the review: who it is for, what the review holds and that it is private.
const reviewIntro = `For John and Fabian, the moderators: this lists everything the importer decided that the wiki
does not state, so you can check it. Where the wiki is ambiguous the importer applied its best guess; change it
in the catalogue after the import. Every item has an ID (such as ALIAS-007) to refer to it by.

**Private: do not publish or commit this file.** It names people in clear, so that you can review it. It never
contains lyrics: inline lyrics are dropped and only their page, section and line count are listed.`

// RenderReview writes the review as Markdown: a title, the introduction and counts, then every section in a fixed
// order, each item on its own line, and "None." for a section with no items.
func RenderReview(r Review) []byte {
	var b strings.Builder
	b.WriteString("# Import review\n\n")
	b.WriteString(reviewIntro + "\n\n")
	b.WriteString("## Counts\n\n| What | Count |\n|---|---|\n")
	for _, k := range slices.Sorted(maps.Keys(r.Counts.PiecesByKind)) {
		fmt.Fprintf(&b, "| Pieces: %s | %d |\n", k, r.Counts.PiecesByKind[k])
	}
	for _, row := range []struct {
		what string
		n    int
	}{
		{"Stub pieces", r.Counts.StubPieces}, {"Persons", r.Counts.Persons()}, {"Bards (with a page)", r.Counts.Bards},
		{"Stub persons", r.Counts.StubPersons}, {"Terms", r.Counts.Terms}, {"Collections", r.Counts.Collections},
		{"Collections page entries", r.Counts.CollectionEntries},
		{"Pieces that had inline lyrics", r.Counts.InlineLyricsPieces},
		{"Events", r.Counts.Events}, {"Links", r.Counts.Links},
		{"Revisions", r.Counts.Revisions},
	} {
		fmt.Fprintf(&b, "| %s | %d |\n", row.what, row.n)
	}
	for k, s := range r.sections() {
		fmt.Fprintf(&b, "\n## %d. %s\n\n%s\n\n", k+1, s.title, s.about)
		if len(*s.items) == 0 {
			b.WriteString("None.\n")

			continue
		}
		for _, it := range *s.items {
			fmt.Fprintf(&b, "- **%s**", it.ID)
			if it.Page != "" {
				fmt.Fprintf(&b, " [%s]", it.Page)
			}
			fmt.Fprintf(&b, " %s\n", it.Text)
		}
	}

	return []byte(b.String())
}
