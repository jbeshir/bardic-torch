package mediawiki

import (
	"sort"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Corpus is a dump parsed for import: its pages indexed (Site), the pieces, the persons and credits, the links,
// and the private names no public text may hold (PrivateNames, decided on the pages without their inline lyrics),
// with every decision taken on a possible person name.
type Corpus struct {
	Dump         Dump
	Site         Site
	Pieces       []PieceRecord
	People       People
	Links        Links
	PrivateNames []PrivateName
	Privacy      []PrivacyDecision
}

// ParseCorpus parses a dump for import.
func ParseCorpus(d Dump) Corpus {
	s := NewSite(d)
	pieces := s.Pieces()
	people := ParsePeople(s, pieces)
	var pages []Page
	for _, p := range d.Pages {
		if p.NS == NSMain || p.NS == NSCategory {
			out := p
			out.Wikitext = WithoutInlineLyrics(p.Wikitext, InlineLyricsBlocks(p.Wikitext).Blocks)
			pages = append(pages, out)
		}
	}
	sort.SliceStable(pages, func(a, b int) bool { return pages[a].PageID < pages[b].PageID })
	names, decisions := PrivateNames(pages, d.Redirects)

	return Corpus{Dump: d, Site: s, Pieces: pieces, People: people, Links: ParseLinks(s, pieces, people),
		PrivateNames: names, Privacy: decisions}
}

// CorpusStats are the research counts of a corpus (report §1 and §3): what the tests assert on both the real
// crawl and the scrubbed copy, which must agree.
type CorpusStats struct {
	PiecesByKind      map[domain.PieceKind]int
	Bards             int
	CollectionEntries int
	Collections       int
	InlineLyricsPages int
	// RedirectsByNS counts the redirect pages by namespace; PieceRedirects and BardRedirects are the page
	// redirects leading to a piece or a bard.
	RedirectsByNS  map[int]int
	PieceRedirects int
	BardRedirects  int
	// RedLinksByNS counts the red links by namespace (main, category, special).
	RedLinksByNS map[int]int
	// ExternalLinks counts the external links the piece and bard pages write.
	ExternalLinks         int
	TrackingParameterURLs int
	PagesWithRepeatedURLs int
	WorksWithoutPage      int
	// DriftPages counts the pieces with facet drift of each kind.
	DriftPages map[FacetDriftKind]int
}

// Stats computes the research counts of c.
func Stats(c Corpus) CorpusStats {
	st := CorpusStats{PiecesByKind: map[domain.PieceKind]int{}, Bards: len(c.People.Bards),
		CollectionEntries: c.Links.Collections.Entries(), Collections: len(c.Links.Collections.Records),
		RedirectsByNS: map[int]int{}, RedLinksByNS: map[int]int{}, DriftPages: map[FacetDriftKind]int{},
		TrackingParameterURLs: c.Links.TrackingParameterURLs(),
		PagesWithRepeatedURLs: len(c.Links.PagesWithRepeatedURLs())}
	for _, p := range c.Pieces {
		st.PiecesByKind[p.Kind]++
		if p.Lyrics.HadInlineLyrics {
			st.InlineLyricsPages++
		}
		kinds := map[FacetDriftKind]bool{}
		for _, d := range p.Drift {
			kinds[d.Kind] = true
		}
		for k := range kinds {
			st.DriftPages[k]++
		}
	}
	for _, b := range c.People.Bards {
		for _, w := range b.Works {
			if !w.Exists {
				st.WorksWithoutPage++
			}
		}
	}
	for _, r := range c.People.RedLinks {
		st.RedLinksByNS[r.NS]++
	}
	for _, pl := range c.Links.Pieces {
		st.ExternalLinks += len(pl.Occurrences)
	}
	for _, pl := range c.Links.Persons {
		if pl.PageID != 0 {
			st.ExternalLinks += len(pl.Occurrences)
		}
	}
	st.countRedirects(c)

	return st
}

// countRedirects counts c's redirects by namespace and the page redirects leading to a piece or a bard.
func (st *CorpusStats) countRedirects(c Corpus) {
	pieces, bards := map[string]bool{}, map[string]bool{}
	for _, p := range c.Pieces {
		pieces[p.Title] = true
	}
	for _, b := range c.People.Bards {
		bards[b.Title] = true
	}
	for _, r := range c.Dump.Redirects {
		st.RedirectsByNS[r.NS]++
		if r.NS != NSMain {
			continue
		}
		switch to := c.Site.ResolveRedirect(r.From); {
		case pieces[to]:
			st.PieceRedirects++
		case bards[to]:
			st.BardRedirects++
		}
	}
}
