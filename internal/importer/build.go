package importer

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// Build maps a parsed wiki onto the catalogue: every term, collection, event, person and piece with its document,
// system fields and one approved import revision (a neutral summary, the page's wiki contributors), the links
// rows, and the review of everything the moderators should check. now is the time of every imported revision.
// It is deterministic: IDs are assigned in a
// fixed order (affiliation terms, then the other terms, each in plan order; collections in page order; events as
// first seen; bards by page ID, then stub persons by name; pieces by page ID, then stub pieces by title), and so
// are slugs, collisions getting "-2", "-3"…
//
// Email addresses in public text are withheld (domain.WithheldEmail) and listed in the review; names are imported as
// written, placeholders of the scrubbed copy included.
func Build(c mediawiki.Corpus, now time.Time) (domain.CatalogueImport, Review) {
	b := newBuilder(c, now)
	b.assignIDs()
	b.buildTerms()
	b.buildCollections()
	b.buildEvents()
	b.buildLinkRows()
	b.buildPersons()
	b.buildPieces()
	b.reviewCorpus()
	b.review.Counts = b.counts()
	b.review.numberItems()

	return b.out, b.review
}

type builder struct {
	c      mediawiki.Corpus
	now    time.Time
	out    domain.CatalogueImport
	review Review
	slugs  domain.SlugAllocator
	rev    int64
	// activity is each page's contributors, by namespace and title.
	activity map[pageKey][]string
	// pages are the dump's pages, by namespace and title.
	pages map[pageKey]mediawiki.Page

	termIDs       map[string]int64
	collectionIDs map[string]int64
	eventIDs      map[string]int64
	personIDs     map[mediawiki.PersonRef]int64
	pieceIDs      map[string]int64
	links         map[string]domain.DocLink
	stubs         []mediawiki.StubPerson
	aliasItems    []aliasItems
}

type pageKey struct {
	ns    int
	title string
}

func newBuilder(c mediawiki.Corpus, now time.Time) *builder {
	b := &builder{c: c, now: now, activity: map[pageKey][]string{},
		pages: map[pageKey]mediawiki.Page{}, termIDs: map[string]int64{}, collectionIDs: map[string]int64{},
		eventIDs: map[string]int64{}, personIDs: map[mediawiki.PersonRef]int64{}, pieceIDs: map[string]int64{},
		links: map[string]domain.DocLink{}}
	for _, a := range c.Dump.Activity {
		b.activity[pageKey{a.NS, a.Title}] = contributors(a.Users)
	}
	for _, p := range c.Dump.Pages {
		b.pages[pageKey{p.NS, p.Title}] = p
	}
	b.stubs = slices.Clone(c.People.Persons.Stubs)
	slices.SortFunc(b.stubs, func(x, y mediawiki.StubPerson) int { return strings.Compare(x.Name, y.Name) })

	return b
}

// contributors are the users of a page's activity, by edit count descending and then by name.
func contributors(users map[string]int) []string {
	return slices.SortedFunc(maps.Keys(users), func(a, b string) int {
		return cmp.Or(cmp.Compare(users[b], users[a]), strings.Compare(a, b))
	})
}

// assignIDs numbers every entity, so that documents can reference any of them.
func (b *builder) assignIDs() {
	for k, t := range b.terms() {
		b.termIDs[t.Category] = int64(k + 1)
	}
	b.assignCollectionAndEventIDs()
	b.assignPersonIDs()
	b.assignPieceIDs()
}

func (b *builder) assignCollectionAndEventIDs() {
	for k, r := range b.c.Links.Collections.Records {
		b.collectionIDs[r.NormalizedURL] = int64(k + 1)
	}
	for k, e := range b.c.Links.Events {
		b.eventIDs[e.Name] = int64(k + 1)
	}
}

// assignPersonIDs numbers the bards, then the stub persons.
func (b *builder) assignPersonIDs() {
	n := int64(0)
	for _, bard := range b.c.People.Bards {
		n++
		b.personIDs[mediawiki.PersonRef(bard.Title)] = n
	}
	for _, s := range b.stubs {
		n++
		b.personIDs[mediawiki.PersonRef(s.Name)] = n
	}
}

// assignPieceIDs numbers the pieces, then the stub pieces.
func (b *builder) assignPieceIDs() {
	n := int64(0)
	for _, p := range b.c.Pieces {
		n++
		b.pieceIDs[p.Title] = n
	}
	for _, sp := range b.c.People.StubPieces {
		n++
		b.pieceIDs[sp.Title] = n
	}
}

