package importer

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// BuildOptions are the inputs of Build besides the corpus: Now is the time of every imported revision.
type BuildOptions struct {
	Now time.Time
}

// Build maps a parsed wiki onto the catalogue: every term, collection, event, person and piece with its document,
// system fields and one approved import revision (a neutral summary, the page's wiki contributors), the links
// rows, and the review of everything the moderators should check. It is deterministic: IDs are assigned in a
// fixed order (affiliation terms, then the other terms, each in plan order; collections in page order; events as
// first seen; bards by page ID, then stub persons by name; pieces by page ID, then stub pieces by title), and so
// are slugs, collisions getting "-2", "-3"…
//
// No public text holds a private name: every free-text field written is withheld (withholder), a link whose URL
// holds a legal name is imported hidden, and each is listed in the review. No name or title holds a placeholder: a
// collection, event, stub person or stub piece whose name (or a collection whose URL) holds a legal name is not
// imported (a bard's hidden alias already finds the bard, mediawiki.PersonIndex), a piece or term alias holding one
// is dropped, and a withheld uploader is left out; each is listed.
func Build(c mediawiki.Corpus, o BuildOptions) (domain.CatalogueImport, Review) {
	b := newBuilder(c, o)
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
	w      withholder
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
	stubPieces    []mediawiki.StubPiece
	// withheldStubs are the stub persons not imported because their name is withheld.
	withheldStubs map[mediawiki.PersonRef]bool
	aliasItems    []aliasItems
}

type pageKey struct {
	ns    int
	title string
}

func newBuilder(c mediawiki.Corpus, o BuildOptions) *builder {
	var hidden []string
	for _, bard := range c.People.Bards {
		for _, a := range bard.Aliases {
			if !a.IsPublic {
				hidden = append(hidden, a.Name)
			}
		}
	}
	b := &builder{c: c, now: o.Now, w: newWithholder(c, hidden), activity: map[pageKey][]string{},
		pages: map[pageKey]mediawiki.Page{}, termIDs: map[string]int64{}, collectionIDs: map[string]int64{},
		eventIDs: map[string]int64{}, personIDs: map[mediawiki.PersonRef]int64{}, pieceIDs: map[string]int64{},
		links: map[string]domain.DocLink{}, withheldStubs: map[mediawiki.PersonRef]bool{}}
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
	out := make([]string, 0, len(users))
	for u := range users {
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(users[b], users[a]), strings.Compare(a, b))
	})

	return out
}

// assignIDs numbers every entity, so that documents can reference any of them. It leaves out, and lists, the
// collections, events, stub persons and stub pieces that cannot be imported without publishing a legal name.
func (b *builder) assignIDs() {
	for k, t := range b.terms() {
		b.termIDs[t.Category] = int64(k + 1)
	}
	b.assignCollectionAndEventIDs()
	b.assignPersonIDs()
	b.assignPieceIDs()
}

func (b *builder) assignCollectionAndEventIDs() {
	n := int64(0)
	for _, r := range b.c.Links.Collections.Records {
		if _, found := b.w.name(r.Name()); len(found) > 0 || b.w.hidesURL(r.URL) {
			b.review.HiddenNames = append(b.review.HiddenNames, item("collection_withheld",
				mediawiki.CollectionsPage, "Collection %q (%s) is not imported: its name or URL holds a possible "+
					"legal name. Its links are imported without it; add it by hand once cleared.", r.Name(), r.URL))

			continue
		}
		n++
		b.collectionIDs[r.NormalizedURL] = n
	}
	n = 0
	for _, e := range b.c.Links.Events {
		if _, found := b.w.name(e.Name); len(found) > 0 {
			b.review.HiddenNames = append(b.review.HiddenNames, item("event_withheld", "", "Event %q is not "+
				"imported: its name holds a possible legal name. Its performances are imported without it.", e.Name))

			continue
		}
		n++
		b.eventIDs[e.Name] = n
	}
}

// assignPersonIDs numbers the bards, then the stub persons whose name holds no legal name (withheldStubs are
// the others).
func (b *builder) assignPersonIDs() {
	n := int64(0)
	for _, bard := range b.c.People.Bards {
		n++
		b.personIDs[mediawiki.PersonRef(bard.Title)] = n
	}
	var stubs []mediawiki.StubPerson
	for _, s := range b.stubs {
		ref := mediawiki.PersonRef(s.Name)
		if _, found := b.w.name(s.Name); len(found) > 0 {
			b.withheldStubs[ref] = true
			b.review.HiddenNames = append(b.review.HiddenNames, item("stub_withheld", firstMention(s),
				"The name %q, credited on %d page(s), holds a possible legal name: no person is created, and its "+
					"credits are imported as unknown with the note %q. Create the person by hand once cleared.",
				s.Name, len(s.Mentions), domain.WithheldName))

			continue
		}
		n++
		b.personIDs[ref] = n
		stubs = append(stubs, s)
	}
	b.stubs = stubs
}

