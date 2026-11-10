// Package scrub replaces the inline lyrics of a crawl of the legacy wiki with placeholder lines, and builds the deny
// list of the lyric n-grams it dropped (Denylist), which TestRepositoryHasNoDenylistedText scans the repository for.
// It is lyric-only: it leaves every name, email address and wiki user as it finds them, so its output from a real
// crawl is private and must never be committed. The copy committed under testdata/wiki-dump was made by an earlier
// scrubber that also replaced names; it is frozen and is never regenerated.
package scrub

import (
	"maps"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// Result is a dump with its inline lyrics replaced, the deny list of the lyrics it dropped, and what was replaced.
type Result struct {
	Dump     mediawiki.Dump
	Denylist Denylist
	Stats    Stats
}

// Stats counts the lyrics blocks Scrub replaced.
type Stats struct {
	PoemBlocks    int
	ProseBlocks   int
	CommentBlocks int
}

// scrubber collects the counts of Scrub.
type scrubber struct {
	stats Stats
}

// Scrub returns d with every inline lyrics block (mediawiki.InlineLyricsBlocks) of every page and category
// description replaced with LyricPlaceholderLine lines, at most InlineLyricsMinLines of them, so the inline-lyrics
// classification of every section is kept. It is pure and deterministic, and scrubbing its output changes nothing.
func Scrub(d mediawiki.Dump) Result {
	var s scrubber
	var bodies, outside []string
	out := d
	out.Pages = slices.Clone(d.Pages)
	for k, p := range d.Pages {
		out.Pages[k].Wikitext, bodies, outside = s.lyrics(p.Wikitext, bodies, outside)
		outside = append(outside, p.Title)
	}
	out.Categories = map[string]mediawiki.Category{}
	for _, key := range slices.Sorted(maps.Keys(d.Categories)) {
		c := d.Categories[key]
		c.DescriptionWikitext, bodies, outside = s.lyrics(c.DescriptionWikitext, bodies, outside)
		out.Categories[key] = c
	}

	return Result{Dump: out, Denylist: BuildDenylist(bodies, outside), Stats: s.stats}
}
