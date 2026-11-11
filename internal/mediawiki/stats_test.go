package mediawiki

import (
	"regexp"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The research counts and lookups the tests assert on, which production code does not need.

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
}

// Stats computes the research counts of c.
func Stats(c Corpus) CorpusStats {
	st := CorpusStats{PiecesByKind: map[domain.PieceKind]int{}, Bards: len(c.People.Bards),
		CollectionEntries: c.Links.Collections.Entries(), Collections: len(c.Links.Collections.Records),
		RedirectsByNS: map[int]int{}, RedLinksByNS: map[int]int{},
		TrackingParameterURLs: c.Links.TrackingParameterURLs(),
		PagesWithRepeatedURLs: len(c.Links.PagesWithRepeatedURLs())}
	for _, p := range c.Pieces {
		st.PiecesByKind[p.Kind]++
		if p.HadInlineLyrics {
			st.InlineLyricsPages++
		}
	}
	for _, b := range c.People.Bards {
		for _, w := range b.Works {
			if !w.Exists {
				st.WorksWithoutPage++
			}
		}
	}
	for _, r := range ClassifyRedLinks(c.Site, c.Pieces, c.People.Bards) {
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

// PageURL is a URL, as entered, on a page and in a section.
type PageURL struct {
	Page    string
	Section SectionKey
	URL     string
}

// BareURLs are the URLs the pieces and bard pages write bare, without brackets.
func (l Links) BareURLs() []PageURL {
	var out []PageURL
	add := func(page string, occurrences []LinkOccurrence) {
		for _, o := range occurrences {
			if !o.Bracketed {
				out = append(out, PageURL{Page: page, Section: o.Section, URL: o.URL})
			}
		}
	}
	for _, pl := range l.Pieces {
		add(pl.Title, pl.Occurrences)
	}
	for _, pl := range l.Persons {
		add(string(pl.Person), pl.Occurrences)
	}

	return out
}

// TrackingParameterURLs counts the external links of the piece pages, as entered, that have a tracking parameter.
func (l Links) TrackingParameterURLs() int {
	n := 0
	for _, pl := range l.Pieces {
		for _, o := range pl.Occurrences {
			if HasTrackingParameter(o.URL) {
				n++
			}
		}
	}

	return n
}

// RepeatedURLs are the URLs, as entered, that a page's occurrences hold more than once, in first-seen order.
func (p PageLinks) RepeatedURLs() []string {
	count := map[string]int{}
	for _, o := range p.Occurrences {
		count[o.URL]++
	}
	var out []string
	for _, o := range p.Occurrences {
		if count[o.URL] > 1 && !slices.Contains(out, o.URL) {
			out = append(out, o.URL)
		}
	}

	return out
}

// PagesWithRepeatedURLs are the titles of the piece pages writing a URL more than once.
func (l Links) PagesWithRepeatedURLs() []string {
	var out []string
	for _, pl := range l.Pieces {
		if len(pl.RepeatedURLs()) > 0 {
			out = append(out, pl.Title)
		}
	}

	return out
}

var trackingParameter = regexp.MustCompile(`[?&](si|feature|utm_[a-z]+|authuser|list|start_radio|pp)=`)

// HasTrackingParameter reports whether a URL, as entered, has a tracking parameter.
func HasTrackingParameter(rawURL string) bool {
	return trackingParameter.MatchString(rawURL)
}

// Stub returns the stub person named ref.
func (p Persons) Stub(ref PersonRef) (StubPerson, bool) {
	k := slices.IndexFunc(p.Stubs, func(s StubPerson) bool { return PersonRef(s.Name) == ref })
	if k < 0 {
		return StubPerson{}, false
	}

	return p.Stubs[k], true
}
