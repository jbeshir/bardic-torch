package mediawiki

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// wikiFixture builds a synthetic dump: categories with their parents and pages with their categories, keeping
// subcategory and member lists consistent as the crawler's are.
type wikiFixture struct {
	categories map[string]*Category
	pages      []Page
	redirects  []Redirect
}

func newWikiFixture() *wikiFixture {
	return &wikiFixture{categories: map[string]*Category{}}
}

// standardWiki is a small wiki with the structural categories and a few terms of each kind.
func standardWiki() *wikiFixture {
	w := newWikiFixture()
	w.category(CategoryTopLevel)
	for _, top := range []string{catMoods, catTopics, "Cultures", "Source Category", CategoryGenres,
		CategoryConservationStatus, CategoryPieces, CategoryBards, catTrueEvents} {
		w.category(top, CategoryTopLevel)
	}
	w.category(catWarlike, catMoods)
	w.category(catComedic, catMoods)
	w.category(catWar, catTopics)
	w.category(patriotic, catTopics)
	w.category(catPatrioticKingdom, patriotic)
	w.category(catPatrioticWales, patriotic)
	w.category("10th Century (Topic)", catTopics)
	w.category(catWelsh, "Cultures")
	w.category("Pre-1600 Sources", "Source Category")
	w.category(cat13thCentury, "Pre-1600 Sources")
	w.category("16th Century", "Pre-1600 Sources")
	w.category(catSCAPieces, "Source Category")
	w.category(catContrafacta, "Source Category")
	w.category(catTraditional, "Source Category")
	for _, g := range []string{CategorySongs, CategoryPoems, CategoryStories, CategoryDances, CategoryInstrumental} {
		w.category(g, CategoryGenres)
	}
	for _, c := range conservationStatuses {
		w.category(c.category, CategoryConservationStatus)
	}
	w.category(CategorySCAKingdom)
	w.category(kingdom, CategorySCAKingdom)
	w.category(CategorySCABards)
	w.redirectCategory(catTraditionalRedirect, catTraditional)
	w.page(Page{Title: wren, Wikitext: "= " + wren + " =\n", Categories: []string{CategoryBards, CategorySCABards,
		kingdom}})

	return w
}

func (w *wikiFixture) category(name string, parents ...string) *Category {
	c, ok := w.categories[name]
	if !ok {
		c = &Category{Name: name}
		w.categories[name] = c
	}
	for _, p := range parents {
		c.Parents = append(c.Parents, p)
		parent := w.category(p)
		parent.Subcategories = append(parent.Subcategories, name)
	}

	return c
}

func (w *wikiFixture) describe(name, description string) {
	c := w.category(name)
	c.PageExists = true
	c.DescriptionWikitext = description
	w.pages = append(w.pages, Page{NS: NSCategory, Title: "Category:" + name, PageID: int64(9000 + len(w.pages)),
		Wikitext: description})
}

func (w *wikiFixture) redirectCategory(from, to string) {
	w.category(from).PageExists = true
	w.pages = append(w.pages, Page{NS: NSCategory, Title: "Category:" + from, PageID: int64(9000 + len(w.pages)),
		Redirect: true, Wikitext: "#REDIRECT [[:Category:" + to + "]]"})
	w.redirects = append(w.redirects, Redirect{From: "Category:" + from, NS: NSCategory, To: ":Category:" + to})
}

// fixtureRevisionTime is when the fixture pages were last revised, so performances up to 2026 have their year.
var fixtureRevisionTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func (w *wikiFixture) page(p Page) Page {
	if p.PageID == 0 {
		p.PageID = int64(100 + len(w.pages))
	}
	if p.LastRevTimestamp.IsZero() {
		p.LastRevTimestamp = fixtureRevisionTime
	}
	p.URL = "https://wiki.example.invalid/wiki/" + strings.ReplaceAll(p.Title, " ", "_")
	for _, c := range p.Categories {
		cat := w.category(c)
		cat.Pages = append(cat.Pages, CategoryMember{Title: p.Title, NS: p.NS})
	}
	w.pages = append(w.pages, p)

	return p
}

func (w *wikiFixture) redirect(from, to string) {
	w.pages = append(w.pages, Page{Title: from, PageID: int64(100 + len(w.pages)), Redirect: true,
		Wikitext: "#REDIRECT [[" + to + "]]"})
	w.redirects = append(w.redirects, Redirect{From: from, To: to})
}

func (w *wikiFixture) dump() Dump {
	d := Dump{Categories: map[string]Category{}, Pages: w.pages, Redirects: w.redirects}
	for name, c := range w.categories {
		d.Categories[name] = *c
	}

	return d
}

func (w *wikiFixture) site() Site {
	return NewSite(w.dump())
}

// pieceWikitext is a piece page: an H1, a song infobox with the given parameters in order and the sections given
// as heading and body pairs.
func pieceWikitext(title string, params [][2]string, sections ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "= %s =\n{{Infobox song\n", title)
	for _, p := range params {
		fmt.Fprintf(&b, "| %s = %s\n", p[0], p[1])
	}
	b.WriteString("}}\n")
	for k := 0; k+1 < len(sections); k += 2 {
		fmt.Fprintf(&b, "== %s ==\n%s\n", sections[k], sections[k+1])
	}

	return b.String()
}

// categoryLink is the infobox's display link to a category.
func categoryLink(name string) string {
	return "[[:Category:" + name + "|" + name + "]]"
}

// pieceCategories are a piece's categories: Pieces and Songs and the others given, sorted as the crawler lists
// them.
func pieceCategories(others ...string) []string {
	out := append([]string{CategoryPieces, CategorySongs}, others...)
	sort.Strings(out)

	return out
}

// parseFixturePiece adds a piece page to w and parses it.
func parseFixturePiece(w *wikiFixture, p Page) PieceRecord {
	p = w.page(p)

	return ParsePiece(p, w.site())
}
