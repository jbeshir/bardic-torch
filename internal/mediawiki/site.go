package mediawiki

import (
	"sort"
	"strings"
)

// Site is what parsing one page needs to know about the rest of the wiki: the classified categories, the terms
// planned from them, the pages and redirects of the main namespace and which categories have a page.
type Site struct {
	Taxonomy Taxonomy
	Terms    TermPlan
	pages    map[string]Page
	ordered  []Page
	// redirects maps a redirect page's title to its target's.
	redirects map[string]string
	// categoryPages are the names of the categories that have a page.
	categoryPages map[string]bool
}

// NewSite indexes a dump.
func NewSite(d Dump) Site {
	taxonomy := ClassifyCategories(d)
	s := Site{Taxonomy: taxonomy, Terms: PlanTerms(taxonomy), pages: map[string]Page{}, redirects: map[string]string{},
		categoryPages: map[string]bool{}}
	for _, p := range d.Pages {
		switch p.NS {
		case NSMain:
			s.pages[p.Title] = p
			s.ordered = append(s.ordered, p)
		case NSCategory:
			s.categoryPages[strings.TrimPrefix(p.Title, categoryPrefix)] = true
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

// HasPage reports whether the main namespace has a page (or a redirect) titled title.
func (s Site) HasPage(title string) bool {
	_, ok := s.pages[title]

	return ok
}

// Bards parses every bard page of the main namespace, by page ID.
func (s Site) Bards() []BardRecord {
	var out []BardRecord
	for _, p := range s.ordered {
		if TypePage(p) == PageTypeBard {
			out = append(out, ParseBard(p, s))
		}
	}

	return out
}

// categoryPrefix is the prefix of a category page's title.
const categoryPrefix = "Category:"

// hasCategoryPage reports whether the category name has a page.
func (s Site) hasCategoryPage(name string) bool {
	return s.categoryPages[name]
}

// caseVariantOf returns the main-namespace page whose title differs from title only by letter case, if any:
// MediaWiki titles are case-sensitive after the first letter, so they are different pages.
func (s Site) caseVariantOf(title string) (string, bool) {
	for _, p := range s.ordered {
		if p.Title != title && strings.EqualFold(p.Title, title) {
			return p.Title, true
		}
	}

	return "", false
}
