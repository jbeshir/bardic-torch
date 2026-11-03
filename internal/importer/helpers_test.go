package importer

import (
	"encoding/json"
	"regexp"
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
	review Review
}

func loadScrubbed(t *testing.T) {
	t.Helper()
	scrubbed.once.Do(func() {
		var d mediawiki.Dump
		d, scrubbed.err = mediawiki.LoadDump(wikidumptest.Scrubbed(t))
		scrubbed.corpus = mediawiki.ParseCorpus(d)
		scrubbed.imp, scrubbed.review = Build(scrubbed.corpus, buildTime)
	})
	require.NoError(t, scrubbed.err)
}

// scrubbedCorpus is the committed scrubbed dump, parsed.
func scrubbedCorpus(t *testing.T) mediawiki.Corpus {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.corpus
}

// scrubbedImport is the import of the committed scrubbed dump and its review.
func scrubbedImport(t *testing.T) (domain.CatalogueImport, Review) {
	t.Helper()
	loadScrubbed(t)

	return scrubbed.imp, scrubbed.review
}

// placeholderForm is any form of a scrubbed copy's name placeholder.
var placeholderForm = regexp.MustCompile(`(?i)legal[ _-]?name[ _-]?\d{3}`)

// publicJSON is the JSON of what of imp a public read can show: every hidden alias, legacy column and hidden link
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
		p.Doc.Aliases = keep(p.Doc.Aliases, func(a domain.DocPersonAlias) bool { return a.IsPublic })
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
// page, the Collections page's entries parsed and the collections (written, or withheld because their name or URL
// holds a legal name: the scrubbed copy's placeholders all count as legal), and the pieces that had inline lyrics.
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

func importCounts(c mediawiki.Corpus, imp domain.CatalogueImport, review Review) buildCounts {
	out := buildCounts{PiecesByKind: map[domain.PieceKind]int{}, CollectionEntries: c.Links.Collections.Entries(),
		Collections: len(imp.Collections)}
	for _, it := range review.HiddenNames {
		if it.Kind == "collection_withheld" {
			out.Collections++
		}
	}
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

// publicFreeText is the public free text of an import (publicJSON's documents): descriptions, notes, credit notes,
// link titles and descriptions, performance descriptions and uploaders. Names and titles are not free text.
func publicFreeText(t testing.TB, imp domain.CatalogueImport) []string {
	t.Helper()
	var public domain.CatalogueImport
	if err := json.Unmarshal([]byte(publicJSON(t, imp)), &public); err != nil {
		t.Fatalf("the public import does not decode")
	}
	var f freeTexts
	for _, x := range public.Terms {
		f.add(x.Doc.DescriptionMD)
	}
	for _, x := range public.Collections {
		f.add(x.Doc.DescriptionMD)
	}
	for _, x := range public.Events {
		f.add(x.Doc.DescriptionMD)
	}
	for _, x := range public.Links {
		f.link(x.Link)
	}
	for _, x := range public.Persons {
		f.add(x.Doc.NotesMD, x.Doc.LicensingNotesMD)
		for _, l := range x.Doc.Links {
			f.link(l.DocLink)
			for _, p := range l.Performances {
				f.performance(p.DocPerformance)
			}
		}
	}
	for _, x := range public.Pieces {
		f.piece(x.Doc)
	}

	return f
}

// freeTexts collects free text.
type freeTexts []string

func (f *freeTexts) add(values ...*string) {
	for _, v := range values {
		if v != nil {
			*f = append(*f, *v)
		}
	}
}

func (f *freeTexts) link(l domain.DocLink) { f.add(l.Title, l.Description) }

func (f *freeTexts) performance(p domain.DocPerformance) {
	f.add(p.Uploader, &p.DescriptionVerbatim)
}

func (f *freeTexts) piece(d domain.PieceDocument) {
	f.add(d.NotesMD, d.VersionNotesMD, d.LicensingNotesMD, d.HistoricalSourceMD)
	for _, c := range d.Credits {
		f.add(c.Note)
	}
	for _, l := range d.Links {
		f.link(l.DocLink)
		for _, p := range l.Performances {
			f.performance(p)
		}
	}
}

// ItemsAbout returns the items about the page titled page, in section order.
func (r *Review) ItemsAbout(page string) []Item {
	var out []Item
	for _, it := range r.Items() {
		if it.Page == page {
			out = append(out, it)
		}
	}

	return out
}
