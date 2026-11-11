package importer

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// buildTime is the revision time of every test import.
var buildTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// scrubbed is the committed scrubbed dump parsed and built once, as every test only reads it.
var scrubbed struct {
	once   sync.Once
	err    error
	corpus mediawiki.Corpus
	imp    domain.CatalogueImport
}

func loadScrubbed(t *testing.T) {
	t.Helper()
	scrubbed.once.Do(func() {
		var d mediawiki.Dump
		d, scrubbed.err = mediawiki.LoadDump(wikidumptest.Scrubbed(t))
		scrubbed.corpus = mediawiki.ParseCorpus(d)
		scrubbed.imp, _ = Build(scrubbed.corpus, buildTime)
	})
	require.NoError(t, scrubbed.err)
}

// scrubbedCorpus is the committed scrubbed dump, parsed.
func scrubbedCorpus(t *testing.T) mediawiki.Corpus {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.corpus
}

// scrubbedImport is the import of the committed scrubbed dump.
func scrubbedImport(t *testing.T) domain.CatalogueImport {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.imp
}

// publicJSON is the JSON of what of imp a public read can show: every legacy column and hidden link
// (rows and attachments) removed.
func publicJSON(t testing.TB, imp domain.CatalogueImport) string {
	t.Helper()
	hidden := map[string]bool{}
	for _, l := range imp.Links {
		u, err := domain.NormalizeURL(l.Link.URL)
		if err != nil {
			t.Fatalf("a links row does not normalise")
		}
		hidden[u] = l.IsHidden
	}
	visible := func(url string) bool {
		u, err := domain.NormalizeURL(url)
		return err == nil && !hidden[u]
	}
	out := domain.CatalogueImport{Terms: slices.Clone(imp.Terms), Collections: imp.Collections,
		Events: slices.Clone(imp.Events),
		Links:  keep(imp.Links, func(l domain.ImportedLink) bool { return !l.IsHidden })}
	for k := range out.Terms {
		out.Terms[k].Legacy = nil
	}
	for k := range out.Events {
		out.Events[k].Legacy = nil
	}
	for _, p := range imp.Persons {
		p.Legacy = nil
		p.Doc.Links = keep(p.Doc.Links, func(l domain.DocPersonLink) bool { return visible(l.URL) })
		out.Persons = append(out.Persons, p)
	}
	for _, p := range imp.Pieces {
		p.Legacy = nil
		p.Doc.Links = keep(p.Doc.Links, func(l domain.DocPieceLink) bool { return visible(l.URL) })
		out.Pieces = append(out.Pieces, p)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("the import does not encode")
	}

	return string(b)
}

// keep returns the elements of values f keeps, in a new slice.
func keep[T any](values []T, f func(T) bool) []T {
	var out []T
	for _, v := range values {
		if f(v) {
			out = append(out, v)
		}
	}

	return out
}

// buildCounts are the research counts an import must keep: the non-stub pieces by kind, the persons with a bard
// page, the Collections page's entries parsed and the collections, and the pieces that had inline lyrics.
type buildCounts struct {
	PiecesByKind      map[domain.PieceKind]int
	BardPersons       int
	CollectionEntries int
	Collections       int
	InlineLyrics      int
}

// researchCounts are the report's counts (§1, §3), on the real crawl and the scrubbed copy alike.
var researchCounts = buildCounts{
	PiecesByKind: map[domain.PieceKind]int{domain.PieceKindSong: 135, domain.PieceKindPoem: 4,
		domain.PieceKindStory: 2, domain.PieceKindDanceMusic: 1},
	BardPersons: 48, CollectionEntries: 62, Collections: 61, InlineLyrics: 11,
}

func importCounts(c mediawiki.Corpus, imp domain.CatalogueImport) buildCounts {
	out := buildCounts{PiecesByKind: map[domain.PieceKind]int{}, CollectionEntries: c.Links.Collections.Entries(),
		Collections: len(imp.Collections)}
	for _, p := range imp.Pieces {
		if !p.Doc.IsStub {
			out.PiecesByKind[p.Doc.Kind]++
		}
		if p.LegacyHadInlineLyrics {
			out.InlineLyrics++
		}
	}
	for _, p := range imp.Persons {
		if p.Legacy != nil && p.Legacy.PageID != nil {
			out.BardPersons++
		}
	}

	return out
}
