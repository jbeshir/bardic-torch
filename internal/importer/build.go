// Package importer builds the catalogue import from a parsed wiki dump (Build). It is pure: it reads values and
// returns new ones, and writes nothing.
package importer

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// Counts are what the import holds, with two counts of what it was read from: the entries of the Collections page
// and the pieces that had inline lyrics.
type Counts struct {
	PiecesByKind       map[string]int
	StubPieces         int
	InlineLyricsPieces int
	CollectionEntries  int
	Bards              int
	StubPersons        int
	Terms              int
	Collections        int
	Events             int
	Links              int
	Revisions          int
}

// Build maps a parsed wiki onto the catalogue: every term, collection, event, person and piece with its document,
// system fields and one approved import revision (a neutral summary, the page's wiki contributors), and the links
// rows, with the counts of what it holds. now is the time of every imported revision.
// It is deterministic: IDs are assigned in a
// fixed order (affiliation terms, then the other terms, each in plan order; collections in page order; events as
// first seen; bards by page ID, then stub persons by name; pieces by page ID, then stub pieces by title), and so
// are slugs, collisions getting "-2", "-3"…
//
// Email addresses in public text are withheld (domain.WithheldEmail); names are imported as written, placeholders
// of the scrubbed copy included.
func Build(c mediawiki.Corpus, now time.Time) (domain.CatalogueImport, Counts) {
	b := newBuilder(c, now)
	b.assignIDs()
	b.buildTerms()
	b.buildCollections()
	b.buildEvents()
	b.buildLinkRows()
	b.buildPersons()
	b.buildPieces()

	return b.out, b.counts()
}

type builder struct {
	c     mediawiki.Corpus
	now   time.Time
	out   domain.CatalogueImport
	slugs domain.SlugAllocator
	rev   int64
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

// free is the text of a public free-text field, with its emails withheld, or nil if empty.
func free(s string) *string {
	out := freeText(s)
	if out == "" {
		return nil
	}

	return &out
}

// freeText is free without the nil for empty text.
func freeText(s string) string {
	return withholdEmails(strings.TrimSpace(domain.WithoutControlCharacters(s)))
}

// withholdEmails is s in NFC with its email addresses replaced by domain.WithheldEmail.
func withholdEmails(s string) string {
	return mediawiki.EmailAddress.ReplaceAllLiteralString(norm.NFC.String(s), domain.WithheldEmail)
}

// plainName is a public name or title (one line), with its email addresses withheld.
func plainName(s string) string {
	return withholdEmails(strings.TrimSpace(domain.WithoutControlCharacters(s)))
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
		name := plainName(t.Name)
		display := plainName(t.DisplayName)
		doc := domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: t.Facet,
			Name: name, DisplayName: display,
			ParentID: b.termRef(t.Parent), RelatedAffiliationID: b.termRef(t.RelatedAffiliation),
			DescriptionMD: free(t.DescriptionMD), Position: position(t.Position),
			Aliases: []string{}}
		seen := map[string]bool{domain.CollationKey(doc.Name): true}
		for _, a := range t.Aliases {
			a = plainName(a)
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
		name := plainName(r.Name())
		doc := domain.CollectionDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
			URL: optionalURL(r.URL), Kind: r.Kind,
			DescriptionMD: free(r.Description),
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
		name := plainName(e.Name)
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

func (b *builder) counts() Counts {
	c := Counts{PiecesByKind: map[string]int{}, Terms: len(b.out.Terms), Collections: len(b.out.Collections),
		Events: len(b.out.Events), Links: len(b.out.Links),
		CollectionEntries: b.c.Links.Collections.Entries()}
	for _, p := range b.out.Pieces {
		if p.LegacyHadInlineLyrics {
			c.InlineLyricsPieces++
		}
		if p.Doc.IsStub {
			c.StubPieces++
		} else {
			c.PiecesByKind[string(p.Doc.Kind)]++
		}
	}
	for _, p := range b.out.Persons {
		if p.Doc.IsStub {
			c.StubPersons++
		} else {
			c.Bards++
		}
	}
	c.Revisions = int(b.rev)

	return c
}
