package mediawiki

import (
	"sort"
	"strings"
)

// Site is what parsing one page needs to know about the rest of the wiki: the classified categories, the terms
// planned from them, and the pages and redirects of the main namespace.
type Site struct {
	Taxonomy Taxonomy
	Terms    TermPlan
	pages    map[string]Page
	ordered  []Page
	// redirects maps a redirect page's title to its target's.
	redirects map[string]string
}

// NewSite indexes a dump.
func NewSite(d Dump) Site {
	taxonomy := ClassifyCategories(d)
	s := Site{Taxonomy: taxonomy, Terms: PlanTerms(taxonomy), pages: map[string]Page{}, redirects: map[string]string{}}
	for _, p := range d.Pages {
		if p.NS == NSMain {
			s.pages[p.Title] = p
			s.ordered = append(s.ordered, p)
		}
	}
	sort.SliceStable(s.ordered, func(a, b int) bool { return s.ordered[a].PageID < s.ordered[b].PageID })
	for _, r := range d.Redirects {
		if r.NS == NSMain {
			s.redirects[r.From] = NormTitle(strings.TrimPrefix(r.To, ":"))
		}
	}

	return s
}

// ResolveRedirect follows redirects from title to the page they lead to.
func (s Site) ResolveRedirect(title string) string {
	seen := map[string]bool{}
	for {
		to, ok := s.redirects[title]
		if !ok || seen[title] {
			return title
		}
		seen[title] = true
		title = to
	}
}

// RedirectsTo returns the titles of the redirects that lead to title, sorted.
func (s Site) RedirectsTo(title string) []string {
	var out []string
	for from := range s.redirects {
		if from != title && s.ResolveRedirect(from) == title {
			out = append(out, from)
		}
	}
	sort.Strings(out)

	return out
}

// Pieces parses every piece page of the main namespace, by page ID.
func (s Site) Pieces() []PieceRecord {
	var out []PieceRecord
	for _, p := range s.ordered {
		if TypePage(p) == PageTypePiece {
			out = append(out, ParsePiece(p, s))
		}
	}

	return out
}