// revision is the next revision: with the contributors of page, if not nil, and summary, or the page's
// revision summary if summary is empty.
func (b *builder) revision(page *mediawiki.Page, summary string) domain.ImportedRevision {
	b.rev++
	r := domain.ImportedRevision{ID: b.rev, Summary: summary, Contributors: []string{}, CreatedAt: b.now}
	if page != nil {
		if summary == "" {
			r.Summary = domain.ImportSummaryPage(page.RevID)
		}
		for _, u := range b.activity[pageKey{page.NS, page.Title}] {
			r.Contributors = append(r.Contributors, clean(u))
		}
	}

	return r
}

// page returns the dump's page titled title in namespace ns.
func (b *builder) page(ns int, title string) (mediawiki.Page, bool) {
	p, ok := b.pages[pageKey{ns, title}]

	return p, ok
}

// legacy is the legacy columns of a page: its title, and its page ID if it fits the column.
func legacy(p mediawiki.Page) *domain.LegacyPage {
	out := &domain.LegacyPage{Title: clean(p.Title)}
	if p.PageID > 0 && p.PageID <= math.MaxUint32 {
		id := uint32(p.PageID)
		out.PageID = &id
	}

	return out
}

// clean is s in NFC without control characters (domain.IsControlCharacter), as every imported string is.
func clean(s string) string {
	return norm.NFC.String(domain.WithoutControlCharacters(s))
}

// cleanURL is clean for a URL: absolute (a "www." or "//" URL gets https, domain.WithScheme, so it is never a
// relative link), and its control characters percent-encoded rather than dropped, so that it normalizes as the
// URL it was read as.
func cleanURL(u string) string {
	return clean(domain.WithControlCharactersEscaped(domain.WithScheme(u)))
}

// listControlCharacters lists s if it holds control characters, which the caller drops.
func (b *builder) listControlCharacters(page, entity, field, s string) {
	if strings.ContainsFunc(s, domain.IsControlCharacter) {
		b.review.Markup = append(b.review.Markup, item("text_characters", page,
			"%s, %s has control characters (such as NUL, ESC or a bidirectional override); they are removed.",
			entity, field))
	}
}

// free is the text of a public free-text field of the entity named page (field names it in the review), with its
// emails withheld, or nil if empty.
func (b *builder) free(page, entity, field, s string) *string {
	out := b.freeText(page, entity, field, s)
	if out == "" {
		return nil
	}

	return &out
}

// freeText is free without the nil for empty text.
func (b *builder) freeText(page, entity, field, s string) string {
	b.listControlCharacters(page, entity, field, s)

	return b.withholdEmails(page, entity, field, strings.TrimSpace(domain.WithoutControlCharacters(s)))
}

// withholdEmails is s in NFC with its email addresses replaced by domain.WithheldEmail, each listed in the review
// with its context.
func (b *builder) withholdEmails(page, entity, field, s string) string {
	s = norm.NFC.String(s)
	spans := mediawiki.EmailAddress.FindAllStringIndex(s, -1)
	if len(spans) == 0 {
		return s
	}
	var out strings.Builder
	starts := make([]int, 0, len(spans))
	at := 0
	for _, sp := range spans {
		out.WriteString(s[at:sp[0]])
		starts = append(starts, out.Len())
		out.WriteString(domain.WithheldEmail)
		at = sp[1]
	}
	out.WriteString(s[at:])
	for k, sp := range spans {
		b.review.Contacts = append(b.review.Contacts, item("email", page,
			"%s, %s: the address %q was withheld. Context: %q.", entity, field, s[sp[0]:sp[1]],
			around(out.String(), starts[k], starts[k]+len(domain.WithheldEmail))))
	}

	return out.String()
}

// contextRunes is how much text on each side of a withheld span the review shows.
const contextRunes = 40

// around is s[start:end] with up to contextRunes runes on each side, on one line.
func around(s string, start, end int) string {
	from := start
	for n := 0; n < contextRunes && from > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(s[:from])
		from -= size
	}
	to := end
	for n := 0; n < contextRunes && to < len(s); n++ {
		_, size := utf8.DecodeRuneInString(s[to:])
		to += size
	}

	return oneLine(s[from:to])
}

// name is a public name or title (one line), with its email addresses withheld; characters an editor could not
// enter are listed.
func (b *builder) name(page, entity, field, s string) string {
	b.listControlCharacters(page, entity, field, s)
	out := b.withholdEmails(page, entity, field, strings.TrimSpace(domain.WithoutControlCharacters(s)))
	if err := domain.CheckTextCharacters(out); err != nil {
		b.review.Markup = append(b.review.Markup, item("text_characters", page,
			"%s, %s has a character an editor could not enter (%v); it is imported as written.", entity, field,
			err))
	}

	return out
}

// position is a sibling position as the column holds it, capped at its range.
func position(n int) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	}

	return int32(n)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	out := clean(s)

	return &out
}

