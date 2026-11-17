package mediawiki

import (
	"slices"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Site is what parsing one page needs to know about the rest of the wiki: the classified categories, the terms
// planned from them, the pages and redirects of the main namespace and which categories have a page.
type Site struct {
	Taxonomy Taxonomy
	Terms    TermPlan
	// Year is the latest year a performance can be from (LatestYear of the dump).
	Year    int
	pages   map[string]Page
	ordered []Page
	// redirects maps a redirect page's title to its target's.
	redirects map[string]string
	// categoryPages are the names of the categories that have a page.
	categoryPages map[string]bool
}

// NewSite indexes a dump.
func NewSite(d Dump) Site {
	taxonomy := ClassifyCategories(d)
	s := Site{Taxonomy: taxonomy, Terms: PlanTerms(taxonomy), Year: LatestYear(d.Pages), pages: map[string]Page{},
		redirects: map[string]string{}, categoryPages: map[string]bool{}}
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

// Page returns the main-namespace page titled title.
func (s Site) Page(title string) (Page, bool) {
	p, ok := s.pages[title]

	return p, ok
}

// Venues are the venue pages of the main namespace, by page ID.
func (s Site) Venues() []Page {
	var out []Page
	for _, p := range s.ordered {
		if TypePage(p) == PageTypeVenue {
			out = append(out, p)
		}
	}

	return out
}

// pieceTitleFolded returns the title of the piece page whose title, or the title of one of whose redirects, is
// title as the collation compares them (domain.CollationKey: letter case and accents aside), if exactly one piece
// page has one: "My Thing Is My Own" finds "My Thing is My Own".
func (s Site) pieceTitleFolded(title string) (string, bool) {
	key := domain.CollationKey(title)
	var found []string
	add := func(from, piece string) {
		if domain.CollationKey(from) == key && !slices.Contains(found, piece) {
			if p, ok := s.pages[piece]; ok && TypePage(p) == PageTypePiece {
				found = append(found, piece)
			}
		}
	}
	for _, p := range s.ordered {
		add(p.Title, p.Title)
	}
	for from := range s.redirects {
		add(from, s.ResolveRedirect(from))
	}
	if len(found) != 1 {
		return "", false
	}

	return found[0], true
}
