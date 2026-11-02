package mediawiki

import (
	"net/url"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// LinkOwner is the piece (by page ID) or the person a link is attached to; exactly one is set.
type LinkOwner struct {
	PieceID int64
	Person  PersonRef
}

// PieceLinks are the links of a piece page.
type PieceLinks struct {
	PageID int64
	Title  string
	PageLinks
}

// PersonLinks are the links attached to a person: their bard page's (PageID is 0 for a stub person) and the
// profile links of the infobox credits naming them by an external link.
type PersonLinks struct {
	Person PersonRef
	PageID int64
	PageLinks
}

// Performance is a performance item placed on the link recording it: what it says, the page it is on, the persons
// its performer links name and the name of the event it is at (an EventRecord's), if any.
type Performance struct {
	PerformanceRecord
	Page          string
	PerformerRefs []PersonRef
	Event         string
}

// LinkConflict is a link attached to several pieces or persons whose pages give it different fields (Fields: url,
// title, description, platform, place_type, collection, locator) from the canonical owner's (the first, pieces by
// page ID before persons), with the fields the owner's page gives (Own), for review. Every document carries the
// canonical fields (canonicalize).
type LinkConflict struct {
	NormalizedURL  string
	Owner          LinkOwner
	CanonicalOwner LinkOwner
	Fields         []string
	Own            LinkFields
}

// Links are the links of the wiki: the collections, the links of every piece page and those attached to persons,
// one set of fields per normalised URL (Canonical, in first-seen order) with the conflicts that resolved, the
// events performances are at with the similar names among them, the performance items with no link to record
// them, which are not imported, and the items of bard pages a piece page already records on the same link
// (RepeatedPerformances), which are not imported again; both are listed for review.
type Links struct {
	Collections          Collections
	Pieces               []PieceLinks
	Persons              []PersonLinks
	Canonical            []LinkFields
	Conflicts            []LinkConflict
	Events               []EventRecord
	SimilarEvents        []SimilarEventNames
	Unlinked             []Performance
	RepeatedPerformances []Performance
}

// ParseLinks extracts the links of the pieces and bard pages, matched to the Collections page's collections, and
// places their performances:
//   - an item on a piece page is a performance of the piece, on the link it starts with;
//   - an item on a bard page whose link a piece page records a performance on is that performance: it is not
//     imported again (report §8.4, dedupe by URL across pages), and the bard keeps the link with no performance;
//   - an item on a bard page whose piece (ParsePerformance: "singing [[P]]", or a link to a work) has a page is
//     recorded once, on the piece's link (added to the piece with role performance if the piece page lacks it);
//     the bard keeps the link, with role performance and no performance;
//   - any other item on a bard page is a performance on the bard's link, of its piece if that is a stub piece.
//
// Infobox credits naming a person by an external link attach it to the person, with role external_profile. Links
// are then made canonical (canonicalize): every document holding a normalised URL carries the same fields.
func ParseLinks(s Site, pieces []PieceRecord, people People) Links {
	page, _ := s.Page(CollectionsPage)
	b := linkBuilder{site: s, people: people, events: newEvents(s.Venues()), pieceIndex: map[string]int{},
		works: newWorkTitles(s, pieces, people.Bards)}
	b.out.Collections = ParseCollections(page, s.Terms)
	websites := bardWebsites(people.Bards)
	var allSites []string
	for _, bard := range people.Bards {
		allSites = append(allSites, websites[PersonRef(bard.Title)]...)
	}
	b.ctx = linkContext{collections: NewCollectionMatcher(b.out.Collections.Records), bardSites: allSites}
	for _, p := range pieces {
		b.addPiece(p, websites)
	}
	for _, bard := range people.Bards {
		b.addBard(bard)
	}
	b.addProfiles(pieces)
	b.canonicalize()
	b.out.Events = b.events.records
	b.out.SimilarEvents = b.events.similar()

	return b.out
}

type linkBuilder struct {
	site       Site
	people     People
	events     *events
	ctx        linkContext
	out        Links
	pieceIndex map[string]int
	works      workTitles
}

// bardWebsites are the normalised URLs of each bard's website links.
func bardWebsites(bards []BardRecord) map[PersonRef][]string {
	out := map[PersonRef][]string{}
	for _, b := range bards {
		for _, sec := range b.Parsed.Sections {
			if sec.Key != SectionWebsite {
				continue
			}
			for _, e := range sec.Content.ExternalLinks {
				if u, err := domain.NormalizeURL(e.URL); err == nil {
					out[PersonRef(b.Title)] = appendNew(out[PersonRef(b.Title)], u)
				}
			}
		}
	}

	return out
}

func (b *linkBuilder) addPiece(p PieceRecord, websites map[PersonRef][]string) {
	ctx := b.ctx
	for _, c := range b.people.Credits[p.PageID] {
		ctx.creditedSites = append(ctx.creditedSites, websites[c.Person]...)
	}
	pl := PieceLinks{PageID: p.PageID, Title: p.Title, PageLinks: extractLinks(p.Parsed, ctx)}
	for _, it := range performanceItems(p.Parsed) {
		perf := b.performance(it, p.Title)
		if k := linkIndex(pl.Links, itemURL(it)); k >= 0 {
			pl.Links[k].Performances = append(pl.Links[k].Performances, perf)
		} else {
			b.out.Unlinked = append(b.out.Unlinked, perf)
		}
	}
	b.pieceIndex[p.Title] = len(b.out.Pieces)
	b.out.Pieces = append(b.out.Pieces, pl)
}

func (b *linkBuilder) addBard(bard BardRecord) {
	pl := PersonLinks{Person: PersonRef(bard.Title), PageID: bard.PageID, PageLinks: extractLinks(bard.Parsed, b.ctx)}
	for _, it := range performanceItems(bard.Parsed) {
		perf := b.performance(it, bard.Title)
		k := linkIndex(pl.Links, itemURL(it))
		piece, onPiece := b.pieceIndex[b.site.ResolveRedirect(perf.Piece)]
		recordedOn := ""
		if k >= 0 && !onPiece {
			recordedOn = b.pieceRecording(pl.Links[k].NormalizedURL)
		}
		switch {
		case k < 0:
			b.out.Unlinked = append(b.out.Unlinked, perf)
		case recordedOn != "":
			perf.Piece = recordedOn
			b.out.RepeatedPerformances = append(b.out.RepeatedPerformances, perf)
		case onPiece:
			b.attachToPiece(piece, pl.Links[k], it.Section, perf)
		default:
			pl.Links[k].Performances = append(pl.Links[k].Performances, perf)
		}
	}
	b.out.Persons = append(b.out.Persons, pl)
}

// pieceRecording is the title of the first piece page that records a performance on a link, or empty.
func (b *linkBuilder) pieceRecording(normalized string) string {
	for _, pl := range b.out.Pieces {
		k := linkIndex(pl.Links, normalized)
		if k >= 0 && slices.ContainsFunc(pl.Links[k].Performances, func(p Performance) bool {
			return p.Page == pl.Title
		}) {
			return pl.Title
		}
	}

	return ""
}

// attachToPiece records a bard page's performance on the piece's link, adding the link to the piece if needed.
func (b *linkBuilder) attachToPiece(piece int, from LinkRecord, section SectionKey, perf Performance) {
	pl := &b.out.Pieces[piece]
	k := linkIndex(pl.Links, from.NormalizedURL)
	if k < 0 {
		rec := LinkRecord{LinkFields: from.LinkFields, CollectionTied: from.CollectionTied}
		pl.Links = append(pl.Links, rec)
		k = len(pl.Links) - 1
	}
	l := &pl.Links[k]
	if slices.ContainsFunc(l.Performances, func(p Performance) bool { return p.Page == pl.Title }) {
		b.out.RepeatedPerformances = append(b.out.RepeatedPerformances, perf)

		return
	}
	l.Roles = appendNew(l.Roles, domain.LinkRolePerformance)
	l.Sections = appendNew(l.Sections, section)
	l.Performances = append(l.Performances, perf)
	l.PlaceType = placeType(*l, b.ctx)
}

// addProfiles attaches the external links of infobox credits to the persons they name, with role
// external_profile: to the bard's links, or to a stub person's, stub persons following the bards by name.
func (b *linkBuilder) addProfiles(pieces []PieceRecord) {
	persons := map[PersonRef]int{}
	for k, pl := range b.out.Persons {
		persons[pl.Person] = k
	}
	stubs := map[PersonRef]PersonLinks{}
	for _, p := range pieces {
		for _, c := range b.people.Credits[p.PageID] {
			if c.Name.Form != NameExternalLink || c.Person == "" {
				continue
			}
			rec, ok := linkOf(LinkOccurrence{URL: c.Name.URL, Text: c.Name.Name, Bracketed: true}, "", b.ctx)
			if !ok {
				continue
			}
			rec.Roles, rec.Sections = []domain.LinkRole{domain.LinkRoleExternalProfile}, nil
			rec.PlaceType = placeType(rec, b.ctx)
			if k, ok := persons[c.Person]; ok {
				b.out.Persons[k].Links = withLink(b.out.Persons[k].Links, rec)
			} else {
				st := stubs[c.Person]
				st.Person, st.Links = c.Person, withLink(st.Links, rec)
				stubs[c.Person] = st
			}
		}
	}
	for _, st := range b.people.Persons.Stubs {
		if pl, ok := stubs[PersonRef(st.Name)]; ok {
			b.out.Persons = append(b.out.Persons, pl)
		}
	}
}

// withLink returns links with rec merged into the link of its normalised URL, or appended.
func withLink(links []LinkRecord, rec LinkRecord) []LinkRecord {
	if k := linkIndex(links, rec.NormalizedURL); k >= 0 {
		out := append([]LinkRecord(nil), links...)
		out[k] = mergeLink(links[k], rec)

		return out
	}

	return append(links, rec)
}

// performance places one performance item: its performers resolved to persons and its event created.
func (b *linkBuilder) performance(it sectionItem, page string) Performance {
	perf := Performance{PerformanceRecord: ParsePerformance(it.ListItem, b.site.Year, b.works.has), Page: page}
	for _, target := range perf.Performers {
		if ref, ok := b.people.Persons.Resolve(performerName(b.site, target)); ok {
			perf.PerformerRefs = appendNew(perf.PerformerRefs, ref)
		}
	}
	if perf.EventText != "" {
		perf.Event = b.events.at(perf.EventText, page)
	}

	return perf
}

// itemURL is the normalised URL of an item's first external link, or empty.
func itemURL(it sectionItem) string {
	links := FindExternalLinks(it.Raw)
	if len(links) == 0 {
		return ""
	}
	u, err := domain.NormalizeURL(links[0].URL)
	if err != nil {
		return ""
	}

	return u
}

func linkIndex(links []LinkRecord, normalized string) int {
	if normalized == "" {
		return -1
	}
	for k, l := range links {
		if l.NormalizedURL == normalized {
			return k
		}
	}

	return -1
}

// canonicalize gives the links one set of fields per normalised URL, as a links row holds them for every document
// attaching it: the first link's (pieces by page ID before persons), except that its title, description and locator,
// which say what a page finds there, are kept only if every document gives the same. Otherwise the title is the
// name of the link's collection, or none, and the description and locator none. Before that, a PDF that pages cite
// at different pages is given each page's as its open parameter ("#page=14", pagePDFLinks), so that each is a link
// with its own locator. The documents whose own fields differ are listed with their own fields.
func (b *linkBuilder) canonicalize() {
	links := b.allLinks()
	pagePDFLinks(links)
	first := map[string]int{}
	var owners []LinkOwner
	var differ []map[string]bool
	for _, l := range links {
		c, ok := first[l.rec.NormalizedURL]
		if !ok {
			first[l.rec.NormalizedURL] = len(b.out.Canonical)
			b.out.Canonical = append(b.out.Canonical, l.rec.LinkFields)
			owners = append(owners, l.owner)
			differ = append(differ, map[string]bool{})

			continue
		}
		fields := differentFields(b.out.Canonical[c], l.rec.LinkFields)
		if len(fields) > 0 {
			b.out.Conflicts = append(b.out.Conflicts, LinkConflict{NormalizedURL: l.rec.NormalizedURL, Owner: l.owner,
				CanonicalOwner: owners[c], Fields: fields, Own: l.rec.LinkFields})
		}
		for _, f := range fields {
			differ[c][f] = true
		}
	}
	labels := map[string]string{}
	for _, r := range b.out.Collections.Records {
		labels[r.NormalizedURL] = r.Label
	}
	for c, d := range differ {
		f := &b.out.Canonical[c]
		if d["title"] {
			f.Title = labels[f.CollectionURL]
		}
		if d["description"] {
			f.Description = ""
		}
		if d["locator"] {
			f.Locator = ""
		}
	}
	for _, l := range links {
		l.rec.LinkFields = b.out.Canonical[first[l.rec.NormalizedURL]]
	}
}

// ownedLink is a link of a piece or person, in place.
type ownedLink struct {
	owner LinkOwner
	rec   *LinkRecord
}

// allLinks are the links of the pieces, by page ID, then of the persons.
func (b *linkBuilder) allLinks() []ownedLink {
	var out []ownedLink
	for i := range b.out.Pieces {
		for k := range b.out.Pieces[i].Links {
			out = append(out, ownedLink{LinkOwner{PieceID: b.out.Pieces[i].PageID}, &b.out.Pieces[i].Links[k]})
		}
	}
	for i := range b.out.Persons {
		for k := range b.out.Persons[i].Links {
			out = append(out, ownedLink{LinkOwner{Person: b.out.Persons[i].Person}, &b.out.Persons[i].Links[k]})
		}
	}

	return out
}

// pagePDFLinks gives the links to a PDF (with no fragment) that documents cite at different pages ("p. 2", "p. 14")
// the PDF open parameter of their page: the URL with "#page=N" is a link of its own. The other links, and those to
// the PDF without a page, are unchanged.
func pagePDFLinks(links []ownedLink) {
	pages := map[string]map[string]bool{}
	for _, l := range links {
		if l.rec.Locator != "" && isPDF(l.rec.NormalizedURL) {
			if pages[l.rec.NormalizedURL] == nil {
				pages[l.rec.NormalizedURL] = map[string]bool{}
			}
			pages[l.rec.NormalizedURL][l.rec.Locator] = true
		}
	}
	for _, l := range links {
		page, ok := strings.CutPrefix(l.rec.Locator, locatorPrefix)
		if !ok || len(pages[l.rec.NormalizedURL]) < 2 {
			continue
		}
		u := l.rec.URL + "#page=" + page
		if normalized, err := domain.NormalizeURL(u); err == nil {
			l.rec.URL, l.rec.NormalizedURL = u, normalized
		}
	}
}

// isPDF reports whether a normalised URL is a PDF's with no fragment: its path ends in ".pdf".
func isPDF(normalized string) bool {
	u, err := url.Parse(normalized)

	return err == nil && u.Fragment == "" && strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

// differentFields names the fields in which two links' fields differ.
func differentFields(a, b LinkFields) []string {
	var out []string
	for _, f := range []struct {
		name string
		same bool
	}{
		{"url", a.URL == b.URL}, {"title", a.Title == b.Title}, {"description", a.Description == b.Description},
		{"platform", a.Platform == b.Platform},
		{"place_type", a.PlaceType == b.PlaceType}, {"collection", a.CollectionURL == b.CollectionURL},
		{"locator", a.Locator == b.Locator},
	} {
		if !f.same {
			out = append(out, f.name)
		}
	}

	return out
}