func optionalURL(u string) *string {
	if u == "" {
		return nil
	}
	out := cleanURL(u)

	return &out
}

func (b *builder) buildTerms() {
	for _, t := range b.terms() {
		id := b.termIDs[t.Category]
		entity := "term " + t.Category
		name := b.name("", entity, "name", t.Name)
		display := b.name("", entity, "display name", t.DisplayName)
		doc := domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: t.Facet,
			Name: name, DisplayName: display,
			ParentID: b.termRef(t.Parent), RelatedAffiliationID: b.termRef(t.RelatedAffiliation),
			DescriptionMD: b.free("", entity, "description", t.DescriptionMD), Position: position(t.Position),
			Aliases: []string{}}
		seen := map[string]bool{domain.CollationKey(doc.Name): true}
		for _, a := range t.Aliases {
			a = b.name("", entity, "alias", a)
			if key := domain.CollationKey(a); !seen[key] {
				seen[key] = true
				doc.Aliases = append(doc.Aliases, a)
			}
		}
		term := domain.ImportedTerm{ID: id, Slug: b.slugs.Allocate(doc.Name, domain.SlugEntityTypeTerm, id), Doc: doc}
		if p, ok := b.page(mediawiki.NSCategory, "Category:"+t.Category); ok {
			term.Legacy = legacy(p)
			term.Revision = b.revision(&p, "")
		} else {
			term.Revision = b.revision(nil, domain.ImportSummaryCategory)
		}
		b.out.Terms = append(b.out.Terms, term)
	}
	if _, err := b.out.TermAncestors(); err != nil {
		b.review.Taxonomy = append(b.review.Taxonomy, item("closure", "",
			"The term tree has no valid closure (%v); the import does not validate.", err))
	}
}

// termRef is the ID of the term planned from category, or nil.
func (b *builder) termRef(category string) *int64 {
	if id, ok := b.termIDs[category]; ok {
		return &id
	}

	return nil
}

func (b *builder) buildCollections() {
	wikiPage, hasPage := b.page(mediawiki.NSMain, mediawiki.CollectionsPage)
	for _, r := range b.c.Links.Collections.Records {
		id, ok := b.collectionIDs[r.NormalizedURL]
		if !ok {
			continue
		}
		page := mediawiki.CollectionsPage
		name := b.name(page, "collection "+r.NormalizedURL, "name", r.Name())
		doc := domain.CollectionDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
			URL: optionalURL(r.URL), Kind: r.Kind,
			DescriptionMD: b.free(page, "collection "+r.NormalizedURL, "description", r.Description),
			RegionTermID:  b.termRef(r.RegionTerm), IsBardicResource: r.IsBardicResource,
			Enumerable: domain.CollectionEnumerableUnknown}
		if r.ParentURL != "" {
			if parent, ok := b.collectionIDs[r.ParentURL]; ok {
				doc.ParentID = &parent
			}
		}
		sortName := domain.SortTitle(name)
		col := domain.ImportedCollection{ID: id, SortName: sortName, SortLetter: domain.SortLetter(sortName), Doc: doc}
		if hasPage {
			col.Revision = b.revision(&wikiPage, domain.ImportSummaryCollections)
		} else {
			col.Revision = b.revision(nil, domain.ImportSummaryCollections)
		}
		b.out.Collections = append(b.out.Collections, col)
	}
}

func (b *builder) buildEvents() {
	for _, e := range b.c.Links.Events {
		id, ok := b.eventIDs[e.Name]
		if !ok {
			continue
		}
		page := ""
		if len(e.Pages) > 0 {
			page = e.Pages[0]
		}
		name := b.name(page, "event", "name", e.Name)
		doc := domain.EventDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name, Kind: e.Kind}
		ev := domain.ImportedEvent{ID: id, Slug: b.slugs.Allocate(doc.Name, domain.SlugEntityTypeEvent, id), Doc: doc}
		if p, ok := b.c.Site.Page(e.Name); ok && e.PageID != 0 {
			ev.Legacy = legacy(p)
			ev.Revision = b.revision(&p, "")
		} else {
			ev.Revision = b.revision(nil, domain.ImportSummaryPerformances)
		}
		b.out.Events = append(b.out.Events, ev)
	}
}

// terms are the planned terms in the order they are imported: the affiliation terms first, so that the topic
// terms related to them come after them, then the others; each in plan order, parents before their children.
func (b *builder) terms() []mediawiki.TermSpec {
	var affiliations, others []mediawiki.TermSpec
	for _, t := range b.c.Site.Terms.Terms {
		if t.Facet == domain.FacetAffiliation {
			affiliations = append(affiliations, t)
		} else {
			others = append(others, t)
		}
	}

	return append(affiliations, others...)
}