// assignPieceIDs numbers the pieces, then the stub pieces whose title holds no legal name.
func (b *builder) assignPieceIDs() {
	n := int64(0)
	for _, p := range b.c.Pieces {
		n++
		b.pieceIDs[p.Title] = n
	}
	var stubPieces []mediawiki.StubPiece
	for _, sp := range b.c.People.StubPieces {
		if _, found := b.w.name(sp.Title); len(found) > 0 {
			page := ""
			if len(sp.Credits) > 0 {
				page = string(sp.Credits[0].Person)
			}
			b.review.HiddenNames = append(b.review.HiddenNames, item("stub_piece_withheld", page, "The work %q "+
				"is not imported: its title holds a possible legal name. Add it by hand once cleared.", sp.Title))

			continue
		}
		n++
		b.pieceIDs[sp.Title] = n
		stubPieces = append(stubPieces, sp)
	}
	b.stubPieces = stubPieces
}

// firstMention is the first page naming a stub person, or "".
func firstMention(s mediawiki.StubPerson) string {
	if len(s.Mentions) > 0 {
		return s.Mentions[0].Page
	}

	return ""
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

// clean is s in NFC, as every imported string is.
func clean(s string) string {
	return norm.NFC.String(s)
}

// free is the text of a public free-text field of the entity named page (field names it in the review), with its
// private names and emails withheld, or nil if empty.
func (b *builder) free(page, entity, field, s string) *string {
	out := b.freeText(page, entity, field, s)
	if out == "" {
		return nil
	}

	return &out
}

// freeText is free without the nil for empty text.
func (b *builder) freeText(page, entity, field, s string) string {
	out, found := b.w.text(strings.TrimSpace(s))
	for _, f := range found {
		if f.Email {
			b.review.Contacts = append(b.review.Contacts, item("email", page,
				"%s, %s: the address %q was withheld. Context: %q.", entity, field, f.Text, f.Context))

			continue
		}
		b.review.HiddenNames = append(b.review.HiddenNames, item("free_text", page,
			"%s, %s: %q was withheld. Context: %q.", entity, field, f.Text, f.Context))
	}

	return out
}

// name is a public name or title (one line), with its legal names withheld (withholder.name), and whether
// nothing was withheld; each withheld span is listed, and so are characters an editor could not enter. A name
// with a span withheld must not be imported (Validate rejects it).
func (b *builder) name(page, entity, field, s string) (string, bool) {
	out, found := b.w.name(strings.TrimSpace(s))
	for _, f := range found {
		b.review.HiddenNames = append(b.review.HiddenNames, item("name", page,
			"%s, %s: %q holds a possible legal name. Context: %q.", entity, field, f.Text, f.Context))
	}
	if err := domain.CheckTextCharacters(out); err != nil {
		b.review.Markup = append(b.review.Markup, item("text_characters", page,
			"%s, %s has a character an editor could not enter (%v); it is imported as written.", entity, field,
			err))
	}

	return out, len(found) == 0
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

func (b *builder) buildTerms() {
	parents := map[int64]*int64{}
	for _, t := range b.terms() {
		id := b.termIDs[t.Category]
		entity := "term " + t.Category
		name, _ := b.name("", entity, "name", t.Name)
		display, _ := b.name("", entity, "display name", t.DisplayName)
		doc := domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: t.Facet,
			Name: name, DisplayName: display,
			ParentID: b.termRef(t.Parent), RelatedAffiliationID: b.termRef(t.RelatedAffiliation),
			DescriptionMD: b.free("", entity, "description", t.DescriptionMD), Position: position(t.Position),
			Aliases: []string{}}
		seen := map[string]bool{domain.CollationKey(doc.Name): true}
		for _, a := range t.Aliases {
			a, ok := b.name("", entity, "alias", a)
			if key := domain.CollationKey(a); ok && !seen[key] {
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
		parents[id] = doc.ParentID
	}
	closure, err := domain.TermClosure(parents)
	if err != nil {
		b.review.Taxonomy = append(b.review.Taxonomy, item("closure", "",
			"The term tree has no valid closure (%v); the import does not validate.", err))
	}
	b.out.TermAncestors = closure
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
		name, _ := b.name(page, "collection "+r.NormalizedURL, "name", r.Name())
		doc := domain.CollectionDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
			URL: optional(r.URL), Kind: r.Kind,
			DescriptionMD: b.free(page, "collection "+r.NormalizedURL, "description", r.Description),
			RegionTermID:  b.termRef(r.RegionTerm), IsBardicResource: r.IsBardicResource,
			Enumerable: domain.CollectionEnumerableUnknown}
		if r.ParentURL != "" {
			if parent, ok := b.collectionIDs[r.ParentURL]; ok {
				doc.ParentID = &parent
			}
		}
		if note := b.w.urlNote(r.URL); note != "" {
			b.review.HiddenNames = append(b.review.HiddenNames, item("collection_url_name", page,
				"Collection %q: its URL %s %s: check it names no one who should be hidden.", r.Name(), r.URL, note))
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
		name, _ := b.name(page, "event", "name", e.Name)
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
